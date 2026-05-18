/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Page } from '@playwright/test';
import { LoginPage } from '../pages/LoginPage';
import { AdminPage } from '../pages/AdminPage';
import { UserManagementPage } from '../pages/UserManagementPage';

/**
 * Backward-compatible thin wrappers that delegate to page objects.
 *
 * These functions preserve the original function signatures so that
 * any code still importing from browser.helper.ts continues to work.
 * New tests should import page objects directly from `../pages/`.
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
    const loginPage = new LoginPage(page);
    await loginPage.loginAndWaitForApp(username, password);
}

// ---------------------------------------------------------------
// Admin navigation
// ---------------------------------------------------------------

/**
 * Navigate to the Admin > Users page.
 */
export async function navigateToAdminUsers(page: Page): Promise<void> {
    const adminPage = new AdminPage(page);
    await adminPage.navigateToUsers();
}

/**
 * Navigate to the Admin > Tokens page.
 */
export async function navigateToAdminTokens(page: Page): Promise<void> {
    const adminPage = new AdminPage(page);
    await adminPage.navigateToTokens();
}

/**
 * Wait until the users table has finished loading.
 */
export async function waitForUsersTable(page: Page): Promise<void> {
    const userPage = new UserManagementPage(page);
    await userPage.waitForUsersTable();
}

// ---------------------------------------------------------------
// User CRUD via UI
// ---------------------------------------------------------------

/**
 * Click the "Create User" button to open the create-user dialog.
 */
export async function clickAddUser(page: Page): Promise<void> {
    const userPage = new UserManagementPage(page);
    await userPage.openCreateDialog();
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
    const userPage = new UserManagementPage(page);
    await userPage.fillCreateForm(username, password, displayName);
}

/**
 * Submit the user creation form by clicking "Create".
 */
export async function submitUserForm(page: Page): Promise<void> {
    const userPage = new UserManagementPage(page);
    await userPage.submitCreateForm();
}

/**
 * Click the edit button for a specific user row.
 */
export async function clickEditUser(
    page: Page,
    username: string,
): Promise<void> {
    const userPage = new UserManagementPage(page);
    await userPage.clickEditUser(username);
}

/**
 * Click the delete button for a specific user row.
 */
export async function clickDeleteUser(
    page: Page,
    username: string,
): Promise<void> {
    const userPage = new UserManagementPage(page);
    await userPage.clickDeleteUser(username);
}

/**
 * Confirm a pending deletion in the DeleteConfirmationDialog.
 */
export async function confirmDelete(page: Page): Promise<void> {
    const userPage = new UserManagementPage(page);
    await userPage.confirmDelete();
}
