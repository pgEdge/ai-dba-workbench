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
 *   Playwright clicks.
 * - Option clicks use page.mouse.click() at real viewport coordinates
 *   obtained from boundingBox(). This fires a genuine isTrusted:true
 *   MouseEvent that bypasses Playwright's actionability checks
 *   (visibility, stability, pointer-events) while still triggering
 *   React's event delegation and MUI's selection logic. This is the
 *   only approach that works reliably across all browsers in CI.
 * - Note: .MuiBackdrop-invisible targets only Select/Menu backdrops.
 *   Dialog backdrops (.MuiBackdrop-root without the invisible class)
 *   remain visible while the edit dialog is open and are not affected.
 */
export class AlertManagementPage extends BasePage {

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Wait for any MUI Select/Menu invisible backdrop to clear.
     * The invisible backdrop (.MuiBackdrop-invisible) is created by
     * MUI's Portal-based Menu when a Select opens. In WebKit CI it
     * lingers after the dropdown closes, blocking all pointer events.
     * Using 'hidden' state: resolves immediately if no backdrop exists.
     */
    private async waitForSelectBackdropGone(): Promise<void> {
        await this.page
            .locator('.MuiBackdrop-invisible')
            .waitFor({ state: 'hidden', timeout: 5_000 })
            .catch(() => {});
    }

    /**
     * Open a MUI TextField-select dropdown and click the named option
     * using page.mouse.click() at the element's viewport coordinates.
     *
     * Why mouse.click(): this fires a genuine isTrusted:true MouseEvent
     * at real coordinates, bypassing Playwright's actionability checks
     * (visibility, stability, pointer-events) while still triggering
     * React's event delegation and MUI's selection logic. This is the
     * only approach that works reliably across all browsers in CI.
     *
     * The backdrop wait before and after prevents WebKit's lingering
     * invisible backdrop from blocking the next interaction.
     */
    private async selectMuiOption(
        comboboxName: RegExp,
        value: string,
    ): Promise<void> {
        await this.waitForSelectBackdropGone();

        await this.innerDialog
            .getByRole('combobox', { name: comboboxName })
            .click();

        const listbox = this.page.getByRole('listbox');
        await expect(listbox).toBeVisible({ timeout: 5_000 });

        const option = listbox.getByRole('option', {
            name: value,
            exact: true,
        });
        await option.waitFor({ state: 'visible', timeout: 5_000 });

        // Use raw mouse coordinates — bypasses actionability while firing
        // a trusted event that properly triggers MUI's React handlers.
        const box = await option.boundingBox();
        if (!box) {
            throw new Error(`Option "${value}" has no bounding box`);
        }
        await this.page.mouse.click(
            box.x + box.width / 2,
            box.y + box.height / 2,
        );

        await expect(listbox).toBeHidden({ timeout: 5_000 });
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
     * Toggle the Enabled switch. Waits for any Select backdrop to clear
     * first — in WebKit CI a lingering backdrop blocks checkbox events.
     */
    async setEnabled(enabled: boolean): Promise<void> {
        await this.waitForSelectBackdropGone();
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
     * Fill the Threshold spinbutton. Waits for backdrop before clicking.
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
     * Click Save and wait for dialog to close. Waits for backdrop first.
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
