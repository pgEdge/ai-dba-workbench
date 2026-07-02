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

const LIGHT_BG = 'rgb(249, 250, 251)';
const DARK_BG = 'rgb(15, 23, 42)';

/**
 * Page object for the theme toggle button in the application
 * header. Provides helpers to switch between light and dark
 * mode and assert the current theme state.
 */
export class ThemePage extends BasePage {
    getToggleButton(): Locator {
        return this.page.getByRole('button', {
            name: /toggle theme/i,
        });
    }

    async switchToDark(): Promise<void> {
        await this.getToggleButton().click();
        await expect(this.page.locator('body')).toHaveCSS(
            'background-color',
            DARK_BG,
            { timeout: 5_000 },
        );
    }

    async switchToLight(): Promise<void> {
        await this.getToggleButton().click();
        await expect(this.page.locator('body')).toHaveCSS(
            'background-color',
            LIGHT_BG,
            { timeout: 5_000 },
        );
    }

    async getStoredTheme(): Promise<string | null> {
        return this.page.evaluate(() =>
            localStorage.getItem('theme-mode'),
        );
    }

    async getBodyBackgroundColor(): Promise<string> {
        return this.page.evaluate(() =>
            getComputedStyle(document.body).backgroundColor,
        );
    }

    async expectDarkMode(): Promise<void> {
        const stored = await this.getStoredTheme();
        expect(stored, 'localStorage theme-mode should be dark').toBe(
            'dark',
        );

        const bg = await this.getBodyBackgroundColor();
        expect(bg, 'body background should be dark').toBe(DARK_BG);

        await this.getToggleButton().hover();
        await expect(
            this.page.getByRole('tooltip', {
                name: /switch to light mode/i,
            }),
            'tooltip should offer switch to light mode',
        ).toBeVisible({ timeout: 3_000 });
    }

    async expectLightMode(): Promise<void> {
        const stored = await this.getStoredTheme();
        expect(
            stored,
            'localStorage theme-mode should be light',
        ).toBe('light');

        const bg = await this.getBodyBackgroundColor();
        expect(bg, 'body background should be light').toBe(LIGHT_BG);

        await this.getToggleButton().hover();
        await expect(
            this.page.getByRole('tooltip', {
                name: /switch to dark mode/i,
            }),
            'tooltip should offer switch to dark mode',
        ).toBeVisible({ timeout: 3_000 });
    }
}
