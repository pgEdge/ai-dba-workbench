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
 * Page object encapsulating all interactions with the Users section
 * of the AdminPanel. Covers the users table, the create-user
 * dialog, the edit-user dialog, and the delete confirmation dialog.
 *
 * Selector strategy:
 * - "Create User" button: role=button, name "Create User"
 *   (from AdminUsers.tsx)
 * - Create dialog title: heading "Create user"
 *   (from AdminUsers.tsx DialogTitle)
 * - Form fields: getByLabel matching MUI TextField labels
 * - Edit/Delete row buttons: aria-label "edit user" / "delete user"
 *   (from AdminUsers.tsx IconButton)
 * - Edit dialog title: heading containing "Edit user:"
 * - Edit dialog save button: role=button, name "Save"
 * - Delete confirmation: uses DeleteConfirmationDialog with a
 *   "Delete" button
 */
export class UserManagementPage extends BasePage {
    // ---------------------------------------------------------------
    // Users table locators
    // ---------------------------------------------------------------

    /** The "Create User" button above the users table. */
    get createUserButton(): Locator {
        return this.page.getByRole('button', { name: /create user/i });
    }

    // ---------------------------------------------------------------
    // Table actions
    // ---------------------------------------------------------------

    /**
     * Wait for the users table to finish loading and render.
     */
    async waitForUsersTable(): Promise<void> {
        await this.waitForLoadingToFinish();
        await this.waitForTable();
    }

    /**
     * Assert that a user row with the given username is visible.
     */
    async expectUserInTable(
        username: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.getTableRow(username),
        ).toBeVisible({ timeout });
    }

    /**
     * Assert that a user row with the given username is hidden.
     */
    async expectUserNotInTable(
        username: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.getTableRow(username),
        ).toBeHidden({ timeout });
    }

    // ---------------------------------------------------------------
    // Create user flow
    // ---------------------------------------------------------------

    /**
     * Open the create-user dialog by clicking "Create User" and
     * waiting for the dialog heading to appear.
     */
    async openCreateDialog(): Promise<void> {
        await this.createUserButton.click();
        await expect(
            this.page.getByRole('heading', { name: 'Create user' }),
        ).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Fill the create-user dialog form fields.
     */
    async fillCreateForm(
        username: string,
        password: string,
        displayName?: string,
    ): Promise<void> {
        // The create dialog renders inside a MUI Dialog; scope to
        // the dialog so label matches are unambiguous.
        const dialog = this.page.getByRole('dialog');

        await dialog.getByLabel('Username').fill(username);
        await dialog.getByLabel('Password').fill(password);
        if (displayName) {
            await dialog.getByLabel('Display Name').fill(displayName);
        }
    }

    /**
     * Submit the create-user dialog by clicking the "Create" button.
     */
    async submitCreateForm(): Promise<void> {
        const dialog = this.page.getByRole('dialog');
        await dialog
            .getByRole('button', { name: /^create$/i })
            .click();
    }

    /**
     * Complete the create-user flow: open dialog, fill, and submit.
     */
    async createUser(
        username: string,
        password: string,
        displayName?: string,
    ): Promise<void> {
        await this.openCreateDialog();
        await this.fillCreateForm(username, password, displayName);
        await this.submitCreateForm();
    }

    // ---------------------------------------------------------------
    // Edit user flow
    // ---------------------------------------------------------------

    /**
     * Click the edit button for a user row identified by username.
     */
    async clickEditUser(username: string): Promise<void> {
        await this.clickRowAction(username, /edit user/i);
    }

    /**
     * Wait for the edit-user dialog to appear and return a locator
     * scoped to the dialog.
     */
    async waitForEditDialog(): Promise<Locator> {
        const dialog = this.page.getByRole('dialog');
        await expect(dialog).toBeVisible({ timeout: 5_000 });
        return dialog;
    }

    /**
     * Update the display name in an open edit-user dialog and save.
     */
    async updateDisplayName(newName: string): Promise<void> {
        const dialog = await this.waitForEditDialog();
        await this.clearAndFillField('Display Name', newName, dialog);
        await dialog
            .getByRole('button', { name: /^save$/i })
            .click();
        await expect(dialog).toBeHidden({ timeout: 10_000 });
    }

    // ---------------------------------------------------------------
    // Delete user flow
    // ---------------------------------------------------------------

    /**
     * Click the delete button for a user row identified by username.
     */
    async clickDeleteUser(username: string): Promise<void> {
        await this.clickRowAction(username, /delete user/i);
    }

    /**
     * Confirm a pending user deletion via the delete confirmation
     * dialog. The DeleteConfirmationDialog renders a "Delete"
     * button.
     */
    async confirmDelete(): Promise<void> {
        await this.confirmDeleteDialog();
    }

    /**
     * Delete a user: click the delete button and confirm.
     */
    async deleteUser(username: string): Promise<void> {
        await this.clickDeleteUser(username);
        await this.confirmDelete();
    }
}
