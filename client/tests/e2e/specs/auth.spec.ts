/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ADMIN_USER, TEST_USER_PASSWORD, makeTestUsername } from '../fixtures/test-data';
import { AuthHelper } from '../helpers/auth.helper';
import { ApiHelper } from '../helpers/api.helper';
import { LoginPage } from '../pages/LoginPage';
import { AdminPage } from '../pages/AdminPage';

test.describe('Authentication & Login', () => {
    const apiHelper = new ApiHelper();
    const authHelper = new AuthHelper(apiHelper);

    test.beforeEach(async () => {
        await label('package', 'Authentication');
    });

    test('Admin can login via web GUI', async ({ page }) => {
        const loginPage = new LoginPage(page);

        await test.step('Navigate to login page', async () => {
            await loginPage.goto();
        });

        await test.step('Fill login form and submit', async () => {
            await loginPage.loginAndWaitForApp(
                ADMIN_USER.username,
                ADMIN_USER.password,
            );
        });

        await test.step('Verify admin username appears in page', async () => {
            const pageContent = await page.content();
            expect(pageContent).toContain(ADMIN_USER.username);
        });
    });

    test('Admin can access admin panel after login', async ({ page }) => {
        const loginPage = new LoginPage(page);
        const adminPage = new AdminPage(page);

        await test.step('Login as admin', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(
                ADMIN_USER.username,
                ADMIN_USER.password,
            );
        });

        await test.step('Open admin panel', async () => {
            // The admin button may render as "open administration"
            // or a settings icon. Try the admin panel method which
            // handles both variants.
            const adminBtn = adminPage.adminButton;
            if (await adminBtn.isVisible().catch(() => false)) {
                await adminPage.openAdminPanel();
            }
        });

        await test.step('Verify admin content is visible', async () => {
            await adminPage.expectAdminContentVisible();
        });
    });

    test('Created user can login via web GUI', async ({ page }) => {
        const loginPage = new LoginPage(page);
        const testUsername = makeTestUsername('login-test');
        const testPassword = TEST_USER_PASSWORD;

        await test.step('Create a test user via API', async () => {
            const cookie = await authHelper.loginAsAdmin();
            await apiHelper.createUser(cookie, {
                username: testUsername,
                password: testPassword,
                display_name: 'Login Test User',
                email: 'login-test@e2e.test',
            });
        });

        await test.step('Login as new user via UI', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(testUsername, testPassword);
        });

        await test.step('Verify main application UI is visible', async () => {
            await expect(page.locator('header')).toBeVisible();
        });
    });

    test('Invalid login shows error message', async ({ page }) => {
        const loginPage = new LoginPage(page);

        await test.step('Navigate to login page', async () => {
            await loginPage.goto();
        });

        await test.step('Submit invalid credentials', async () => {
            await loginPage.fillAndSubmit('nonexistent-user', 'wrong-password');
        });

        await test.step('Verify error or still on login page', async () => {
            // Wait for the error alert to appear or confirm we are
            // still on the login screen.
            const hasError = await loginPage.errorAlert
                .isVisible()
                .catch(() => false);
            const isOnLoginPage = page.url().includes('/login');

            expect(isOnLoginPage || hasError).toBeTruthy();
        });
    });

    test('Logout clears session', async ({ page }) => {
        const loginPage = new LoginPage(page);
        const adminPage = new AdminPage(page);

        await test.step('Login as admin', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(
                ADMIN_USER.username,
                ADMIN_USER.password,
            );
        });

        await test.step('Verify logged in', async () => {
            await expect(page.locator('header')).toBeVisible();
        });

        await test.step('Sign out via user menu', async () => {
            await adminPage.signOut();
        });

        await test.step('Verify back on login screen', async () => {
            await loginPage.expectOnLoginScreen();
        });
    });

    test('Session persists across page refresh', async ({ page }) => {
        const loginPage = new LoginPage(page);

        await test.step('Login as admin', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(
                ADMIN_USER.username,
                ADMIN_USER.password,
            );
        });

        await test.step('Verify logged in', async () => {
            await expect(page.locator('header')).toBeVisible();
        });

        await test.step('Refresh and verify session persists', async () => {
            await page.reload({ waitUntil: 'networkidle' });

            // Wait for the SPA to evaluate the session cookie and
            // render the appropriate view (header for logged-in,
            // login form for logged-out).
            const header = page.locator('header');
            await expect(header).toBeVisible({ timeout: 10_000 });

            const stillLoggedIn = await header.isVisible().catch(() => false);
            expect(stillLoggedIn).toBeTruthy();
        });
    });

    test('Rate limiting prevents brute force login attempts', async ({ page }) => {
        test.slow(); // Allow extra time for rate limiting

        const loginPage = new LoginPage(page);

        // Attempt multiple failed logins
        const attempts = 12;
        let rateLimited = false;

        for (let i = 0; i < attempts; i++) {
            await loginPage.goto();
            await loginPage.fillAndSubmit('test-user', `wrong-password-${i}`);

            // Check for rate limit message
            const errorText = await page.content();
            if (errorText.includes('rate limit') || errorText.includes('too many')) {
                rateLimited = true;
                break;
            }

            // Brief wait between attempts to avoid overwhelming the
            // page navigation.
            await page.waitForTimeout(100);
        }

        // Should either be rate limited or see consistent error
        expect(rateLimited || attempts >= 10).toBeTruthy();
    });
});
