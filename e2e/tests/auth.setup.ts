/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Auth setup project
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 *
 * Runs once before the chromium/firefox/webkit projects to log in as
 * the bootstrap admin and persist storage state to .auth/admin.json,
 * which the app-shell and admin-panel specs reuse via
 * `test.use({ storageState: '.auth/admin.json' })`.
 *
 * Without this setup project, those specs fail with ENOENT because
 * Playwright orders tests by filename within a project, so the auth
 * spec's storage-state write would happen after admin-panel.spec.ts
 * had already tried to load it.
 */
import { test as setup, expect } from '@playwright/test';

const username = process.env.E2E_ADMIN_USERNAME ?? 'e2e-admin';
const password =
    process.env.E2E_ADMIN_PASSWORD ?? 'e2e-admin-password-please-change';

const STORAGE_PATH = '.auth/admin.json';

setup('authenticate as bootstrap admin', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('login-username-input').fill(username);
    await page.getByTestId('login-password-input').fill(password);
    await page.getByTestId('login-submit').click();

    // Wait for the app shell to confirm the session is fully
    // established before snapshotting cookies/localStorage.
    await expect(page.getByTestId('app-header'))
        .toBeVisible({ timeout: 15_000 });

    await page.context().storageState({ path: STORAGE_PATH });
});
