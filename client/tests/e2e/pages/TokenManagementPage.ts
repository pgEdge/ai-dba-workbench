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
 * create-token dialog, and the "token created" success dialog.
 *
 * Selector strategy:
 * - "Create Token" button: role=button, name "Create Token"
 *   (from AdminTokenScopes.tsx)
 * - Create dialog: role=dialog (MUI Dialog)
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
     * Open the create-token dialog and wait for the dialog to
     * become visible.
     */
    async openCreateDialog(): Promise<void> {
        await this.createTokenButton.click();
        await this.waitForDialog();
    }

    /**
     * Fill the owner field in the create-token dialog. The owner
     * is an MUI Autocomplete; type the username and select the
     * matching option from the dropdown.
     */
    async selectOwner(username: string): Promise<void> {
        const dialog = this.page.getByRole('dialog');
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
        const dialog = this.page.getByRole('dialog');
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
        const dialog = this.page.getByRole('dialog');
        await dialog
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
    // Assertions
    // ---------------------------------------------------------------

    /**
     * Assert that the token creation succeeded by checking for the
     * success dialog text or the token row in the table.
     */
    async expectTokenCreated(
        identifierPattern: RegExp,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.page
                .getByText(/token created|save this token/i)
                .or(
                    this.page.getByRole('row', {
                        name: identifierPattern,
                    }),
                )
                .first(),
        ).toBeVisible({ timeout });
    }
}
