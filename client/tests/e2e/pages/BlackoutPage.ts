/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Locator, expect } from '@playwright/test';
import { BasePage } from './BasePage';

/**
 * Page object for the database "Blackout" feature that lives in the
 * StatusPanel selection header and its management/create dialogs.
 *
 * Selector strategy (from `components/StatusPanel/SelectionHeader.tsx`,
 * `components/BlackoutManagementDialog.tsx`, `components/BlackoutDialog.tsx`
 * and `components/ClusterNavigator/StatusIndicator.tsx`):
 *
 * - Management trigger: the SelectionHeader IconButton has no aria-label,
 *   so it is selected as the (only) `<button>` containing the local
 *   `BlackoutIcon`. That icon is built with MUI `createSvgIcon` and so
 *   carries `data-testid="FrontHandSharpIcon"` in the built e2e client
 *   (the same mechanism behind the CheckCircleOutlineIcon/UndoIcon test
 *   ids other page objects already rely on). The header only renders once
 *   a tree node is selected, so callers must select a server first.
 * - Management dialog: a non-fullscreen MUI Dialog whose title reads
 *   "Blackout management"; isolated with a `hasText` filter.
 * - Create flow: "New One Time Blackout" opens the BlackoutDialog titled
 *   "Start blackout" with a Scope radio group (Estate/Group/Cluster/
 *   Server), a Start Now / Schedule Future toggle (Start Now is the
 *   default), duration preset chips plus custom "Hours"/"Minutes" fields,
 *   a "Reason" field and a "Create Blackout" submit button.
 * - Active-row indicator: StatusIndicator overlays a `PauseCircleIcon`
 *   (Tooltip "Blackout active") on the server row inside
 *   `data-testid="cluster-navigator"`.
 * - Stop: a "Stop" button in the management dialog's Active section.
 * - Delete (non-active only): an icon button (Tooltip "Delete blackout")
 *   in the management dialog's Blackouts section that opens a
 *   DeleteConfirmationDialog titled "Delete blackout".
 */
export class BlackoutPage extends BasePage {
    // ---------------------------------------------------------------
    // Structural locators
    // ---------------------------------------------------------------

    /**
     * The SelectionHeader management trigger: the only `<button>` that
     * contains the BlackoutIcon (`data-testid="FrontHandSharpIcon"`).
     */
    get trigger(): Locator {
        return this.page
            .locator('button')
            .filter({
                has: this.page.locator('[data-testid="FrontHandSharpIcon"]'),
            })
            .first();
    }

    /** The "Blackout management" dialog (non-fullscreen MUI Dialog). */
    get managementDialog(): Locator {
        return this.page
            .getByRole('dialog')
            .filter({ hasText: 'Blackout management' });
    }

    /** The "Start blackout" create dialog. */
    get startDialog(): Locator {
        return this.page
            .getByRole('dialog')
            .filter({ hasText: 'Start blackout' });
    }

    /** The DeleteConfirmationDialog for a blackout (accessible name). */
    get deleteConfirmDialog(): Locator {
        return this.page.getByRole('dialog', { name: 'Delete blackout' });
    }

    /**
     * The blackout indicator (`PauseCircleIcon`) overlaid on a server row
     * in the navigator, scoped so it never matches the identical icon used
     * elsewhere (management dialog list rows, status overlays for other
     * servers).
     */
    serverBlackoutIndicator(serverName: string): Locator {
        return this.serverRow(serverName)
            .locator('[data-testid="PauseCircleIcon"]')
            .first();
    }

    /** The navigator row for a server. */
    private serverRow(serverName: string): Locator {
        return this.page
            .getByTestId('cluster-navigator')
            .locator('.server-item-row')
            .filter({ hasText: serverName })
            .first();
    }

    /**
     * Ensure the server's navigator row is visible: its cluster may be
     * collapsed (the create/management dialog flow re-renders and collapses
     * the tree), which hides the row AND its blackout overlay. Expand
     * collapsed clusters until the row shows. Idempotent — never toggles a
     * cluster whose row is already visible.
     */
    private async ensureServerRowVisible(serverName: string): Promise<void> {
        const row = this.serverRow(serverName);
        if (await row.isVisible().catch(() => false)) {
            return;
        }
        const clusters = this.page
            .getByTestId('cluster-navigator')
            .locator('.draggable-cluster');
        const count = await clusters.count();
        for (let i = 0; i < count; i++) {
            if (await row.isVisible().catch(() => false)) {
                return;
            }
            await clusters
                .nth(i)
                .locator('.cluster-header')
                .getByRole('button')
                .first()
                .click({ force: true })
                .catch(() => undefined);
        }
        await expect(
            row,
            `server row "${serverName}" should be visible after expanding its cluster`,
        ).toBeVisible({ timeout: 10_000 });
    }

    // ---------------------------------------------------------------
    // Management dialog
    // ---------------------------------------------------------------

    /**
     * Open the blackout management dialog via the SelectionHeader trigger.
     * The header only renders once a tree node is selected, so a server
     * must already be selected.
     */
    async openManagement(): Promise<void> {
        await expect(
            this.trigger,
            'Blackout management trigger should be visible in the selection header',
        ).toBeVisible({ timeout: 15_000 });
        await this.trigger.click();
        await expect(
            this.managementDialog,
            'Blackout management dialog should open',
        ).toBeVisible({ timeout: 10_000 });
    }

    /** Close the management dialog (Escape dismisses the MUI Dialog). */
    async closeManagement(): Promise<void> {
        if (!(await this.managementDialog.isVisible().catch(() => false))) {
            return;
        }
        await this.page.keyboard.press('Escape');
        await expect(
            this.managementDialog,
            'Blackout management dialog should close',
        ).toBeHidden({ timeout: 10_000 });
    }

