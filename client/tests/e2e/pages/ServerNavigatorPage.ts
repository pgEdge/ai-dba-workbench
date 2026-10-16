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
     *
     * Uses `force: true` to bypass the cluster-header / MuiCollapse
     * wrapper that intercepts pointer events and causes Playwright to
     * retry indefinitely (320+ × 500 ms ≈ 180 s timeout) in CI on
     * Firefox and WebKit.
     */
    async selectServer(name: string): Promise<void> {
        const row = this.getServerRow(name).first();
        await expect(row).toBeVisible({ timeout: 15_000 });
        await row.click({ force: true });
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

    /**
     * Delete a server through the GUI: hover its row to reveal the action
     * buttons, click the delete IconButton (the one carrying `DeleteIcon`, as
     * opposed to the edit `SettingsIcon`), and confirm in the "Delete Server"
     * confirmation dialog. Waits until the row is gone.
     */
    async deleteServer(name: string): Promise<void> {
        const row = this.getServerRow(name).first();
        await expect(row, `server "${name}" should be present`).toBeVisible({
            timeout: 15_000,
        });
        await this.deleteServerRow(row);
        await expect(
            row,
            `server "${name}" should be gone after delete`,
        ).toBeHidden({ timeout: 15_000 });
    }

    /**
     * Delete a specific server ROW (a `.server-item-row` locator) via the GUI:
     * hover to reveal the action buttons, click the delete IconButton (the one
     * carrying `DeleteIcon`), and confirm in the "Delete Server" dialog.
     */
    private async deleteServerRow(row: Locator): Promise<void> {
        await row.hover();
        await row
            .locator('.action-buttons')
            .locator('button')
            .filter({
                has: this.page.locator('[data-testid="DeleteIcon"]'),
            })
            .first()
            .click({ force: true });

        const dialog = this.page
            .getByRole('dialog')
            .filter({ hasText: 'Delete Server' });
        await expect(
            dialog,
            'Delete Server confirmation dialog should open',
        ).toBeVisible({ timeout: 10_000 });
        await dialog.getByRole('button', { name: /^delete$/i }).click();
        await expect(dialog).toBeHidden({ timeout: 15_000 });
    }

    /**
     * Delete EVERY server under a cluster via the GUI, one at a time, until the
     * cluster is empty. A cluster can hold more servers than any single spec
     * registered (other specs add to the shared physical cluster), so a
     * cleanup must remove them all. The loop stops when the cluster's
     * "Delete cluster" control renders — that control only appears at zero
     * servers, so its presence is the authoritative "empty" signal. Throws if
     * the cluster cannot be emptied (e.g. members keep re-appearing).
     */
    async deleteAllServersInCluster(clusterName: string): Promise<void> {
        // Wait for the navigator tree to render the cluster first — otherwise
        // a not-yet-loaded tree reads as count() === 0 and the loop would exit
        // immediately, deleting nothing.
        await expect(
            this.page
                .locator('.draggable-cluster')
                .filter({ hasText: clusterName })
                .first(),
            `cluster "${clusterName}" should be present before cleanup`,
        ).toBeVisible({ timeout: 30_000 });

        for (let attempt = 0; attempt < 25; attempt += 1) {
            const clusterItem = this.page
                .locator('.draggable-cluster')
                .filter({ hasText: clusterName })
                .first();
            if ((await clusterItem.count()) === 0) {
                return; // cluster no longer present
            }

            // The delete-cluster button is rendered only when the cluster has
            // zero servers — treat its presence as "empty".
            const emptyControl = clusterItem.getByRole('button', {
                name: `Delete cluster ${clusterName}`,
            });
            if ((await emptyControl.count()) > 0) {
                return;
            }

            // Expand if collapsed (no visible rows), then delete the first.
            let rows = clusterItem.locator('.server-item-row');
            if ((await rows.count()) === 0) {
                await clusterItem
                    .locator('.cluster-header')
                    .getByRole('button')
                    .first()
                    .click({ force: true })
                    .catch(() => undefined);
                rows = clusterItem.locator('.server-item-row');
            }
            if ((await rows.count()) === 0) {
                // Neither empty-control nor rows yet — data may be refetching.
                await this.page.waitForTimeout(1_000);
                continue;
            }

            await this.deleteServerRow(rows.first());
            await this.page.waitForTimeout(500);
        }
        throw new Error(
            `Could not empty cluster "${clusterName}" — servers remain after ` +
            '25 deletions (members may be re-appearing via auto-detection)',
        );
    }

    /**
     * Delete a cluster through the GUI. The cluster's delete control only
     * renders when it has zero servers, so delete its members first. Hovers
     * the cluster header to reveal the `Delete cluster <name>` button, clicks
     * it, and confirms in the "Delete Cluster" dialog. Waits until the cluster
     * is gone from the navigator.
     */
    async deleteCluster(clusterName: string): Promise<void> {
        const clusterItem = this.page
            .locator('.draggable-cluster')
            .filter({ hasText: clusterName })
            .first();
        await expect(
            clusterItem,
            `cluster "${clusterName}" should be present`,
        ).toBeVisible({ timeout: 15_000 });

        await clusterItem.locator('.cluster-header').hover();
        await clusterItem
            .getByRole('button', { name: `Delete cluster ${clusterName}` })
            .click({ force: true });

        const dialog = this.page
            .getByRole('dialog')
            .filter({ hasText: 'Delete Cluster' });
        await expect(
            dialog,
            'Delete Cluster confirmation dialog should open',
        ).toBeVisible({ timeout: 10_000 });
        await dialog.getByRole('button', { name: /^delete$/i }).click();
        await expect(dialog).toBeHidden({ timeout: 15_000 });

        await expect(
            this.page
                .locator('.draggable-cluster')
                .filter({ hasText: clusterName }),
            `cluster "${clusterName}" should be gone after delete`,
        ).toHaveCount(0, { timeout: 15_000 });
    }
}
