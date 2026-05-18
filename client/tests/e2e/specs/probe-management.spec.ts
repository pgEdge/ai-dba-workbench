/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test } from '@playwright/test';
import { label } from 'allure-js-commons';
import { AdminPage } from '../pages/AdminPage';
import { ProbeManagementPage } from '../pages/ProbeManagementPage';

// ---------------------------------------------------------------
// Probe Management Tests
// ---------------------------------------------------------------

test.describe('Probe Management', () => {
    // Apply admin storage state to all browser-based tests in this
    // describe block so the UI tests skip the manual login flow.
    test.use({ storageState: '.auth/admin.json' });

    test.beforeEach(async () => {
        await label('package', 'Probe Management');
    });

    // -------------------------------------------------------
    // 1. Edit probe via UI
    // -------------------------------------------------------
    test('edit probe via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const probePage = new ProbeManagementPage(page);

        await test.step('Navigate to Admin > Probe Defaults', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToProbes();
        });

        await test.step('Edit probe: set Retention=2, Interval=20, disable', async () => {
            await probePage.clickEditProbe('Connectivity');
            await probePage.fillRetentionDays('2');
            await probePage.fillCollectionInterval('20');
            await probePage.setEnabled(false);
            await probePage.saveEdit();
        });

        await test.step('Verify table reflects first update', async () => {
            await probePage.expectSuccessAlert('Connectivity');
            await probePage.expectRetentionInTable('Connectivity', '2');
            await probePage.expectIntervalInTable('Connectivity', '20s');
            await probePage.expectProbeDisabled('Connectivity');
        });

        await test.step('Re-open and enable only', async () => {
            await probePage.clickEditProbe('Connectivity');
            await probePage.setEnabled(true);
            await probePage.saveEdit();
        });

        await test.step('Verify table reflects re-enable', async () => {
            await probePage.expectProbeEnabled('Connectivity');
            await probePage.expectRetentionInTable('Connectivity', '2');
            await probePage.expectIntervalInTable('Connectivity', '20s');
        });
    });
});
