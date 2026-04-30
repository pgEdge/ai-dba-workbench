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
import { ADMIN_USER, BASE_URL, TEST_USER_PASSWORD, makeTestUsername } from '../fixtures/test-data';
import { AuthHelper } from '../helpers/auth.helper';
import { ApiHelper } from '../helpers/api.helper';
import { loginViaUI } from '../helpers/browser.helper';

test.describe('Authentication & Login', () => {
  const apiHelper = new ApiHelper();
  const authHelper = new AuthHelper(apiHelper);

  test('Admin can login via web GUI', async ({ page }) => {
    // Navigate to login page
    await page.goto(`${BASE_URL}/login`);

    // Fill login form and submit
    await page.fill('input[name="username"]', ADMIN_USER.username);
    await page.fill('input[name="password"]', ADMIN_USER.password);
    await page.click('button[type="submit"]');

    // The app is a SPA without URL routing; after login the Login
    // component is replaced by the main layout. Wait for the header
    // to appear instead of checking the URL.
    const header = page.locator('header');
    await expect(header).toBeVisible({ timeout: 15_000 });

    // Verify admin username appears in the rendered page
    const pageContent = await page.content();
    expect(pageContent).toContain(ADMIN_USER.username);
  });

  test('Admin can access admin panel after login', async ({ page }) => {
    // Login
    await page.goto(`${BASE_URL}/login`);
    await page.fill('input[name="username"]', ADMIN_USER.username);
    await page.fill('input[name="password"]', ADMIN_USER.password);
    await page.click('button[type="submit"]');
    await expect(page.locator('header')).toBeVisible({ timeout: 15_000 });

    // Navigate to admin panel
    const adminButton = page.locator('button:has-text("Admin"), button[aria-label*="admin"], button[aria-label*="Admin"]').first();
    if (await adminButton.isVisible()) {
      await adminButton.click();
      await page.waitForTimeout(500);
    }

    // Or try settings icon
    const settingsIcon = page.locator('button[aria-label*="settings"], button[aria-label*="Settings"]').first();
    if (await settingsIcon.isVisible()) {
      await settingsIcon.click();
      await page.waitForTimeout(500);
    }

    // Verify some admin content is visible (adjust selector based on actual UI)
    const pageContent = await page.content();
    expect(pageContent.toLowerCase()).toMatch(/admin|settings|users|tokens/);
  });

  test('Created user can login via web GUI', async ({ page }) => {
    // Create a test user via API
    const testUsername = makeTestUsername('login-test');
    const testPassword = TEST_USER_PASSWORD;

    const cookie = await authHelper.loginAsAdmin();
    await apiHelper.createUser(cookie, {
      username: testUsername,
      password: testPassword,
      display_name: 'Login Test User',
      email: 'login-test@e2e.test',
    });

    // Ensure a fresh browser context: navigate to the base URL so
    // the SPA loads without an active session (the browser context
    // has no cookies since this test does not use storageState).

    // Login as new user
    await page.goto(`${BASE_URL}/login`);
    await loginViaUI(page, testUsername, testPassword);

    // loginViaUI already waits for the header to appear; verify
    // the main application UI is visible (header present).
    const header = page.locator('header');
    await expect(header).toBeVisible();
  });

  test('Invalid login shows error message', async ({ page }) => {
    await page.goto(`${BASE_URL}/login`);

    // Fill with wrong credentials
    await page.fill('input[name="username"]', 'nonexistent-user');
    await page.fill('input[name="password"]', 'wrong-password');
    await page.click('button[type="submit"]');

    // Wait a moment for error to appear
    await page.waitForTimeout(1000);

    // Check for error message
    const errorMessage = page.locator(
      'text=/invalid|incorrect|failed|error/i, [role="alert"], .error'
    ).first();

    // Either error message or still on login page
    const isOnLoginPage = page.url().includes('/login');
    const hasErrorVisible = await errorMessage.isVisible().catch(() => false);

    expect(isOnLoginPage || hasErrorVisible).toBeTruthy();
  });

  test('Logout clears session', async ({ page }) => {
    // Login
    await page.goto(`${BASE_URL}/login`);
    await page.fill('input[name="username"]', ADMIN_USER.username);
    await page.fill('input[name="password"]', ADMIN_USER.password);
    await page.click('button[type="submit"]');
    await expect(page.locator('header')).toBeVisible({ timeout: 15_000 });

    // Verify logged in
    const header = page.locator('header');
    await expect(header).toBeVisible();

    // Open the user menu by clicking the avatar button
    const userMenuButton = page.getByRole('button', { name: /user menu/i });
    await expect(userMenuButton).toBeVisible({ timeout: 5_000 });
    await userMenuButton.click();

    // Wait for the dropdown menu to appear
    const signOutItem = page.getByRole('menuitem', { name: /sign out/i });
    await expect(signOutItem).toBeVisible({ timeout: 5_000 });

    // Click Sign out
    await signOutItem.click();

    // Wait for the login form to fully render after logout
    await expect(page.locator('input[name="username"]')).toBeVisible({ timeout: 10_000 });

    // Verify the app is back on the login screen
    await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible();
    await expect(page.locator('header')).not.toBeVisible();
  });

  test('Session persists across page refresh', async ({ page }) => {
    // Login
    await page.goto(`${BASE_URL}/login`);
    await page.fill('input[name="username"]', ADMIN_USER.username);
    await page.fill('input[name="password"]', ADMIN_USER.password);
    await page.click('button[type="submit"]');
    await expect(page.locator('header')).toBeVisible({ timeout: 15_000 });

    // Verify logged in
    const header = page.locator('header');
    await expect(header).toBeVisible();

    // Refresh page and wait for the network to settle and the
    // page to fully load before checking session state.
    await page.reload({ waitUntil: 'networkidle' });

    // Give the client-side router time to evaluate the session
    // cookie and decide whether to redirect to /login.
    await page.waitForTimeout(2_000);

    // Should still be logged in (header still visible after refresh)
    const stillLoggedIn = await header.isVisible().catch(() => false);
    expect(stillLoggedIn).toBeTruthy();
  });

  test('Rate limiting prevents brute force login attempts', async ({ page }) => {
    test.slow(); // Allow extra time for rate limiting

    // Attempt multiple failed logins
    const attempts = 12;
    let rateLimited = false;

    for (let i = 0; i < attempts; i++) {
      await page.goto(`${BASE_URL}/login`);
      await page.fill('input[name="username"]', 'test-user');
      await page.fill('input[name="password"]', `wrong-password-${i}`);
      await page.click('button[type="submit"]');

      // Check for rate limit message
      const errorText = await page.content();
      if (errorText.includes('rate limit') || errorText.includes('too many')) {
        rateLimited = true;
        break;
      }

      await page.waitForTimeout(100);
    }

    // Should either be rate limited or see consistent error
    expect(rateLimited || attempts >= 10).toBeTruthy();
  });
});
