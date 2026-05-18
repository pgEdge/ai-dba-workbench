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
 * Page object encapsulating all interactions with the Groups section
 * of the AdminPanel. Covers the groups table, the create-group
 * dialog, the edit-group dialog, the delete confirmation dialog,
 * and the member management expanded panel.
 *
 * Selector strategy:
 * - "Create Group" button: role=button, name "Create Group"
 *   (from AdminGroups.tsx)
 * - Create dialog title: heading "Create group"
 *   (from AdminGroups.tsx DialogTitle)
 * - Form fields: getByRole('textbox') scoped to innerDialog
 * - Edit/Delete row buttons: aria-label "edit group" / "delete group"
 *   (from AdminGroups.tsx IconButton)
 * - Edit dialog title: heading "Edit group"
 * - Edit dialog save button: role=button, name "Save"
 * - Delete confirmation: uses DeleteConfirmationDialog with a
 *   "Delete" button
 * - Row expand: clicking the TableRow triggers onClick handler
 * - Add Member button: role=button, name "Add Member" in expanded
 *   panel
 * - Add member dialog title: heading "Add member"
 * - Select User: TextField with select prop, label "Select User"
 * - Remove member: aria-label "remove member" scoped to ListItem
 */
export class GroupManagementPage extends BasePage {
    // ---------------------------------------------------------------
    // Groups table locators
    // ---------------------------------------------------------------

    /** The "Create Group" button above the groups table. */
    get createGroupButton(): Locator {
        return this.page.getByRole('button', {
            name: /create group/i,
        });
    }

    // ---------------------------------------------------------------
    // Assertions
    // ---------------------------------------------------------------

    /**
     * Assert that a group row with the given name is visible.
     */
    async expectGroupInTable(
        name: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.getTableRow(name),
        ).toBeVisible({ timeout });
    }

    /**
     * Assert that a group row with the given name is hidden.
     */
    async expectGroupNotInTable(
        name: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.getTableRow(name),
        ).toBeHidden({ timeout });
    }

    /**
     * Assert that the description text is visible in the table row
     * matching the given group name.
     */
    async expectDescriptionInTable(
        name: string,
        description: string,
        timeout: number = 10_000,
    ): Promise<void> {
        const row = this.getTableRow(name);
        await expect(
            row.getByText(description),
        ).toBeVisible({ timeout });
    }

    // ---------------------------------------------------------------
    // Create group flow
    // ---------------------------------------------------------------

    /**
     * Open the create-group dialog by clicking "Create Group" and
     * waiting for the dialog heading to appear.
     */
    async openCreateDialog(): Promise<void> {
        await this.createGroupButton.click();
        await expect(
            this.innerDialog.getByRole('heading', {
                name: 'Create group',
            }),
        ).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Complete the create-group flow: open dialog, fill name and
     * description, and submit.
     */
    async createGroup(
        name: string,
        description: string,
    ): Promise<void> {
        await this.openCreateDialog();
        const dialog = this.innerDialog;
        await dialog.getByRole('textbox', { name: 'Name' }).fill(name);
        await dialog
            .getByRole('textbox', { name: 'Description' })
            .fill(description);
        await dialog
            .getByRole('button', { name: /^create$/i })
            .click();
        await this.waitForDialogToClose();
    }

    // ---------------------------------------------------------------
    // Edit group flow
    // ---------------------------------------------------------------

    /**
     * Click the edit button for a group row identified by name.
     */
    async clickEditGroup(name: string): Promise<void> {
        await this.clickRowAction(name, /edit group/i);
        await expect(
            this.innerDialog.getByRole('heading', {
                name: 'Edit group',
            }),
        ).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Edit a group: click edit, clear and fill name and description,
     * then save.
     */
    async editGroup(
        name: string,
        newName: string,
        newDescription: string,
    ): Promise<void> {
        await this.clickEditGroup(name);
        const dialog = this.innerDialog;
        await this.clearAndFillField('Name', newName, dialog);
        await this.clearAndFillField(
            'Description',
            newDescription,
            dialog,
        );
        await dialog
            .getByRole('button', { name: /^save$/i })
            .click();
        await this.waitForDialogToClose();
    }

    // ---------------------------------------------------------------
    // Delete group flow
    // ---------------------------------------------------------------

    /**
     * Delete a group: click the delete button and confirm via the
     * delete confirmation dialog.
     */
    async deleteGroup(name: string): Promise<void> {
        await this.clickRowAction(name, /delete group/i);
        await this.confirmDeleteDialog();
    }

    // ---------------------------------------------------------------
    // Row expansion
    // ---------------------------------------------------------------

    /**
     * Expand a group row by clicking it, then wait for the "Add
     * Member" button to appear confirming the panel has rendered.
     */
    async expandGroupRow(name: string): Promise<void> {
        await this.getTableRow(name).click();
        await expect(
            this.page.getByRole('button', { name: /add member/i }),
        ).toBeVisible({ timeout: 5_000 });
    }

    // ---------------------------------------------------------------
    // Member management
    // ---------------------------------------------------------------

    /**
     * Add a member to the currently expanded group. Opens the "Add
     * Member" dialog, selects the user from the dropdown, and clicks
     * Add.
     */
    async addMember(username: string): Promise<void> {
        await this.page
            .getByRole('button', { name: /add member/i })
            .click();
        await expect(
            this.innerDialog.getByRole('heading', {
                name: 'Add member',
            }),
        ).toBeVisible({ timeout: 5_000 });

        const dialog = this.innerDialog;

        // The "Select User" field is a MUI TextField with select
        // prop. Click to open the dropdown menu, then select the
        // option from the portal-rendered listbox.
        await dialog.getByLabel(/select user/i).click();
        await this.page
            .getByRole('option', { name: username })
            .click();

        await dialog
            .getByRole('button', { name: /^add$/i })
            .click();
        await this.waitForDialogToClose();
    }

    /**
     * Assert that a member with the given username is visible in the
     * expanded group's member list.
     */
    async expectMemberInList(
        username: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.page
                .locator('li')
                .filter({ hasText: username }),
        ).toBeVisible({ timeout });
    }

    /**
     * Assert that a member with the given username is not visible in
     * the expanded group's member list.
     */
    async expectMemberNotInList(
        username: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.page
                .locator('li')
                .filter({ hasText: username }),
        ).toBeHidden({ timeout });
    }

    /**
     * Remove a member from the currently expanded group. Locates the
     * ListItem containing the username and clicks the "remove member"
     * button within it.
     */
    async removeMember(username: string): Promise<void> {
        const memberItem = this.page
            .locator('li')
            .filter({ hasText: username });
        await memberItem
            .getByRole('button', { name: /remove member/i })
            .click();
    }
}
