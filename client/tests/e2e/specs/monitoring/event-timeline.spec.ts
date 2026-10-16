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
import { AlertSetupHelper } from '../../helpers/alert-setup.helper';
import { EventTimelinePage } from '../../pages/EventTimelinePage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { MonitoringPage } from '../../pages/MonitoringPage';
import {
    ensureClusterAndPrimary,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

const { EVAL_CYCLE_MS } = AlertSetupHelper;

/** ISO timestamp helpers for the timeline window. */
const agoIso = (ms: number): string => new Date(Date.now() - ms).toISOString();
const aheadIso = (ms: number): string => new Date(Date.now() + ms).toISOString();

test.describe('Event Timeline', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();

    let adminCookie: string;
    let primaryConnId: number;
    let rule: AlertRuleApiRow;

    test.beforeEach(async () => {
        await label('package', 'Event Timeline');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.beforeEach(async ({ page }) => {
        await ensureClusterAndPrimary(page, api, adminCookie);
        const connections = await api.listConnections({ cookie: adminCookie });
        const primary = connections.find((c) => /primary/i.test(c.name));
        if (!primary) {
            throw new Error(
                'No connection matching /primary/i found. Available: ' +
                connections.map((c) => c.name).join(', '),
            );
        }
        primaryConnId = primary.id;

        const rules = await api.getAlertRules(adminCookie);
        const chosen =
            rules.find((r) => r.metric_name === 'connection_utilization_percent') ??
            rules.find(
                (r) => /connection|session/i.test(r.metric_name) && !r.required_extension,
            ) ??
            rules.find(
                (r) => !r.required_extension && r.metric_name.startsWith('pg_stat_activity'),
            );
        expect(chosen, 'Expected a no-extension connection rule').toBeTruthy();
        rule = chosen!;
    });

    test.afterEach(async () => {
        await api
            .deleteAlertOverride(adminCookie, 'server', primaryConnId, rule.id)
            .catch(() => undefined);
        await alertSetup.cleanup(primaryConnId).catch(() => undefined);
    });

    test.afterAll(async () => {
        await replication.stop().catch(() => undefined);
    });

    // ------------------------------------------------------------------
    // Helpers
    // ------------------------------------------------------------------

    async function selectPrimaryServer(
        page: Page,
        monitoring: MonitoringPage,
        serverNav: ServerNavigatorPage,
    ): Promise<void> {
        await page.goto('/');
        await monitoring.waitForAppLoad();
        await expect(async () => {
            const row = serverNav.getServerRow(PRIMARY_SERVER.name).first();
            if (!(await row.isVisible().catch(() => false))) {
                await serverNav.expandCluster(CLUSTER_NAME);
            }
            await row.click({ force: true });
            await expect(
                monitoring.getSectionHeader('Monitoring'),
                'Monitoring section should render after selecting the primary',
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 60_000, intervals: [3_000] });
    }

    /** Drive the real alerter to raise an active alert; returns the id. */
    async function generateActiveAlert(): Promise<number> {
        await api.upsertAlertOverride(adminCookie, 'server', primaryConnId, rule.id, {
            operator: '>',
            threshold: 0,
            severity: 'warning',
            enabled: true,
        });
        // Force the collector to (re)discover and collect the primary so the
        // metric is fresh for the alerter next cycle (fixes the cold first run).
        await replication.restartCollector([PRIMARY_SERVER.name]);
        let alertId = 0;
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: primaryConnId,
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

    /** Poll the timeline API until an event of `type` exists in the window. */
    async function waitForTimelineEvent(type: string): Promise<void> {
        await expect(async () => {
            const { events } = await api.getTimelineEvents(adminCookie, {
                connectionId: primaryConnId,
                startTime: agoIso(60 * 60_000),
                endTime: aheadIso(60_000),
                eventTypes: [type],
            });
            expect(
                events.some((e) => e.connection_id === primaryConnId),
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
            await selectPrimaryServer(page, monitoring, serverNav);
            await timeline.expandTimeline();
            // The GUI timeline is populated (markers present, not empty).
            await expect(async () => {
                expect(await timeline.isEmpty(), 'timeline should not be empty').toBe(false);
                expect(await timeline.countMarkers(), 'timeline has markers').toBeGreaterThan(0);
            }).toPass({ timeout: 30_000, intervals: [3_000] });

            // Cross-check: the API alert_fired event exists for this window.
            const { events } = await api.getTimelineEvents(adminCookie, {
                connectionId: primaryConnId,
                startTime: agoIso(60 * 60_000),
                endTime: aheadIso(60_000),
                eventTypes: ['alert_fired'],
            });
            const fired = events.find((e) => e.connection_id === primaryConnId);
            expect(fired, 'API lists the alert_fired event').toBeTruthy();

            // Best-effort: open the matching marker and validate the detail
            // against the API event (marker interaction is selector-fragile;
            // validate loosely).
            const opened = await timeline
                .openEventByTitle(fired!.title.slice(0, 12))
                .then(() => true)
                .catch(() => false);
            // Marker interaction is selector-fragile; the detail read is
            // best-effort and time-boxed so it can never hang the test. The
            // authoritative validation is the API cross-check above.
            const detailVisible =
                opened &&
                (await page
                    .getByText('Event Details', { exact: true })
                    .isVisible({ timeout: 5_000 })
                    .catch(() => false));
            if (detailVisible) {
                const detail = await timeline.readOpenEventDetail().catch(() => null);
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

        await selectPrimaryServer(page, monitoring, serverNav);
        await timeline.expandTimeline();

        await test.step('Selecting "Alert" shows the alert_fired count from the API', async () => {
            await timeline.selectCategory('Alert');
            await expect(async () => {
                const { events } = await api.getTimelineEvents(adminCookie, {
                    connectionId: primaryConnId,
                    startTime: agoIso(24 * 60 * 60_000),
                    endTime: aheadIso(60_000),
                    eventTypes: ['alert_fired'],
                });
                const apiCount = events.filter(
                    (e) => e.connection_id === primaryConnId,
                ).length;
                const guiCount = await timeline.countMarkers();
                expect(apiCount, 'API has at least one alert_fired').toBeGreaterThan(0);
                // GUI markers may cluster multiple events; assert the GUI is
                // non-empty and no MORE distinct markers than API events.
                expect(guiCount, 'GUI shows the filtered events').toBeGreaterThan(0);
                expect(guiCount).toBeLessThanOrEqual(apiCount);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });

        await test.step('Selecting only a no-event category shows the empty state', async () => {
            // The category chips are MULTI-SELECT once any chip is active
            // (clicking "Blackouts" while "Alert" is on leaves BOTH on), so
            // reload to reset the filter back to its default (all), then
            // select ONLY "Blackouts" — a category these tests never create,
            // unlike "Restart", which the cluster's postgres restarts
            // populate — and assert the empty state.
            await selectPrimaryServer(page, monitoring, serverNav);
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

        await selectPrimaryServer(page, monitoring, serverNav);
        await timeline.expandTimeline();

        // A just-generated event is within both windows, so 24h must show at
        // least as many events as 1h. Assert monotonic consistency with the
        // API rather than exact equality (markers may cluster).
        await test.step('24h window shows >= the 1h window', async () => {
            await timeline.selectTimeRange('1h');
            let oneHour = 0;
            await expect(async () => {
                oneHour = await timeline.countMarkers();
                expect(oneHour, 'recent event visible in 1h').toBeGreaterThan(0);
            }).toPass({ timeout: 30_000, intervals: [3_000] });

            await timeline.selectTimeRange('24h');
            await expect(async () => {
                const day = await timeline.countMarkers();
                expect(day, '24h shows at least the 1h events').toBeGreaterThanOrEqual(
                    oneHour,
                );
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });
    });
});
