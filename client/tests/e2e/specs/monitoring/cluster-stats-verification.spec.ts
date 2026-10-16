/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { randomBytes } from 'crypto';
import { test, expect, type Page } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ApiHelper, type MetricSeries } from '../../helpers/api.helper';
import { AuthHelper } from '../../helpers/auth.helper';
import { ReplicationHelper } from '../../helpers/replication.helper';
import { StoreHelper } from '../../helpers/store.helper';
import {
    ensureClusterAndPrimary,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { API_URL } from '../../fixtures/test-data';

// ---------------------------------------------------------------
// Constants
// ---------------------------------------------------------------

/** Probes whose collection interval is lowered so a fresh sample lands
 *  within the test window rather than after the 300s/600s defaults. */
const TARGET_PROBES = [
    'pg_stat_statements',
    'pg_sys_cpu_usage_info',
    'pg_sys_memory_info',
    'pg_sys_load_avg_info',
    'pg_sys_disk_info',
];

/** Lowered collection interval, in seconds. */
const FAST_INTERVAL_SECONDS = 5;

/** Times the marker query is executed; every layer must agree on this. */
const MARKER_RUNS = 12;

/**
 * Numeric comparison tolerances. The CI host is effectively idle, so
 * system-resource values are stable across the few seconds separating
 * the GUI render, the served-metrics fetch, and the stored-row read.
 *
 * "GUI vs served" compares the rendered tile against the same bucketed
 * /metrics/query series the tile is built from (tight). "served vs
 * stored" compares /metrics/latest against the raw stored row — the
 * same data by construction, differing only if a new sample lands
 * between the two reads. "GUI vs stored" absorbs bucket-averaging (the
 * tile averages the window) versus a single latest stored sample.
 */
const TOL = {
    memGuiVsServedPct: 2.0,
    memGuiVsStoredPct: 3.0,
    diskGuiVsServedPct: 2.0,
    loadGuiVsServed: 0.5,
    loadGuiVsStored: 1.0,
    // CPU is inherently volatile between samples; idle% on an idle box is
    // high and stable, but usage (100-idle) can still swing, so the CPU
    // bounds (GUI vs served, and GUI vs the single stored sample) are
    // generous.
    cpuGuiVsServedPct: 20.0,
};

// ---------------------------------------------------------------
// Local helpers
// ---------------------------------------------------------------

/**
 * Reproduce SystemResourcesSection.extractLatestValue: return the last
 * non-zero value of a metric series (empty trailing buckets are
 * zero-filled by the backend), 0 when all points are zero, or null when
 * the series is absent/empty.
 */
function latestSeriesValue(
    series: MetricSeries[],
    metric: string,
): number | null {
    const found = series.find((s) => s.metric === metric);
    if (!found || found.data.length === 0) {
        return null;
    }
    for (let i = found.data.length - 1; i >= 0; i--) {
        if (found.data[i].value !== 0) {
            return found.data[i].value;
        }
    }
    return 0;
}

/** Coerce a possibly-string JSON/psql numeric into a finite number. */
function toNumber(value: unknown): number {
    const n = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(n)) {
        throw new Error(`Expected numeric value, got: ${String(value)}`);
    }
    return n;
}

/**
 * Select the primary server in the navigator and wait for the
 * Monitoring section to render. Mirrors the resilient expand/select
 * retry loop used by server-monitoring.spec.ts.
 */
async function selectPrimaryServer(
    page: Page,
    monitoring: MonitoringPage,
    serverNav: ServerNavigatorPage,
): Promise<void> {
    await page.goto('/');
    await monitoring.waitForAppLoad();

    await expect(async () => {
        const primaryRow = serverNav
            .getServerRow(PRIMARY_SERVER.name)
            .first();
        const rowVisible = await primaryRow.isVisible().catch(() => false);
        if (!rowVisible) {
            await serverNav.expandCluster(CLUSTER_NAME);
        }
        await primaryRow.click({ force: true });
        await expect(
            monitoring.getSectionHeader('Monitoring'),
            'Monitoring section should appear after server selection',
        ).toBeVisible({ timeout: 5_000 });
    }).toPass({ timeout: 60_000, intervals: [3_000] });
}

