/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Auth flow E2E
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { test, expect } from '../fixtures/error-boundary';

const username = process.env.E2E_ADMIN_USERNAME ?? 'e2e-admin';
const password =
    process.env.E2E_ADMIN_PASSWORD ?? 'e2e-admin-password-please-change';

test.describe('login flow', () => {
    test('renders the login page', async ({
        page,
        assertNoErrorBoundary,
    }) => {
        await page.goto('/');
        await expect(page.getByTestId('login-username-input'))
            .toBeVisible();
        await expect(page.getByTestId('login-password-input'))
            .toBeVisible();
        await expect(page.getByTestId('login-submit'))
            .toBeVisible();
        await assertNoErrorBoundary();
    });

    test('logs in with correct credentials', async ({
        page,
        assertNoErrorBoundary,
        assertNoConsoleErrors,
    }) => {
        // The auth.setup.ts project writes `.auth/admin.json` once
        // up-front (see playwright.config.ts). This behavioural test
        // asserts the same flow without needing to persist state.
        await page.goto('/');
        await page.getByTestId('login-username-input').fill(username);
        await page.getByTestId('login-password-input').fill(password);
        await page.getByTestId('login-submit').click();

        await expect(page.getByTestId('app-header'))
            .toBeVisible({ timeout: 15_000 });
        await expect(page.getByTestId('cluster-navigator'))
            .toBeVisible();

        await assertNoErrorBoundary();
        assertNoConsoleErrors();
    });

    test('rejects wrong credentials', async ({
        page,
        assertNoErrorBoundary,
    }) => {
        await page.goto('/');
        await page.getByTestId('login-username-input').fill(username);
        await page.getByTestId('login-password-input').fill('not-the-password');
        await page.getByTestId('login-submit').click();

        await expect(page.getByTestId('login-error'))
            .toBeVisible({ timeout: 10_000 });
        await expect(page.getByTestId('app-header')).toHaveCount(0);
        await assertNoErrorBoundary();
    });
});
