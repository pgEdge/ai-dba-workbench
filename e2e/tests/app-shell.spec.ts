/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - App shell E2E
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { test, expect } from '../fixtures/error-boundary';

test.use({ storageState: '.auth/admin.json' });

test.describe('app shell', () => {
    test('renders header and cluster navigator after login', async ({
        page,
        assertNoErrorBoundary,
        assertNoConsoleErrors,
    }) => {
        await page.goto('/');
        await expect(page.getByTestId('app-header'))
            .toBeVisible({ timeout: 15_000 });
        await expect(page.getByTestId('cluster-navigator'))
            .toBeVisible();
        await assertNoErrorBoundary();
        assertNoConsoleErrors();
    });

    test('opens and closes the chat FAB without crashing', async ({
        page,
        assertNoErrorBoundary,
        assertNoConsoleErrors,
    }) => {
        await page.goto('/');
        await expect(page.getByTestId('chat-fab'))
            .toBeVisible({ timeout: 15_000 });
        await page.getByTestId('chat-fab').click();
        // Whatever chat panel renders, it should not be the
        // ErrorBoundary fallback.
        await assertNoErrorBoundary();
        // Re-click to close (the FAB is the same element regardless
        // of state in this iteration).
        await page.keyboard.press('Escape');
        await assertNoErrorBoundary();
        assertNoConsoleErrors();
    });

    test('opens and closes the admin panel', async ({
        page,
        assertNoErrorBoundary,
        assertNoConsoleErrors,
    }) => {
        await page.goto('/');
        await expect(page.getByTestId('admin-panel-trigger'))
            .toBeVisible({ timeout: 15_000 });
        await page.getByTestId('admin-panel-trigger').click();

        await expect(page.getByTestId('admin-panel'))
            .toBeVisible({ timeout: 10_000 });
        await assertNoErrorBoundary();

        await page.keyboard.press('Escape');
        await expect(page.getByTestId('admin-panel'))
            .toBeHidden({ timeout: 10_000 });
        await assertNoErrorBoundary();
        assertNoConsoleErrors();
    });
});
