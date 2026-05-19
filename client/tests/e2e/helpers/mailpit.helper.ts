/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// ---------------------------------------------------------------
// Types
// ---------------------------------------------------------------

interface MailpitAddress {
    Name: string;
    Address: string;
}

export interface MailpitMessage {
    ID: string;
    From: MailpitAddress;
    To: MailpitAddress[];
    Subject: string;
    Date: string;
}

interface MailpitMessagesResponse {
    total: number;
    messages: MailpitMessage[];
}

// ---------------------------------------------------------------
// MailpitHelper
// ---------------------------------------------------------------

/**
 * Thin fetch-based wrapper around the Mailpit REST API for
 * verifying email delivery in E2E tests.
 *
 * Mailpit captures SMTP traffic and exposes a JSON API for
 * querying received messages, enabling assertions on email
 * content without a real mail server.
 */
export class MailpitHelper {
    private readonly baseUrl: string;

    constructor(baseUrl?: string) {
        this.baseUrl = (
            baseUrl ?? process.env.MAILPIT_URL ?? 'http://mailpit:8025'
        ).replace(/\/+$/, '');
    }

    /**
     * Fetch all messages currently held by Mailpit.
     */
    async getMessages(): Promise<MailpitMessage[]> {
        const res = await fetch(`${this.baseUrl}/api/v1/messages`);
        if (!res.ok) {
            throw new Error(
                `Mailpit GET /api/v1/messages failed: ${res.status}`,
            );
        }
        const data = (await res.json()) as MailpitMessagesResponse;
        return data.messages ?? [];
    }

    /**
     * Return the most recent message, or null when the inbox
     * is empty.
     */
    async getLatestMessage(): Promise<MailpitMessage | null> {
        const messages = await this.getMessages();
        return messages.length > 0 ? messages[0] : null;
    }

    /**
     * Find the first message addressed to the given email.
     */
    async findMessageByRecipient(
        email: string,
    ): Promise<MailpitMessage | null> {
        const messages = await this.getMessages();
        return (
            messages.find((m) =>
                m.To.some(
                    (addr) =>
                        addr.Address.toLowerCase() === email.toLowerCase(),
                ),
            ) ?? null
        );
    }

    /**
     * Delete all messages from the Mailpit inbox.
     */
    async deleteAllMessages(): Promise<void> {
        const res = await fetch(`${this.baseUrl}/api/v1/messages`, {
            method: 'DELETE',
        });
        if (!res.ok) {
            throw new Error(
                `Mailpit DELETE /api/v1/messages failed: ${res.status}`,
            );
        }
    }

    /**
     * Poll Mailpit until a message addressed to `recipientEmail`
     * appears, or throw after `timeout` milliseconds.
     */
    async waitForMessage(
        recipientEmail: string,
        timeout: number = 30_000,
    ): Promise<MailpitMessage> {
        const start = Date.now();
        while (Date.now() - start < timeout) {
            const msg = await this.findMessageByRecipient(recipientEmail);
            if (msg) {
                return msg;
            }
            await new Promise((r) => setTimeout(r, 500));
        }
        throw new Error(
            `Mailpit: no message for ${recipientEmail} within ${timeout}ms`,
        );
    }
}
