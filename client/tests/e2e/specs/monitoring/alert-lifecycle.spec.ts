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
import { StatusPanelAlertsPage } from '../../pages/StatusPanelAlertsPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { MonitoringPage } from '../../pages/MonitoringPage';
import {
    ensureClusterAndPrimary,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { skipIfAiProviderLimited } from '../../helpers/ai.helper';
import { API_URL } from '../../fixtures/test-data';

// The alert workflow is driven through the REAL alerter (no direct alert
// row inserts), so tests wait up to a couple of evaluation cycles for the
// engine to act. See AlertSetupHelper for the timing constants.
const { EVAL_CYCLE_MS, ALERT_CLEANER_MS, REFIRE_COOLDOWN_MS } = AlertSetupHelper;

test.describe('Alert Lifecycle', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();

    let adminCookie: string;
    let primaryConnId: number;
    let rule: AlertRuleApiRow;

    test.beforeEach(async () => {
        await label('package', 'Alert Management');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();

        // Locked decision: the suite requires AI to be enabled.
        const caps = await api.getCapabilities(adminCookie);
        expect(
            caps.ai_enabled,
            'AI must be enabled for this suite (set E2E_AI_ENABLED=true and an ' +
            'LLM key). The alert AI-overview steps require it.',
        ).toBe(true);
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

        // Pick a built-in rule we can drive with a metric seed and that
        // needs no extension. Prefer connection utilisation (fed by
        // pg_stat_activity, which seedConnectionCount populates); fall back
        // to any no-extension rule over pg_stat_activity.
        const rules = await api.getAlertRules(adminCookie);
        const chosen =
            rules.find((r) => r.metric_name === 'connection_utilization_percent') ??
            rules.find(
                (r) => /connection|session/i.test(r.metric_name) && !r.required_extension,
            ) ??
            rules.find(
                (r) => !r.required_extension && r.metric_name.startsWith('pg_stat_activity'),
            );
        expect(
            chosen,
            'Expected a built-in, no-extension connection rule to drive alerts',
        ).toBeTruthy();
        rule = chosen!;
    });

    test.afterEach(async () => {
        // These tables are NOT covered by the global e2e-test-* teardown,
        // so each test cleans the alerts / thresholds / seeded metrics it
        // created for the primary connection.
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

    /** Select the primary server and wait for its StatusPanel to render. */
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
     * Drive the real alerter to raise an active alert for `rule` at the
     * given severity: set a controlled server override (threshold 0, '>'),
     * then re-seed the connection metric each poll so a fresh violating
     * snapshot is always the newest at evaluation time (the live collector
     * may otherwise supersede a one-shot seed). Resolves with the alert id.
     */
    async function generateActiveAlert(severity: string): Promise<number> {
        await api.upsertAlertOverride(adminCookie, 'server', primaryConnId, rule.id, {
            operator: '>',
            threshold: 0,
            severity,
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
            expect(active, `an active alert for rule ${rule.name} should fire`).toBeTruthy();
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

        await test.step('Generate an active alert via the real alerter', async () => {
            await generateActiveAlert('warning');
        });

        await test.step('Verify the alert in the GUI active list + cross-check API', async () => {
            await selectPrimaryServer(page, monitoring, serverNav);
            await alerts.expandActive();
            const row = alerts.getActiveAlertRow();
            // Wait (bounded) for the alert to propagate into the GUI before
            // reading it, so a not-yet-rendered row fails fast with a clear
            // message instead of hanging until the test timeout. A server
            // selection renders no server chip, so read with showServer=false.
            await expect(
                row,
                'the active alert row should render in the status panel',
            ).toBeVisible({ timeout: 60_000 });
            const guiRow = await alerts.readAlertRow(row, false);
            expect(guiRow.severity?.toLowerCase()).toBe('warning');
            expect((guiRow.type ?? '').toLowerCase()).toContain('threshold');

            const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                connectionId: primaryConnId,
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
            const row = alerts.getActiveAlertRow();
            await expect(row, 'active row present before acknowledge').toBeVisible({
                timeout: 30_000,
            });
            await alerts.acknowledge(row, {
                reason: 'e2e known issue',
                falsePositive: true,
            });

            await alerts.expandAcknowledged();
            acknowledgedRow = alerts.getAcknowledgedAlertRow();
            await expect(
                acknowledgedRow,
                'acknowledged row present after acknowledge',
            ).toBeVisible({ timeout: 30_000 });
            const acked = await alerts.readAlertRow(acknowledgedRow, false);
            expect(acked.falsePositive, 'row shows False Positive chip').toBe(true);
            expect(acked.ackedBy ?? '', 'row shows Acked by admin').toContain('admin');

            // Cross-check the backend state.
            await expect(async () => {
                const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
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
            expect(await dialog.hasError(), 'analysis should not fail').toBe(false);
            const text = await dialog.text();
            expect(text.trim().length, 'analysis should not be empty').toBeGreaterThan(20);

            // Run EVERY generated SQL block and require them ALL to execute
            // cleanly. Strict by request: this surfaces AI query-quality
            // problems instead of silently skipping them, so it WILL go red
            // whenever the model emits a non-executable query (e.g. a
            // hallucinated column like `max_connections` on pg_stat_database).
            const { total, errors } = await dialog.runAllGeneratedQueries(30_000);
            expect(
                errors,
                `all ${total} AI-generated queries should execute without error`,
            ).toEqual([]);
            await dialog.close();
        });

        await test.step('Restore the acknowledged alert to active', async () => {
            await alerts.expandAcknowledged();
            const row = alerts.getAcknowledgedAlertRow();
            await expect(row, 'acknowledged row present before restore').toBeVisible({
                timeout: 30_000,
            });
            await alerts.restore(row);

            await expect(async () => {
                const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
                    excludeCleared: true,
                });
                const apiRow = apiAlerts.find((a) => a.rule_id === rule.id);
                expect(apiRow?.status, 'alert should be active again').toBe('active');
                expect(apiRow?.acknowledged_at ?? null, 'ack cleared').toBeNull();
            }).toPass({ timeout: 15_000, intervals: [2_000] });
        });
    });

    test('deduplication: new event at the same severity keeps a single acknowledged alert', async () => {
        test.setTimeout(300_000);
        await generateActiveAlert('warning');

        // Acknowledge via API (this test focuses on the dedup behaviour).
        const before = await alertSetup.readAlertsFromStore(primaryConnId);
        const target = before.find((a) => a.ruleId === rule.id && a.status === 'active');
        expect(target, 'active alert to acknowledge').toBeTruthy();
        await api.acknowledgeAlert(adminCookie, {
            alertId: target!.id,
            message: 'dedup test',
        });

        // Re-seed the SAME violating metric at the SAME severity for a full
        // extra evaluation cycle, then assert nothing new fired and the
        // alert stayed acknowledged (backend updates the row in place).
        const deadline = Date.now() + EVAL_CYCLE_MS * 2;
        while (Date.now() < deadline) {
            await new Promise((r) => setTimeout(r, 5_000));
        }
        const after = await alertSetup.readAlertsFromStore(primaryConnId);
        const forRule = after.filter((a) => a.ruleId === rule.id);
        expect(forRule.length, 'no duplicate alert row for the rule').toBe(1);
        expect(forRule[0].status, 'stays acknowledged').toBe('acknowledged');
        expect(forRule[0].id, 'same alert row id').toBe(target!.id);
    });

    test('severity escalation reactivates an acknowledged alert', async () => {
        test.setTimeout(300_000);
        const alertId = await generateActiveAlert('warning');
        await api.acknowledgeAlert(adminCookie, { alertId, message: 'ack before escalation' });

        // Raise the override severity and re-seed; the backend reactivates
        // the acknowledged alert (status → active, ack removed) when the
        // severity changes, without creating a duplicate.
        await api.upsertAlertOverride(adminCookie, 'server', primaryConnId, rule.id, {
            operator: '>',
            threshold: 0,
            severity: 'critical',
            enabled: true,
        });
        await expect(async () => {
            const rows = await alertSetup.readAlertsFromStore(primaryConnId);
            const forRule = rows.filter((a) => a.ruleId === rule.id);
            expect(forRule.length, 'still a single alert row').toBe(1);
            expect(forRule[0].status, 'reactivated to active').toBe('active');
            expect(forRule[0].severity, 'escalated to critical').toBe('critical');
            expect(forRule[0].id, 'same alert id (no duplicate)').toBe(alertId);
        }).toPass({ timeout: EVAL_CYCLE_MS * 3, intervals: [5_000] });
    });

    test('cleared alert then a new alert for the same rule', async () => {
        // Longest test: generate + clear + 5-min re-fire cooldown + regenerate.
        test.setTimeout(600_000);
        const firstId = await generateActiveAlert('warning');

        // Clear the violation deterministically. The alerter's resolution
        // check compares the live value against the alert's OWN stored
        // threshold (0), NOT the override, so raising the override can never
        // clear a `> 0` alert. Remove the forcing override so the evaluator
        // stops re-firing (and resetting the stored threshold), then raise
        // the stored threshold so the next cleaner cycle (~30s) resolves it.
        await api.deleteAlertOverride(adminCookie, 'server', primaryConnId, rule.id);
        await alertSetup.raiseStoredThreshold(firstId);
        await expect(async () => {
            const rows = await alertSetup.readAlertsFromStore(primaryConnId);
            const cleared = rows.find((a) => a.id === firstId);
            expect(cleared?.status, 'first alert should clear').toBe('cleared');
        }).toPass({ timeout: ALERT_CLEANER_MS * 6, intervals: [5_000] });

        // A cleared alert is blocked from re-firing for the 5-minute
        // cooldown; wait it out, then re-arm the low threshold and confirm
        // a NEW active alert appears while the old one stays cleared.
        await new Promise((r) => setTimeout(r, REFIRE_COOLDOWN_MS + 10_000));
        await api.upsertAlertOverride(adminCookie, 'server', primaryConnId, rule.id, {
            operator: '>',
            threshold: 0,
            severity: 'warning',
            enabled: true,
        });
        const secondId = await generateActiveAlert('warning');
        expect(secondId, 'a new alert row is created').not.toBe(firstId);

        const rows = await alertSetup.readAlertsFromStore(primaryConnId);
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
