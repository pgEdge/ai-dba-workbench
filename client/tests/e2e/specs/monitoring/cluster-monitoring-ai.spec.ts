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
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import type { AnalysisDialog } from '../../pages/AnalysisDialog';
import {
    ensureClusterAndPrimary,
    primeSystemMetrics,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { skipIfAiProviderLimited } from '../../helpers/ai.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * Structural assertion of an AI analysis dialog: it finished (no error),
 * produced non-trivial content, and — when the AI emitted a runnable SQL
 * block — that query executes without error. AI text is non-deterministic,
 * so assertions are structural/semantic, never exact string matches.
 */
async function assertAnalysisIsMeaningful(
    dialog: AnalysisDialog,
    expectContains?: string,
): Promise<void> {
    await dialog.waitForReady();
    // A provider usage/rate limit is environmental — skip rather than fail.
    // Any other error still trips the hasError assertion below.
    await skipIfAiProviderLimited(dialog);
    expect(await dialog.hasError(), 'analysis should not report failure').toBe(false);
    const text = await dialog.text();
    expect(text.trim().length, 'analysis should not be empty').toBeGreaterThan(40);
    if (expectContains) {
        expect(
            text.toLowerCase(),
            `analysis should reference "${expectContains}"`,
        ).toContain(expectContains.toLowerCase());
    }
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

test.describe('Cluster & Server AI Overview', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const replication = new ReplicationHelper();

    let adminCookie: string;
    let primaryConnId: number;

    test.beforeEach(async () => {
        await label('package', 'Monitoring');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();

        // Locked decision: this suite requires AI enabled — never skip.
        const caps = await api.getCapabilities(adminCookie);
        expect(
            caps.ai_enabled,
            'AI must be enabled (E2E_AI_ENABLED=true + LLM key) for the AI Overview suite',
        ).toBe(true);
    });

    test.beforeEach(async ({ page }) => {
        await ensureClusterAndPrimary(page, api, adminCookie);
        const connections = await api.listConnections({ cookie: adminCookie });
        const primary = connections.find((c) => /primary/i.test(c.name));
        if (!primary) {
            throw new Error('No connection matching /primary/i found');
        }
        primaryConnId = primary.id;
    });

    test.afterAll(async () => {
        await replication.stop().catch(() => undefined);
    });

    // ------------------------------------------------------------------
    // Navigation helpers
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
     * Best-effort selection of the top-level cluster node. The cluster
     * header name triggers onSelectCluster; the chevron only toggles
     * expansion. This selector is flagged for live-DOM verification.
     */
    async function selectClusterNode(page: Page): Promise<void> {
        await page.goto('/');
        await expect(async () => {
            await page
                .locator('.draggable-cluster')
                .filter({ hasText: CLUSTER_NAME })
                .first()
                .locator('.cluster-header')
                .getByText(CLUSTER_NAME, { exact: false })
                .first()
                .click({ force: true });
            await expect(
                page.getByText('AI Overview', { exact: true }),
                'AI Overview panel should render for the cluster',
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 60_000, intervals: [3_000] });
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    test('server AI Overview generates a meaningful analysis', async ({ page }) => {
        test.setTimeout(480_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await selectPrimaryServer(page, monitoring, serverNav);

        await test.step('AI Overview panel renders and produces a summary', async () => {
            await monitoring.expandAIOverview();
            // The panel eventually shows a non-empty summary (SSE or poll).
            await expect(
                page.getByText('AI Overview', { exact: true }),
                'AI Overview panel is present',
            ).toBeVisible({ timeout: 15_000 });
        });

        await test.step('Run full analysis → Server analysis dialog', async () => {
            const dialog = await monitoring.openFullAnalysis(false);
            await assertAnalysisIsMeaningful(dialog, PRIMARY_SERVER.name);
            await dialog.close();
        });
    });

    test('per-metric AI (brain) opens a Chart analysis for the metric', async ({
        page,
    }) => {
        test.setTimeout(600_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // The System Resources tiles are backed by the pg_sys_* probes,
        // whose default 600s interval leaves the newest sample stale (or
        // absent) on a warm stack. Prime fresh samples first so the Memory
        // Usage tile resolves to a real value instead of "no data".
        await test.step('Prime fresh system_stats samples', async () => {
            await primeSystemMetrics(api, adminCookie, primaryConnId);
        });

        await selectPrimaryServer(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring');
        await monitoring.expandSection('System Resources');

        // Ensure the KPI tiles have resolved to real values first.
        await expect(async () => {
            const mem = await monitoring.readKpiTile('Memory Usage');
            expect(mem.value, 'Memory Usage tile has a numeric value').not.toBeNull();
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
            // The cluster dashboard renders comparative charts; assert at
            // least one known chart panel is present (canvas — structure
            // only). Titles from ComparativeChartsSection.
            await expect(async () => {
                const present =
                    (await monitoring.chartPanelVisible('Connection Count')) ||
                    (await monitoring.chartPanelVisible('Transaction Rate')) ||
                    (await monitoring.chartPanelVisible('Cache Hit Ratio'));
                expect(present, 'a cluster comparative chart is visible').toBe(true);
            }).toPass({ timeout: 30_000, intervals: [3_000] });
        });

        await test.step('Cluster AI Overview → Cluster analysis dialog', async () => {
            await monitoring.expandAIOverview().catch(() => undefined);
            const dialog = await monitoring.openFullAnalysis(true);
            await assertAnalysisIsMeaningful(dialog, CLUSTER_NAME.split(' ')[0]);
            await dialog.close();
        });
    });
});
