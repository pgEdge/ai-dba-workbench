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
 * Page object encapsulating all interactions with the Probe Defaults
 * section of the AdminPanel. Covers the probes table and the
 * edit-probe dialog.
 *
 * Selector strategy:
 * - Edit row button: aria-label "edit probe" (from AdminProbes.tsx)
 * - Edit dialog title: heading "Edit probe: {friendlyName}"
 *   (from AdminProbes.tsx DialogTitle)
 * - Enabled switch in dialog: role=checkbox inside innerDialog
 *   (MUI Switch without a label element)
 * - Collection Interval field: label "Collection Interval (seconds)"
 * - Retention Days field: label "Retention Days"
 * - Save button: role=button, name "Save"
 * - Cancel button: role=button, name "Cancel"
 * - Success alert: Alert severity="success" containing probe name
 * - Enabled switch in table: disabled Switch (role=checkbox) in row
 * - Interval in table: rendered as "{n}s"
 * - Retention in table: rendered as plain number
 */
export class ProbeManagementPage extends BasePage {
    // ---------------------------------------------------------------
    // Edit dialog flow
    // ---------------------------------------------------------------

    /**
     * Open the edit dialog for a probe by clicking the edit button
     * on the row matching the given friendly name, then wait for
     * the edit dialog heading to confirm the dialog has rendered.
     */
    async clickEditProbe(friendlyName: string): Promise<void> {
        await this.clickRowAction(friendlyName, /edit probe/i);
        await this.waitForEditDialog(friendlyName);
    }

    /**
     * Wait for the edit-probe dialog to appear by checking for
     * the dialog heading that includes the probe friendly name.
     */
    async waitForEditDialog(friendlyName: string): Promise<void> {
        await expect(
            this.innerDialog.getByRole('heading', {
                name: `Edit probe: ${friendlyName}`,
            }),
        ).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Clear and fill the Collection Interval field inside the
     * edit dialog.
     */
    async fillCollectionInterval(value: string): Promise<void> {
        await this.clearAndFillField(
            'Collection Interval (seconds)',
            value,
            this.innerDialog,
        );
    }

    /**
     * Clear and fill the Retention Days field inside the edit
     * dialog.
     */
    async fillRetentionDays(value: string): Promise<void> {
        await this.clearAndFillField(
            'Retention Days',
            value,
            this.innerDialog,
        );
    }

    /**
     * Return the current enabled state of the switch inside the
     * edit dialog. The MUI Switch renders as role=checkbox.
     */
    async isEnabledInDialog(): Promise<boolean> {
        return this.innerDialog.getByRole('checkbox').isChecked();
    }

    /**
     * Set the enabled switch inside the edit dialog to the
     * desired state. Only clicks the switch if the current state
     * does not match the desired state.
     */
    async setEnabled(enabled: boolean): Promise<void> {
        const current = await this.isEnabledInDialog();
        if (current !== enabled) {
            await this.innerDialog.getByRole('checkbox').click();
        }
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
     * Assert that the table row matching the probe friendly name
     * displays the expected interval value (e.g. "20s").
     */
    async expectIntervalInTable(
        friendlyName: string,
        intervalSeconds: string,
    ): Promise<void> {
        const row = this.getTableRow(friendlyName);
        await expect(
            row.getByText(intervalSeconds, { exact: true }),
        ).toBeVisible({ timeout: 10_000 });
    }

    /**
     * Assert that the table row matching the probe friendly name
     * displays the expected retention days value.
     */
    async expectRetentionInTable(
        friendlyName: string,
        days: string,
    ): Promise<void> {
        const row = this.getTableRow(friendlyName);
        await expect(
            row.getByText(days, { exact: true }),
        ).toBeVisible({ timeout: 10_000 });
    }

    /**
     * Assert that the probe is enabled in the table by checking
     * the disabled Switch (role=checkbox) is checked.
     */
    async expectProbeEnabled(
        friendlyName: string,
    ): Promise<void> {
        const row = this.getTableRow(friendlyName);
        await expect(
            row.getByRole('checkbox'),
        ).toBeChecked({ timeout: 10_000 });
    }

    /**
     * Assert that the probe is disabled in the table by checking
     * the disabled Switch (role=checkbox) is not checked.
     */
    async expectProbeDisabled(
        friendlyName: string,
    ): Promise<void> {
        const row = this.getTableRow(friendlyName);
        await expect(
            row.getByRole('checkbox'),
        ).not.toBeChecked({ timeout: 10_000 });
    }

    // ---------------------------------------------------------------
    // Toast / alert assertions
    // ---------------------------------------------------------------

    /**
     * Wait for the success alert to appear and assert that it
     * contains the probe friendly name.
     */
    async expectSuccessAlert(friendlyName: string): Promise<void> {
        await this.waitForToast(
            new RegExp(`Probe "${friendlyName}" updated successfully`),
        );
    }
}
