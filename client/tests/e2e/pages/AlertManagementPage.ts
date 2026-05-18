/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { expect } from '@playwright/test';
import { BasePage } from './BasePage';

/**
 * Page object encapsulating all interactions with the Alert Rules
 * section of the AdminPanel. Covers the alert rules table and the
 * edit-alert dialog.
 *
 * Selector strategy:
 * - Edit row button: aria-label "edit alert rule" (from AdminAlertRules.tsx)
 * - Edit dialog: innerDialog (non-fullscreen [role="dialog"])
 * - Enabled checkbox in dialog: role=checkbox, name "Enabled"
 * - Operator combobox: role=combobox, name matching /Operator/
 * - Threshold field: role=spinbutton, name "Threshold"
 * - Severity combobox: role=combobox, name matching /Severity/
 * - Save button: role=button, name "Save"
 * - Enabled switch in table: MuiSwitch-root with input[type="checkbox"]
 */
export class AlertManagementPage extends BasePage {
    // ---------------------------------------------------------------
    // Edit dialog flow
    // ---------------------------------------------------------------

    /**
     * Open the edit dialog for an alert rule by clicking the edit
     * button on the row matching the given alert name.
     */
    async clickEditAlert(alertName: string): Promise<void> {
        const row = this.page.getByRole('row', { name: alertName });
        await row.getByLabel('edit alert rule').click();
    }

    /**
     * Wait for the edit-alert dialog to appear by checking that
     * the inner (non-fullscreen) dialog is visible.
     */
    async waitForEditDialog(): Promise<void> {
        await expect(this.innerDialog).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Set the Enabled checkbox inside the edit dialog to the
     * desired state. Only toggles the checkbox if the current
     * state does not match the desired state.
     */
    async setEnabled(enabled: boolean): Promise<void> {
        const checkbox = this.innerDialog.getByRole('checkbox', {
            name: 'Enabled',
        });
        const current = await checkbox.isChecked();
        if (current !== enabled) {
            if (enabled) {
                await checkbox.check();
            } else {
                await checkbox.uncheck();
            }
        }
    }

    /**
     * Select an operator from the Operator combobox inside the
     * edit dialog. Clicks the combobox to open the dropdown, then
     * clicks the matching option.
     */
    async selectOperator(operator: string): Promise<void> {
        await this.innerDialog
            .getByRole('combobox', { name: /Operator/ })
            .click();
        await this.page
            .getByRole('option', { name: operator, exact: true })
            .click();
    }

    /**
     * Clear and fill the Threshold field inside the edit dialog.
     */
    async fillThreshold(value: string): Promise<void> {
        const field = this.innerDialog.getByRole('spinbutton', {
            name: 'Threshold',
        });
        await field.click();
        await field.fill(value);
    }

    /**
     * Select a severity from the Severity combobox inside the
     * edit dialog. Clicks the combobox to open the dropdown, then
     * clicks the matching option.
     */
    async selectSeverity(severity: string): Promise<void> {
        await this.innerDialog
            .getByRole('combobox', { name: /Severity/ })
            .click();
        await this.page
            .getByRole('option', { name: severity })
            .click();
    }

    /**
     * Click the Save button inside the edit dialog and wait for
     * the dialog to close.
     */
    async saveEdit(): Promise<void> {
        await this.innerDialog
            .getByRole('button', { name: /^save$/i })
            .click();
        await this.waitForDialogToClose();
    }

    // ---------------------------------------------------------------
    // Table assertions
    // ---------------------------------------------------------------

    /**
     * Assert that the table row matching the alert name displays
     * the expected operator and threshold value (e.g. ">= 2").
     */
    async expectOperatorThresholdInTable(
        alertName: string,
        value: string,
    ): Promise<void> {
        const row = this.page.getByRole('row', { name: alertName });
        await expect(
            row.getByRole('cell', { name: value, exact: true }),
        ).toBeVisible({ timeout: 10_000 });
    }

    /**
     * Assert that the table row matching the alert name displays
     * the expected severity label.
     */
    async expectSeverityInTable(
        alertName: string,
        severity: string,
    ): Promise<void> {
        const row = this.page.getByRole('row', { name: alertName });
        await expect(
            row.getByText(severity, { exact: true }),
        ).toBeVisible({ timeout: 10_000 });
    }

    /**
     * Assert that the alert rule is enabled or disabled in the
     * table by checking the MUI Switch input checkbox within the
     * matching row.
     */
    async expectEnabledInTable(
        alertName: string,
        enabled: boolean,
    ): Promise<void> {
        const row = this.page.getByRole('row', { name: alertName });
        const switchInput = row
            .locator('.MuiSwitch-root')
            .locator('input[type="checkbox"]');
        if (enabled) {
            await expect(switchInput).toBeChecked({ timeout: 10_000 });
        } else {
            await expect(switchInput).not.toBeChecked({
                timeout: 10_000,
            });
        }
    }
}
