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
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { StatusPanelAlertsPage } from '../../pages/StatusPanelAlertsPage';
import {
    ensureSpockClusterAndNodes,
    SPOCK_CLUSTER_NAME,
    SPOCK_NODE1,
} from '../../helpers/spock-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

const { EVAL_CYCLE_MS, ALERT_CLEANER_MS } = AlertSetupHelper;

interface OnlineCount {
    online: number;
    total: number;
}

/**
 * Longest alphabetic word (>= 4 chars) of an alert title — a scoped,
 * case-insensitive row filter (see cluster-status.spec.ts).
 */
const longestWord = (title: string): string =>
    (title || '')
        .split(/[^a-zA-Z]+/)
        .filter((w) => w.length >= 4)
        .sort((a, b) => b.length - a.length)[0] ?? '';

/**
 * Spock-cluster mirror of cluster-status: healthy nodes report online, an
 * active alert surfaces in the status panel and clears, and a connection
 * error raises/clears a critical alert. Per-connection scoping keeps these
 * robust on the shared estate (the physical cluster may also be registered).
 */
test.describe('Cluster Overview Status (Spock)', () => {
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
        await label('package', 'Monitoring (Spock)');
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
                `No connection named "${SPOCK_NODE1.name}" found`,
            );
        }
        nodeConnId = node.id;
        const rules = await api.getAlertRules(adminCookie);
        rule =
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
            )!;
    });

    test.afterEach(async () => {
        await replication.setCollectorRunning(true).catch(() => undefined);
        await alertSetup
            .setConnectionError(nodeConnId, null)
            .catch(() => undefined);
        if (rule) {
            await api
                .deleteAlertOverride(adminCookie, 'server', nodeConnId, rule.id)
                .catch(() => undefined);
        }
        await alertSetup.cleanup(nodeConnId).catch(() => undefined);
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

    async function readOnlineCount(page: Page): Promise<OnlineCount | null> {
        const text = await page
            .getByText(/\d+\s+online of\s+\d+\s+server/i)
            .first()
            .innerText()
            .catch(() => '');
        const match = text.match(/(\d+)\s+online of\s+(\d+)/i);
        return match
            ? { online: Number(match[1]), total: Number(match[2]) }
            : null;
    }

    async function expectOnline(
        page: Page,
        monitoring: MonitoringPage,
        predicate: (c: OnlineCount) => void,
        timeout = 180_000,
    ): Promise<void> {
        // Navigate ONCE, then poll the estate summary in place. Re-navigating
        // inside the poll reloads the tree and reads "N online of M" before it
        // re-populates, so under the full-run's load the poll kept catching
        // the transient "0 of 0". The summary auto-refreshes, so polling
        // without reloading lets it settle to the real count.
        await page.goto('/');
        await monitoring.waitForAppLoad();
        await expect(async () => {
            const count = await readOnlineCount(page);
            expect(
                count,
                'the "N online of M servers" summary is present',
            ).not.toBeNull();
            predicate(count!);
        }).toPass({ timeout, intervals: [5_000] });
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('healthy spock nodes are reported online in the navigator', async ({
        page,
    }) => {
        test.setTimeout(180_000);
        const monitoring = new MonitoringPage(page);

        // The estate summary is GLOBAL (spans every registered cluster,
        // including the physical one), so we do not assert online === total.
        // Instead assert all three spock nodes contribute to the online count.
        await expectOnline(page, monitoring, (c) => {
            expect(
                c.total,
                'all three spock nodes are registered',
            ).toBeGreaterThanOrEqual(3);
            expect(
                c.online,
                'all three spock nodes are online',
            ).toBeGreaterThanOrEqual(3);
        });
    });

    test('an active alert surfaces in the status panel, and clearing it recovers', async ({
        page,
    }) => {
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const alerts = new StatusPanelAlertsPage(page);

        let alertId = 0;
        let titleFrag = 'utilization';
        await test.step('Generate an active alert on the primary spock node', async () => {
            await api.upsertAlertOverride(
                adminCookie,
                'server',
                nodeConnId,
                rule.id,
                { operator: '>', threshold: 0, severity: 'warning', enabled: true },
            );
            await replication.restartCollector([SPOCK_NODE1.name]);
            await expect(async () => {
                const { alerts: fired } = await api.getAlerts(adminCookie, {
                    connectionId: nodeConnId,
                    excludeCleared: true,
                });
                const active = fired.find(
                    (a) => a.rule_id === rule.id && a.status === 'active',
                );
                expect(active, 'an active alert should fire').toBeTruthy();
                alertId = active!.id;
                titleFrag = longestWord(active!.title) || titleFrag;
            }).toPass({ timeout: EVAL_CYCLE_MS * 4, intervals: [5_000] });
        });

        await test.step('The GUI status panel shows the active alert (node stays online)', async () => {
            await selectSpockNode(page, monitoring, serverNav);
            await alerts.expandActive();
            await expect(
                alerts.getActiveAlertRow(titleFrag),
                `the "${titleFrag}" alert row should render in the status panel`,
            ).toBeVisible({ timeout: 60_000 });
        });

        await test.step('Clearing the alert removes it from the active list', async () => {
            await api.deleteAlertOverride(
                adminCookie,
                'server',
                nodeConnId,
                rule.id,
            );
            await alertSetup.raiseStoredThreshold(alertId);
            await expect(async () => {
                const { alerts: still } = await api.getAlerts(adminCookie, {
                    connectionId: nodeConnId,
                    excludeCleared: true,
                });
                expect(
                    still.some(
                        (a) => a.rule_id === rule.id && a.status === 'active',
                    ),
                    'the active alert should clear',
                ).toBe(false);
            }).toPass({ timeout: ALERT_CLEANER_MS * 6, intervals: [5_000] });

            await selectSpockNode(page, monitoring, serverNav);
            await alerts.expandActive();
            await expect(
                alerts.getActiveAlertRow(titleFrag),
                `the "${titleFrag}" alert row should be gone after clearing`,
            ).toBeHidden({ timeout: 30_000 });
        });
    });

    test('a connection error raises a critical alert in the GUI, and recovery clears it', async ({
        page,
    }) => {
        // Generous overall budget: the fire wait (EVAL_CYCLE_MS*3) plus the
        // clear wait (EVAL_CYCLE_MS*8) can be long under the full-run load.
        test.setTimeout(720_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const alerts = new StatusPanelAlertsPage(page);

        await test.step('Stop the collector, force a connection error → a critical alert fires', async () => {
            await replication.setCollectorRunning(false);
            await alertSetup.setConnectionError(
                nodeConnId,
                'e2e forced connection error',
            );
            await expect(async () => {
                const { alerts: fired } = await api.getAlerts(adminCookie, {
                    connectionId: nodeConnId,
                    excludeCleared: true,
                });
                expect(
                    fired.some(
                        (a) =>
                            a.connection_id === nodeConnId &&
                            a.severity === 'critical',
                    ),
                    'a critical connection alert should fire',
                ).toBe(true);
            }).toPass({ timeout: EVAL_CYCLE_MS * 3, intervals: [5_000] });
        });

        await test.step('The GUI status panel surfaces the connection alert', async () => {
            await selectSpockNode(page, monitoring, serverNav);
            await alerts.expandActive();
            await expect(
                alerts.getActiveAlertRow('connection error'),
                'the connection-error alert row should render in the status panel',
            ).toBeVisible({ timeout: 60_000 });
        });

        await test.step('Clearing the error + resuming the collector clears the alert', async () => {
            await alertSetup.setConnectionError(nodeConnId, null);
            await replication.setCollectorRunning(true);
            await expect(async () => {
                const { alerts: still } = await api.getAlerts(adminCookie, {
                    connectionId: nodeConnId,
                    excludeCleared: true,
                });
                expect(
                    still.some(
                        (a) =>
                            a.connection_id === nodeConnId &&
                            a.severity === 'critical',
                    ),
                    'the critical connection alert should clear',
                ).toBe(false);
            }).toPass({ timeout: EVAL_CYCLE_MS * 8, intervals: [5_000] });
        });
    });
});
