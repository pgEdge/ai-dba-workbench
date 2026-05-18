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
 * Page object encapsulating all interactions with the Tokens
 * section of the AdminPanel. Covers the tokens table, the
 * create-token dialog, the "token created" success dialog,
 * the edit-token dialog, and the delete confirmation dialog.
 *
 * Selector strategy:
 * - "Create Token" button: role=button, name "Create Token"
 *   (from AdminTokenScopes.tsx)
 * - Create dialog: innerDialog (excludes full-screen Admin Panel)
 * - Owner autocomplete: getByLabel matching /owner|user/i
 * - Annotation field: getByLabel matching /annotation|name|description/i
 * - Submit: role=button, name /create|save/i
 * - Success indicator: text matching /token created|save this token/i
 *   or a table row matching the token prefix
 */
export class TokenManagementPage extends BasePage {
    // ---------------------------------------------------------------
    // Token table locators
    // ---------------------------------------------------------------

    /** The "Create Token" button above the tokens table. */
    get createTokenButton(): Locator {
        return this.page.getByRole('button', {
            name: /create token/i,
        });
    }

    // ---------------------------------------------------------------
    // Create token flow
    // ---------------------------------------------------------------

    /**
     * Open the create-token dialog and wait for the dialog heading
     * to become visible.
     */
    async openCreateDialog(): Promise<void> {
        await this.createTokenButton.click();
        await expect(
            this.innerDialog.getByRole('heading', { name: /create token/i }),
        ).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Fill the owner field in the create-token dialog. The owner
     * is an MUI Autocomplete; type the username and select the
     * matching option from the dropdown.
     */
    async selectOwner(username: string): Promise<void> {
        const dialog = this.innerDialog;
        const ownerSelect = dialog.getByLabel(/owner|user/i);
        if (await ownerSelect.isVisible().catch(() => false)) {
            await ownerSelect.fill(username);
            const option = this.page.getByRole('option', {
                name: new RegExp(username),
            });
            if (await option.isVisible().catch(() => false)) {
                await option.click();
            }
        }
    }

    /**
     * Fill the annotation / name field in the create-token dialog.
     */
    async fillAnnotation(annotation: string): Promise<void> {
        const dialog = this.innerDialog;
        const annotationField = dialog.getByLabel(
            /annotation|name|description/i,
        );
        if (await annotationField.isVisible().catch(() => false)) {
            await annotationField.fill(annotation);
        }
    }

    /**
     * Submit the create-token dialog.
     */
    async submitCreateForm(): Promise<void> {
        await this.innerDialog
            .getByRole('button', { name: /^create$/i })
            .click();
    }

    /**
     * Complete the create-token flow: open the dialog, select the
     * owner, fill the annotation, and submit.
     */
    async createToken(
        ownerUsername: string,
        annotation: string,
    ): Promise<void> {
        await this.openCreateDialog();
        await this.selectOwner(ownerUsername);
        await this.fillAnnotation(annotation);
        await this.submitCreateForm();
    }

    // ---------------------------------------------------------------
    // Scope selection helpers
    // ---------------------------------------------------------------

    /** Select an MCP privilege in an open create or edit dialog. */
    async selectMcpPrivilege(label: string): Promise<void> {
        await this.innerDialog
            .getByRole('combobox', { name: /allowed mcp privileges/i })
            .click();
        await this.page.getByRole('option', { name: label }).click();
    }

    /** Select an admin permission in an open create or edit dialog. */
    async selectAdminPermission(label: string): Promise<void> {
        await this.innerDialog
            .getByRole('combobox', { name: /allowed admin permissions/i })
            .click();
        await this.page.getByRole('option', { name: label }).click();
    }

    // ---------------------------------------------------------------
    // Token created dialog
    // ---------------------------------------------------------------

    /** Close the "Token created" success dialog. */
    async closeCreatedTokenDialog(): Promise<void> {
        await this.innerDialog
            .getByRole('button', { name: /^close$/i })
            .click();
        await this.waitForDialogToClose();
    }

    // ---------------------------------------------------------------
    // Edit token flow
    // ---------------------------------------------------------------

    /** Click the edit button for a specific token row. */
    async clickEditToken(annotation: string): Promise<void> {
        const row = this.page.getByRole('row', { name: new RegExp(annotation) });
        await row.getByRole('button', { name: /edit token/i }).click();
        await expect(
            this.innerDialog.getByRole('heading', { name: /edit token/i }),
        ).toBeVisible({ timeout: 5_000 });
    }

    /** Click Save in the open edit-token dialog and wait for it to close. */
    async saveEditDialog(): Promise<void> {
        await this.innerDialog
            .getByRole('button', { name: /^save$/i })
            .click();
        await this.waitForDialogToClose();
    }

    // ---------------------------------------------------------------
    // Delete token flow
    // ---------------------------------------------------------------

    /** Click the delete button for a specific token row. */
    async clickDeleteToken(annotation: string): Promise<void> {
        const row = this.page.getByRole('row', { name: new RegExp(annotation) });
        await row.getByRole('button', { name: /delete token/i }).click();
    }

    // ---------------------------------------------------------------
    // Token row expansion
    // ---------------------------------------------------------------

    /**
     * Click a token row to expand it and reveal the Token Scope section.
     * Clicks the first cell (expand icon) to avoid triggering edit/delete.
     */
    async expandTokenRow(annotation: string): Promise<void> {
        const row = this.page.getByRole('row', { name: new RegExp(annotation) });
        await row.locator('td').first().click();
        await expect(this.page.getByText('Token Scope')).toBeVisible({ timeout: 5_000 });
    }

    // ---------------------------------------------------------------
    // Assertions
    // ---------------------------------------------------------------

    /** Assert that a token row with the given annotation is visible. */
    async expectTokenInTable(
        annotation: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.page.getByRole('row', { name: new RegExp(annotation) }),
        ).toBeVisible({ timeout });
    }

    /** Assert that a token row with the given annotation is not visible. */
    async expectTokenNotInTable(
        annotation: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.page.getByRole('row', { name: new RegExp(annotation) }),
        ).toBeHidden({ timeout });
    }
}
