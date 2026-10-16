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
 *
 * RBAC denials are NOT asserted here by matching refusal wording (the
 * LLM rewords its refusal every run). The specs assert the authorization
 * boundary at the API/tool layer and use these readers only for a
 * secondary, data-keyed no-leak check (see rbac-chat.helper.ts).
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
     * Non-throwing variant of {@link waitForResponse}: returns `true` when
     * the response settled (the input re-enabled) and `false` on timeout,
     * instead of throwing. Used by the provider-limit-aware chat helper to
     * decide between skipping (an environmental AI provider limit) and
     * failing (a genuine hang) without swallowing the latter.
     *
     * @param timeout - Maximum time to wait in milliseconds.
     */
    async didResponseSettle(timeout: number = 60_000): Promise<boolean> {
        await this.page.waitForTimeout(500);
        try {
            await expect(this.chatInput).toBeEnabled({ timeout });
            return true;
        } catch {
            return false;
        }
    }

    /**
     * Any error text currently visible for the last turn: the panel
     * transcript (a failed turn renders an error bubble in it) folded
     * together with any MUI Alert/snackbar (`role="alert"`). Used to
     * classify a stuck response as an AI provider usage/rate limit
     * (environmental) versus a genuine failure. Returns '' when nothing is
     * present.
     */
    async responseErrorText(): Promise<string> {
        const parts: string[] = [];
        const transcript = await this.readTranscript();
        if (transcript.length > 0) {
            parts.push(transcript);
        }
        const alerts = this.page.getByRole('alert');
        const count = await alerts.count().catch(() => 0);
        for (let i = 0; i < count; i++) {
            const text = await alerts
                .nth(i)
                .innerText()
                .catch(() => '');
            if (text.length > 0) {
                parts.push(text);
            }
        }
        return parts.join('\n');
    }

    /**
     * Get the assistant's latest reply text.
     *
     * The app puts no role or test id on individual chat message rows —
     * the bubbles are styled with MUI `sx`, which compiles to hashed CSS
     * classes, not inline styles — so there is no stable per-row selector
     * to isolate a single assistant bubble without touching product code.
     * This reader therefore returns the visible transcript of the whole
     * chat panel (every rendered message), anchored on the panel's stable
     * `role="complementary"` / `aria-label="AI Chat Panel"` (index.tsx).
     *
     * For the RBAC no-leak check that is a feature, not a compromise:
     * scanning the entire transcript catches a forbidden connection
     * disclosed in ANY assistant turn, and the generic test prompts never
     * contain another connection's host:port, so folding in the user's own
     * messages and the static panel chrome cannot cause a false positive
     * (see rbac-chat.helper.ts).
     *
     * @param timeout - Maximum time to wait for the response to settle.
     */
    async getLastAssistantMessage(
        timeout: number = 30_000,
    ): Promise<string> {
        await this.waitForResponse(timeout);
        // Allow the DOM to settle after the response completes.
        await this.page.waitForTimeout(300);

        const text = await this.readTranscript();
        if (text.length === 0) {
            throw new Error('No chat transcript found in the chat panel');
        }
        return text;
    }

    /**
     * Alias of {@link getLastAssistantMessage}. The panel transcript read
     * already includes any Markdown results table rendered in an assistant
     * bubble (tables are wrapped in a `[data-testid="chat-table-container"]`
     * scroll box whose text `innerText` folds in), so no separate table
     * pass is needed. Retained as a distinct name for caller intent.
     *
     * @param timeout - Maximum time to wait for a message to appear.
     */
    async getLastAssistantMessageWithTables(
        timeout: number = 30_000,
    ): Promise<string> {
        return this.getLastAssistantMessage(timeout);
    }

    /**
     * Best-effort variant of {@link getLastAssistantMessage} that returns
     * `null` instead of throwing when no transcript can be read.
     *
     * A denied turn with no server selected can unmount the chat input
     * (so the response never "re-enables") and even the message list, so
     * this reader never throws: it swallows a `waitForResponse` timeout,
     * then returns the panel transcript if any is present or `null`
     * otherwise. Callers use it for the secondary chat no-leak check; the
     * authoritative RBAC proof is the API/tool-layer denial.
     *
     * @param timeout - Maximum time to wait for the response to settle.
     */
    async tryGetLastAssistantMessage(
        timeout: number = 30_000,
    ): Promise<string | null> {
        try {
            await this.waitForResponse(timeout);
        } catch {
            // The input never re-enabled (e.g. it unmounted). Fall
            // through and read whatever messages are already present.
        }
        // Allow the DOM to settle after the response completes.
        await this.page.waitForTimeout(300);

        try {
            const text = await this.readTranscript();
            return text.length > 0 ? text : null;
        } catch {
            return null;
        }
    }

    // ---------------------------------------------------------------
    // Internal helpers
    // ---------------------------------------------------------------

    /** The chat panel root (stable product hook: role=complementary). */
    private get chatPanel(): Locator {
        return this.page.getByRole('complementary', {
            name: /AI Chat Panel/i,
        });
    }

    /**
     * Read the visible text of the whole chat panel, or `''` when the
     * panel is not present (e.g. a denied turn unmounted it).
     */
    private async readTranscript(): Promise<string> {
        if ((await this.chatPanel.count()) === 0) {
            return '';
        }
        return (await this.chatPanel.innerText()).trim();
    }
}
