/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ApiHelper } from '../helpers/api.helper';
import { AuthHelper } from '../helpers/auth.helper';
import { ServerNavigatorPage } from '../pages/ServerNavigatorPage';
import { MonitoringPage } from '../pages/MonitoringPage';
import { API_URL } from '../fixtures/test-data';

// ---------------------------------------------------------------
// Helper: check whether the server has AI features enabled.
// Uses a one-off admin login to call /api/v1/capabilities so
// the check is independent of any page-level session state.
// ---------------------------------------------------------------
async function checkAiEnabled(
    api: ApiHelper,
    auth: AuthHelper,
): Promise<boolean> {
    try {
        const adminCookie = await auth.loginAsAdmin();
        const result = await api.rawGet('/api/v1/capabilities', {
            Cookie: adminCookie,
        });
        const body = result.body as { ai_enabled?: boolean } | null;
        return body?.ai_enabled === true;
    } catch {
        return false;
    }
}

// ---------------------------------------------------------------
// Server Monitoring Dashboard
// ---------------------------------------------------------------

test.describe('Server Monitoring Dashboard', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);

    let primaryConnId: number;
    let aiEnabled = false;
    let adminCookie: string;

    test.beforeAll(async () => {
        // Step 1: login as admin
        adminCookie = await auth.loginAsAdmin();

        // Step 2: find primary connection
        const connections = await api.listConnections({ cookie: adminCookie });
        const primary = connections.find((c) => /primary/i.test(c.name));
        if (!primary) {
            throw new Error(
                'No connection matching /primary/i found. ' +
                'This spec requires connections set up by replication-cluster.spec.ts. ' +
                `Available: ${connections.map((c) => c.name).join(', ')}`,
            );
        }
        primaryConnId = primary.id;

        // Step 3: enable pg_stat_statements extension
        try {
            await api.rawPost(
                '/api/v1/mcp/tools/call',
                {
                    name: 'query_database',
                    arguments: {
                        connection_id: primaryConnId,
                        query: 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements',
                    },
                },
                { Cookie: adminCookie },
            );
        } catch {
            // Extension may already exist or user lacks permission; ignore.
        }

        // Step 4: reset pg_stat_statements for fresh data
        try {
            await api.rawPost(
                '/api/v1/mcp/tools/call',
                {
                    name: 'query_database',
                    arguments: {
                        connection_id: primaryConnId,
                        query: 'SELECT pg_stat_statements_reset()',
                    },
                },
                { Cookie: adminCookie },
            );
        } catch {
            // Reset may fail if the extension is not available; ignore.
        }

        // Step 5: generate query activity to populate pg_stat_statements
        const workloadQueries = [
            'SELECT count(*) FROM pg_stat_user_tables',
            'SELECT * FROM pg_stat_statements LIMIT 5',
            'SELECT version()',
            'SELECT current_database(), current_user',
        ];

        for (let i = 0; i < 3; i++) {
            for (const query of workloadQueries) {
                try {
                    await api.rawPost(
                        '/api/v1/mcp/tools/call',
                        {
                            name: 'query_database',
                            arguments: {
                                connection_id: primaryConnId,
                                query,
                            },
                        },
                        { Cookie: adminCookie },
                    );
                } catch {
                    // Individual query failures are non-critical; continue.
                }
            }
        }

        // Step 6: check if AI is enabled
        aiEnabled = await checkAiEnabled(api, auth);
        console.log(
            `[server-monitoring] AI enabled: ${aiEnabled}. ` +
            (aiEnabled
                ? 'AI Overview step will run.'
                : 'AI Overview step will be skipped.'),
        );
    });

    test('monitoring dashboard displays system statistics, top queries, and server details', async ({ page }) => {
        test.setTimeout(120_000);
        await label('package', 'Monitoring');

        const serverNav = new ServerNavigatorPage(page);
        const monitoring = new MonitoringPage(page);

        // ----------------------------------------------------------
        // Step 1: Navigate to primary server
        // ----------------------------------------------------------
        await test.step('Navigate to primary server', async () => {
            await page.goto('/');
            await monitoring.waitForAppLoad();

            // Retry the expand-and-select loop. expandCluster() is a
            // toggle, so if a previous spec left the cluster expanded the
            // first call collapses it. On the next retry the row is hidden
            // → we expand again. Also handles Firefox timing where the
            // click lands but React's onClick does not fire on the first
            // attempt, leaving the Monitoring section absent.
            await expect(async () => {
                const primaryRow = serverNav
                    .getServerRow('primary-node')
                    .first();
                const rowVisible = await primaryRow
                    .isVisible()
                    .catch(() => false);
                if (!rowVisible) {
                    await serverNav.expandCluster('Test Binary Cluster');
                }
                await primaryRow.click({ force: true });
                await expect(
                    monitoring.getSectionHeader('Monitoring'),
                    'Monitoring section should appear after server selection',
                ).toBeVisible({ timeout: 5_000 });
            }).toPass({ timeout: 45_000, intervals: [3_000] });
        });

        // ----------------------------------------------------------
        // Step 2: Verify Monitoring section is visible and expanded
        // ----------------------------------------------------------
        await test.step('Verify Monitoring section is visible and expanded', async () => {
            const monitoringHeader = monitoring.getSectionHeader('Monitoring');
            await expect(
                monitoringHeader,
                'Monitoring section header should be visible',
            ).toBeVisible({ timeout: 15_000 });

            // Expand if collapsed
            await monitoring.expandSection('Monitoring');
            await monitoring.expectSectionExpanded('Monitoring');
        });

        // ----------------------------------------------------------
        // Step 3: Validate System Resources section
        // ----------------------------------------------------------
        await test.step('Validate System Resources section expand and collapse', async () => {
            const sysHeader = monitoring.getSectionHeader('System Resources');
            await expect(
                sysHeader,
                'System Resources section header should be visible',
            ).toBeVisible({ timeout: 10_000 });

            // Expand if not already expanded
            await monitoring.expandSection('System Resources');
            await monitoring.expectSectionExpanded('System Resources');

            // Collapse it
            await monitoring.collapseSection('System Resources');
            await monitoring.expectSectionCollapsed('System Resources');

            // Re-expand it
            await monitoring.expandSection('System Resources');
            await monitoring.expectSectionExpanded('System Resources');

            // Verify all four KPI tiles are visible after expand.
            // KpiTile always sets aria-label="${label}: ${value}[unit]" on
            // its Paper element — regardless of whether system_stats data
            // exists and regardless of role. Use getByLabel() to match it.
            // Values will be "--" when system_stats is not installed, but
            // the tiles are always rendered.
            const kpiLabels: RegExp[] = [
                /^CPU Usage:/i,
                /^Memory Usage:/i,
                /^Disk Usage:/i,
                /^Load Average:/i,
            ];
            for (const kpiLabel of kpiLabels) {
                await expect(
                    page.getByLabel(kpiLabel),
                    `KPI tile matching ${kpiLabel} should be visible`,
                ).toBeVisible({ timeout: 10_000 });
            }

            // Verify all five chart panel titles are visible.
            // ChartPanel always renders its title Typography regardless of
            // whether system_stats data is present.
            const chartTitles = [
                'CPU Usage Over Time',
                'Memory Usage Over Time',
                'Disk Space',
                'Load Average Over Time',
                'Network I/O',
            ];
            for (const chartTitle of chartTitles) {
                await expect(
                    page.getByText(chartTitle, { exact: true }),
                    `Chart panel title "${chartTitle}" should be visible`,
                ).toBeVisible({ timeout: 10_000 });
            }
        });

        // ----------------------------------------------------------
        // Step 4: Validate Top Queries section
        // ----------------------------------------------------------
        await test.step('Validate Top Queries section', async () => {
            const topQueriesHeader = monitoring.getSectionHeader('Top Queries');
            await expect(
                topQueriesHeader,
                'Top Queries section header should be visible',
            ).toBeVisible({ timeout: 10_000 });

            // Expand if not already expanded
            await monitoring.expandSection('Top Queries');

            // Wait for loading to finish
            await monitoring.waitForTopQueriesLoad();

            // Check for query rows or empty state — both are valid
            const queryRows = page.getByRole('button', {
                name: /view details for query/i,
            });
            const emptyState = page.getByText(
                'No query statistics available. Is the pg_stat_statements extension installed?',
            );

            // Use toPass to poll until either rows or empty state appears
            await expect(async () => {
                const rowCount = await queryRows.count();
                const emptyVisible = await emptyState.isVisible().catch(
                    () => false,
                );
                expect(
                    rowCount > 0 || emptyVisible,
                    'Either query rows or empty state should be visible',
                ).toBe(true);
            }).toPass({ timeout: 15_000, intervals: [500] });

            const rowCount = await queryRows.count();
            if (rowCount > 0) {
                await expect(
                    queryRows.first(),
                    'At least one query row should be visible',
                ).toBeVisible();
            } else {
                await expect(
                    emptyState,
                    'Empty state message should be visible when no queries exist',
                ).toBeVisible();
            }

            // Test the "Hide monitoring queries" toggle
            const toggle = monitoring.getHideMonitoringQueriesSwitch();
            await expect(
                toggle,
                'Hide monitoring queries switch should be visible',
            ).toBeVisible({ timeout: 5_000 });

            const wasChecked = await toggle.isChecked();

            // Click via the switchBase span — clicking the <input> with
            // force:true is unreliable in WebKit (onChange never fires).
            await monitoring.clickHideMonitoringQueriesSwitch();

            // Wait for the toggle state to change
            await expect(async () => {
                const nowChecked = await toggle.isChecked();
                expect(
                    nowChecked,
                    'Toggle checked state should have changed',
                ).not.toBe(wasChecked);
            }).toPass({ timeout: 10_000, intervals: [500] });

            // Toggle back to original state
            await monitoring.clickHideMonitoringQueriesSwitch();

            await expect(async () => {
                const restoredChecked = await toggle.isChecked();
                expect(
                    restoredChecked,
                    'Toggle should be restored to original state',
                ).toBe(wasChecked);
            }).toPass({ timeout: 10_000, intervals: [500] });
        });

        // ----------------------------------------------------------
        // Step 5: Validate sections can collapse independently
        // ----------------------------------------------------------
        await test.step('Validate sections collapse independently', async () => {
            const sectionTitles = [
                'System Resources',
                'PostgreSQL Overview',
                'Top Queries',
            ];

            for (const title of sectionTitles) {
                const header = monitoring.getSectionHeader(title);
                // Skip sections that are not visible in this view
                const isVisible = await header.isVisible().catch(() => false);
                if (!isVisible) {
                    continue;
                }

                // If expanded, collapse, then re-expand
                const expanded = await monitoring.isSectionExpanded(title);
                if (expanded) {
                    await monitoring.collapseSection(title);
                    await monitoring.expectSectionCollapsed(title);

                    await monitoring.expandSection(title);
                    await monitoring.expectSectionExpanded(title);
                } else {
                    // If collapsed, expand, then re-collapse
                    await monitoring.expandSection(title);
                    await monitoring.expectSectionExpanded(title);

                    await monitoring.collapseSection(title);
                    await monitoring.expectSectionCollapsed(title);

                    // Leave expanded for subsequent steps
                    await monitoring.expandSection(title);
                    await monitoring.expectSectionExpanded(title);
                }
            }
        });

        // ----------------------------------------------------------
        // Step 6: Validate AI Overview (conditional on aiEnabled)
        // ----------------------------------------------------------
        await test.step('Validate AI Overview', async () => {
            if (!aiEnabled) {
                // AI is not configured; skip this step.
                return;
            }

            const aiToggle = monitoring.getAIOverviewToggle();

            // AI Overview toggle may not render if the overview
            // returned null (no data yet). Wait briefly for it.
            const aiToggleVisible = await expect(async () => {
                await expect(
                    aiToggle,
                    'AI Overview toggle should be visible when AI is enabled',
                ).toBeVisible();
            }).toPass({ timeout: 15_000, intervals: [1_000] }).then(
                () => true,
            ).catch(() => false);

            if (!aiToggleVisible) {
                // Component returned null — no overview data yet.
                return;
            }

            // Expand AI Overview if collapsed
            await monitoring.expandAIOverview();

            // Verify expanded state
            await expect(
                aiToggle,
                'AI Overview should show Collapse label when expanded',
            ).toHaveAttribute('aria-label', /collapse ai overview/i);

            // Wait for AI content: either "Generating overview..." (status
            // = generating) or the "Refresh overview" button (ready state,
            // visible when generated_at is set). MUI sx styles compile to
            // CSS classes — not inline styles — so attribute selectors like
            // [style*="white-space: pre-wrap"] do not match.
            await expect(async () => {
                const generatingVisible = await page
                    .getByText('Generating overview...')
                    .isVisible()
                    .catch(() => false);

                // In the ready state the Refresh button is rendered.
                const refreshVisible = await page
                    .getByRole('button', { name: 'Refresh overview' })
                    .isVisible()
                    .catch(() => false);

                expect(
                    generatingVisible || refreshVisible,
                    'AI Overview should show generating message or summary text',
                ).toBe(true);
            }).toPass({ timeout: 20_000, intervals: [1_000] });

            // If in ready state, assert the summary text is non-empty
            const isReadyState = await page
                .getByRole('button', { name: 'Refresh overview' })
                .isVisible()
                .catch(() => false);
            if (isReadyState) {
                // The summary Typography is a MuiTypography-body2 inside
                // the Paper that contains the "AI Overview" label. Find
                // it and assert its text content.
                const aiPaper = page
                    .locator('.MuiPaper-root')
                    .filter({ hasText: 'AI Overview' })
                    .first();
                const summaryEl = aiPaper
                    .locator('.MuiTypography-body2')
                    .first();
                await expect(
                    summaryEl,
                    'AI Overview summary should be visible',
                ).toBeVisible({ timeout: 5_000 });
                const summaryText = await summaryEl.textContent();
                expect(
                    (summaryText ?? '').trim().length,
                    'AI Overview summary text should be non-empty',
                ).toBeGreaterThan(20);
            }

            // Check for Refresh overview button
            const refreshButton = page.getByRole('button', {
                name: 'Refresh overview',
            });
            const refreshVisible = await refreshButton.isVisible().catch(
                () => false,
            );
            if (refreshVisible) {
                await expect(
                    refreshButton,
                    'Refresh overview button should have correct aria-label',
                ).toHaveAttribute('aria-label', 'Refresh overview');
            }

            // Collapse and re-expand AI Overview
            await monitoring.collapseAIOverview();
            await expect(
                aiToggle,
                'AI Overview should show Expand label when collapsed',
            ).toHaveAttribute('aria-label', /expand ai overview/i);

            await monitoring.expandAIOverview();
            await expect(
                aiToggle,
                'AI Overview should show Collapse label after re-expand',
            ).toHaveAttribute('aria-label', /collapse ai overview/i);

            // Check for "Run full analysis" button
            const analyzeButton = page.getByRole('button', {
                name: 'Run full analysis',
            });
            const analyzeVisible = await analyzeButton.isVisible().catch(
                () => false,
            );
            if (analyzeVisible) {
                await analyzeButton.click();

                // Assert the analysis dialog opens (fullscreen Dialog)
                const analysisDialog = page.locator(
                    '.MuiDialog-paperFullScreen',
                );
                await expect(
                    analysisDialog,
                    'Server Analysis dialog should open',
                ).toBeVisible({ timeout: 15_000 });

                // Assert a close button exists in the dialog
                const closeAnalysis = page.getByRole('button', {
                    name: /close analysis/i,
                });
                await expect(
                    closeAnalysis,
                    'Analysis dialog close button should exist',
                ).toBeVisible({ timeout: 5_000 });

                // Close the analysis dialog
                await closeAnalysis.click();
                await expect(
                    analysisDialog,
                    'Analysis dialog should close',
                ).toBeHidden({ timeout: 10_000 });
            }
        });

        // ----------------------------------------------------------
        // Step 7: Validate Server Details dialog
        // ----------------------------------------------------------
        await test.step('Validate Server Details dialog', async () => {
            await monitoring.openServerInfoDialog();

            // Assert dialog title
            await expect(
                page.getByText(/Server Information:/i),
                'Server Info dialog title should be visible',
            ).toBeVisible({ timeout: 10_000 });

            // Assert close button exists
            await expect(
                page.getByRole('button', { name: 'close server info' }),
                'Server Info dialog close button should be visible',
            ).toBeVisible();

            // Assert at least one section header exists in the dialog.
            // Section.tsx renders a Box with role="button" and aria-expanded,
            // but no aria-label. Locate by filtering for role="button" elements
            // that contain the known section title text.
            const dialog = page.locator('.MuiDialog-paperFullScreen');
            const dialogSectionHeader = dialog
                .getByRole('button')
                .filter({
                    hasText:
                        /System & Hardware|PostgreSQL|Databases|Configuration/i,
                })
                .first();
            await expect(
                dialogSectionHeader,
                'At least one section should exist in the Server Info dialog',
            ).toBeVisible({ timeout: 10_000 });

            // Test expand/collapse of the first section in the dialog
            const sectionTitle = await dialogSectionHeader.textContent();
            const isExpanded = await dialogSectionHeader.getAttribute(
                'aria-expanded',
            );

            // Toggle the section
            await dialogSectionHeader.click();

            // Assert the state changed
            const newExpanded = await dialogSectionHeader.getAttribute(
                'aria-expanded',
            );
            expect(
                newExpanded,
                `Section "${sectionTitle}" expanded state should toggle`,
            ).not.toBe(isExpanded);

            // Toggle back
            await dialogSectionHeader.click();
            const restoredExpanded = await dialogSectionHeader.getAttribute(
                'aria-expanded',
            );
            expect(
                restoredExpanded,
                `Section "${sectionTitle}" should return to original state`,
            ).toBe(isExpanded);

            // Close the Server Info dialog
            await monitoring.closeServerInfoDialog();
        });
    });
});