// ---------------------------------------------------------------
// Cluster Stats Verification
// ---------------------------------------------------------------

test.describe('Cluster Stats Verification', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const store = new StoreHelper();
    const replication = new ReplicationHelper();

    // Per-run marker TABLE. A leading SQL comment is stripped by
    // pg_stat_statements (it records from the statement parse location),
    // and a result-column alias does not change the query id (so it
    // merges into an existing entry). A uniquely-named table is the
    // reliable marker: it yields a distinct query id and its name appears
    // verbatim in the normalised query text. The name is a valid SQL
    // identifier and short, so it lands within the 40-char aria-label the
    // GUI row exposes.
    const MARKER = `e2emk_${randomBytes(4).toString('hex')}`;
    const MARKER_QUERY = `SELECT count(*) FROM ${MARKER}`;

    let adminCookie: string;
    let primaryConnId: number;

    test.beforeEach(async () => {
        await label('package', 'Monitoring');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test('enables Top Queries and System Stats, then verifies the GUI against stored metrics', async ({
        page,
    }) => {
        test.setTimeout(420_000);

        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // ----------------------------------------------------------
        // Setup: cluster, primary server, and connection id
        // ----------------------------------------------------------
        await test.step('Ensure cluster and primary server are registered', async () => {
            await ensureClusterAndPrimary(page, api, adminCookie);
        });

        await test.step('Resolve the primary connection id', async () => {
            const connections = await api.listConnections({
                cookie: adminCookie,
            });
            const primary = connections.find((c) => /primary/i.test(c.name));
            if (!primary) {
                throw new Error(
                    'No connection matching /primary/i found after setup. ' +
                    `Available: ${connections.map((c) => c.name).join(', ')}`,
                );
            }
            primaryConnId = primary.id;
        });

        // ----------------------------------------------------------
        // Enable features: fast probes, extensions, workload
        // ----------------------------------------------------------
        await test.step('Enable target probes and lower their collection interval', async () => {
            const configs = await api.getProbeConfigs(adminCookie);
            for (const probeName of TARGET_PROBES) {
                const cfg = configs.find((c) => c.name === probeName);
                expect(
                    cfg,
                    `Probe config "${probeName}" should exist`,
                ).toBeTruthy();
                await api.updateProbeConfig(adminCookie, cfg!.id, {
                    is_enabled: true,
                    collection_interval_seconds: FAST_INTERVAL_SECONDS,
                });
            }
        });

        await test.step('Create marker table and generate a known workload', async () => {
            // Ensure the monitoring extensions and the per-run marker
            // table exist on the primary. This runs as the postgres
            // superuser (docker exec psql), because the MCP query_database
            // tool executes SQL in a read-only transaction and cannot run
            // DDL such as CREATE EXTENSION / CREATE TABLE. The extensions
            // are also created by start-replication.sh at cluster startup;
            // recreating them here (IF NOT EXISTS) keeps the spec
            // self-sufficient if the cluster was set up by another path.
            await store.execPrimary(
                'CREATE EXTENSION IF NOT EXISTS pg_stat_statements; ' +
                'CREATE EXTENSION IF NOT EXISTS system_stats; ' +
                `DROP TABLE IF EXISTS ${MARKER}; ` +
                `CREATE TABLE ${MARKER} (n int); ` +
                `INSERT INTO ${MARKER} VALUES (1); ` +
                // Reset pg_stat_statements AFTER the DDL so the marker
                // table name only survives in the workload SELECT below.
                // Otherwise the DROP/CREATE/INSERT statements (each with
                // calls=1) also contain the table name and the
                // marker-row lookup could pick one of those instead of
                // the SELECT. Reset runs here as the superuser; the MCP
                // query_database tool cannot (read-only transaction).
                'SELECT pg_stat_statements_reset();',
            );

            // Drive the workload through the app's query_database path
            // (a plain SELECT is permitted by the read-only transaction),
            // so it is recorded and collected exactly like real traffic.
            for (let i = 0; i < MARKER_RUNS; i++) {
                await api.queryDatabase(
                    adminCookie,
                    primaryConnId,
                    MARKER_QUERY,
                );
            }
        });

        await test.step('Restart the collector to pick up the new config', async () => {
            await replication.restartCollector([PRIMARY_SERVER.name]);
        });

        // ----------------------------------------------------------
        // Wait for a real collection cycle to store the data
        // ----------------------------------------------------------
        await test.step('Wait for the marker query to be collected and stored', async () => {
            await expect(async () => {
                const res = await api.getTopQueries(adminCookie, {
                    connectionId: primaryConnId,
                    limit: 100,
                });
                const row = res.rows.find((r) => r.query.includes(MARKER));
                expect(
                    row,
                    'Marker query should appear in stored top-queries',
                ).toBeTruthy();
                expect(
                    row!.calls,
                    'Marker call count should equal the number of executions',
                ).toBe(MARKER_RUNS);
                expect(row!.database_name).toBe(PRIMARY_SERVER.db);
            }).toPass({ timeout: 150_000, intervals: [5_000] });
        });

        await test.step('Wait for system_stats samples to be collected and stored', async () => {
            // The server-scoped pg_sys_* probes have no dimension columns,
            // so /api/v1/metrics/latest cannot serve them; confirm the
            // collector has stored samples by reading the metrics tables
            // directly (the same ground truth the comparisons use).
            await expect(async () => {
                for (const table of [
                    'pg_sys_cpu_usage_info',
                    'pg_sys_memory_info',
                    'pg_sys_load_avg_info',
                ]) {
                    const count = await store.queryScalar(
                        `SELECT count(*) FROM metrics.${table} ` +
                        `WHERE connection_id = ${primaryConnId}`,
                    );
                    expect(
                        Number.parseInt(count ?? '0', 10),
                        `A stored sample should exist for ${table}`,
                    ).toBeGreaterThan(0);
                }
            }).toPass({ timeout: 180_000, intervals: [5_000] });
        });

        // ----------------------------------------------------------
        // TOP QUERIES: GUI vs API vs stored table
        // ----------------------------------------------------------
        await test.step('Verify Top Queries GUI matches the API and the stored table', async () => {
            await selectPrimaryServer(page, monitoring, serverNav);
            await monitoring.expandSection('Monitoring');
            await monitoring.expandSection('Top Queries');
            await monitoring.waitForTopQueriesLoad();

            // Read the GUI row (poll: the marker sorts to the top by
            // total_exec_time thanks to pg_sleep, so it lands on page 1).
            let guiRow!: Awaited<
                ReturnType<MonitoringPage['readTopQueryRow']>
            >;
            await expect(async () => {
                const row = monitoring.getTopQueryRowByText(MARKER).first();
                await expect(
                    row,
                    'Marker query row should be visible in the GUI',
                ).toBeVisible({ timeout: 5_000 });
                guiRow = await monitoring.readTopQueryRow(row);
                expect(guiRow.query).toContain(MARKER);
            }).toPass({ timeout: 60_000, intervals: [3_000] });

            // API row (exclude_collector mirrors the GUI's default toggle).
            const apiRes = await api.getTopQueries(adminCookie, {
                connectionId: primaryConnId,
                limit: 100,
                excludeCollector: true,
            });
            const apiRow = apiRes.rows.find((r) => r.query.includes(MARKER));
            expect(
                apiRow,
                'Marker query should be present in the top-queries API',
            ).toBeTruthy();

            // Stored ground truth: the marker's calls at the latest
            // collection snapshot for this connection.
            const storedCallsRaw = await store.queryScalar(
                'SELECT calls FROM metrics.pg_stat_statements ' +
                `WHERE connection_id = ${primaryConnId} ` +
                `AND query LIKE '%${MARKER}%' ` +
                'AND collected_at = (SELECT MAX(collected_at) ' +
                'FROM metrics.pg_stat_statements ' +
                `WHERE connection_id = ${primaryConnId})`,
            );
            expect(
                storedCallsRaw,
                'Marker row should exist in metrics.pg_stat_statements',
            ).not.toBeNull();
            const storedCalls = Number.parseInt(storedCallsRaw!, 10);

            // Assertions: GUI == API == stored, and all equal MARKER_RUNS.
            expect(guiRow.database).toBe(PRIMARY_SERVER.db);
            expect(guiRow.query).toContain(MARKER);
            expect(guiRow.calls).toBe(apiRow!.calls);
            expect(apiRow!.calls).toBe(storedCalls);
            expect(guiRow.calls).toBe(MARKER_RUNS);
        });

        // ----------------------------------------------------------
        // SYSTEM STATS: GUI KPI tiles vs served vs stored
        // ----------------------------------------------------------
        await test.step('Verify System Resources KPI tiles match stored metrics', async () => {
            await monitoring.expandSection('Monitoring');
            await monitoring.expandSection('System Resources');

            // Wait until the Memory tile has resolved to a real value.
            await expect(async () => {
                const mem = await monitoring.readKpiTile('Memory Usage');
                expect(
                    mem.value,
                    'Memory Usage tile should show a numeric value',
                ).not.toBeNull();
            }).toPass({ timeout: 60_000, intervals: [3_000] });

            const memTile = await monitoring.readKpiTile('Memory Usage');
            const diskTile = await monitoring.readKpiTile('Disk Usage');
            const loadTile = await monitoring.readKpiTile('Load Average');
            const cpuTile = await monitoring.readKpiTile('CPU Usage');

            // --- Memory: used/total * 100 (%) --------------------------
            await test.step('Memory tile reflects used/total from stored metrics', async () => {
                expect(memTile.value).not.toBeNull();
                expect(memTile.unit).toBe('%');

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_memory_info',
                    connectionId: primaryConnId,
                    metrics: ['used_memory', 'total_memory'],
                    timeRange: '1h',
                    buckets: 30,
                    aggregation: 'avg',
                });
                const usedServed = latestSeriesValue(series, 'used_memory');
                const totalServed = latestSeriesValue(series, 'total_memory');
                expect(usedServed).not.toBeNull();
                expect(totalServed).not.toBeNull();
                expect(totalServed!).toBeGreaterThan(0);
                const servedPct = (usedServed! / totalServed!) * 100;

                const stored = await store.query(
                    'SELECT used_memory, total_memory ' +
                    'FROM metrics.pg_sys_memory_info ' +
                    `WHERE connection_id = ${primaryConnId} ` +
                    'ORDER BY collected_at DESC LIMIT 1',
                );
                expect(stored.length).toBeGreaterThan(0);
                const storedPct =
                    (toNumber(stored[0][0]) / toNumber(stored[0][1])) * 100;

                // GUI faithfully renders the served series.
                expect(
                    Math.abs(memTile.value! - servedPct),
                    'Memory tile should match the served metrics series',
                ).toBeLessThanOrEqual(TOL.memGuiVsServedPct);
                // GUI value is grounded in the stored metric.
                expect(
                    Math.abs(memTile.value! - storedPct),
                    'Memory tile should reflect the stored used/total ratio',
                ).toBeLessThanOrEqual(TOL.memGuiVsStoredPct);
            });

            // --- Disk: used/(used+free) * 100 (%) ----------------------
            // The GUI aggregates all mount points via /metrics/query; a
            // single stored row is ambiguous for multi-mount hosts, so the
            // disk check compares the tile against that same served series
            // (still a numeric assertion on the displayed value).
            await test.step('Disk tile reflects the served disk-usage series', async () => {
                expect(diskTile.value).not.toBeNull();
                expect(diskTile.unit).toBe('%');
                expect(diskTile.value!).toBeGreaterThanOrEqual(0);
                expect(diskTile.value!).toBeLessThanOrEqual(100);

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_disk_info',
                    connectionId: primaryConnId,
                    metrics: ['used_space', 'free_space'],
                    timeRange: '1h',
                    buckets: 30,
                    aggregation: 'avg',
                });
                const usedServed = latestSeriesValue(series, 'used_space');
                const freeServed = latestSeriesValue(series, 'free_space');
                expect(usedServed).not.toBeNull();
                expect(freeServed).not.toBeNull();
                const total = usedServed! + freeServed!;
                expect(total).toBeGreaterThan(0);
                const servedPct = (usedServed! / total) * 100;

                expect(
                    Math.abs(diskTile.value! - servedPct),
                    'Disk tile should match the served disk-usage series',
                ).toBeLessThanOrEqual(TOL.diskGuiVsServedPct);
            });

            // --- Load average (1 minute), unitless --------------------
            await test.step('Load Average tile reflects stored load metric', async () => {
                expect(loadTile.value).not.toBeNull();
                expect(loadTile.unit).toBeNull();

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_load_avg_info',
                    connectionId: primaryConnId,
                    metrics: ['load_avg_one_minute'],
                    timeRange: '1h',
                    buckets: 30,
                    aggregation: 'avg',
                });
                const served =
                    latestSeriesValue(series, 'load_avg_one_minute') ?? 0;

                const storedLoad = toNumber(
                    (await store.queryScalar(
                        'SELECT load_avg_one_minute ' +
                        'FROM metrics.pg_sys_load_avg_info ' +
                        `WHERE connection_id = ${primaryConnId} ` +
                        'ORDER BY collected_at DESC LIMIT 1',
                    )) ?? 'NaN',
                );

                expect(
                    Math.abs(loadTile.value! - served),
                    'Load tile should match the served load series',
                ).toBeLessThanOrEqual(TOL.loadGuiVsServed);
                expect(
                    Math.abs(loadTile.value! - storedLoad),
                    'Load tile should reflect the stored load metric',
                ).toBeLessThanOrEqual(TOL.loadGuiVsStored);
            });

            // --- CPU usage: 100 - idle (%) ----------------------------
            await test.step('CPU tile reflects 100 - idle from stored metrics', async () => {
                expect(cpuTile.value).not.toBeNull();
                expect(cpuTile.unit).toBe('%');
                expect(cpuTile.value!).toBeGreaterThanOrEqual(0);
                expect(cpuTile.value!).toBeLessThanOrEqual(100);

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_cpu_usage_info',
                    connectionId: primaryConnId,
                    metrics: [
                        'usermode_normal_process_percent',
                        'kernelmode_process_percent',
                        'idle_mode_percent',
                    ],
                    timeRange: '1h',
                    buckets: 30,
                    aggregation: 'avg',
                });
                const idleServed = latestSeriesValue(
                    series,
                    'idle_mode_percent',
                );
                expect(idleServed).not.toBeNull();
                const servedPct = 100 - idleServed!;

                const idleStored = toNumber(
                    (await store.queryScalar(
                        'SELECT idle_mode_percent ' +
                        'FROM metrics.pg_sys_cpu_usage_info ' +
                        `WHERE connection_id = ${primaryConnId} ` +
                        'ORDER BY collected_at DESC LIMIT 1',
                    )) ?? 'NaN',
                );

                // GUI reflects the served series (generous: CPU is volatile).
                expect(
                    Math.abs(cpuTile.value! - servedPct),
                    'CPU tile should track the served CPU series',
                ).toBeLessThanOrEqual(TOL.cpuGuiVsServedPct);
                // GUI value is grounded in the stored CPU metric. CPU is
                // volatile and the tile averages the window while the
                // stored row is a single sample, so the bound is generous.
                expect(
                    Math.abs(cpuTile.value! - (100 - idleStored)),
                    'CPU tile should reflect 100 - stored idle',
                ).toBeLessThanOrEqual(TOL.cpuGuiVsServedPct);
            });
        });
    });
});
