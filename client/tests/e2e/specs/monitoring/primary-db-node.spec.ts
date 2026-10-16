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
import { StatusPanelAlertsPage } from '../../pages/StatusPanelAlertsPage';
import { EventTimelinePage } from '../../pages/EventTimelinePage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import type { AnalysisDialog } from '../../pages/AnalysisDialog';
import {
    ensureClusterAndPrimary,
    primeSystemMetrics,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { StoreHelper } from '../../helpers/store.helper';
import { API_URL } from '../../fixtures/test-data';

const { EVAL_CYCLE_MS } = AlertSetupHelper;

const agoIso = (ms: number): string => new Date(Date.now() - ms).toISOString();
const aheadIso = (ms: number): string => new Date(Date.now() + ms).toISOString();

async function assertAnalysisIsMeaningful(dialog: AnalysisDialog): Promise<void> {
    await dialog.waitForReady();
    expect(await dialog.hasError(), 'analysis should not fail').toBe(false);
    expect((await dialog.text()).trim().length, 'analysis not empty').toBeGreaterThan(40);
    // Run EVERY generated SQL block and require them ALL to execute cleanly
    // (strict, by request): surfaces AI query-quality problems instead of
    // skipping them. Non-SQL blocks ("no executable SQL found") are excluded
    // by the helper. NOTE: this WILL go red whenever the model emits a
    // non-executable query (hallucinated column, syntax error, `$1`
    // placeholders) — that detection is the point.
    const { total, errors } = await dialog.runAllGeneratedQueries(30_000);
    expect(
        errors,
        `all ${total} AI-generated queries should execute without error`,
    ).toEqual([]);
}

test.describe('Primary Database Node', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();
    const store = new StoreHelper();

    let adminCookie: string;
    let primaryConnId: number;
    let rule: AlertRuleApiRow;

    /**
     * Force a FRESH pg_sys collection immediately before reading the System
     * Resources tiles/graphs. This must run per-read, not once: the
     * collector does not reliably keep pg_sys fresh (its lowered interval
     * reverts, and each test's afterEach `cleanup` wipes recent rows), so
     * the newest sample otherwise ages out and the tiles show "No data".
     * A collector restart re-collects immediately because cleanup left no
     * recent row, and primeSystemMetrics waits until the sample lands.
     */
    async function ensureSystemMetrics(): Promise<void> {
        await primeSystemMetrics(api, adminCookie, primaryConnId);
    }

    test.beforeEach(async () => {
        await label('package', 'Monitoring');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();
        const caps = await api.getCapabilities(adminCookie);
        expect(caps.ai_enabled, 'AI must be enabled for this suite').toBe(true);
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
    // Helpers (inlined per-spec, mirroring the sibling specs)
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
            const active = alerts.find((a) => a.rule_id === rule.id && a.status === 'active');
            expect(active, 'an active alert should fire').toBeTruthy();
            alertId = active!.id;
        }).toPass({ timeout: EVAL_CYCLE_MS * 4, intervals: [5_000] });
        return alertId;
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('monitoring metrics render on the primary node and match the backend', async ({
        page,
    }) => {
        test.setTimeout(420_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // System Resources tiles are backed by the pg_sys_* probes; prime
        // fresh samples so the tiles resolve instead of showing "no data".
        await ensureSystemMetrics();

        await selectPrimaryServer(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.expandSection('System Resources');

        await test.step('KPI tiles resolve to real values', async () => {
            await expect(async () => {
                const mem = await monitoring.readKpiTile('Memory Usage');
                expect(mem.value, 'Memory Usage has a value').not.toBeNull();
                expect(mem.value!).toBeGreaterThanOrEqual(0);
                expect(mem.value!).toBeLessThanOrEqual(100);
            }).toPass({ timeout: 90_000, intervals: [3_000] });
        });

        await test.step('The metric is backed by a stored system_stats sample', async () => {
            // The server-scoped pg_sys_* probes have no dimension columns, so
            // /api/v1/metrics/latest cannot serve them (it 400s with "no
            // dimension columns found"). Assert the collector has stored a
            // memory sample by reading the metrics table directly — the same
            // ground truth the UI aggregates.
            const count = await store.queryScalar(
                'SELECT count(*) FROM metrics.pg_sys_memory_info ' +
                `WHERE connection_id = ${primaryConnId}`,
            );
            expect(
                Number.parseInt(count ?? '0', 10),
                'a stored memory sample exists for the primary',
            ).toBeGreaterThan(0);
        });
    });

    test('database details shown match the backend connection + server version', async ({
        page,
    }) => {
        test.setTimeout(180_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // Backend truth for the version string.
        const versionRes = await api.executeConnectionQuery(adminCookie, primaryConnId, {
            query: 'SELECT version()',
        });
        const versionText =
            versionRes.results?.[0]?.rows?.[0]?.[0] ?? '';
        const majorMatch = versionText.match(/PostgreSQL\s+(\d+)/i);
        const majorVersion = majorMatch?.[1] ?? '';

        await selectPrimaryServer(page, monitoring, serverNav);

        await test.step('The inline detail bar reflects host/port/database + version', async () => {
            // Server details render as an inline bar beneath the AI Overview
            // panel (HOST · PORT · DATABASE · USER · POSTGRESQL · OS · ROLE),
            // not in a modal. Assert its distinctive cell values against the
            // registered connection config and the backend version() result.
            await expect(
                page.getByText(PRIMARY_SERVER.db, { exact: true }).first(),
                'DATABASE cell shows the maintenance database',
            ).toBeVisible({ timeout: 15_000 });
            await expect(
                page.getByText(String(PRIMARY_SERVER.port), { exact: true }).first(),
                'PORT cell shows the connection port',
            ).toBeVisible();
            await expect(
                page.getByText(PRIMARY_SERVER.host, { exact: true }).first(),
                'HOST cell shows the connection host',
            ).toBeVisible();
            if (majorVersion) {
                await expect(
                    page.getByText(new RegExp(`\\b${majorVersion}\\.\\d`)).first(),
                    'POSTGRESQL cell shows the server major version',
                ).toBeVisible();
            }
        });
    });

    test('server + per-metric AI Overview produce meaningful analyses', async ({ page }) => {
        test.setTimeout(600_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await selectPrimaryServer(page, monitoring, serverNav);

        await test.step('Server AI Overview', async () => {
            const dialog = await monitoring.openFullAnalysis(false);
            await assertAnalysisIsMeaningful(dialog);
            await dialog.close();
        });

        await test.step('Per-metric AI on Memory Usage', async () => {
            // Prime a fresh pg_sys sample immediately before reading the tile
            // (the Server AI step above can run for minutes, during which the
            // metric ages out), then re-load the dashboard so it fetches the
            // just-collected value.
            await ensureSystemMetrics();
            await selectPrimaryServer(page, monitoring, serverNav);
            await monitoring.expandSection('Monitoring');
            await monitoring.expandSection('System Resources');
            await expect(async () => {
                const mem = await monitoring.readKpiTile('Memory Usage');
                expect(mem.value).not.toBeNull();
            }).toPass({ timeout: 90_000, intervals: [3_000] });
            const dialog = await monitoring.openMetricAnalysis('Memory Usage');
            await assertAnalysisIsMeaningful(dialog);
            await dialog.close();
        });
    });

    test('alert lifecycle + timeline event on the primary node', async ({ page }) => {
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const alerts = new StatusPanelAlertsPage(page);
        const timeline = new EventTimelinePage(page);

        const alertId = await test.step('Generate an active alert', async () => {
            return generateActiveAlert();
        });

        await test.step('Verify + acknowledge + restore via the GUI', async () => {
            // Only this primary connection has an alert, and the StatusPanel
            // is scoped to the selected server, so the single active row is
            // unambiguous. We do NOT filter by the API title — the row shows
            // a friendly-cased title, so the raw name never matches as text.
            await selectPrimaryServer(page, monitoring, serverNav);
            await alerts.expandActive();
            const row = alerts.getActiveAlertRow();
            await expect(row, 'active alert row should render').toBeVisible({
                timeout: 60_000,
            });
            const fields = await alerts.readAlertRow(row, false);
            expect(fields.severity?.toLowerCase()).toBe('warning');

            await alerts.acknowledge(row, { reason: 'primary node ack' });
            await alerts.expandAcknowledged();
            await expect(async () => {
                const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
                });
                expect(apiAlerts.find((a) => a.id === alertId)?.status).toBe('acknowledged');
            }).toPass({ timeout: 45_000, intervals: [2_000] });

            const ackRow = alerts.getAcknowledgedAlertRow();
            await expect(ackRow, 'acknowledged row should render').toBeVisible({
                timeout: 30_000,
            });
            await alerts.restore(ackRow);
            await expect(async () => {
                const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                    connectionId: primaryConnId,
                    excludeCleared: true,
                });
                expect(apiAlerts.find((a) => a.id === alertId)?.status).toBe('active');
            }).toPass({ timeout: 15_000, intervals: [2_000] });
        });

        await test.step('The alert appears on the primary-node timeline', async () => {
            await expect(async () => {
                const { events } = await api.getTimelineEvents(adminCookie, {
                    connectionId: primaryConnId,
                    startTime: agoIso(60 * 60_000),
                    endTime: aheadIso(60_000),
                    eventTypes: ['alert_fired'],
                });
                expect(
                    events.some((e) => e.connection_id === primaryConnId),
                    'alert_fired event present for the primary',
                ).toBe(true);
            }).toPass({ timeout: 30_000, intervals: [3_000] });

            await timeline.expandTimeline();
            await timeline.selectCategory('Alert');
            await expect(async () => {
                expect(await timeline.isEmpty(), 'timeline shows the alert event').toBe(false);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });
    });

    test('a monitoring graph on the primary node is populated from the backend', async ({
        page,
    }) => {
        test.setTimeout(420_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // Prime the pg_sys_cpu_usage_info probe so the CPU series has fresh
        // points within the 1h window the graph queries.
        await ensureSystemMetrics();

        await selectPrimaryServer(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.expandSection('System Resources');

        await expect(async () => {
            expect(
                await monitoring.chartPanelVisible('CPU Usage Over Time'),
                'CPU chart panel renders',
            ).toBe(true);
        }).toPass({ timeout: 30_000, intervals: [3_000] });

        await expect(async () => {
            const series = await api.getMetricsQuery(adminCookie, {
                probeName: 'pg_sys_cpu_usage_info',
                connectionId: primaryConnId,
                metrics: ['idle_mode_percent'],
                timeRange: '1h',
                buckets: 30,
                aggregation: 'avg',
            });
            const idle = series.find((s) => s.metric === 'idle_mode_percent');
            expect(idle?.data.some((d) => d.value !== 0), 'CPU series has data').toBe(true);
        }).toPass({ timeout: 120_000, intervals: [5_000] });
    });
});
