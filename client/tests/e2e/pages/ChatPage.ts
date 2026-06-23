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
 * Page object for the Ask Ellie chat panel. Encapsulates opening
 * and closing the chat panel, sending messages, waiting for
 * responses, and asserting on assistant message content.
 *
 * Selector strategy:
 * - FAB: `data-testid="chat-fab"` / `aria-label="open chat"` or
 *   `aria-label="close chat"` (from ChatFAB.tsx)
 * - Chat input: `aria-label="Chat message input"` (from ChatInput.tsx)
 * - Send button: `aria-label="Send message"` (from ChatInput.tsx)
 * - Error responses: detected by regex on message text content
 */
export class ChatPage extends BasePage {
    // ---------------------------------------------------------------
    // Locators
    // ---------------------------------------------------------------

    /** The floating action button that toggles the chat panel. */
    get chatFab(): Locator {
        return this.page.getByTestId('chat-fab');
    }

    /** The chat message textarea input. */
    get chatInput(): Locator {
        return this.page.getByRole('textbox', { name: /ask ellie a question/i });
    }

    /** The send message button. */
    get sendButton(): Locator {
        return this.page.getByRole('button', { name: /send message/i });
    }

    // ---------------------------------------------------------------
    // Actions
    // ---------------------------------------------------------------

    /**
     * Open the chat panel by clicking the FAB. Waits for the chat
     * input to become visible, confirming the panel has rendered.
     */
    async openChat(): Promise<void> {
        // Only open if the chat input is not already visible.
        const inputVisible = await this.chatInput
            .isVisible()
            .catch(() => false);
        if (inputVisible) {
            return;
        }

        // Allow extra time — the FAB only renders once the server
        // detail panel loads after a server is selected.
        await expect(this.chatFab).toBeVisible({ timeout: 30_000 });
        await this.chatFab.click();
        await expect(this.chatInput).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Close the chat panel by clicking the FAB. Waits for the chat
     * input to become hidden, confirming the panel has closed.
     */
    async closeChat(): Promise<void> {
        // Only close if the chat input is currently visible.
        const inputVisible = await this.chatInput
            .isVisible()
            .catch(() => false);
        if (!inputVisible) {
            return;
        }

        await this.chatFab.click();
        await expect(this.chatInput).toBeHidden({ timeout: 5_000 });
    }

    /**
     * Type a message into the chat input and click the Send button.
     * Does NOT wait for the response; call `waitForResponse` after.
     */
    async sendMessage(text: string): Promise<void> {
        await expect(this.chatInput).toBeVisible({ timeout: 5_000 });
        await expect(this.chatInput).toBeEnabled({ timeout: 5_000 });
        await this.chatInput.fill(text);
        await expect(this.sendButton).toBeEnabled({ timeout: 5_000 });
        await this.sendButton.click();
    }

    /**
     * Wait for the LLM response to complete. The chat input is
     * disabled while the assistant is streaming; this method waits
     * until the input re-enables, indicating the response is done.
     *
     * @param timeout - Maximum time to wait in milliseconds.
     */
    async waitForResponse(timeout: number = 60_000): Promise<void> {
        // Wait briefly for the disabled state to take effect after
        // the message is sent, then wait for re-enablement which
        // signals the response has finished streaming.
        await this.page.waitForTimeout(500);
        await expect(this.chatInput).toBeEnabled({ timeout });
    }

    /**
     * Get the text content of the last assistant message bubble.
     * After the response completes, extracts visible text from the
     * chat panel area, excluding the input region.
     *
     * @param timeout - Maximum time to wait for a message to appear.
     */
    async getLastAssistantMessage(
        timeout: number = 30_000,
    ): Promise<string> {
        // Wait for the response to finish streaming.
        await this.waitForResponse(timeout);

        // Allow the DOM to settle after the response completes.
        await this.page.waitForTimeout(300);

        // Assistant messages are rendered with align-items: flex-start
        // inside the chat panel. Locate all such containers and
        // return the text of the last one.
        const assistantContainers = this.page.locator(
            '[style*="align-items: flex-start"], ' +
            '[style*="align-items:flex-start"]',
        );

        const count = await assistantContainers.count();
        if (count === 0) {
            throw new Error(
                'No assistant messages found in the chat panel',
            );
        }

        const lastText = await assistantContainers
            .nth(count - 1)
            .innerText();

        return lastText.trim();
    }

    // ---------------------------------------------------------------
    // Assertions
    // ---------------------------------------------------------------

    /**
     * Assert that the assistant's response contains an authorization
     * or permission error. Checks for common error keywords in the
     * page content after the response has completed.
     *
     * @param timeout - Maximum time to wait for error text to appear.
     */
    async expectErrorResponse(timeout: number = 10_000): Promise<void> {
        const errorPattern =
            /the model attempted to call|permission|authorized|forbidden|access denied|not allowed|403|401|unauthorized|error|denied|cannot|unable/i;

        await expect(async () => {
            const pageContent = await this.page.content();
            expect(
                pageContent,
                'Expected an authorization/permission error in the chat response',
            ).toMatch(errorPattern);
        }).toPass({ timeout, intervals: [1_000] });
    }
}
