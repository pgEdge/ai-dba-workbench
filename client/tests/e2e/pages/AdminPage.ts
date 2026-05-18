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

/**
 * Page object for the application header and the full-screen
 * AdminPanel dialog. Encapsulates opening the admin panel,
 * navigating between admin sections, user menu interactions,
 * and logout.
 *
 * Selector strategy:
 * - Admin button: `aria-label="open administration"` (from Header.tsx)
 * - Admin panel title: heading "Administration" (from AdminPanel index.tsx)
 * - Nav items: ListItemButton role=button with exact section label
 * - User menu: `aria-label="user menu"` (from Header.tsx)
 * - Sign out: menuitem "Sign out" (from Header.tsx)
 */
export class AdminPage extends BasePage {
    // ---------------------------------------------------------------
    // Header locators
    // ---------------------------------------------------------------

    /** The settings icon button that opens the admin panel. */
    get adminButton(): Locator {
        return this.page.getByRole('button', {
            name: /open administration/i,
        });
    }

    /** The user avatar button that opens the user dropdown menu. */
    get userMenuButton(): Locator {
        return this.page.getByRole('button', { name: /user menu/i });
    }

    /** The "Sign out" menu item inside the user dropdown. */
    get signOutMenuItem(): Locator {
        return this.page.getByRole('menuitem', { name: /sign out/i });
    }

    /** The close button inside the admin panel toolbar. */
    get closeAdminButton(): Locator {
        return this.page.getByRole('button', {
            name: /close administration/i,
        });
    }

    // ---------------------------------------------------------------
    // Admin panel actions
    // ---------------------------------------------------------------

    /**
     * Open the full-screen AdminPanel dialog by clicking the
     * settings icon in the header. Waits for the "Administration"
     * heading to confirm the panel has rendered.
     */
    async openAdminPanel(): Promise<void> {
        await expect(this.adminButton).toBeVisible({ timeout: 60_000 });
        await this.adminButton.click();
        await expect(
            this.page.getByRole('heading', { name: 'Administration' }),
        ).toBeVisible({ timeout: 5_000 });
    }

    /**
     * Click a navigation item in the admin panel sidebar.
     * The items are MUI ListItemButtons rendered with role=button.
     */
    async selectSection(sectionName: string): Promise<void> {
        await this.page
            .getByRole('button', {
                name: new RegExp(`^${sectionName}$`, 'i'),
            })
            .click();
    }

    /**
     * Navigate to the Admin > Users section. Opens the admin panel
     * if not already open, clicks "Users", and waits for the users
     * table to load.
     */
    async navigateToUsers(): Promise<void> {
        await this.openAdminPanel();
        await this.selectSection('Users');
        await this.waitForLoadingToFinish();
        await this.waitForTable();
    }

    /**
     * Navigate to the Admin > Tokens section. Opens the admin panel
     * if not already open, clicks "Tokens", and waits for loading.
     */
    async navigateToTokens(): Promise<void> {
        await this.openAdminPanel();
        await this.selectSection('Tokens');
        await this.waitForLoadingToFinish();
    }

    // ---------------------------------------------------------------
    // User menu actions
    // ---------------------------------------------------------------

    /**
     * Open the user dropdown menu by clicking the avatar button.
     */
    async openUserMenu(): Promise<void> {
        await expect(this.userMenuButton).toBeVisible({ timeout: 5_000 });
        await this.userMenuButton.click();
    }

    /**
     * Sign out: open the user menu and click "Sign out". Waits for
     * the login form to appear confirming the session was cleared.
     */
    async signOut(): Promise<void> {
        await this.openUserMenu();
        await expect(this.signOutMenuItem).toBeVisible({ timeout: 5_000 });
        await this.signOutMenuItem.click();

        // Wait for the login form to render after logout.
        await expect(
            this.page.getByLabel('Username'),
        ).toBeVisible({ timeout: 10_000 });
    }

    // ---------------------------------------------------------------
    // Assertions
    // ---------------------------------------------------------------

    /**
     * Assert that the admin panel is open by checking the
     * "Administration" heading.
     */
    async expectAdminPanelVisible(): Promise<void> {
        await expect(
            this.page.getByRole('heading', { name: 'Administration' }),
        ).toBeVisible();
    }

    /**
     * Assert that some admin content is visible in the page.
     */
    async expectAdminContentVisible(): Promise<void> {
        const pageContent = await this.page.content();
        expect(pageContent.toLowerCase()).toMatch(
            /admin|settings|users|tokens/,
        );
    }
}
