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
import { BlackoutPage } from '../../pages/BlackoutPage';
import {
    ensureClusterAndPrimary,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

const { EVAL_CYCLE_MS } = AlertSetupHelper;

/**
 * Blackout GUI coverage.
 *
 * A blackout window suppresses the real alerter: while `start_time <= now
 * <= end_time`, `IsBlackoutActive(connection_id)` makes the alerter skip
 * every alert for that connection. These specs drive the blackout
 * lifecycle through the GUI (create, stop, delete) and prove the
 * suppression invariant end-to-end against the real alerter:
 *
 *  - Test A: a live alert condition is suppressed for >= 1 eval cycle
 *    while the blackout is active, then fires once the window expires.
 *  - Test B: the same suppression, ended early via the GUI Stop control,
 *    followed by a GUI Delete of the now-inactive blackout.
 *
 * The post-suppression firing is what makes the suppression meaningful:
 * it proves the condition was live throughout, so its absence during the
 * window was suppression, not a missing metric.
 *
 * These are slow specs (they observe real alerter cycles), so they group
 * with the other alerter-driven monitoring specs. AI is not required.
 */
test.describe('Blackout suppression and management', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();

    let adminCookie: string;
    let primaryConnId: number;
    let rule: AlertRuleApiRow;

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.beforeEach(async ({ page }) => {
        await label('package', 'Monitoring');
        await ensureClusterAndPrimary(page, api, adminCookie);

        const connections = await api.listConnections({ cookie: adminCookie });
        const primary = connections.find((c) => /primary/i.test(c.name));
        if (!primary) {
            throw new Error('No connection matching /primary/i found');
        }
        primaryConnId = primary.id;

        // A no-extension threshold rule whose live value is always > 0 for a
        // running database, so a `> 0` override fires deterministically.
        const rules = await api.getAlertRules(adminCookie);
        const picked =
            rules.find(
                (r) => r.metric_name === 'connection_utilization_percent',
            ) ??
            rules.find(
                (r) =>
                    /connection|session/i.test(r.metric_name) &&
                    !r.required_extension,
            );
        if (!picked) {
            throw new Error('No suitable no-extension alert rule found');
        }
        rule = picked;
    });

    test.afterEach(async () => {
        // Blackout tables are not covered by the shared e2e teardown, so the
        // spec self-cleans: remove every blackout for the connection (Test A
        // leaves an expired one; a failed Test B may leave an active one),
        // then drop the forcing override and the seeded alert rows.
        if (primaryConnId) {
            const { blackouts } = await api
                .listBlackouts(adminCookie, { connectionId: primaryConnId })
                .catch(() => ({ blackouts: [], total_count: 0 }));
            for (const b of blackouts) {
                await api.deleteBlackout(adminCookie, b.id).catch(() => undefined);
            }
            if (rule) {
                await api
                    .deleteAlertOverride(adminCookie, 'server', primaryConnId, rule.id)
                    .catch(() => undefined);
            }
            await alertSetup.cleanup(primaryConnId).catch(() => undefined);
        }
    });

    test.afterAll(async () => {
        // stop() is a no-op by design — the shared cluster is torn down with
        // the stack, not per spec (see ReplicationHelper.stop).
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
     * Poll the alert list for `holdMs`, asserting on every pass that no
     * active alert exists for our rule. `toPass` cannot express "stays
     * empty", so this re-asserts emptiness across the observation window
     * (which must span >= 1 alerter eval cycle to be meaningful). The
     * internal delay spaces the checks; it is an observation window, not a
     * wait on a state transition.
     */
    async function assertAlertSuppressed(holdMs: number): Promise<void> {
        const deadline = Date.now() + holdMs;
        do {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: primaryConnId,
                excludeCleared: true,
            });
            expect(
                alerts.some((a) => a.rule_id === rule.id && a.status === 'active'),
                'no alert should fire while the blackout is active',
            ).toBe(false);
            if (Date.now() < deadline) {
                await new Promise<void>((resolve) => setTimeout(resolve, 10_000));
            }
        } while (Date.now() < deadline);
    }

    /** Enable the `> 0` override so the rule would fire if not suppressed. */
    async function armAlertOverride(): Promise<void> {
        await api.upsertAlertOverride(adminCookie, 'server', primaryConnId, rule.id, {
            operator: '>',
            threshold: 0,
            severity: 'warning',
            enabled: true,
        });
    }

    /** Poll until an active alert for our rule is present. */
    async function expectAlertFires(): Promise<void> {
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: primaryConnId,
                excludeCleared: true,
            });
            expect(
                alerts.some((a) => a.rule_id === rule.id && a.status === 'active'),
                'the alert should fire once suppression ends',
            ).toBe(true);
        }).toPass({ timeout: EVAL_CYCLE_MS * 4, intervals: [5_000] });
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('a server blackout suppresses alerts, then expires and they resume', async ({
        page,
    }) => {
        // A 5-minute window is the smallest that comfortably fits: create it
        // via the GUI, verify it active, observe >= 1 full eval cycle
        // (EVAL_CYCLE_MS = 60s) of suppression with margin, then let it
        // expire. The overall timeout covers the window plus the ~30s
        // BlackoutContext refetch and the ~60-90s post-expiry alerter lag.
        test.setTimeout(600_000);
        const WINDOW_MINUTES = 5;
        const reason = `e2e blackout suppress ${Date.now()}`;

        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const blackout = new BlackoutPage(page);

        await selectPrimaryServer(page, monitoring, serverNav);

        // Prime a fresh metric BEFORE the window opens so the alert condition
        // is live; the running collector keeps it fresh thereafter.
        await replication.restartCollector([PRIMARY_SERVER.name]);

        let blackoutId = 0;
        let endTime = 0;
        await test.step('Create a one-time server blackout via the GUI', async () => {
            await blackout.openManagement();
            await blackout.createOneTimeServerBlackout({
                minutes: WINDOW_MINUTES,
                reason,
            });
            // Poll: the dialog re-renders from context after the create's
            // refetch, so the Active section may lag the dialog by a tick.
            await expect(async () => {
                expect(
                    await blackout.activeCount(),
                    'the new blackout should be listed active in the management dialog',
                ).toBeGreaterThanOrEqual(1);
            }).toPass({ timeout: 10_000, intervals: [500] });
            await blackout.closeManagement();

            // GUI indicator on the primary-node row.
            await blackout.serverBlackoutIndicatorVisible(PRIMARY_SERVER.name);

            // API cross-check: capture the id and end time.
            const { blackouts } = await api.listBlackouts(adminCookie, {
                connectionId: primaryConnId,
                active: true,
            });
            const active = blackouts.find(
                (b) => b.reason === reason && b.connection_id === primaryConnId,
            );
            if (!active) {
                throw new Error('created blackout not found via the API');
            }
            blackoutId = active.id;
            endTime = new Date(active.end_time).getTime();
        });

        await test.step('The blackout suppresses a live alert condition', async () => {
            // Arm the override AFTER the window is active: it would fire on the
            // next eval cycle if not suppressed.
            await armAlertOverride();
            // Observe >= 1 full eval cycle with margin; the list must stay
            // empty throughout.
            await assertAlertSuppressed(EVAL_CYCLE_MS * 1.5);
            expect(
                Date.now(),
                'suppression must be observed before the window expires',
            ).toBeLessThan(endTime);
        });

        await test.step('After expiry the blackout clears and the alert fires', async () => {
            // The server computes is_active from the window bounds, so it
            // flips to false at end_time. Size the wait to the ACTUAL end_time
            // (captured above) rather than a fixed guess — this step starts
            // partway through the window, so a fixed timeout can elapse before
            // the window even ends. Wait until end_time + a buffer for the
            // server's NOW() evaluation and poll granularity.
            const expiryTimeout = Math.max(
                endTime - Date.now() + 90_000,
                60_000,
            );
            await expect(async () => {
                const { blackouts } = await api.listBlackouts(adminCookie, {
                    connectionId: primaryConnId,
                });
                const row = blackouts.find((b) => b.id === blackoutId);
                expect(row, 'the blackout should still exist').toBeTruthy();
                expect(
                    row?.is_active,
                    'the blackout should be inactive after expiry',
                ).toBe(false);
            }).toPass({ timeout: expiryTimeout, intervals: [5_000] });

            await blackout.waitForServerBlackoutIndicatorGone(PRIMARY_SERVER.name);

            // The alert now fires — proving the condition was live all along,
            // so its earlier absence was genuine suppression.
            await expectAlertFires();
        });
    });

    test('a blackout can be stopped and deleted via the GUI, resuming alerts', async ({
        page,
    }) => {
        test.setTimeout(360_000);
        // Ended via Stop, not natural expiry, so a long window is fine.
        const WINDOW_MINUTES = 60;
        const reason = `e2e blackout stop-delete ${Date.now()}`;

        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const blackout = new BlackoutPage(page);

        await selectPrimaryServer(page, monitoring, serverNav);
        await replication.restartCollector([PRIMARY_SERVER.name]);

        let blackoutId = 0;
        await test.step('Create a one-time server blackout via the GUI', async () => {
            await blackout.openManagement();
            await blackout.createOneTimeServerBlackout({
                minutes: WINDOW_MINUTES,
                reason,
            });
            // Poll: the dialog re-renders from context after the create's
            // refetch, so the Active section may lag the dialog by a tick.
            await expect(async () => {
                expect(
                    await blackout.activeCount(),
                    'the new blackout should be listed active in the management dialog',
                ).toBeGreaterThanOrEqual(1);
            }).toPass({ timeout: 10_000, intervals: [500] });
            await blackout.closeManagement();
            await blackout.serverBlackoutIndicatorVisible(PRIMARY_SERVER.name);

            const { blackouts } = await api.listBlackouts(adminCookie, {
                connectionId: primaryConnId,
                active: true,
            });
            const active = blackouts.find(
                (b) => b.reason === reason && b.connection_id === primaryConnId,
            );
            if (!active) {
                throw new Error('created blackout not found via the API');
            }
            blackoutId = active.id;
        });

        await test.step('Suppression holds while the blackout is active', async () => {
            await armAlertOverride();
            await assertAlertSuppressed(EVAL_CYCLE_MS);
        });

        await test.step('Stopping the blackout via the GUI resumes alerts', async () => {
            await blackout.openManagement();
            await blackout.stopActive();

            await expect(async () => {
                const { blackouts } = await api.listBlackouts(adminCookie, {
                    connectionId: primaryConnId,
                });
                const row = blackouts.find((b) => b.id === blackoutId);
                expect(row, 'the blackout should still exist after stop').toBeTruthy();
                expect(
                    row?.is_active,
                    'the blackout should be inactive after stop',
                ).toBe(false);
            }).toPass({ timeout: 60_000, intervals: [5_000] });

            await blackout.waitForServerBlackoutIndicatorGone(PRIMARY_SERVER.name);
            await expectAlertFires();
        });

        await test.step('Deleting the inactive blackout via the GUI removes it', async () => {
            // The management dialog is still open after Stop; the blackout is
            // now in the non-active section with a delete control.
            await blackout.deleteFirstInactive();
            await expect(async () => {
                const { blackouts } = await api.listBlackouts(adminCookie, {
                    connectionId: primaryConnId,
                });
                expect(
                    blackouts.some((b) => b.id === blackoutId),
                    'the deleted blackout should be gone',
                ).toBe(false);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });
    });
});
