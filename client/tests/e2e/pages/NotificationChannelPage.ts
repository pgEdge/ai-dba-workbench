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
 * Page object for all four notification channel types: Email,
 * Slack, Mattermost, and Webhook. Encapsulates CRUD operations,
 * form interactions, and table assertions within the AdminPanel.
 *
 * Selector strategy:
 * - "Add Channel" button: role=button, name "Add Channel"
 * - Channel dialog: innerDialog (non-fullscreen [role="dialog"])
 * - Form fields: getByLabel scoped to innerDialog
 * - Tab navigation: role=tab, name matching tab label
 * - Save/Create button: role=button, name "Save" or "Create"
 * - Edit row button: aria-label "edit channel"
 * - Delete row button: aria-label "delete channel"
 * - Test email button: aria-label "send test email"
 * - Test notification button: aria-label "send test notification"
 * - Email recipients: placeholder-based text fields in add row
 *
 * Cross-browser notes:
 * - MUI TextField-select renders a Portal-based Menu. Option
 *   selection uses page.getByRole('option') since MUI renders
 *   the listbox outside the dialog DOM.
 */
export class NotificationChannelPage extends BasePage {

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Wait for any MUI Select/Menu invisible backdrop to clear.
     */
    private async waitForSelectBackdropGone(): Promise<void> {
        await this.page
            .locator('.MuiBackdrop-invisible')
            .waitFor({ state: 'hidden', timeout: 5_000 })
            .catch(() => {});
    }

    // ---------------------------------------------------------------
    // Common channel operations
    // ---------------------------------------------------------------

    /**
     * Click the "Add Channel" button and wait for the create
     * dialog to appear.
     */
    async clickAddChannel(): Promise<void> {
        await this.page
            .getByRole('button', { name: /add channel/i })
            .click();
        await this.waitForChannelDialog();
    }

    /**
     * Wait for the channel create/edit dialog to become visible.
     */
    async waitForChannelDialog(): Promise<void> {
        await expect(this.innerDialog).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Fill the channel "Name" field inside the dialog.
     */
    async fillChannelName(name: string): Promise<void> {
        // getByRole with exact name uses ARIA accessible name matching
        // which excludes the MUI required-field asterisk (* aria-hidden).
        // getByLabel('Name', exact) fails because the raw label DOM text
        // is "Name *"; getByLabel without exact hits 'SMTP Username' and
        // 'From Name' too. Role-based exact match is the only safe option.
        await this.innerDialog
            .getByRole('textbox', { name: 'Name', exact: true })
            .fill(name);
    }

    /**
     * Fill the channel "Description" field inside the dialog.
     */
    async fillChannelDescription(desc: string): Promise<void> {
        await this.fillField('Description', desc, this.innerDialog);
    }

    /**
     * Click the Save or Create button inside the dialog and wait
     * for the dialog to close.
     */
    async saveChannel(): Promise<void> {
        await this.waitForSelectBackdropGone();
        const saveBtn = this.innerDialog.getByRole('button', {
            name: /^(save|create)$/i,
        });
        await saveBtn.click();
        await this.waitForDialogToClose();
    }

    /**
     * Click the edit button for a channel row identified by name.
     */
    async clickEditChannel(name: string): Promise<void> {
        await this.clickRowAction(name, /edit channel/i);
        await this.waitForChannelDialog();
    }

    /**
     * Click the test notification button for a channel row and
     * wait for the test to complete (loading spinner disappears).
     *
     * Email channels use "send test email"; other channel types
     * use "send test notification". This method tries both.
     */
    async clickTestChannel(name: string): Promise<void> {
        const row = this.getTableRow(name);
        const testBtn = row.getByRole('button', {
            name: /send test (email|notification)/i,
        });
        await testBtn.click();
        // Wait for the loading spinner to disappear, indicating
        // the test request has completed.
        await expect(testBtn).toBeEnabled({ timeout: 30_000 });
    }

    /**
     * Click the delete button for a channel row, then confirm
     * deletion in the confirmation dialog.
     */
    async clickDeleteChannel(name: string): Promise<void> {
        // Move the mouse away from any action buttons to dismiss
        // lingering MUI tooltips that would intercept the click.
        await this.page.mouse.move(0, 0);
        await this.clickRowAction(name, /delete channel/i);
        await this.confirmDeleteDialog();
    }

    /**
     * Assert that a channel with the given name appears in the
     * table.
     */
    async expectChannelInTable(
        name: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.getTableRow(name),
        ).toBeVisible({ timeout });
    }

