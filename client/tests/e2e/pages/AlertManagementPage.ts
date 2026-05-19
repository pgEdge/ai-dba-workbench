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
 * - Option selection uses keyboard navigation (Home + ArrowDown + Enter)
 *   instead of mouse clicks. This is coordinate-free and immune to
 *   MUI's CSS entry animation, which shifts option positions during
 *   the transition and causes page.mouse.click() at boundingBox()
 *   coordinates to miss the target in WebKit CI. Keyboard navigation
 *   relies on MUI's built-in accessibility support and works reliably
 *   across all browsers.
 * - Note: .MuiBackdrop-invisible targets only Select/Menu backdrops.
 *   Dialog backdrops (.MuiBackdrop-root without the invisible class)
 *   remain visible while the edit dialog is open and are not affected.
 */
export class AlertManagementPage extends BasePage {

    // ---------------------------------------------------------------
    // Option order arrays (must match the component source)
    // ---------------------------------------------------------------

    private readonly OPERATORS = ['>', '>=', '<', '<=', '==', '!='];
    private readonly SEVERITIES = ['info', 'warning', 'critical'];

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
     * Open a MUI TextField-select dropdown and select an option using
     * keyboard navigation: Home to reset to the first item, then
     * ArrowDown N times to reach the target, then Enter to confirm.
     *
     * Why keyboard nav instead of mouse.click(): MUI's CSS entry
     * animation shifts option positions during the transition, causing
     * page.mouse.click() at boundingBox() coordinates to miss the
     * target in WebKit CI. Keyboard navigation is coordinate-free,
     * animation-independent, and leverages MUI's built-in accessibility
     * support. It works reliably across all browsers.
     *
     * The caller must supply `optionOrder`, the ordered list of option
     * values as they appear in the dropdown. This array determines
     * how many ArrowDown presses are needed to reach `value`.
     *
     * The backdrop wait before and after prevents WebKit's lingering
     * invisible backdrop from blocking the next interaction.
     */
    private async selectMuiOption(
        comboboxName: RegExp,
        value: string,
        optionOrder: string[],
    ): Promise<void> {
        await this.waitForSelectBackdropGone();

        await this.innerDialog
            .getByRole('combobox', { name: comboboxName })
            .click();

        const listbox = this.page.getByRole('listbox');
        await expect(listbox).toBeVisible({ timeout: 5_000 });

        const idx = optionOrder.indexOf(value);
        if (idx === -1) {
            throw new Error(
                `Option "${value}" not found in optionOrder list`,
            );
        }

        // Home resets focus to the first option, regardless of which
        // item MUI auto-focused when the listbox opened.
        await this.page.keyboard.press('Home');
        for (let i = 0; i < idx; i++) {
            await this.page.keyboard.press('ArrowDown');
        }
        await this.page.keyboard.press('Enter');

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
        await this.selectMuiOption(
            /Operator/, operator, this.OPERATORS,
        );
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
        await this.selectMuiOption(
            /Severity/, severity, this.SEVERITIES,
        );
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
