/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Locator, expect } from '@playwright/test';
import { BasePage } from './BasePage';
import { BASE_URL } from '../fixtures/test-data';

/**
 * Page object encapsulating all interactions with the Login page.
 *
 * Selector strategy:
 * - Username field: `data-testid="login-username-input"`
 * - Password field: `data-testid="login-password-input"`
 * - Sign In button: `data-testid="login-submit"`
 * - Error alert: ARIA `role="alert"`
 */
export class LoginPage extends BasePage {
    // ---------------------------------------------------------------
    // Locators
    // ---------------------------------------------------------------

    /** The username text field (identified by data-testid). */
    get usernameField(): Locator {
        return this.page.getByTestId('login-username-input');
    }

    /** The password text field (identified by data-testid). */
    get passwordField(): Locator {
        return this.page.getByTestId('login-password-input');
    }

    /** The "Sign In" submit button (identified by data-testid). */
    get signInButton(): Locator {
        return this.page.getByTestId('login-submit');
    }

    /** The error alert displayed on failed login. */
    get errorAlert(): Locator {
        return this.page.getByRole('alert');
    }

    // ---------------------------------------------------------------
    // Actions
    // ---------------------------------------------------------------

    /**
     * Navigate to the login page.
     */
    async goto(): Promise<void> {
        await this.navigate(`${BASE_URL}/login`);
    }

    /**
     * Fill the login form with the given credentials and submit.
     * Does NOT wait for the result; use `loginAndWaitForApp` for
     * the full login-and-verify flow.
     */
    async fillAndSubmit(
        username: string,
        password: string,
    ): Promise<void> {
        await this.usernameField.fill(username);
        await this.passwordField.fill(password);
        await this.signInButton.click();
    }

    /**
     * Perform a full login: fill credentials, submit, and wait for
     * the main application header to confirm a successful session.
     */
    async loginAndWaitForApp(
        username: string,
        password: string,
    ): Promise<void> {
        await this.fillAndSubmit(username, password);
        await this.waitForAppLoad();
    }

    // ---------------------------------------------------------------
    // Assertions
    // ---------------------------------------------------------------

    /**
     * Assert that the login form is visible (username field, password
     * field, and submit button are all rendered).
     */
    async expectLoginFormVisible(): Promise<void> {
        await expect(this.usernameField).toBeVisible();
        await expect(this.passwordField).toBeVisible();
        await expect(this.signInButton).toBeVisible();
    }

    /**
     * Assert that the error alert is visible after a failed login.
     */
    async expectErrorVisible(): Promise<void> {
        await expect(this.errorAlert).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Assert that the application is still on the login screen
     * (the header is not visible and the sign-in button is present).
     */
    async expectOnLoginScreen(): Promise<void> {
        await expect(this.signInButton).toBeVisible();
        await expect(this.page.locator('header')).not.toBeVisible();
    }
}
