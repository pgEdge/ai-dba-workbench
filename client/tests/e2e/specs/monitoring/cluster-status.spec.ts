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
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { StatusPanelAlertsPage } from '../../pages/StatusPanelAlertsPage';
import {
    ensureClusterAndPrimary,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

const { EVAL_CYCLE_MS, ALERT_CLEANER_MS } = AlertSetupHelper;

interface OnlineCount {
    online: number;
    total: number;
}

/**
 * The longest alphabetic word (>= 4 chars) of an alert title, used as a
 * case-insensitive row filter that scopes to OUR alert and skips the
 * unrelated "Stale Metrics: Pg Sys ..." staleness alerts other specs'
 * pg_sys priming leaves active on the shared stack. For
 * connection_utilization_percent the title friendly-cases to "Connection
 * Utilization", whose longest word ("utilization") survives the mapping.
 */
const longestWord = (title: string): string =>
    (title || '')
        .split(/[^a-zA-Z]+/)
        .filter((w) => w.length >= 4)
        .sort((a, b) => b.length - a.length)[0] ?? '';

test.describe('Cluster Overview Status', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();

    let adminCookie: string;
    let primaryConnId: number;
    let rule: AlertRuleApiRow;

    test.beforeEach(async () => {
        await label('package', 'Monitoring');
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
            throw new Error('No connection matching /primary/i found');
        }
        primaryConnId = primary.id;
        const rules = await api.getAlertRules(adminCookie);
        rule =
            rules.find((r) => r.metric_name === 'connection_utilization_percent') ??
            rules.find(
                (r) => /connection|session/i.test(r.metric_name) && !r.required_extension,
            ) ??
            rules.find(
                (r) => !r.required_extension && r.metric_name.startsWith('pg_stat_activity'),
            )!;
    });

    test.afterEach(async () => {
        // Ensure the collector is running again — the connection-error test
        // stops it to hold an injected error steady; a no-op if already up.
        await replication.setCollectorRunning(true).catch(() => undefined);
        await alertSetup.setConnectionError(primaryConnId, null).catch(() => undefined);
        if (rule) {
            await api
                .deleteAlertOverride(adminCookie, 'server', primaryConnId, rule.id)
                .catch(() => undefined);
        }
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

    /**
     * Read the navigator estate summary ("N online of M servers"), the
     * authoritative GUI signal for per-server reachability. Returns null
     * until the tree has loaded and rendered the summary.
     */
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

    /** Poll the navigator online summary until `predicate` holds. */
    async function expectOnline(
        page: Page,
        monitoring: MonitoringPage,
        predicate: (c: OnlineCount) => void,
        timeout = 120_000,
    ): Promise<void> {
        await expect(async () => {
            await page.goto('/');
            await monitoring.waitForAppLoad();
            const count = await readOnlineCount(page);
            expect(count, 'the "N online of M servers" summary is present').not.toBeNull();
            predicate(count!);
        }).toPass({ timeout, intervals: [5_000] });
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('healthy primary is reported online in the navigator', async ({ page }) => {
        test.setTimeout(180_000);
        const monitoring = new MonitoringPage(page);

        // Default state: fresh collection, no forced error, no alerts — the
        // estate summary counts every registered server as online.
        await expectOnline(page, monitoring, (c) => {
            expect(c.total, 'the primary is registered').toBeGreaterThanOrEqual(1);
            expect(c.online, 'every registered server is online').toBe(c.total);
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
        await test.step('Generate an active alert on the primary', async () => {
            await api.upsertAlertOverride(adminCookie, 'server', primaryConnId, rule.id, {
                operator: '>',
                threshold: 0,
                severity: 'warning',
                enabled: true,
            });
            // Force a fresh collection so the alerter fires on its next cycle
            // (fixes the cold first-run case for this describe).
            await replication.restartCollector([PRIMARY_SERVER.name]);
            await expect(async () => {
                const { alerts: fired } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
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

        await test.step('The GUI status panel shows the active alert (server stays online)', async () => {
            await selectPrimaryServer(page, monitoring, serverNav);
            await alerts.expandActive();
            // Scope to OUR alert: the shared stack carries unrelated staleness
            // alerts, so an unscoped "list not empty" check is not meaningful.
            await expect(
                alerts.getActiveAlertRow(titleFrag),
                `the "${titleFrag}" alert row should render in the status panel`,
            ).toBeVisible({ timeout: 60_000 });
            // A warning alert does not take the server offline.
            const count = await readOnlineCount(page);
            if (count) {
                expect(count.online, 'an alert does not make the server offline').toBe(
                    count.total,
                );
            }
        });

        await test.step('Clearing the alert removes it from the active list', async () => {
            // Deterministic clear: the alerter compares the live value against
            // the alert's OWN stored threshold (0), not the override, so
            // raising the override can never clear a `> 0` alert. Remove the
            // override (stop re-firing) and raise the stored threshold so the
            // next cleaner cycle resolves it.
            await api.deleteAlertOverride(adminCookie, 'server', primaryConnId, rule.id);
            await alertSetup.raiseStoredThreshold(alertId);
            await expect(async () => {
                const { alerts: still } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
                    excludeCleared: true,
                });
                expect(
                    still.some((a) => a.rule_id === rule.id && a.status === 'active'),
                    'the active alert should clear',
                ).toBe(false);
            }).toPass({ timeout: ALERT_CLEANER_MS * 6, intervals: [5_000] });

            // GUI: OUR alert row is gone (other staleness alerts may remain,
            // so assert on the scoped row, not an empty list).
            await selectPrimaryServer(page, monitoring, serverNav);
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
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const alerts = new StatusPanelAlertsPage(page);

        // NOTE: setting connections.connection_error does NOT flip the server
        // to "offline" in the navigator count (it stays "N online of N");
        // instead the alerter's connection-error evaluator raises a CRITICAL
        // alert, which the UI surfaces as a warning badge + a status-panel
        // row. Clearing the error auto-resolves that alert via the engine's
        // dedicated connection-alert clear path — a fast, deterministic cycle.

        await test.step('Stop the collector, force a connection error → a critical alert fires', async () => {
            // The running collector clears a connection's error on its next
            // successful probe (scheduler.go), which would race the alerter
            // and often clear our injected error before it is evaluated. Pause
            // the collector so the error holds steady; the alerter is a
            // separate service and keeps evaluating.
            await replication.setCollectorRunning(false);
            await alertSetup.setConnectionError(primaryConnId, 'e2e forced connection error');
            await expect(async () => {
                const { alerts: fired } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
                    excludeCleared: true,
                });
                expect(
                    fired.some(
                        (a) => a.connection_id === primaryConnId && a.severity === 'critical',
                    ),
                    'a critical connection alert should fire',
                ).toBe(true);
            }).toPass({ timeout: EVAL_CYCLE_MS * 3, intervals: [5_000] });
        });

        await test.step('The GUI status panel surfaces the connection alert', async () => {
            await selectPrimaryServer(page, monitoring, serverNav);
            await alerts.expandActive();
            // The connection alert's title is "Connection error: <host>";
            // scope to it so unrelated staleness alerts do not satisfy this.
            await expect(
                alerts.getActiveAlertRow('connection error'),
                'the connection-error alert row should render in the status panel',
            ).toBeVisible({ timeout: 60_000 });
        });

        await test.step('Clearing the error + resuming the collector clears the alert', async () => {
            await alertSetup.setConnectionError(primaryConnId, null);
            // Resume the collector so subsequent specs collect normally; the
            // alerter clears the connection alert once the error is gone.
            await replication.setCollectorRunning(true);
            await expect(async () => {
                const { alerts: still } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
                    excludeCleared: true,
                });
                expect(
                    still.some(
                        (a) => a.connection_id === primaryConnId && a.severity === 'critical',
                    ),
                    'the critical connection alert should clear',
                ).toBe(false);
            }).toPass({ timeout: EVAL_CYCLE_MS * 4, intervals: [5_000] });
        });
    });
});
