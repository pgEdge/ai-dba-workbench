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
import { SpockReplicationHelper } from '../../helpers/spock-replication.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import type { AnalysisDialog } from '../../pages/AnalysisDialog';
import {
    ensureSpockClusterAndNodes,
    primeSpockNodeMetrics,
    SPOCK_CLUSTER_NAME,
    SPOCK_NODE1,
} from '../../helpers/spock-cluster-setup.helper';
import { skipIfAiProviderLimited } from '../../helpers/ai.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * Spock-cluster mirror of cluster-monitoring-ai: the server, per-metric,
 * and cluster AI Overview analyses run against the spock cluster and its
 * primary node. AI text is non-deterministic, so assertions are structural;
 * every AI-generated SQL block must execute cleanly (strict, by request).
 */
async function assertAnalysisIsMeaningful(
    dialog: AnalysisDialog,
    expectContains?: string,
): Promise<void> {
    await dialog.waitForReady();
    // A provider usage/rate limit is environmental — skip rather than fail.
    // Any other error still trips the hasError assertion below.
    await skipIfAiProviderLimited(dialog);
    expect(
        await dialog.hasError(),
        'analysis should not report failure',
    ).toBe(false);
    const text = await dialog.text();
    expect(
        text.trim().length,
        'analysis should not be empty',
    ).toBeGreaterThan(40);
    if (expectContains) {
        expect(
            text.toLowerCase(),
            `analysis should reference "${expectContains}"`,
        ).toContain(expectContains.toLowerCase());
    }
    // Strict (by request): every generated SQL block must execute cleanly.
    // Non-SQL blocks and unfillable $1 templates are excluded by the helper;
    // this WILL go red if the model emits a genuinely broken query.
    const { total, errors } = await dialog.runAllGeneratedQueries(30_000);
    expect(
        errors,
        `all ${total} AI-generated queries should execute without error`,
    ).toEqual([]);
}

test.describe('Cluster & Server AI Overview (Spock)', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const spock = new SpockReplicationHelper();

    let adminCookie: string;
    let nodeConnId: number;

    test.beforeEach(async () => {
        await label('package', 'Monitoring (Spock)');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await spock.start();
        adminCookie = await auth.loginAsAdmin();

        const caps = await api.getCapabilities(adminCookie);
        expect(
            caps.ai_enabled,
            'AI must be enabled (E2E_AI_ENABLED=true + LLM key) for the AI ' +
            'Overview suite',
        ).toBe(true);
    });

    test.beforeEach(async ({ page }) => {
        test.setTimeout(180_000);
        await ensureSpockClusterAndNodes(page, api, adminCookie);
        const connections = await api.listConnections({ cookie: adminCookie });
        const node = connections.find((c) => c.name === SPOCK_NODE1.name);
        if (!node) {
            throw new Error(`No connection named "${SPOCK_NODE1.name}" found`);
        }
        nodeConnId = node.id;
    });

    test.afterAll(async () => {
        await spock.stop().catch(() => undefined);
    });

    // ------------------------------------------------------------------
    // Navigation helpers
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

    async function selectClusterNode(page: Page): Promise<void> {
        await page.goto('/');
        // The cluster-header name click fires onSelectCluster, switching the
        // StatusPanel to a ClusterSelection. A prior test in this describe
        // leaves the primary node SERVER selected, and the server view also
        // renders an "AI Overview" panel — so guarding on "AI Overview" would
        // pass even when the click failed to switch scope, opening a per-server
        // "Server analysis" instead of the "Cluster analysis" this test wants.
        // Guard instead on the "Topology" CollapsibleSection, which StatusPanel
        // renders ONLY for a cluster selection (never for a server or estate),
        // keeping the assertion inside toPass so the click retries until the
        // selection genuinely becomes the cluster.
        await expect(async () => {
            await page
                .locator('.draggable-cluster')
                .filter({ hasText: SPOCK_CLUSTER_NAME })
                .first()
                .locator('.cluster-header')
                .getByText(SPOCK_CLUSTER_NAME, { exact: false })
                .first()
                .click({ force: true });
            await expect(
                page.getByRole('button', { name: /Topology section/i }),
                'cluster-only Topology section should render for the cluster',
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 60_000, intervals: [3_000] });
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('server AI Overview generates a meaningful analysis', async ({
        page,
    }) => {
        test.setTimeout(480_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await selectSpockNode(page, monitoring, serverNav);

        await test.step('AI Overview panel renders and produces a summary', async () => {
            await monitoring.expandAIOverview();
            await expect(
                page.getByText('AI Overview', { exact: true }),
                'AI Overview panel is present',
            ).toBeVisible({ timeout: 15_000 });
        });

        await test.step('Run full analysis → Server analysis dialog', async () => {
            const dialog = await monitoring.openFullAnalysis(false);
            await assertAnalysisIsMeaningful(dialog, SPOCK_NODE1.name);
            await dialog.close();
        });
    });

    test('per-metric AI (brain) opens a Chart analysis for the metric', async ({
        page,
    }) => {
        test.setTimeout(600_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await test.step('Prime fresh system_stats samples', async () => {
            await primeSpockNodeMetrics(api, adminCookie, nodeConnId);
        });

        await selectSpockNode(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.expandSection('System Resources');

        await expect(async () => {
            const mem = await monitoring.readKpiTile('Memory Usage');
            expect(
                mem.value,
                'Memory Usage tile has a numeric value',
            ).not.toBeNull();
        }).toPass({ timeout: 90_000, intervals: [3_000] });

        await test.step('Open the brain analysis on the Memory Usage tile', async () => {
            const dialog = await monitoring.openMetricAnalysis('Memory Usage');
            await assertAnalysisIsMeaningful(dialog, 'memory');
            await dialog.close();
        });
    });

    test('cluster monitoring metrics render and cluster AI Overview works', async ({
        page,
    }) => {
        test.setTimeout(480_000);
        const monitoring = new MonitoringPage(page);

        await test.step('Select the cluster node and verify metrics render', async () => {
            await selectClusterNode(page);
            await expect(async () => {
                const present =
                    (await monitoring.chartPanelVisible('Connection Count')) ||
                    (await monitoring.chartPanelVisible('Transaction Rate')) ||
                    (await monitoring.chartPanelVisible('Cache Hit Ratio'));
                expect(
                    present,
                    'a cluster comparative chart is visible',
                ).toBe(true);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });

        await test.step('Cluster AI Overview → Cluster analysis dialog', async () => {
            await monitoring.expandAIOverview().catch(() => undefined);
            const dialog = await monitoring.openFullAnalysis(true);
            await assertAnalysisIsMeaningful(
                dialog,
                SPOCK_CLUSTER_NAME.split(' ')[0],
            );
            await dialog.close();
        });
    });
});
