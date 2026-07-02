/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ThemePage } from '../pages/ThemePage';

test.describe('Theme Switching', () => {
    test.use({ storageState: '.auth/admin.json' });

    test('theme preference switches between light and dark and persists across reload', async ({
        page,
    }) => {
        await label('package', 'Theme');

        const themePage = new ThemePage(page);

        await page.goto('/');
        await themePage.waitForAppLoad();

        await test.step('Ensure starting state is light mode', async () => {
            const stored = await themePage.getStoredTheme();
            if (stored === 'dark') {
                await themePage.switchToLight();
            }
            await themePage.expectLightMode();
        });

        await test.step('Switch to dark mode', async () => {
            await themePage.switchToDark();
            await themePage.expectDarkMode();
        });

        await test.step('Dark mode persists after page reload', async () => {
            await page.reload();
            await page.waitForLoadState('load');
            await themePage.waitForAppLoad();
            await themePage.expectDarkMode();
        });

        await test.step('Switch back to light mode', async () => {
            await themePage.switchToLight();
            await themePage.expectLightMode();
        });

        await test.step('Light mode persists after page reload', async () => {
            await page.reload();
            await page.waitForLoadState('load');
            await themePage.waitForAppLoad();
            await themePage.expectLightMode();
        });
    });
});
