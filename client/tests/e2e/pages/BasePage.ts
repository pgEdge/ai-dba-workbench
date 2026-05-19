/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Base page object providing common utilities shared by all page
 * objects in the E2E suite. Every page object extends this class
 * to inherit navigation, waiting, dialog, table, and toast helpers.
 */
export class BasePage {
    readonly page: Page;

    constructor(page: Page) {
        this.page = page;
    }

    // ---------------------------------------------------------------
    // Navigation
    // ---------------------------------------------------------------

    /**
     * Navigate to the given path relative to the base URL configured
     * in playwright.config.ts and wait for the page to load.
     */
    async navigate(path: string = '/'): Promise<void> {
        await this.page.goto(path);
    }

    /**
     * Wait for the application header to appear, confirming that
     * the main layout has rendered after login or page load.
     */
    async waitForAppLoad(timeout: number = 30_000): Promise<void> {
        await expect(this.page.locator('header')).toBeVisible({
            timeout,
        });
    }

    // ---------------------------------------------------------------
    // Loading indicators
    // ---------------------------------------------------------------

    /**
     * Wait for any visible progress bar (MUI CircularProgress or
     * LinearProgress) to disappear.
     */
    async waitForLoadingToFinish(
        timeout: number = 15_000,
    ): Promise<void> {
        const loader = this.page.getByRole('progressbar');
        if (await loader.isVisible().catch(() => false)) {
            await loader.waitFor({ state: 'hidden', timeout });
        }
    }

    // ---------------------------------------------------------------
    // Dialog helpers
    // ---------------------------------------------------------------

    /**
     * Locator scoped to non-fullscreen dialogs only.
     * Excludes the Admin Panel full-screen dialog wrapper so that
     * assertions targeting inner (edit/delete/create) dialogs do not
     * accidentally match the always-open Admin Panel.
     */
    protected get innerDialog(): Locator {
        return this.page.locator('[role="dialog"]:not(.MuiDialog-paperFullScreen)');
    }

    /**
     * Wait for a dialog to become visible. Returns a locator scoped
     * to the MUI Dialog paper element.
     *
     * When `fullScreen` is true the locator targets the full-screen
     * dialog variant used by the AdminPanel.
     */
    async waitForDialog(
        options: { fullScreen?: boolean; timeout?: number } = {},
    ): Promise<Locator> {
        const { fullScreen = false, timeout = 5_000 } = options;

        const dialog = fullScreen
            ? this.page.locator('.MuiDialog-paperFullScreen')
            : this.page.getByRole('dialog');

        await expect(dialog.first()).toBeVisible({ timeout });
        return dialog.first();
    }

    /**
     * Wait for a dialog to close (become hidden).
     */
    async waitForDialogToClose(
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(this.innerDialog).toBeHidden({
            timeout,
        });
    }

    /**
     * Confirm a pending deletion in the DeleteConfirmationDialog.
     * The dialog renders a "Delete" button with `color="error"`.
     */
    async confirmDeleteDialog(
        timeout: number = 10_000,
    ): Promise<void> {
        const dialog = this.innerDialog;
        await expect(dialog).toBeVisible({ timeout: 5_000 });
        await dialog
            .getByRole('button', { name: /^delete$/i })
            .click();
        await expect(dialog).toBeHidden({ timeout });
    }

    // ---------------------------------------------------------------
    // Table helpers
    // ---------------------------------------------------------------

    /**
     * Wait for a data table to appear on the page.
     */
    async waitForTable(timeout: number = 10_000): Promise<Locator> {
        const table = this.page
            .getByRole('table')
            .or(this.page.locator('[role="grid"]'));
        await expect(table).toBeVisible({ timeout });
        return table;
    }

    /**
     * Locate a table row that contains the given text.
     */
    getTableRow(text: string | RegExp): Locator {
        return this.page.getByRole('row', {
            name: typeof text === 'string' ? new RegExp(text) : text,
        });
    }

    /**
     * Click an action button within a specific table row.
     */
    async clickRowAction(
        rowText: string | RegExp,
        actionName: RegExp,
    ): Promise<void> {
        const row = this.getTableRow(rowText);
        await row.getByRole('button', { name: actionName }).click();
    }

    // ---------------------------------------------------------------
    // Toast / Notification helpers
    // ---------------------------------------------------------------

    /**
     * Wait for a toast notification (MUI Snackbar / Alert) matching
     * the given text pattern to appear.
     */
    async waitForToast(
        textPattern: RegExp,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.page.getByText(textPattern).first(),
        ).toBeVisible({ timeout });
    }

    // ---------------------------------------------------------------
    // Form helpers
    // ---------------------------------------------------------------

    /**
     * Fill a form field identified by its label.
     */
    async fillField(
        label: string | RegExp,
        value: string,
        scope?: Locator,
    ): Promise<void> {
        const container = scope ?? this.page;
        await container.getByLabel(label).fill(value);
    }

    /**
     * Clear and fill a form field identified by its label.
     */
    async clearAndFillField(
        label: string | RegExp,
        value: string,
        scope?: Locator,
    ): Promise<void> {
        const field = (scope ?? this.page).getByLabel(label);
        await field.clear();
        await field.fill(value);
    }
}
