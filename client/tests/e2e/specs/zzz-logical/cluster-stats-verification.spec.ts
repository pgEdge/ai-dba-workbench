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
import { LogicalReplicationHelper } from '../../helpers/logical-replication.helper';
import { StoreHelper } from '../../helpers/store.helper';
import {
    ensureLogicalClusterAndNodes,
    LOGICAL_CLUSTER_NAME,
    LOGICAL_NODE1,
} from '../../helpers/logical-cluster-setup.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { API_URL } from '../../fixtures/test-data';

// ---------------------------------------------------------------
// Constants
// ---------------------------------------------------------------

const TARGET_PROBES = [
    'pg_stat_statements',
    'pg_sys_cpu_usage_info',
    'pg_sys_memory_info',
    'pg_sys_load_avg_info',
    'pg_sys_disk_info',
];

const FAST_INTERVAL_SECONDS = 5;
const MARKER_RUNS = 12;

const TOL = {
    memGuiVsServedPct: 2.0,
    memGuiVsStoredPct: 3.0,
    diskGuiVsServedPct: 2.0,
    loadGuiVsServed: 0.5,
    loadGuiVsStored: 1.0,
    cpuGuiVsServedPct: 20.0,
};

// ---------------------------------------------------------------
// Local helpers
// ---------------------------------------------------------------

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

function toNumber(value: unknown): number {
    const n = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(n)) {
        throw new Error(`Expected numeric value, got: ${String(value)}`);
    }
    return n;
}

async function selectPublisherServer(
    page: Page,
    monitoring: MonitoringPage,
    serverNav: ServerNavigatorPage,
): Promise<void> {
    await page.goto('/');
    await monitoring.waitForAppLoad();

    await expect(async () => {
        const nodeRow = serverNav.getServerRow(LOGICAL_NODE1.name).first();
        const rowVisible = await nodeRow.isVisible().catch(() => false);
        if (!rowVisible) {
            await serverNav.expandCluster(LOGICAL_CLUSTER_NAME);
        }
        await nodeRow.click({ force: true });
        await expect(
            monitoring.getSectionHeader('Monitoring'),
            'Monitoring section should appear after server selection',
        ).toBeVisible({ timeout: 5_000 });
    }).toPass({ timeout: 60_000, intervals: [3_000] });
}

// ---------------------------------------------------------------
// Cluster Stats Verification (Logical)
// ---------------------------------------------------------------