    /**
     * The number of active blackouts listed in the management dialog. Each
     * active blackout renders exactly one "Stop" button in the Active
     * section, so the button count is the active count.
     */
    async activeCount(): Promise<number> {
        return this.managementDialog
            .getByRole('button', { name: 'Stop' })
            .count();
    }

    /**
     * True when the management dialog lists at least one blackout or
     * schedule (i.e. it is not showing the empty-state message).
     */
    async isManagementBlackoutListed(): Promise<boolean> {
        const empty = await this.managementDialog
            .getByText('No blackouts or schedules configured')
            .isVisible()
            .catch(() => false);
        return !empty;
    }

    // ---------------------------------------------------------------
    // Create
    // ---------------------------------------------------------------

    /**
     * Create a one-time, server-scoped blackout through the GUI. Requires
     * the management dialog to be open and a server to be selected (the
     * server scope derives its connection_id from the current selection).
     *
     * The duration is entered through the custom Hours/Minutes fields
     * rather than a preset chip so any duration is expressible: minutes are
     * split into whole hours (max 72) and remaining minutes (0-59), which
     * covers both a short custom window (e.g. 5m -> Minutes=5) and a longer
     * one (e.g. 60m -> Hours=1). Typing into either field clears the
     * default preset selection.
     */
    async createOneTimeServerBlackout(options: {
        minutes: number;
        reason: string;
    }): Promise<void> {
        await this.managementDialog
            .getByRole('button', { name: 'New One Time Blackout' })
            .click();

        const dialog = this.startDialog;
        await expect(
            dialog,
            'Start blackout dialog should open',
        ).toBeVisible({ timeout: 10_000 });

        // Server scope (default when a server is selected; set explicitly).
        await dialog.getByRole('radio', { name: 'Server' }).check();

        // "Start Now" is the default timing mode; confirm it is present.
        await expect(
            dialog.getByRole('button', { name: 'Start Now' }),
            'Start Now should be the default timing mode',
        ).toBeVisible();

        const hours = Math.floor(options.minutes / 60);
        const mins = options.minutes % 60;
        if (hours > 0) {
            await dialog.getByLabel('Hours').fill(String(hours));
        }
        if (mins > 0) {
            await dialog.getByLabel('Minutes').fill(String(mins));
        }

        await dialog.getByLabel('Reason').fill(options.reason);

        await dialog
            .getByRole('button', { name: 'Create Blackout' })
            .click();

        // The create dialog closes on success; the management dialog stays
        // open and now lists the new blackout in its Active section.
        await expect(
            dialog,
            'Start blackout dialog should close after creation',
        ).toBeHidden({ timeout: 15_000 });
    }

    // ---------------------------------------------------------------
    // Stop / Delete
    // ---------------------------------------------------------------

    /**
     * Stop the active blackout through the management dialog's Active
     * section. Requires the management dialog to be open. Waits until the
     * Active section clears (no more "Stop" buttons), confirming the GUI
     * has processed the stop and refetched.
     */
    async stopActive(): Promise<void> {
        const stop = this.managementDialog.getByRole('button', {
            name: 'Stop',
        });
        await expect(
            stop.first(),
            'An active blackout with a Stop button should be listed',
        ).toBeVisible({ timeout: 10_000 });
        await stop.first().click();
        await expect(
            stop,
            'The Active section should clear after stopping the blackout',
        ).toHaveCount(0, { timeout: 15_000 });
    }

    /**
     * Delete the first non-active blackout through the management dialog.
     * Requires the management dialog to be open with a non-active blackout
     * present (Stop it first if it is active). Clicks the row's delete icon
     * button (Tooltip "Delete blackout") and confirms in the
     * DeleteConfirmationDialog.
     */
    async deleteFirstInactive(): Promise<void> {
        const deleteButton = this.managementDialog
            .locator('button')
            .filter({
                has: this.page.locator('[data-testid="DeleteIcon"]'),
            })
            .first();
        await expect(
            deleteButton,
            'A non-active blackout with a delete button should be listed',
        ).toBeVisible({ timeout: 10_000 });
        await deleteButton.click();

        const confirm = this.deleteConfirmDialog;
        await expect(
            confirm,
            'Delete confirmation dialog should open',
        ).toBeVisible({ timeout: 10_000 });
        await confirm.getByRole('button', { name: /^delete$/i }).click();
        await expect(
            confirm,
            'Delete confirmation dialog should close after confirming',
        ).toBeHidden({ timeout: 15_000 });
    }

    // ---------------------------------------------------------------
    // Indicator waits (bounded; BlackoutContext refetches every ~30s)
    // ---------------------------------------------------------------

    /**
     * Assert the blackout indicator is visible on the given server row
     * within a bounded wait. After a GUI create the BlackoutContext
     * refetches immediately, so the overlay appears within a few refresh
     * cycles.
     */
    async serverBlackoutIndicatorVisible(
        serverName: string,
        timeout: number = 45_000,
    ): Promise<void> {
        await this.ensureServerRowVisible(serverName);
        await expect(
            this.serverBlackoutIndicator(serverName),
            `Blackout indicator should be visible on the "${serverName}" row`,
        ).toBeVisible({ timeout });
    }

    /**
     * Assert the blackout indicator has cleared from the given server row.
     * The BlackoutContext refetches every ~30s, so a stopped/expired
     * blackout's overlay clears within roughly that window.
     */
    async waitForServerBlackoutIndicatorGone(
        serverName: string,
        timeout: number = 60_000,
    ): Promise<void> {
        await this.ensureServerRowVisible(serverName);
        await expect(
            this.serverBlackoutIndicator(serverName),
            `Blackout indicator should clear from the "${serverName}" row`,
        ).toBeHidden({ timeout });
    }
}
