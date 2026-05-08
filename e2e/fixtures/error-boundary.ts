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
    errors: string[];
}

export interface ErrorBoundaryFixtures {
    consoleSink: ConsoleSink;
    assertNoErrorBoundary: () => Promise<void>;
    assertNoConsoleErrors: () => void;
}

const wireConsole = (page: Page, sink: ConsoleSink) => {
    page.on('console', (msg) => {
        if (msg.type() === 'error') {
            sink.errors.push(`[console.error] ${msg.text()}`);
        }
    });
    page.on('pageerror', (err) => {
        sink.errors.push(`[pageerror] ${err.message}`);
    });
};

export const test = base.extend<ErrorBoundaryFixtures>({
    consoleSink: async ({ page }, use) => {
        const sink: ConsoleSink = { errors: [] };
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
            expect(
                consoleSink.errors,
                `Unexpected console errors:\n${consoleSink.errors.join('\n')}`,
            ).toEqual([]);
        });
    },
});

export { expect };