    /**
     * Assert that no channel with the given name appears in the
     * table.
     */
    async expectChannelNotInTable(
        name: string,
        timeout: number = 10_000,
    ): Promise<void> {
        await expect(
            this.getTableRow(name),
        ).toBeHidden({ timeout });
    }

    // ---------------------------------------------------------------
    // Email-specific methods
    // ---------------------------------------------------------------

    /**
     * Fill SMTP settings fields inside the email channel dialog.
     */
    async fillEmailSettings(opts: {
        smtpHost: string;
        smtpPort: string;
        fromAddress: string;
        fromName?: string;
        username?: string;
        password?: string;
        useTls?: boolean;
    }): Promise<void> {
        const dialog = this.innerDialog;
        await this.fillField('SMTP Host', opts.smtpHost, dialog);
        await this.clearAndFillField('SMTP Port', opts.smtpPort, dialog);
        await this.fillField('From Address', opts.fromAddress, dialog);

        if (opts.fromName) {
            await this.fillField('From Name', opts.fromName, dialog);
        }
        if (opts.username) {
            await this.fillField('SMTP Username', opts.username, dialog);
        }
        if (opts.password) {
            await this.fillField('SMTP Password', opts.password, dialog);
        }
        if (opts.useTls === false) {
            // The TLS switch defaults to true; uncheck it if false.
            const tlsSwitch = dialog.locator(
                '[aria-label="Toggle use TLS"]',
            );
            const isChecked = await tlsSwitch.isChecked();
            if (isChecked) {
                await tlsSwitch.uncheck();
            }
        }
    }

    /**
     * Switch to the "Recipients" tab inside the email channel
     * dialog.
     */
    async switchToRecipientsTab(): Promise<void> {
        await this.innerDialog
            .getByRole('tab', { name: /recipients/i })
            .click();
    }

    /**
     * Add an email recipient in the Recipients tab. Fills the
     * placeholder-based input fields and clicks "Add".
     */
    async addEmailRecipient(
        email: string,
        displayName?: string,
    ): Promise<void> {
        const dialog = this.innerDialog;
        await dialog
            .getByPlaceholder('Email address')
            .fill(email);
        if (displayName) {
            await dialog
                .getByPlaceholder('Display name')
                .fill(displayName);
        }
        await dialog
            .getByRole('button', { name: /^add$/i })
            .click();
    }

    /**
     * Switch back to the "Settings" tab inside the email channel
     * dialog.
     */
    async switchToSettingsTab(): Promise<void> {
        await this.innerDialog
            .getByRole('tab', { name: /settings/i })
            .click();
    }

    // ---------------------------------------------------------------
    // Slack / Mattermost methods
    // ---------------------------------------------------------------

    /**
     * Fill the "Webhook URL" field (used by both Slack and
     * Mattermost channel dialogs).
     */
    async fillWebhookUrl(url: string): Promise<void> {
        await this.fillField('Webhook URL', url, this.innerDialog);
    }

    // ---------------------------------------------------------------
    // Webhook-specific methods
    // ---------------------------------------------------------------

    /**
     * Fill the "Endpoint URL" field in the webhook channel dialog.
     */
    async fillWebhookEndpointUrl(url: string): Promise<void> {
        await this.fillField('Endpoint URL', url, this.innerDialog);
    }

    /**
     * Switch to a specific tab in the webhook channel dialog.
     */
    async switchToWebhookTab(
        tabName: 'Settings' | 'Headers' | 'Authentication' | 'Templates',
    ): Promise<void> {
        await this.innerDialog
            .getByRole('tab', { name: new RegExp(`^${tabName}$`, 'i') })
            .click();
    }

    // ---------------------------------------------------------------
    // Assertions
    // ---------------------------------------------------------------

    /**
     * Wait for any success-level toast notification to appear.
     */
    async expectSuccessToast(): Promise<void> {
        await this.waitForToast(/successfully/i);
    }

    /**
     * Wait for success feedback after a test notification send.
     */
    async expectTestNotificationSuccess(): Promise<void> {
        await this.waitForToast(/test.*sent successfully/i);
    }
}
