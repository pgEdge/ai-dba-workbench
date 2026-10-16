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
import { StatusPanelAlertsPage } from '../../pages/StatusPanelAlertsPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { MonitoringPage } from '../../pages/MonitoringPage';
import {
    ensureLogicalClusterAndNodes,
    LOGICAL_CLUSTER_NAME,
    LOGICAL_NODE1,
} from '../../helpers/logical-cluster-setup.helper';
import { skipIfAiProviderLimited } from '../../helpers/ai.helper';
import { API_URL } from '../../fixtures/test-data';

// Driven through the REAL alerter (no direct alert inserts); tests wait for
// the engine to act. See AlertSetupHelper for the timing constants.
const { EVAL_CYCLE_MS, ALERT_CLEANER_MS, REFIRE_COOLDOWN_MS } = AlertSetupHelper;

/**
 * Longest alphabetic word (>= 4 chars) of an alert title — a scoped,
 * case-insensitive status-panel row filter. On the shared physical+logical
 * estate the selected node can carry other active alerts (e.g. leftover
 * staleness alerts), so an unscoped row getter may act on the wrong one.
 */
const longestWord = (title: string): string =>
    (title || '')
        .split(/[^a-zA-Z]+/)
        .filter((w) => w.length >= 4)
        .sort((a, b) => b.length - a.length)[0] ?? '';

/**
 * Logical-cluster mirror of alert-lifecycle: generate → verify →
 * acknowledge (false positive) → AI overview → restore; deduplication;
 * severity escalation; cleared-then-new. Same real-alerter mechanics as the
 * physical suite, driven against the logical publisher node.
 */
