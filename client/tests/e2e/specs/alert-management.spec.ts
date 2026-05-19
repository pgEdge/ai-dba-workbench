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
import { AlertManagementPage } from '../pages/AlertManagementPage';

// ---------------------------------------------------------------
// Alert Management Tests
// ---------------------------------------------------------------

test.describe('Alert Management', () => {
    // Apply admin storage state to all browser-based tests in this
    // describe block so the UI tests skip the manual login flow.
    test.use({ storageState: '.auth/admin.json' });

    test.beforeEach(async () => {
        await label('package', 'Alert Management');
    });

    // -------------------------------------------------------
    // 1. Edit alert rule via UI
    // -------------------------------------------------------
    test('edit alert rule via UI', async ({ page }) => {
        test.slow();
        const adminPage = new AdminPage(page);
        const alertPage = new AlertManagementPage(page);
        const alertName = 'Connection Utilization';

        await test.step('Navigate to Admin > Alert Rules', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToAlertRules();
        });

        await test.step('First edit: change operator, threshold, severity', async () => {
            await alertPage.clickEditAlert(alertName);
            await alertPage.waitForEditDialog();
            await alertPage.selectOperator('>=');
            await alertPage.fillThreshold('2');
            await alertPage.selectSeverity('info');
            await alertPage.setEnabled(true);
            await alertPage.saveEdit();
        });

        await test.step('Verify first edit in table', async () => {
            await alertPage.expectOperatorThresholdInTable(alertName, '>= 2');
            await alertPage.expectSeverityInTable(alertName, 'info');
            await alertPage.expectEnabledInTable(alertName, true);
        });

        await test.step('Second edit: disable alert', async () => {
            await alertPage.clickEditAlert(alertName);
            await alertPage.waitForEditDialog();
            await alertPage.setEnabled(false);
            await alertPage.saveEdit();
        });

        await test.step('Verify alert is disabled', async () => {
            await alertPage.expectEnabledInTable(alertName, false);
        });

        await test.step('Third edit: enable, restore operator, change severity', async () => {
            await alertPage.clickEditAlert(alertName);
            await alertPage.waitForEditDialog();
            await alertPage.setEnabled(true);
            await alertPage.selectOperator('>');
            await alertPage.selectSeverity('critical');
            await alertPage.saveEdit();
        });

        await test.step('Verify third edit in table', async () => {
            await alertPage.expectEnabledInTable(alertName, true);
            await alertPage.expectSeverityInTable(alertName, 'critical');
            await alertPage.expectOperatorThresholdInTable(alertName, '> 2');
        });

        await test.step('Restore to original state', async () => {
            await alertPage.clickEditAlert(alertName);
            await alertPage.waitForEditDialog();
            await alertPage.setEnabled(true);
            await alertPage.fillThreshold('90');
            await alertPage.selectSeverity('warning');
            await alertPage.saveEdit();
        });
    });
});
