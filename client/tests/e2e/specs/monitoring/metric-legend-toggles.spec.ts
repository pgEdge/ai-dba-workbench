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
import {
    ensureClusterAndPrimary,
    primeSystemMetrics,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * The monitoring charts render an accessible DOM legend (one toggle button
 * per series) in place of the ECharts canvas legend, so a real user — and
 * this test — can click each series on/off. This exercises that toggle
 * through the GUI across every monitoring section, asserting the series'
 * shown/hidden state via `aria-pressed` (the canvas itself is not
 * DOM-inspectable). The chart's underlying data is validated elsewhere;
 * here the focus is the legend toggle interaction.
 */
test.describe('Monitoring Chart Legend Toggles', () => {
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

    test('every rendered chart legend toggles its series off and back on', async ({
        page,
    }) => {
        test.setTimeout(300_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // A chart only renders legend items for series that have data, so
        // prime the pg_sys_* samples to guarantee the System Resources
        // charts (CPU/Memory/…) render their legends.
        await primeSystemMetrics(api, adminCookie, primaryConnId);

        await selectPrimaryServer(page, monitoring, serverNav);
        await monitoring.expandSection('Monitoring').catch(() => undefined);
        for (const section of [
            'System Resources',
            'PostgreSQL Overview',
            'WAL and Replication',
        ]) {
            await monitoring.expandSection(section).catch(() => undefined);
        }

        // Wait until the DOM legends have rendered.
        const toggles = monitoring.legendToggles();
        await expect(async () => {
            expect(
                await toggles.count(),
                'monitoring charts should render DOM legend toggles',
            ).toBeGreaterThan(0);
        }).toPass({ timeout: 90_000, intervals: [3_000] });

        // Round-trip every rendered legend toggle: shown → hidden → shown.
        // Series are shown by default (aria-pressed="true"). Each toggle is
        // restored before moving on so charts are left as we found them.
        const exercised: string[] = [];
        const count = await toggles.count();
        for (let i = 0; i < count; i++) {
            const btn = toggles.nth(i);
            if (!(await btn.isVisible().catch(() => false))) {
                continue;
            }
            const testId = (await btn.getAttribute('data-testid')) ?? `#${i}`;

            await expect(btn, `${testId} starts shown`).toHaveAttribute(
                'aria-pressed',
                'true',
            );
            await btn.scrollIntoViewIfNeeded().catch(() => undefined);
            await btn.click();
            await expect(btn, `${testId} hides its series`).toHaveAttribute(
                'aria-pressed',
                'false',
            );
            await btn.click();
            await expect(btn, `${testId} shows its series again`).toHaveAttribute(
                'aria-pressed',
                'true',
            );
            exercised.push(testId);
        }

        // The primed System Resources charts are guaranteed present, so their
        // CPU and Memory legends must have been exercised; and we should have
        // toggled a meaningful number of legends across the sections.
        expect(
            exercised.some((t) => t.startsWith('legend-toggle-cpu-')),
            'a CPU series legend toggle was exercised',
        ).toBe(true);
        expect(
            exercised.some((t) => t.startsWith('legend-toggle-memory-')),
            'a Memory series legend toggle was exercised',
        ).toBe(true);
        expect(
            exercised.length,
            'multiple chart legend toggles were exercised',
        ).toBeGreaterThanOrEqual(4);
    });
});