test.describe('Alert Lifecycle (Logical)', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();
    const logical = new LogicalReplicationHelper();

    let adminCookie: string;
    let nodeConnId: number;
    let rule: AlertRuleApiRow;

    test.beforeEach(async () => {
        await label('package', 'Alert Management (Logical)');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await logical.start();
        adminCookie = await auth.loginAsAdmin();

        // The AI-overview step requires AI enabled (same as the physical suite).
        const caps = await api.getCapabilities(adminCookie);
        expect(
            caps.ai_enabled,
            'AI must be enabled for this suite (E2E_AI_ENABLED=true + an LLM ' +
            'key). The alert AI-overview step requires it.',
        ).toBe(true);
    });

    test.beforeEach(async ({ page }) => {
        test.setTimeout(180_000);
        await ensureLogicalClusterAndNodes(page, api, adminCookie);
        const connections = await api.listConnections({ cookie: adminCookie });
        const node = connections.find((c) => c.name === LOGICAL_NODE1.name);
        if (!node) {
            throw new Error(
                `No connection named "${LOGICAL_NODE1.name}" found. ` +
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
        expect(
            chosen,
            'Expected a built-in, no-extension connection rule to drive alerts',
        ).toBeTruthy();
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

    async function generateActiveAlert(severity: string): Promise<number> {
        await api.upsertAlertOverride(
            adminCookie,
            'server',
            nodeConnId,
            rule.id,
            { operator: '>', threshold: 0, severity, enabled: true },
        );
        await replication.restartCollector([LOGICAL_NODE1.name]);
        let alertId = 0;
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: nodeConnId,
                excludeCleared: true,
            });
            const active = alerts.find(
                (a) => a.rule_id === rule.id && a.status === 'active',
            );
            expect(
                active,
                `an active alert for rule ${rule.name} should fire`,
            ).toBeTruthy();
            alertId = active!.id;
        }).toPass({ timeout: EVAL_CYCLE_MS * 4, intervals: [5_000] });
        return alertId;
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('generate → verify → acknowledge (false positive) → AI overview → restore', async ({
        page,
    }) => {
        test.setTimeout(420_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const alerts = new StatusPanelAlertsPage(page);

        let titleFrag = 'utilization';
        await test.step('Generate an active alert via the real alerter', async () => {
            await generateActiveAlert('warning');
            // Capture our alert's title fragment to scope status-panel rows to
            // OUR alert (the shared estate may carry other active alerts).
            const { alerts: fired } = await api.getAlerts(adminCookie, {
                connectionId: nodeConnId,
                excludeCleared: true,
            });
            const ours = fired.find(
                (a) => a.rule_id === rule.id && a.status === 'active',
            );
            titleFrag = longestWord(ours?.title ?? '') || titleFrag;
        });

        await test.step('Verify the alert in the GUI active list + cross-check API', async () => {
            await selectPublisherServer(page, monitoring, serverNav);
            await alerts.expandActive();
            const row = alerts.getActiveAlertRow(titleFrag);
            await expect(
                row,
                'the active alert row should render in the status panel',
            ).toBeVisible({ timeout: 60_000 });
            const guiRow = await alerts.readAlertRow(row, false);
            expect(guiRow.severity?.toLowerCase()).toBe('warning');
            expect((guiRow.type ?? '').toLowerCase()).toContain('threshold');

            const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                connectionId: nodeConnId,
                excludeCleared: true,
            });
            const apiRow = apiAlerts.find(
                (a) => a.rule_id === rule.id && a.status === 'active',
            );
            expect(apiRow, 'API should list the active alert').toBeTruthy();
            expect(apiRow!.severity).toBe('warning');
        });

        let acknowledgedRow;
        await test.step('Acknowledge via the GUI dialog with reason + false positive', async () => {
            await alerts.expandActive();
            const row = alerts.getActiveAlertRow(titleFrag);
            await expect(row, 'active row present before acknowledge').toBeVisible({
                timeout: 30_000,
            });
            await alerts.acknowledge(row, {
                reason: 'e2e known issue',
                falsePositive: true,
            });

            await alerts.expandAcknowledged();
            acknowledgedRow = alerts.getAcknowledgedAlertRow(titleFrag);
            await expect(
                acknowledgedRow,
                'acknowledged row present after acknowledge',
            ).toBeVisible({ timeout: 30_000 });
            const acked = await alerts.readAlertRow(acknowledgedRow, false);
            expect(acked.falsePositive, 'row shows False Positive chip').toBe(
                true,
            );
            expect(
                acked.ackedBy ?? '',
                'row shows Acked by admin',
            ).toContain('admin');

            await expect(async () => {
                const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                    connectionId: nodeConnId,
                });
                const apiRow = apiAlerts.find((a) => a.rule_id === rule.id);
                expect(apiRow?.status).toBe('acknowledged');
                expect(apiRow?.false_positive).toBe(true);
                expect(apiRow?.ack_message).toBe('e2e known issue');
            }).toPass({ timeout: 15_000, intervals: [2_000] });
        });

        await test.step('AI Overview for the acknowledged alert', async () => {
            const dialog = await alerts.openAnalyze(acknowledgedRow!);
            await dialog.waitForReady(120_000);
            // A provider usage/rate limit is environmental — skip rather than
            // fail. Any other error still trips the hasError assertion below.
            await skipIfAiProviderLimited(dialog);
            expect(await dialog.hasError(), 'analysis should not fail').toBe(
                false,
            );
            const text = await dialog.text();
            expect(
                text.trim().length,
                'analysis should not be empty',
            ).toBeGreaterThan(20);

            // Strict (by request): every generated query must execute cleanly.
            const { total, errors } = await dialog.runAllGeneratedQueries(
                30_000,
            );
            expect(
                errors,
                `all ${total} AI-generated queries should execute without error`,
            ).toEqual([]);
            await dialog.close();
        });

        await test.step('Restore the acknowledged alert to active', async () => {
            await alerts.expandAcknowledged();
            const row = alerts.getAcknowledgedAlertRow(titleFrag);
            await expect(
                row,
                'acknowledged row present before restore',
            ).toBeVisible({ timeout: 30_000 });
            await alerts.restore(row);

            await expect(async () => {
                const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                    connectionId: nodeConnId,
                    excludeCleared: true,
                });
                const apiRow = apiAlerts.find((a) => a.rule_id === rule.id);
                expect(apiRow?.status, 'alert should be active again').toBe(
                    'active',
                );
                expect(
                    apiRow?.acknowledged_at ?? null,
                    'ack cleared',
                ).toBeNull();
            }).toPass({ timeout: 15_000, intervals: [2_000] });
        });
    });

    test('deduplication: new event at the same severity keeps a single acknowledged alert', async () => {
        test.setTimeout(300_000);
        await generateActiveAlert('warning');

        const before = await alertSetup.readAlertsFromStore(nodeConnId);
        const target = before.find(
            (a) => a.ruleId === rule.id && a.status === 'active',
        );
        expect(target, 'active alert to acknowledge').toBeTruthy();
        await api.acknowledgeAlert(adminCookie, {
            alertId: target!.id,
            message: 'dedup test',
        });

        const deadline = Date.now() + EVAL_CYCLE_MS * 2;
        while (Date.now() < deadline) {
            await new Promise((r) => setTimeout(r, 5_000));
        }
        const after = await alertSetup.readAlertsFromStore(nodeConnId);
        const forRule = after.filter((a) => a.ruleId === rule.id);
        expect(forRule.length, 'no duplicate alert row for the rule').toBe(1);
        expect(forRule[0].status, 'stays acknowledged').toBe('acknowledged');
        expect(forRule[0].id, 'same alert row id').toBe(target!.id);
    });

    test('severity escalation reactivates an acknowledged alert', async () => {
        test.setTimeout(300_000);
        const alertId = await generateActiveAlert('warning');
        await api.acknowledgeAlert(adminCookie, {
            alertId,
            message: 'ack before escalation',
        });

        await api.upsertAlertOverride(
            adminCookie,
            'server',
            nodeConnId,
            rule.id,
            { operator: '>', threshold: 0, severity: 'critical', enabled: true },
        );
        await expect(async () => {
            const rows = await alertSetup.readAlertsFromStore(nodeConnId);
            const forRule = rows.filter((a) => a.ruleId === rule.id);
            expect(forRule.length, 'still a single alert row').toBe(1);
            expect(forRule[0].status, 'reactivated to active').toBe('active');
            expect(forRule[0].severity, 'escalated to critical').toBe(
                'critical',
            );
            expect(forRule[0].id, 'same alert id (no duplicate)').toBe(alertId);
        }).toPass({ timeout: EVAL_CYCLE_MS * 3, intervals: [5_000] });
    });

    test('cleared alert then a new alert for the same rule', async () => {
        test.setTimeout(600_000);
        const firstId = await generateActiveAlert('warning');

        await api.deleteAlertOverride(adminCookie, 'server', nodeConnId, rule.id);
        await alertSetup.raiseStoredThreshold(firstId);
        await expect(async () => {
            const rows = await alertSetup.readAlertsFromStore(nodeConnId);
            const cleared = rows.find((a) => a.id === firstId);
            expect(cleared?.status, 'first alert should clear').toBe('cleared');
        }).toPass({ timeout: ALERT_CLEANER_MS * 6, intervals: [5_000] });

        await new Promise((r) => setTimeout(r, REFIRE_COOLDOWN_MS + 10_000));
        await api.upsertAlertOverride(
            adminCookie,
            'server',
            nodeConnId,
            rule.id,
            { operator: '>', threshold: 0, severity: 'warning', enabled: true },
        );
        const secondId = await generateActiveAlert('warning');
        expect(secondId, 'a new alert row is created').not.toBe(firstId);

        const rows = await alertSetup.readAlertsFromStore(nodeConnId);
        expect(
            rows.find((a) => a.id === firstId)?.status,
            'previous alert remains cleared',
        ).toBe('cleared');
        expect(
            rows.find((a) => a.id === secondId)?.status,
            'new alert is active',
        ).toBe('active');
    });
});
