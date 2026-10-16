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
import { ApiHelper } from '../../helpers/api.helper';
import { AuthHelper } from '../../helpers/auth.helper';
import { ReplicationHelper } from '../../helpers/replication.helper';
import { SpockReplicationHelper } from '../../helpers/spock-replication.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import {
    ensureSpockClusterAndNodes,
    primeSpockNodeMetrics,
    SPOCK_CLUSTER_NAME,
    SPOCK_NODE1,
} from '../../helpers/spock-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * Spock-cluster mirror of metric-filters-switches-graphs: exercises the
 * monitoring metrics UI (Top Queries "hide monitoring queries" switch, time
 * range presets, section collapse persistence, and a System Resources graph
 * cross-checked against the backend) against the primary spock node.
 */
test.describe('Monitoring Filters, Switches & Graphs (Spock)', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const replication = new ReplicationHelper();
    const spock = new SpockReplicationHelper();

    let adminCookie: string;
    let nodeConnId: number;

    test.beforeEach(async () => {
        await label('package', 'Monitoring (Spock)');
    });

    test.beforeAll(async () => {
        test.setTimeout(300_000);
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

    /**
     * Lower the pg_stat_statements collection interval, drive a workload,
     * restart the collector, and wait until the top-queries store has data.
     */
    async function ensureTopQueriesData(): Promise<void> {
        await api
            .queryDatabase(
                adminCookie,
                nodeConnId,
                'SELECT count(*) FROM documents',
            )
            .catch(() => undefined);

        const configs = await api.getProbeConfigs(adminCookie);
        const pss = configs.find((c) => c.name === 'pg_stat_statements');
        if (pss) {
            await api.updateProbeConfig(adminCookie, pss.id, {
                is_enabled: true,
                collection_interval_seconds: 5,
            });
        }
        for (let i = 0; i < 8; i++) {
            await api
                .queryDatabase(
                    adminCookie,
                    nodeConnId,
                    'SELECT pg_sleep(0.02), count(*) FROM documents',
                )
                .catch(() => undefined);
        }
        await replication.restartCollector([SPOCK_NODE1.name]);

        await expect(async () => {
            const res = await api.getTopQueries(adminCookie, {
                connectionId: nodeConnId,
                limit: 50,
                excludeCollector: false,
            });
            expect(
                res.rows.length,
                'top-queries data should be collected',
            ).toBeGreaterThan(0);
        }).toPass({ timeout: 150_000, intervals: [5_000] });
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('"Hide monitoring queries" switch toggles and respects the backend filter', async ({
        page,
    }) => {
        test.setTimeout(360_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await ensureTopQueriesData();
        await selectSpockNode(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.expandSection('Top Queries');
        await monitoring.waitForTopQueriesLoad();

        await test.step('Backend: excluding collector queries yields <= all queries', async () => {
            const all = await api.getTopQueries(adminCookie, {
                connectionId: nodeConnId,
                limit: 100,
                excludeCollector: false,
            });
            const excluded = await api.getTopQueries(adminCookie, {
                connectionId: nodeConnId,
                limit: 100,
                excludeCollector: true,
            });
            expect(
                excluded.rows.length,
                'excluding collector queries returns no more rows',
            ).toBeLessThanOrEqual(all.rows.length);
        });

        await test.step('Toggling the switch flips its state and respects hide semantics', async () => {
            const hiddenInitially = await monitoring.isMonitoringQueriesHidden();
            const countRows = async (): Promise<number> => {
                await monitoring.waitForTopQueriesLoad();
                return page
                    .getByRole('button', { name: /view details for query/i })
                    .count();
            };
            const before = await countRows();
            await monitoring.clickHideMonitoringQueriesSwitch();
            await expect(async () => {
                expect(
                    await monitoring.isMonitoringQueriesHidden(),
                    'switch state flips after click',
                ).toBe(!hiddenInitially);
            }).toPass({ timeout: 10_000, intervals: [1_000] });
            const after = await countRows();
            if (await monitoring.isMonitoringQueriesHidden()) {
                expect(
                    after,
                    'hiding does not grow the set',
                ).toBeLessThanOrEqual(before);
            } else {
                expect(
                    after,
                    'showing does not shrink the set',
                ).toBeGreaterThanOrEqual(before);
            }
        });

        await test.step('Toggling back returns the switch to its original state', async () => {
            const before = await monitoring.isMonitoringQueriesHidden();
            await monitoring.clickHideMonitoringQueriesSwitch();
            await expect(async () => {
                expect(
                    await monitoring.isMonitoringQueriesHidden(),
                    'switch flips back on a second click',
                ).toBe(!before);
            }).toPass({ timeout: 10_000, intervals: [1_000] });
            if (!(await monitoring.isMonitoringQueriesHidden())) {
                await monitoring.clickHideMonitoringQueriesSwitch();
            }
        });
    });

    test('time-range presets keep the dashboard rendering; custom pauses auto-refresh', async ({
        page,
    }) => {
        test.setTimeout(240_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await selectSpockNode(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');

        await test.step('Selecting presets re-renders without breaking the dashboard', async () => {
            for (const preset of ['1h', '24h', '7d']) {
                await monitoring.selectTimeRange(preset);
                await expect(
                    monitoring.getSectionHeader('Monitoring'),
                    `dashboard still present on ${preset}`,
                ).toBeVisible({ timeout: 10_000 });
                expect(
                    await monitoring.isAutoRefreshPaused(),
                    `presets do not pause auto-refresh (${preset})`,
                ).toBe(false);
            }
        });

        await test.step('A custom range pauses auto-refresh', async () => {
            await monitoring.selectTimeRange('Custom').catch(() => undefined);
            const apply = page.getByRole('button', { name: /^apply$/i });
            const applyReady = await apply
                .isEnabled({ timeout: 3_000 })
                .catch(() => false);
            if (applyReady) {
                await apply.click();
                await expect(async () => {
                    expect(await monitoring.isAutoRefreshPaused()).toBe(true);
                }).toPass({ timeout: 10_000, intervals: [1_000] });
                await monitoring.selectTimeRange('24h');
                expect(await monitoring.isAutoRefreshPaused()).toBe(false);
            }
        });
    });

    test('a section collapse persists across reload', async ({ page }) => {
        test.setTimeout(180_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await selectSpockNode(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.collapseSection('Top Queries');
        await page.reload();
        await selectSpockNode(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.expectSectionCollapsed('Top Queries');
        await monitoring.expandSection('Top Queries');
    });

    test('a monitoring graph renders and its series is populated from the backend', async ({
        page,
    }) => {
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // Prime pg_sys samples for the freshly-added node so the collector has
        // CPU data to graph and to back the /metrics/query series.
        await primeSpockNodeMetrics(api, adminCookie, nodeConnId);

        await selectSpockNode(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.expandSection('System Resources');

        await test.step('The CPU chart panel is visible (canvas — structure only)', async () => {
            await expect(async () => {
                expect(
                    await monitoring.chartPanelVisible('CPU Usage Over Time'),
                    'CPU chart panel should render',
                ).toBe(true);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });

        await test.step('The backing /metrics/query series has data points', async () => {
            await expect(async () => {
                const series = await api.getMetricsQuery(adminCookie, {
                    probeName: 'pg_sys_cpu_usage_info',
                    connectionId: nodeConnId,
                    metrics: ['idle_mode_percent'],
                    timeRange: '1h',
                    buckets: 30,
                    aggregation: 'avg',
                });
                const idle = series.find(
                    (s) => s.metric === 'idle_mode_percent',
                );
                expect(idle, 'idle_mode_percent series exists').toBeTruthy();
                expect(
                    idle!.data.some((d) => d.value !== 0),
                    'the CPU series has non-zero data points',
                ).toBe(true);
            }).toPass({ timeout: 120_000, intervals: [5_000] });
        });
    });
});