test.describe('Cluster Stats Verification (Logical)', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const store = new StoreHelper();
    const replication = new ReplicationHelper();
    const logical = new LogicalReplicationHelper();

    // Per-run marker table (see the physical spec for the rationale).
    const MARKER = `e2emk_${randomBytes(4).toString('hex')}`;
    const MARKER_QUERY = `SELECT count(*) FROM ${MARKER}`;

    let adminCookie: string;
    let nodeConnId: number;

    test.beforeEach(async () => {
        await label('package', 'Monitoring (Logical)');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await logical.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.afterAll(async () => {
        await logical.stop().catch(() => undefined);
    });

    test('enables Top Queries and System Stats, then verifies the GUI against stored metrics', async ({
        page,
    }) => {
        test.setTimeout(420_000);

        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await test.step('Ensure the logical cluster and its nodes are registered', async () => {
            await ensureLogicalClusterAndNodes(page, api, adminCookie);
        });

        await test.step('Resolve the publisher node connection id', async () => {
            const connections = await api.listConnections({
                cookie: adminCookie,
            });
            const node = connections.find(
                (c) => c.name === LOGICAL_NODE1.name,
            );
            if (!node) {
                throw new Error(
                    `No connection named "${LOGICAL_NODE1.name}" found. ` +
                    `Available: ${connections.map((c) => c.name).join(', ')}`,
                );
            }
            nodeConnId = node.id;
        });

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
            // DDL on the PUBLISHER node as superuser (the MCP query_database
            // tool runs read-only and cannot run DDL / reset).
            await logical.execNode1(
                'CREATE EXTENSION IF NOT EXISTS pg_stat_statements; ' +
                'CREATE EXTENSION IF NOT EXISTS system_stats; ' +
                `DROP TABLE IF EXISTS ${MARKER}; ` +
                `CREATE TABLE ${MARKER} (n int); ` +
                `INSERT INTO ${MARKER} VALUES (1); ` +
                'SELECT pg_stat_statements_reset();',
            );

            for (let i = 0; i < MARKER_RUNS; i++) {
                await api.queryDatabase(
                    adminCookie,
                    nodeConnId,
                    MARKER_QUERY,
                );
            }
        });

        await test.step('Restart the collector to pick up the new config', async () => {
            await replication.restartCollector([LOGICAL_NODE1.name]);
        });

        await test.step('Wait for the marker query to be collected and stored', async () => {
            await expect(async () => {
                const res = await api.getTopQueries(adminCookie, {
                    connectionId: nodeConnId,
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
                expect(row!.database_name).toBe(LOGICAL_NODE1.db);
            }).toPass({ timeout: 150_000, intervals: [5_000] });
        });

        await test.step('Wait for system_stats samples to be collected and stored', async () => {
            await expect(async () => {
                for (const table of [
                    'pg_sys_cpu_usage_info',
                    'pg_sys_memory_info',
                    'pg_sys_load_avg_info',
                ]) {
                    const count = await store.queryScalar(
                        `SELECT count(*) FROM metrics.${table} ` +
                        `WHERE connection_id = ${nodeConnId}`,
                    );
                    expect(
                        Number.parseInt(count ?? '0', 10),
                        `A stored sample should exist for ${table}`,
                    ).toBeGreaterThan(0);
                }
            }).toPass({ timeout: 180_000, intervals: [5_000] });
        });

        await test.step('Verify Top Queries GUI matches the API and the stored table', async () => {
            await selectPublisherServer(page, monitoring, serverNav);
            await monitoring.expandSection('Monitoring');
            await monitoring.expandSection('Top Queries');
            await monitoring.waitForTopQueriesLoad();

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

            const apiRes = await api.getTopQueries(adminCookie, {
                connectionId: nodeConnId,
                limit: 100,
                excludeCollector: true,
            });
            const apiRow = apiRes.rows.find((r) => r.query.includes(MARKER));
            expect(
                apiRow,
                'Marker query should be present in the top-queries API',
            ).toBeTruthy();

            const storedCallsRaw = await store.queryScalar(
                'SELECT calls FROM metrics.pg_stat_statements ' +
                `WHERE connection_id = ${nodeConnId} ` +
                `AND query LIKE '%${MARKER}%' ` +
                'AND collected_at = (SELECT MAX(collected_at) ' +
                'FROM metrics.pg_stat_statements ' +
                `WHERE connection_id = ${nodeConnId})`,
            );
            expect(
                storedCallsRaw,
                'Marker row should exist in metrics.pg_stat_statements',
            ).not.toBeNull();
            const storedCalls = Number.parseInt(storedCallsRaw!, 10);

            expect(guiRow.database).toBe(LOGICAL_NODE1.db);
            expect(guiRow.query).toContain(MARKER);
            expect(guiRow.calls).toBe(apiRow!.calls);
            expect(apiRow!.calls).toBe(storedCalls);
            expect(guiRow.calls).toBe(MARKER_RUNS);
        });

        await test.step('Verify System Resources KPI tiles match stored metrics', async () => {
            await monitoring.expandSection('Monitoring');
            await monitoring.expandSection('System Resources');

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

            await test.step('Memory tile reflects used/total from stored metrics', async () => {
                expect(memTile.value).not.toBeNull();
                expect(memTile.unit).toBe('%');

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_memory_info',
                    connectionId: nodeConnId,
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
                    `WHERE connection_id = ${nodeConnId} ` +
                    'ORDER BY collected_at DESC LIMIT 1',
                );
                expect(stored.length).toBeGreaterThan(0);
                const storedPct =
                    (toNumber(stored[0][0]) / toNumber(stored[0][1])) * 100;

                expect(
                    Math.abs(memTile.value! - servedPct),
                    'Memory tile should match the served metrics series',
                ).toBeLessThanOrEqual(TOL.memGuiVsServedPct);
                expect(
                    Math.abs(memTile.value! - storedPct),
                    'Memory tile should reflect the stored used/total ratio',
                ).toBeLessThanOrEqual(TOL.memGuiVsStoredPct);
            });

            await test.step('Disk tile reflects the served disk-usage series', async () => {
                expect(diskTile.value).not.toBeNull();
                expect(diskTile.unit).toBe('%');
                expect(diskTile.value!).toBeGreaterThanOrEqual(0);
                expect(diskTile.value!).toBeLessThanOrEqual(100);

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_disk_info',
                    connectionId: nodeConnId,
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

            await test.step('Load Average tile reflects stored load metric', async () => {
                expect(loadTile.value).not.toBeNull();
                expect(loadTile.unit).toBeNull();

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_load_avg_info',
                    connectionId: nodeConnId,
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
                        `WHERE connection_id = ${nodeConnId} ` +
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

            await test.step('CPU tile reflects 100 - idle from stored metrics', async () => {
                expect(cpuTile.value).not.toBeNull();
                expect(cpuTile.unit).toBe('%');
                expect(cpuTile.value!).toBeGreaterThanOrEqual(0);
                expect(cpuTile.value!).toBeLessThanOrEqual(100);

                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_cpu_usage_info',
                    connectionId: nodeConnId,
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
                        `WHERE connection_id = ${nodeConnId} ` +
                        'ORDER BY collected_at DESC LIMIT 1',
                    )) ?? 'NaN',
                );

                expect(
                    Math.abs(cpuTile.value! - servedPct),
                    'CPU tile should track the served CPU series',
                ).toBeLessThanOrEqual(TOL.cpuGuiVsServedPct);
                expect(
                    Math.abs(cpuTile.value! - (100 - idleStored)),
                    'CPU tile should reflect 100 - stored idle',
                ).toBeLessThanOrEqual(TOL.cpuGuiVsServedPct);
            });
        });
    });
});
