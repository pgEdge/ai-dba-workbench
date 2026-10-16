/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect, type Page } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ApiHelper, type AlertRuleApiRow } from '../../helpers/api.helper';
import { AuthHelper } from '../../helpers/auth.helper';
import { ReplicationHelper } from '../../helpers/replication.helper';
import { SpockReplicationHelper } from '../../helpers/spock-replication.helper';
import { AlertSetupHelper } from '../../helpers/alert-setup.helper';
import { EventTimelinePage } from '../../pages/EventTimelinePage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { MonitoringPage } from '../../pages/MonitoringPage';
import {
    ensureSpockClusterAndNodes,
    SPOCK_CLUSTER_NAME,
    SPOCK_NODE1,
} from '../../helpers/spock-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

const { EVAL_CYCLE_MS } = AlertSetupHelper;

const agoIso = (ms: number): string => new Date(Date.now() - ms).toISOString();
const aheadIso = (ms: number): string =>
    new Date(Date.now() + ms).toISOString();

/**
 * Spock-cluster mirror of event-timeline: alert lifecycle produces
 * alert_fired / alert_acknowledged timeline events (GUI cross-checked with
 * the API), category filters restrict to the selected type, and time-range
 * presets change the events shown — all against the primary spock node.
 */
test.describe('Event Timeline (Spock)', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();
    const spock = new SpockReplicationHelper();

    let adminCookie: string;
    let nodeConnId: number;
    let rule: AlertRuleApiRow;

    test.beforeEach(async () => {
        await label('package', 'Event Timeline (Spock)');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await spock.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.beforeEach(async ({ page }) => {
        test.setTimeout(180_000);
        await ensureSpockClusterAndNodes(page, api, adminCookie);
        const connections = await api.listConnections({ cookie: adminCookie });
        const node = connections.find((c) => c.name === SPOCK_NODE1.name);
        if (!node) {
            throw new Error(
                `No connection named "${SPOCK_NODE1.name}" found. ` +
                'Available: ' + connections.map((c) => c.name).join(', '),
            );
        }
        nodeConnId = node.id;

        const rules = await api.getAlertRules(adminCookie);
        const chosen =
            rules.find(
                (r) => r.metric_name === 'connection_utilization_percent',
            ) ??
            rules.find(
                (r) =>
                    /connection|session/i.test(r.metric_name) &&
                    !r.required_extension,
            ) ??
            rules.find(
                (r) =>
                    !r.required_extension &&
                    r.metric_name.startsWith('pg_stat_activity'),
            );
        expect(chosen, 'Expected a no-extension connection rule').toBeTruthy();
        rule = chosen!;
    });

    test.afterEach(async () => {
        if (rule && nodeConnId) {
            await api
                .deleteAlertOverride(adminCookie, 'server', nodeConnId, rule.id)
                .catch(() => undefined);
        }
        if (nodeConnId) {
            await alertSetup.cleanup(nodeConnId).catch(() => undefined);
        }
    });

    test.afterAll(async () => {
        await spock.stop().catch(() => undefined);
    });

    // ------------------------------------------------------------------
    // Helpers
    // ------------------------------------------------------------------

    async function selectSpockNode(
        page: Page,
        monitoring: MonitoringPage,
        serverNav: ServerNavigatorPage,
    ): Promise<void> {
        await page.goto('/');
        await monitoring.waitForAppLoad();
        await expect(async () => {
            const row = serverNav.getServerRow(SPOCK_NODE1.name).first();
            if (!(await row.isVisible().catch(() => false))) {
                await serverNav.expandCluster(SPOCK_CLUSTER_NAME);
            }
            await row.click({ force: true });
            await expect(
                monitoring.getSectionHeader('Monitoring'),
                'Monitoring section should render after selecting the node',
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 60_000, intervals: [3_000] });
    }

    async function generateActiveAlert(): Promise<number> {
        await api.upsertAlertOverride(
            adminCookie,
            'server',
            nodeConnId,
            rule.id,
            { operator: '>', threshold: 0, severity: 'warning', enabled: true },
        );
        await replication.restartCollector([SPOCK_NODE1.name]);
        let alertId = 0;
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: nodeConnId,
                excludeCleared: true,
            });
            const active = alerts.find(
                (a) => a.rule_id === rule.id && a.status === 'active',
            );
            expect(active, 'an active alert should fire').toBeTruthy();
            alertId = active!.id;
        }).toPass({ timeout: EVAL_CYCLE_MS * 4, intervals: [5_000] });
        return alertId;
    }

    async function waitForTimelineEvent(type: string): Promise<void> {
        await expect(async () => {
            const { events } = await api.getTimelineEvents(adminCookie, {
                connectionId: nodeConnId,
                startTime: agoIso(60 * 60_000),
                endTime: aheadIso(60_000),
                eventTypes: [type],
            });
            expect(
                events.some((e) => e.connection_id === nodeConnId),
                `a ${type} timeline event should exist`,
            ).toBe(true);
        }).toPass({ timeout: 30_000, intervals: [3_000] });
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('alert lifecycle produces alert_fired / acknowledged events matching the API', async ({
        page,
    }) => {
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const timeline = new EventTimelinePage(page);

        const alertId = await test.step('Generate an alert (fires an alert_fired event)', async () => {
            const id = await generateActiveAlert();
            await waitForTimelineEvent('alert_fired');
            return id;
        });

        await test.step('The timeline shows the event and it matches the API', async () => {
            await selectSpockNode(page, monitoring, serverNav);
            await timeline.expandTimeline();
            await expect(async () => {
                expect(
                    await timeline.isEmpty(),
                    'timeline should not be empty',
                ).toBe(false);
                expect(
                    await timeline.countMarkers(),
                    'timeline has markers',
                ).toBeGreaterThan(0);
            }).toPass({ timeout: 30_000, intervals: [3_000] });

            const { events } = await api.getTimelineEvents(adminCookie, {
                connectionId: nodeConnId,
                startTime: agoIso(60 * 60_000),
                endTime: aheadIso(60_000),
                eventTypes: ['alert_fired'],
            });
            const fired = events.find((e) => e.connection_id === nodeConnId);
            expect(fired, 'API lists the alert_fired event').toBeTruthy();

            const opened = await timeline
                .openEventByTitle(fired!.title.slice(0, 12))
                .then(() => true)
                .catch(() => false);
            const detailVisible =
                opened &&
                (await page
                    .getByText('Event Details', { exact: true })
                    .isVisible({ timeout: 5_000 })
                    .catch(() => false));
            if (detailVisible) {
                const detail = await timeline
                    .readOpenEventDetail()
                    .catch(() => null);
                if (detail?.server) {
                    expect(detail.server).toContain(fired!.server_name);
                }
                await timeline.closeEventDetail().catch(() => undefined);
            }
        });

        await test.step('Acknowledging produces an alert_acknowledged event', async () => {
            await api.acknowledgeAlert(adminCookie, {
                alertId,
                message: 'timeline ack',
            });
            await waitForTimelineEvent('alert_acknowledged');
        });
    });

    test('category filters restrict the timeline to the selected event type', async ({
        page,
    }) => {
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const timeline = new EventTimelinePage(page);

        await generateActiveAlert();
        await waitForTimelineEvent('alert_fired');

        await selectSpockNode(page, monitoring, serverNav);
        await timeline.expandTimeline();

        await test.step('Selecting "Alert" shows the alert_fired count from the API', async () => {
            await timeline.selectCategory('Alert');
            await expect(async () => {
                const { events } = await api.getTimelineEvents(adminCookie, {
                    connectionId: nodeConnId,
                    startTime: agoIso(24 * 60 * 60_000),
                    endTime: aheadIso(60_000),
                    eventTypes: ['alert_fired'],
                });
                const apiCount = events.filter(
                    (e) => e.connection_id === nodeConnId,
                ).length;
                const guiCount = await timeline.countMarkers();
                expect(
                    apiCount,
                    'API has at least one alert_fired',
                ).toBeGreaterThan(0);
                expect(
                    guiCount,
                    'GUI shows the filtered events',
                ).toBeGreaterThan(0);
                expect(guiCount).toBeLessThanOrEqual(apiCount);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });

        await test.step('Selecting only a no-event category shows the empty state', async () => {
            await selectSpockNode(page, monitoring, serverNav);
            await timeline.expandTimeline();
            await timeline.selectCategory('Blackouts');
            await expect(async () => {
                expect(
                    await timeline.isEmpty(),
                    'Blackouts-filtered timeline should be empty',
                ).toBe(true);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });
    });

    test('time-range presets change the events shown consistently with the API', async ({
        page,
    }) => {
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const timeline = new EventTimelinePage(page);

        await generateActiveAlert();
        await waitForTimelineEvent('alert_fired');

        await selectSpockNode(page, monitoring, serverNav);
        await timeline.expandTimeline();

        await test.step('24h window shows >= the 1h window', async () => {
            await timeline.selectTimeRange('1h');
            let oneHour = 0;
            await expect(async () => {
                oneHour = await timeline.countMarkers();
                expect(
                    oneHour,
                    'recent event visible in 1h',
                ).toBeGreaterThan(0);
            }).toPass({ timeout: 30_000, intervals: [3_000] });

            await timeline.selectTimeRange('24h');
            await expect(async () => {
                const day = await timeline.countMarkers();
                expect(
                    day,
                    '24h shows at least the 1h events',
                ).toBeGreaterThanOrEqual(oneHour);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });
    });
});
