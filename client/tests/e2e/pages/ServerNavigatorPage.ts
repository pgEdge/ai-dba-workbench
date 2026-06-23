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
 * Page object for the left-hand server navigator tree. Encapsulates
 * interactions with server rows, cluster expand/collapse, and the
 * full-screen server edit dialog.
 *
 * Selector strategy:
 * - Navigator scope: `data-testid="cluster-navigator"` (all server
 *   row queries are scoped inside this container to prevent false
 *   matches from dialogs or other page regions)
 * - Server rows: `.server-item-row` inside the cluster-navigator
 * - Cluster wrapper: `.draggable-cluster` with `.cluster-header`
 * - Edit dialog: `.MuiDialog-paperFullScreen` (fullscreen Dialog)
 * - Close edit button: `aria-label="close edit server"`
 * - Share checkbox: label "Share with all users"
 * - Action buttons: `.action-buttons` inside each server row
 */
export class ServerNavigatorPage extends BasePage {
    /**
     * Locator for all visible server rows in the navigator.
     * Scoped inside `data-testid="cluster-navigator"` to avoid
     * matching rows rendered in dialogs or elsewhere on the page.
     */
    get serverRows(): Locator {
        return this.page
            .getByTestId('cluster-navigator')
            .locator('.server-item-row');
    }

    /** Locate a specific server row by name within the navigator. */
    getServerRow(name: string): Locator {
        return this.page
            .getByTestId('cluster-navigator')
            .locator('.server-item-row')
            .filter({ hasText: name });
    }

    /**
     * Click a server row to select it.
     */
    async selectServer(name: string): Promise<void> {
        await this.getServerRow(name).first().click();
    }

    /**
     * Hover over a server row to reveal its action buttons,
     * then click the settings (edit) icon.
     */
    async openServerEditDialog(name: string): Promise<void> {
        const row = this.getServerRow(name).first();
        await row.hover();
        // The action buttons container has class "action-buttons".
        // The first button inside it is the SettingsIcon (edit).
        await row
            .locator('.action-buttons')
            .getByRole('button')
            .first()
            .click();
        // Wait for the fullscreen edit dialog to appear.
        await expect(
            this.page.locator('.MuiDialog-paperFullScreen'),
        ).toBeVisible({ timeout: 10_000 });
    }

    /**
     * Enable "Share with all users" in the open edit dialog and save.
     * The checkbox is only rendered for superusers.
     */
    async enableSharedForAllUsers(): Promise<void> {
        const dialog = this.page.locator('.MuiDialog-paperFullScreen');
        await expect(dialog).toBeVisible({ timeout: 5_000 });

        const checkbox = dialog.getByLabel('Share with all users');
        await expect(checkbox).toBeVisible({ timeout: 5_000 });

        // Only check if not already checked.
        const isChecked = await checkbox.isChecked();
        if (!isChecked) {
            await checkbox.check();
        }

        await dialog.getByRole('button', { name: /save/i }).click();

        // Wait for save success alert.
        await expect(
            dialog.getByText('Server settings saved successfully'),
        ).toBeVisible({ timeout: 10_000 });

        // Wait for the post-save data refetch to complete so that any
        // toolbar re-renders triggered by fetchClusterData settle before
        // the caller attempts to interact with the dialog again.
        await this.page.waitForLoadState('load');
    }

    /**
     * Close the fullscreen edit dialog.
     *
     * After a successful save, `fetchClusterData` re-renders the dialog
     * toolbar which continuously detaches and reattaches the close button
     * — making it impossible to click reliably. Use `force: true` to
     * bypass Playwright's stability check and fire the click directly,
     * then confirm the dialog has hidden. If the dialog has already
     * auto-dismissed (e.g. after a save on some builds), return early.
     *
     * The click uses an explicit 10s timeout so that a missing or
     * detached close button fails fast instead of inheriting the
     * test's full timeout (180s in CI). When the click fails (e.g. in
     * WebKit where the button never appears in the DOM), the fallback
     * sends Escape — which MUI dialogs accept by default — to dismiss
     * the dialog and prevent the hang from cascading into later tests.
     */
    async closeEditDialog(): Promise<void> {
        const dialog = this.page.locator('.MuiDialog-paperFullScreen');

        // If the dialog already closed on its own, nothing to do.
        const isVisible = await dialog.isVisible().catch(() => false);
        if (!isVisible) {
            return;
        }

        try {
            // force: true bypasses the "element not stable" / "element
            // detached" actionability checks that block a regular click
            // while the toolbar is mid-rerender after a save.
            // timeout: 10_000 caps the wait so a missing button does
            // not hang for the full test timeout (180s in CI).
            await this.page
                .getByRole('button', { name: /close edit server/i })
                .click({ force: true, timeout: 10_000 });
        } catch {
            // Fallback: the close button was not found or not
            // clickable within 10s (common in WebKit CI). Press
            // Escape to dismiss the MUI dialog instead.
            await this.page.keyboard.press('Escape');
        }

        await expect(dialog).toBeHidden({ timeout: 5_000 });
    }

    /**
     * Assert that only the named servers are visible in the tree.
     * Waits for the expected servers and asserts no extra rows.
     */
    async expectOnlyServersVisible(expectedNames: string[]): Promise<void> {
        // All expected servers must be visible.
        for (const name of expectedNames) {
            await expect(
                this.getServerRow(name).first(),
                `Expected server "${name}" to be visible`,
            ).toBeVisible({ timeout: 15_000 });
        }

        // Wait briefly for any in-flight data to settle, then count.
        await this.page.waitForLoadState('load');
        const count = await this.serverRows.count();
        expect(
            count,
            `Expected exactly ${expectedNames.length} server row(s), got ${count}`,
        ).toBe(expectedNames.length);
    }

    /**
     * Assert that the server tree shows the empty state message,
     * meaning no servers are visible for the current user.
     */
    async expectEmptyServerTree(timeout: number = 15_000): Promise<void> {
        await expect(
            this.page.getByText(/no servers configured/i),
            'Server tree should show empty state when no servers are shared',
        ).toBeVisible({ timeout });
    }

    /**
     * Expand a cluster in the navigator so its server rows are
     * visible. Uses the chevron button inside .cluster-header.
     */
    async expandCluster(clusterName: string): Promise<void> {
        const clusterHeader = this.page
            .locator('.draggable-cluster')
            .filter({ hasText: clusterName })
            .first()
            .locator('.cluster-header');

        await expect(clusterHeader).toBeVisible({ timeout: 15_000 });
        await clusterHeader
            .getByRole('button')
            .first()
            .click();
    }
}
