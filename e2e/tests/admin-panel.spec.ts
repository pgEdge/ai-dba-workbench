/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Admin Panel E2E
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { test, expect } from '../fixtures/error-boundary';

test.use({ storageState: '.auth/admin.json' });

// Item ids are sourced from
// client/src/components/AdminPanel/index.tsx (NAV_SECTIONS).
// Memories is intentionally excluded — it requires AI to be enabled.
const ADMIN_ITEMS = [
    'users',
    'groups',
    'permissions',
    'token_scopes',
    'probes',
    'alert_rules',
    'email_channels',
    'slack_channels',
    'mattermost_channels',
    'webhook_channels',
] as const;

test.describe('admin panel sections', () => {
    test.beforeEach(async ({ page }) => {
        await page.goto('/');
        await page.getByTestId('admin-panel-trigger').click();
        await expect(page.getByTestId('admin-panel'))
            .toBeVisible({ timeout: 15_000 });
    });

    for (const id of ADMIN_ITEMS) {
        test(`opens the ${id} section without crashing`, async ({
            page,
            assertNoErrorBoundary,
            assertNoConsoleErrors,
        }) => {
            const item = page.getByTestId(`admin-panel-item-${id}`);
            await expect(item).toBeVisible({ timeout: 10_000 });
            await item.click();

            await expect(page.getByTestId('admin-panel-section-content'))
                .toBeVisible({ timeout: 10_000 });
            await assertNoErrorBoundary();
            assertNoConsoleErrors();
        });
    }
});
