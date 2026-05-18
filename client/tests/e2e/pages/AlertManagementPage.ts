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
 * - Edit row button: aria-label "edit alert rule"
 * - Edit dialog: innerDialog (non-fullscreen [role="dialog"])
 * - Enabled checkbox: role=checkbox, name "Enabled"
 * - Operator combobox: role=combobox, name matching /Operator/
 * - Threshold field: role=spinbutton, name "Threshold"
 * - Severity combobox: role=combobox, name matching /Severity/
 * - Save button: role=button, name "Save"
 * - Enabled switch in table: .MuiSwitch-root input[type="checkbox"]
 *
 * Cross-browser notes:
 * - MUI TextField-select renders a Portal-based Menu. The Menu's Modal
 *   wraps an invisible backdrop (.MuiBackdrop-invisible). In WebKit CI
 *   this backdrop lingers after the listbox closes, blocking subsequent
 *   Playwright clicks. The fix is to wait for .MuiBackdrop-invisible to
 *   be hidden before each combobox/field click.
 * - Option clicks use element.click() via evaluate() to bypass
 *   Playwright's "stable" actionability check, which WebKit fails during
 *   MUI's CSS entry transition.
 * - Note: .MuiBackdrop-invisible targets only Select/Menu backdrops.
 *   Dialog backdrops (.MuiBackdrop-root without the invisible class)
 *   remain visible while the edit dialog is open and are not affected.
 */
export class AlertManagementPage extends BasePage {

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Wait for the MUI Select/Menu invisible backdrop to be fully
     * removed before interacting with the next element. Required in
     * WebKit CI where the backdrop lingers after the listbox closes.
     */
    private async waitForSelectBackdropGone(): Promise<void> {
        await this.page
            .locator('.MuiBackdrop-invisible')
            .waitFor({ state: 'hidden', timeout: 5_000 })
            .catch(() => {
                // No invisible backdrop present — nothing to wait for.
            });
    }

    /**
     * Open an MUI TextField-select dropdown and click the named option.
     * Waits for the invisible backdrop to clear both before opening and
     * after closing to avoid WebKit pointer-event blocking.
     */
    private async selectMuiOption(
        comboboxName: RegExp,
        value: string,
    ): Promise<void> {
        // Ensure no lingering Select backdrop blocks the combobox click.
        await this.waitForSelectBackdropGone();

        await this.innerDialog
            .getByRole('combobox', { name: comboboxName })
            .click();

        const listbox = this.page.getByRole('listbox');
        await expect(listbox).toBeVisible({ timeout: 5_000 });

        // Use evaluate click to bypass WebKit's "stable" check during
        // MUI's CSS entry transition on the option items.
        const option = listbox.getByRole('option', {
            name: value,
            exact: true,
        });
        await option.waitFor({ state: 'visible', timeout: 5_000 });
        await option.evaluate((el) => (el as HTMLElement).click());

        await expect(listbox).toBeHidden({ timeout: 5_000 });

        // Wait for the invisible backdrop to be removed before any
        // subsequent interaction.
        await this.waitForSelectBackdropGone();
    }

    // ---------------------------------------------------------------
    // Edit dialog flow
    // ---------------------------------------------------------------

    async clickEditAlert(alertName: string): Promise<void> {
        const row = this.page.getByRole('row', { name: alertName });
        await row.getByLabel('edit alert rule').click();
    }

    async waitForEditDialog(): Promise<void> {
        await expect(this.innerDialog).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Set the Enabled checkbox inside the edit dialog. Only toggles
     * if the current state does not match the desired state.
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

    async selectOperator(operator: string): Promise<void> {
        await this.selectMuiOption(/Operator/, operator);
    }

    /**
     * Fill the Threshold field. Waits for any Select backdrop to clear
     * first so WebKit does not block the click.
     */
    async fillThreshold(value: string): Promise<void> {
        await this.waitForSelectBackdropGone();
        const field = this.innerDialog.getByRole('spinbutton', {
            name: 'Threshold',
        });
        await field.click();
        await field.fill(value);
    }

    async selectSeverity(severity: string): Promise<void> {
        await this.selectMuiOption(/Severity/, severity);
    }

    /**
     * Click Save and wait for the dialog to close. Waits for any
     * Select backdrop to clear first.
     */
    async saveEdit(): Promise<void> {
        await this.waitForSelectBackdropGone();
        await this.innerDialog
            .getByRole('button', { name: /^save$/i })
            .click();
        await this.waitForDialogToClose();
    }

    // ---------------------------------------------------------------
    // Table assertions
    // ---------------------------------------------------------------

    async expectOperatorThresholdInTable(
        alertName: string,
        value: string,
    ): Promise<void> {
        const row = this.page.getByRole('row', { name: alertName });
        await expect(
            row.getByRole('cell', { name: value, exact: true }),
        ).toBeVisible({ timeout: 10_000 });
    }

    async expectSeverityInTable(
        alertName: string,
        severity: string,
    ): Promise<void> {
        const row = this.page.getByRole('row', { name: alertName });
        await expect(
            row.getByText(severity, { exact: true }),
        ).toBeVisible({ timeout: 10_000 });
    }

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
