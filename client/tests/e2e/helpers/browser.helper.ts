/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Page, expect } from '@playwright/test';

/**
 * Playwright page helpers for common UI interactions in the
 * AI DBA Workbench client application.
 *
 * These helpers use ARIA labels and role selectors because the
 * production components do not carry data-testid attributes.
 */

// ---------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------

/**
 * Log in through the web UI by filling the username and password
 * fields and clicking "Sign In".
 */
export async function loginViaUI(
    page: Page,
    username: string,
    password: string,
): Promise<void> {
    await page.getByLabel('Username').fill(username);
    await page.getByLabel('Password').fill(password);
    await page.getByRole('button', { name: 'Sign In' }).click();
    // The application is a SPA without URL-based routing; after a
    // successful login the Login component is replaced by the main
    // layout. Wait for the main application header to appear instead
    // of relying on a URL change.
    await expect(page.locator('header')).toBeVisible({ timeout: 15_000 });
}

// ---------------------------------------------------------------
// Admin navigation
// ---------------------------------------------------------------

/**
 * Click the admin/settings icon button in the application header.
 * The button uses aria-label="open administration" and renders a
 * SettingsIcon with no visible text.
 */
async function openAdminPanel(page: Page): Promise<void> {
    const adminBtn = page
        .getByRole('button', { name: /open administration/i })
        .or(page.locator('button[aria-label="open administration"]'))
        .first();
    await adminBtn.waitFor({ state: 'visible', timeout: 60_000 });
    await adminBtn.click();
    // Allow the admin dialog and its navigation list to render.
    await page.waitForTimeout(300);
}

/**
 * Navigate to the Admin > Users page by clicking the admin icon
 * in the header and then the "Users" navigation item.
 */
export async function navigateToAdminUsers(page: Page): Promise<void> {
    await openAdminPanel(page);
    // The admin navigation items are MUI ListItemButtons (role=button),
    // not links.
    await page.getByRole('button', { name: /^Users$/i }).click();
    await waitForUsersTable(page);
}

/**
 * Navigate to the Admin > Tokens page by clicking the admin icon
 * in the header and then the "Tokens" navigation item.
 */
export async function navigateToAdminTokens(page: Page): Promise<void> {
    await openAdminPanel(page);
    await page.getByRole('button', { name: /^Tokens$/i }).click();
    // Wait for the tokens content to render.
    const loader = page.getByRole('progressbar');
    if (await loader.isVisible().catch(() => false)) {
        await loader.waitFor({ state: 'hidden', timeout: 15_000 });
    }
}

/**
 * Wait until the users table has finished loading.
 */
export async function waitForUsersTable(page: Page): Promise<void> {
    // Wait for any loading indicator to disappear.
    const loader = page.getByRole('progressbar');
    if (await loader.isVisible().catch(() => false)) {
        await loader.waitFor({ state: 'hidden', timeout: 15_000 });
    }
    // Confirm the table is present.
    await expect(
        page.getByRole('table').or(page.locator('[role="grid"]')),
    ).toBeVisible({ timeout: 10_000 });
}

// ---------------------------------------------------------------
// User CRUD via UI
// ---------------------------------------------------------------

/**
 * Click the "Add" button to open the create-user dialog.
 */
export async function clickAddUser(page: Page): Promise<void> {
    await page.getByRole('button', { name: /add/i }).click();
}

/**
 * Fill the user creation dialog form fields.
 */
export async function fillUserForm(
    page: Page,
    username: string,
    password: string,
    displayName?: string,
): Promise<void> {
    await page.getByLabel('Username', { exact: true }).fill(username);
    await page.getByLabel('Password', { exact: true }).fill(password);
    if (displayName) {
        await page.getByLabel('Display Name').fill(displayName);
    }
}

/**
 * Submit the user creation form by clicking "Create".
 */
export async function submitUserForm(page: Page): Promise<void> {
    await page.getByRole('button', { name: /create/i }).click();
}

/**
 * Click the edit button for a specific user row identified by
 * username.
 */
export async function clickEditUser(
    page: Page,
    username: string,
): Promise<void> {
    const row = page.getByRole('row', { name: new RegExp(username) });
    await row.getByRole('button', { name: /edit/i }).click();
}

/**
 * Click the delete button for a specific user row identified by
 * username.
 */
export async function clickDeleteUser(
    page: Page,
    username: string,
): Promise<void> {
    const row = page.getByRole('row', { name: new RegExp(username) });
    await row.getByRole('button', { name: /delete/i }).click();
}

/**
 * Confirm a pending deletion in the DeleteConfirmationDialog.
 */
export async function confirmDelete(page: Page): Promise<void> {
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 5_000 });
    await dialog.getByRole('button', { name: /confirm|delete|yes/i }).click();
    await expect(dialog).toBeHidden({ timeout: 5_000 });
}
