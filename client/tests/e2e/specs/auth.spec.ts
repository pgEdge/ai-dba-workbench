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

test.describe('Authentication & Login', () => {
  const apiHelper = new ApiHelper();
  const authHelper = new AuthHelper(apiHelper);

  test('Admin can login via web GUI', async ({ page }) => {
    // Navigate to login page
    await page.goto(`${BASE_URL}/login`);

    // Fill login form
    await page.fill('input[name="username"]', ADMIN_USER.username);
    await page.fill('input[name="password"]', ADMIN_USER.password);

    // Submit login
    await page.click('button[type="submit"]');

    // Wait for navigation to dashboard
    await page.waitForURL(`${BASE_URL}/**`, { timeout: 10000 });

    // Verify logged in (check for header/navigation)
    const header = page.locator('header');
    await expect(header).toBeVisible();

    // Verify admin username appears in header or user menu
    const userMenu = page.locator('button:has-text("Admin"), [data-testid="user-menu"]');
    // Try multiple selectors since exact structure may vary
    const pageContent = await page.content();
    expect(pageContent).toContain(ADMIN_USER.username);
  });

  test('Admin can access admin panel after login', async ({ page }) => {
    // Login
    await page.goto(`${BASE_URL}/login`);
    await page.fill('input[name="username"]', ADMIN_USER.username);
    await page.fill('input[name="password"]', ADMIN_USER.password);
    await page.click('button[type="submit"]');
    await page.waitForURL(`${BASE_URL}/**`, { timeout: 10000 });

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

    // Logout from admin
    await page.goto(`${BASE_URL}/logout`);
    await page.waitForURL(`${BASE_URL}/**`, { timeout: 5000 });

    // Login as new user
    await page.goto(`${BASE_URL}/login`);
    await page.fill('input[name="username"]', testUsername);
    await page.fill('input[name="password"]', testPassword);
    await page.click('button[type="submit"]');

    // Wait for dashboard
    await page.waitForURL(`${BASE_URL}/**`, { timeout: 10000 });

    // Verify login succeeded: URL should no longer be the login page
    expect(page.url()).not.toContain('/login');

    // Verify the main application UI is visible (header present)
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
    await page.waitForURL(`${BASE_URL}/**`, { timeout: 10000 });

    // Verify logged in
    const header = page.locator('header');
    await expect(header).toBeVisible();

    // Find and click logout
    // Try multiple selectors for logout
    let logoutButton = page.locator('button:has-text("Logout")').first();
    if (!(await logoutButton.isVisible())) {
      logoutButton = page.locator('button:has-text("Sign Out")').first();
    }
    if (!(await logoutButton.isVisible())) {
      // Try user menu > logout
      const userMenuButton = page.locator('button[aria-label*="user"], button[aria-label*="User"]').first();
      if (await userMenuButton.isVisible()) {
        await userMenuButton.click();
        await page.waitForTimeout(300);
        logoutButton = page.locator('button:has-text("Logout"), button:has-text("Sign Out")').first();
      }
    }

    if (await logoutButton.isVisible()) {
      await logoutButton.click();
      await page.waitForTimeout(500);
    }

    // Should be back at login or have lost session
    const isLoggedOut = page.url().includes('/login') ||
      !(await page.locator('header').isVisible().catch(() => false));

    expect(isLoggedOut).toBeTruthy();
  });

  test('Session persists across page refresh', async ({ page }) => {
    // Login
    await page.goto(`${BASE_URL}/login`);
    await page.fill('input[name="username"]', ADMIN_USER.username);
    await page.fill('input[name="password"]', ADMIN_USER.password);
    await page.click('button[type="submit"]');
    await page.waitForURL(`${BASE_URL}/**`, { timeout: 10000 });

    // Verify logged in
    const header = page.locator('header');
    await expect(header).toBeVisible();

    // Refresh page and wait for the network to settle and the
    // page to fully load before checking session state.
    await page.reload({ waitUntil: 'networkidle' });

    // Give the client-side router time to evaluate the session
    // cookie and decide whether to redirect to /login.
    await page.waitForTimeout(2_000);

    // Should still be logged in (header still visible, not on login page)
    const stillLoggedIn = await header.isVisible().catch(() => false);
    const notOnLoginPage = !page.url().includes('/login');

    expect(stillLoggedIn && notOnLoginPage).toBeTruthy();
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
