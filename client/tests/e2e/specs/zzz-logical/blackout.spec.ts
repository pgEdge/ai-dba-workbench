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
import { LogicalReplicationHelper } from '../../helpers/logical-replication.helper';
import { AlertSetupHelper } from '../../helpers/alert-setup.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { BlackoutPage } from '../../pages/BlackoutPage';
import {
    ensureLogicalClusterAndNodes,
    LOGICAL_CLUSTER_NAME,
    LOGICAL_NODE1,
} from '../../helpers/logical-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

const { EVAL_CYCLE_MS } = AlertSetupHelper;

/**
 * Logical-cluster mirror of the blackout suite: a server blackout suppresses
 * the real alerter for the logical publisher node while active, then alerts
 * resume on natural expiry (Test A) or when stopped + deleted via the GUI
 * (Test B). BlackoutPage is scope-agnostic; the blackout targets whichever
 * server is selected.
 */
test.describe('Blackout suppression and management (Logical)', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();
    const logical = new LogicalReplicationHelper();

    let adminCookie: string;
    let nodeConnId: number;
    let rule: AlertRuleApiRow;

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await logical.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.beforeEach(async ({ page }) => {
        await label('package', 'Monitoring (Logical)');
        test.setTimeout(180_000);
        await ensureLogicalClusterAndNodes(page, api, adminCookie);

        const connections = await api.listConnections({ cookie: adminCookie });
        const node = connections.find((c) => c.name === LOGICAL_NODE1.name);
        if (!node) {
            throw new Error(`No connection named "${LOGICAL_NODE1.name}" found`);
        }
        nodeConnId = node.id;

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
        if (nodeConnId) {
            const { blackouts } = await api
                .listBlackouts(adminCookie, { connectionId: nodeConnId })
                .catch(() => ({ blackouts: [], total_count: 0 }));
            for (const b of blackouts) {
                await api
                    .deleteBlackout(adminCookie, b.id)
                    .catch(() => undefined);
            }
            if (rule) {
                await api
                    .deleteAlertOverride(
                        adminCookie,
                        'server',
                        nodeConnId,
                        rule.id,
                    )
                    .catch(() => undefined);
            }
            await alertSetup.cleanup(nodeConnId).catch(() => undefined);
        }
    });

    test.afterAll(async () => {
        await logical.stop().catch(() => undefined);
    });

    // ------------------------------------------------------------------
    // Helpers
    // ------------------------------------------------------------------

    async function selectPublisherServer(
        page: Page,
        monitoring: MonitoringPage,
        serverNav: ServerNavigatorPage,
    ): Promise<void> {
        await page.goto('/');
        await monitoring.waitForAppLoad();
        await expect(async () => {
            const row = serverNav.getServerRow(LOGICAL_NODE1.name).first();
            if (!(await row.isVisible().catch(() => false))) {
                await serverNav.expandCluster(LOGICAL_CLUSTER_NAME);
            }
            await row.click({ force: true });
            await expect(
                monitoring.getSectionHeader('Monitoring'),
                'Monitoring section should render after selecting the node',
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 60_000, intervals: [3_000] });
    }

    async function assertAlertSuppressed(holdMs: number): Promise<void> {
        const deadline = Date.now() + holdMs;
        do {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: nodeConnId,
                excludeCleared: true,
            });
            expect(
                alerts.some(
                    (a) => a.rule_id === rule.id && a.status === 'active',
                ),
                'no alert should fire while the blackout is active',
            ).toBe(false);
            if (Date.now() < deadline) {
                await new Promise<void>((resolve) =>
                    setTimeout(resolve, 10_000),
                );
            }
        } while (Date.now() < deadline);
    }

    async function armAlertOverride(): Promise<void> {
        await api.upsertAlertOverride(
            adminCookie,
            'server',
            nodeConnId,
            rule.id,
            { operator: '>', threshold: 0, severity: 'warning', enabled: true },
        );
    }

    async function expectAlertFires(): Promise<void> {
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: nodeConnId,
                excludeCleared: true,
            });
            expect(
                alerts.some(
                    (a) => a.rule_id === rule.id && a.status === 'active',
                ),
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
        test.setTimeout(600_000);
        const WINDOW_MINUTES = 5;
        const reason = `e2e logical blackout suppress ${Date.now()}`;

        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const blackout = new BlackoutPage(page);

        await selectPublisherServer(page, monitoring, serverNav);
        await replication.restartCollector([LOGICAL_NODE1.name]);

        let blackoutId = 0;
        let endTime = 0;
        await test.step('Create a one-time server blackout via the GUI', async () => {
            await blackout.openManagement();
            await blackout.createOneTimeServerBlackout({
                minutes: WINDOW_MINUTES,
                reason,
            });
            await expect(async () => {
                expect(
                    await blackout.activeCount(),
                    'the new blackout should be listed active in the dialog',
                ).toBeGreaterThanOrEqual(1);
            }).toPass({ timeout: 10_000, intervals: [500] });
            await blackout.closeManagement();

            await blackout.serverBlackoutIndicatorVisible(LOGICAL_NODE1.name);

            const { blackouts } = await api.listBlackouts(adminCookie, {
                connectionId: nodeConnId,
                active: true,
            });
            const active = blackouts.find(
                (b) => b.reason === reason && b.connection_id === nodeConnId,
            );
            if (!active) {
                throw new Error('created blackout not found via the API');
            }
            blackoutId = active.id;
            endTime = new Date(active.end_time).getTime();
        });

        await test.step('The blackout suppresses a live alert condition', async () => {
            await armAlertOverride();
            await assertAlertSuppressed(EVAL_CYCLE_MS * 1.5);
            expect(
                Date.now(),
                'suppression must be observed before the window expires',
            ).toBeLessThan(endTime);
        });

        await test.step('After expiry the blackout clears and the alert fires', async () => {
            const expiryTimeout = Math.max(
                endTime - Date.now() + 90_000,
                60_000,
            );
            await expect(async () => {
                const { blackouts } = await api.listBlackouts(adminCookie, {
                    connectionId: nodeConnId,
                });
                const row = blackouts.find((b) => b.id === blackoutId);
                expect(row, 'the blackout should still exist').toBeTruthy();
                expect(
                    row?.is_active,
                    'the blackout should be inactive after expiry',
                ).toBe(false);
            }).toPass({ timeout: expiryTimeout, intervals: [5_000] });

            await blackout.waitForServerBlackoutIndicatorGone(
                LOGICAL_NODE1.name,
            );
            await expectAlertFires();
        });
    });

    test('a blackout can be stopped and deleted via the GUI, resuming alerts', async ({
        page,
    }) => {
        test.setTimeout(360_000);
        const WINDOW_MINUTES = 60;
        const reason = `e2e logical blackout stop-delete ${Date.now()}`;

        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const blackout = new BlackoutPage(page);

        await selectPublisherServer(page, monitoring, serverNav);
        await replication.restartCollector([LOGICAL_NODE1.name]);

        let blackoutId = 0;
        await test.step('Create a one-time server blackout via the GUI', async () => {
            await blackout.openManagement();
            await blackout.createOneTimeServerBlackout({
                minutes: WINDOW_MINUTES,
                reason,
            });
            await expect(async () => {
                expect(
                    await blackout.activeCount(),
                    'the new blackout should be listed active in the dialog',
                ).toBeGreaterThanOrEqual(1);
            }).toPass({ timeout: 10_000, intervals: [500] });
            await blackout.closeManagement();
            await blackout.serverBlackoutIndicatorVisible(LOGICAL_NODE1.name);

            const { blackouts } = await api.listBlackouts(adminCookie, {
                connectionId: nodeConnId,
                active: true,
            });
            const active = blackouts.find(
                (b) => b.reason === reason && b.connection_id === nodeConnId,
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
                    connectionId: nodeConnId,
                });
                const row = blackouts.find((b) => b.id === blackoutId);
                expect(
                    row,
                    'the blackout should still exist after stop',
                ).toBeTruthy();
                expect(
                    row?.is_active,
                    'the blackout should be inactive after stop',
                ).toBe(false);
            }).toPass({ timeout: 60_000, intervals: [5_000] });

            await blackout.waitForServerBlackoutIndicatorGone(
                LOGICAL_NODE1.name,
            );
            await expectAlertFires();
        });

        await test.step('Deleting the inactive blackout via the GUI removes it', async () => {
            await blackout.deleteFirstInactive();
            await expect(async () => {
                const { blackouts } = await api.listBlackouts(adminCookie, {
                    connectionId: nodeConnId,
                });
                expect(
                    blackouts.some((b) => b.id === blackoutId),
                    'the deleted blackout should be gone',
                ).toBe(false);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });
    });
});
