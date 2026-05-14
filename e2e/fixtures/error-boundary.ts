/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Playwright shared fixtures
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { test as base, expect, type Page } from '@playwright/test';

export interface ConsoleSink {
    /** Console-error / pageerror messages, used by assertNoConsoleErrors. */
    errors: string[];
    /** All non-2xx HTTP responses, attached to assertion messages for
     *  diagnostic context. Not part of the assertion itself, because
     *  some flows (e.g. rejected logins) legitimately produce 4xx
     *  responses without surfacing a console.error. */
    httpErrors: string[];
}

export interface ErrorBoundaryFixtures {
    consoleSink: ConsoleSink;
    assertNoErrorBoundary: () => Promise<void>;
    assertNoConsoleErrors: () => void;
}

// Browsers emit a generic console.error for every non-2xx fetch
// response: "Failed to load resource: the server responded with a
// status of N (...)". This message has no URL and no stack, and the
// app's React code already turns real failures into ApiError objects
// or pageerrors. Filtering this noise lets `assertNoConsoleErrors`
// focus on actual JS-level problems.
const isResourceLoadNoise = (text: string): boolean => {
    return /^Failed to load resource:\s+the server responded with a status of \d+/i
        .test(text);
};

const wireConsole = (page: Page, sink: ConsoleSink) => {
    page.on('console', (msg) => {
        if (msg.type() !== 'error') {
            return;
        }
        const text = msg.text();
        if (isResourceLoadNoise(text)) {
            return;
        }
        sink.errors.push(`[console.error] ${text}`);
    });
    page.on('pageerror', (err) => {
        sink.errors.push(`[pageerror] ${err.message}`);
    });
    // Track non-2xx HTTP responses so failure diagnostics can pinpoint
    // which endpoint generated the generic "Failed to load resource"
    // console.error. We do NOT push these into `errors` because some
    // flows (e.g. rejected login or first-time-user "no current
    // connection" 404s) legitimately return 4xx without surfacing a
    // real console error.
    page.on('response', (response) => {
        const status = response.status();
        if (status >= 400) {
            sink.httpErrors.push(
                `[http ${status}] ${response.request().method()} ${response.url()}`,
            );
        }
    });
};

export const test = base.extend<ErrorBoundaryFixtures>({
    consoleSink: async ({ page }, use) => {
        const sink: ConsoleSink = { errors: [], httpErrors: [] };
        wireConsole(page, sink);
        await use(sink);
    },
    assertNoErrorBoundary: async ({ page }, use) => {
        await use(async () => {
            const fallback = page.locator(
                '[data-testid="error-boundary-fallback"]',
            );
            await expect(fallback, 'ErrorBoundary fallback should not be visible')
                .toHaveCount(0);
        });
    },
    assertNoConsoleErrors: async ({ consoleSink }, use) => {
        await use(() => {
            const httpContext = consoleSink.httpErrors.length > 0
                ? `\nHTTP responses (>=400) seen during this test:\n${consoleSink.httpErrors.join('\n')}`
                : '';
            expect(
                consoleSink.errors,
                `Unexpected console errors:\n${consoleSink.errors.join('\n')}${httpContext}`,
            ).toEqual([]);
        });
    },
});

export { expect };
