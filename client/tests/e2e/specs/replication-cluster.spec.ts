/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect, type Page } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ReplicationHelper } from '../helpers/replication.helper';

// ---------------------------------------------------------------
// Constants
// ---------------------------------------------------------------

const GROUP_NAME = 'Test Replication Group';
const GROUP_DESC = 'Binary replication test group';
const CLUSTER_NAME = 'Test Binary Cluster';
const CLUSTER_DESC = '3-node replication cluster';
const PG_HOST = process.env['E2E_REPLICATION_HOST'] ?? 'postgres';
const PG_DB = 'ragdb';
const PG_USER = 'postgres';
const PG_PASS = process.env['POSTGRES_PASSWORD'] ?? 'postgres';

// ---------------------------------------------------------------
// Server configuration
// ---------------------------------------------------------------

interface ServerConfig {
    name: string;
    host: string;
    port: number;
    db: string;
    user: string;
    pass: string;
    role: 'Primary' | 'Standby';
}

const SERVERS: ServerConfig[] = [
    {
        name: 'primary-node',
        host: PG_HOST,
        port: 5440,
        db: PG_DB,
        user: PG_USER,
        pass: PG_PASS,
        role: 'Primary',
    },
    {
        name: 'standby-node-1',
        host: PG_HOST,
        port: 5441,
        db: PG_DB,
        user: PG_USER,
        pass: PG_PASS,
        role: 'Standby',
    },
    {
        name: 'standby-node-2',
        host: PG_HOST,
        port: 5442,
        db: PG_DB,
        user: PG_USER,
        pass: PG_PASS,
        role: 'Standby',
    },
];

// ---------------------------------------------------------------
// Helper: add a server via the UI
// ---------------------------------------------------------------

async function addServer(page: Page, cfg: ServerConfig): Promise<void> {
    // Use count() to detect if server exists even when hidden in collapsed tree
    const serverExists = await page.getByText(cfg.name).count() > 0;
    if (!serverExists) {
        // Guard: wait for any in-flight fetches to settle and ensure
        // the app is fully rendered before interacting with the menu.
        // After drag-and-drop, moveClusterToGroup triggers an async
        // fetchClusterData that can cause a re-render mid-interaction.
        await page.waitForLoadState('networkidle');
        await expect(
            page.locator('header'),
            'App header should be visible before opening Add Server menu',
        ).toBeVisible({ timeout: 10_000 });

        // Wait for any lingering dialog (e.g. ClusterConfigDialog
        // still animating out after an Escape press) to fully detach
        // from the DOM before attempting to open a new one.
        const existingDialog = page.getByRole('dialog');
        if (await existingDialog.count() > 0) {
            await existingDialog.first().waitFor({ state: 'detached', timeout: 5_000 })
                .catch(() => {
                    // If dialog is still present, press Escape to dismiss
                });
            // Re-check after waiting; press Escape if still present
            if (await existingDialog.count() > 0) {
                await page.keyboard.press('Escape');
                await existingDialog.first().waitFor({ state: 'detached', timeout: 5_000 });
            }
        }

        // Retry the full sequence (open menu -> click Add Server ->
        // wait for dialog) because a concurrent fetchClusterData
        // re-render can swallow the click or unmount the menu.
        await expect(async () => {
            // Dismiss any menu that may be lingering from a prior attempt
            const menuVisible = await page.getByRole('menuitem', { name: 'Add Server' }).count() > 0;
            if (menuVisible) {
                await page.keyboard.press('Escape');
                await page.waitForTimeout(200);
            }

            await page
                .getByRole('button', { name: 'Add server or group' })
                .click();
            await page.getByRole('menuitem', { name: 'Add Server' })
                .waitFor({ state: 'visible', timeout: 5_000 });
            await page.getByRole('menuitem', { name: 'Add Server' }).click();

            // Wait for the server dialog to appear
            await expect(
                page.getByRole('dialog'),
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 20_000, intervals: [1_000] });

        // Fill connection details
        // Use getByRole for Name to avoid matching Username (substring issue)
        await page.getByRole('textbox', { name: 'Name', exact: true }).fill(cfg.name);
        await page.getByLabel('Host').fill(cfg.host);
        await page.getByLabel('Port').fill(String(cfg.port));
        await page.getByLabel('Maintenance Database').fill(cfg.db);
        await page.getByLabel('Username').fill(cfg.user);
        await page.getByLabel('Password').fill(cfg.pass);

        // Scroll down to see the Cluster section and select cluster
        await page.getByRole('combobox', { name: 'Cluster' }).fill(CLUSTER_NAME);
        await page
            .getByRole('option', { name: CLUSTER_NAME })
            .first()
            .click();

        // Select role (appears after cluster is selected)
        await page.getByLabel('Role').click();
        await page
            .getByRole('option', { name: cfg.role })
            .click();

        // Save and wait for dialog to close (confirms save succeeded)
        await page.getByRole('button', { name: 'Save' }).click();
        await expect(
            page.getByRole('dialog'),
            `Dialog should close after saving server "${cfg.name}"`,
        ).not.toBeVisible({ timeout: 15_000 });
    }

    // Assert server appears in navigator (may be inside collapsed cluster)
    await expect(async () => {
        const count = await page.getByText(cfg.name).count();
        expect(count, `Server "${cfg.name}" should appear in the navigator`).toBeGreaterThan(0);
    }).toPass({ timeout: 10_000, intervals: [500] });
}

// ---------------------------------------------------------------
// Replication Cluster E2E Tests
// ---------------------------------------------------------------

test.describe('Replication Cluster', () => {
    test.use({ storageState: '.auth/admin.json' });

    const replication = new ReplicationHelper();

    test.beforeEach(async () => {
        await label('package', 'Replication Cluster');
    });

    test.beforeAll(async () => {
        test.setTimeout(180_000);
        await replication.start();
    });

    test.afterAll(async () => {
        await replication.stop();
    });

    // -----------------------------------------------------------
    // Full replication cluster lifecycle
    // -----------------------------------------------------------
    test('create cluster group, cluster, servers, and verify topology', async ({
        page,
    }) => {
        test.setTimeout(300_000);

        await page.goto('/');
        await expect(
            page.locator('header'),
            'Application header should be visible after load',
        ).toBeVisible({ timeout: 10_000 });
        // Wait for navigator data to load before existence checks
        await page.waitForLoadState('networkidle');

        // -------------------------------------------------------
        // Step 1 — Create Cluster Group
        // -------------------------------------------------------
        await test.step('Create cluster group', async () => {
            const groupExists = await page
                .getByText(GROUP_NAME)
                .count() > 0;
            if (!groupExists) {
                await page
                    .getByRole('button', { name: 'Add server or group' })
                    .click();
                await page
                    .getByRole('menuitem', { name: 'Add Cluster Group' })
                    .waitFor({ state: 'visible', timeout: 5_000 });
                await page
                    .getByRole('menuitem', { name: 'Add Cluster Group' })
                    .click();

                await expect(
                    page.getByRole('dialog'),
                    'Group creation dialog should open',
                ).toBeVisible();

                await page
                    .getByRole('textbox', { name: 'Name' })
                    .fill(GROUP_NAME);
                await page
                    .getByRole('textbox', { name: 'Description' })
                    .fill(GROUP_DESC);
                await page
                    .getByRole('button', { name: 'Save' })
                    .click();
                // Dialog closes on success; if it stays open the group already
                // existed — press Escape to dismiss and continue
                const dialogClosed = await page
                    .getByRole('dialog')
                    .waitFor({ state: 'hidden', timeout: 5_000 })
                    .then(() => true)
                    .catch(() => false);
                if (!dialogClosed) {
                    await page.keyboard.press('Escape');
                    await page.waitForTimeout(300);
                }
            }

            await expect(
                page.getByText(GROUP_NAME).first(),
                `Group "${GROUP_NAME}" should appear in the tree`,
            ).toBeVisible({ timeout: 10_000 });
        });

        // -------------------------------------------------------
        // Step 2 — Create Cluster
        // -------------------------------------------------------
        await test.step('Create binary replication cluster', async () => {
            const clusterExists = await page
                .getByText(CLUSTER_NAME)
                .count() > 0;
            if (!clusterExists) {
                await page
                    .getByRole('button', { name: 'Add server or group' })
                    .click();
                await page
                    .getByRole('menuitem', { name: 'Add Cluster', exact: true })
                    .waitFor({ state: 'visible', timeout: 5_000 });
                await page
                    .getByRole('menuitem', { name: 'Add Cluster', exact: true })
                    .click();

                await expect(
                    page.getByRole('dialog'),
                    'Cluster creation dialog should open',
                ).toBeVisible();

                await page
                    .getByRole('textbox', { name: 'Name' })
                    .fill(CLUSTER_NAME);
                await page
                    .getByRole('textbox', { name: 'Description' })
                    .fill(CLUSTER_DESC);

                await page
                    .getByRole('combobox', { name: 'Replication Type' })
                    .click();
                await page
                    .getByRole('option', { name: 'Binary (Physical)' })
                    .click();

                await page
                    .getByRole('button', { name: 'Create' })
                    .click();

                await expect(
                    page.getByText('Cluster created successfully'),
                    'Success toast should appear after cluster creation',
                ).toBeVisible({ timeout: 10_000 });

                await page
                    .getByRole('button', { name: 'close cluster settings' })
                    .click();
            }

            await expect(
                page.getByText(CLUSTER_NAME).first(),
                `Cluster "${CLUSTER_NAME}" should appear in the tree`,
            ).toBeVisible({ timeout: 10_000 });
        });

        // -------------------------------------------------------
        // Step 3 — Assign Cluster to Group (drag-and-drop)
        // -------------------------------------------------------
        await test.step('Drag cluster into the group', async () => {
            // Check if the cluster is already nested under the group
            const alreadyNested = await page
                .locator('.group-item-row')
                .filter({ hasText: GROUP_NAME })
                .locator('..')
                .getByText(CLUSTER_NAME)
                .first()
                .isVisible();

            if (!alreadyNested) {
                // Hover over the cluster to reveal the drag handle
                const clusterWrapper = page
                    .locator('.draggable-cluster')
                    .filter({ hasText: CLUSTER_NAME })
                    .first();
                await clusterWrapper.hover();

                // Drag handle is the first role=button inside the
                // draggable-cluster wrapper
                const dragHandle = clusterWrapper
                    .getByRole('button')
                    .first();
                const targetGroup = page
                    .locator('.group-item-row')
                    .filter({ hasText: GROUP_NAME })
                    .first();

                // Use manual mouse simulation instead of dragTo()
                // because WebKit does not support the HTML5 drag
                // API that Playwright's dragTo() relies on.
                const dragBox = await dragHandle.boundingBox();
                const targetBox = await targetGroup.boundingBox();
                if (dragBox && targetBox) {
                    const startX = dragBox.x + dragBox.width / 2;
                    const startY = dragBox.y + dragBox.height / 2;
                    const endX = targetBox.x + targetBox.width / 2;
                    const endY = targetBox.y + targetBox.height / 2;

                    await page.mouse.move(startX, startY);
                    await page.mouse.down();
                    // Move in steps to trigger drag events
                    const steps = 10;
                    for (let i = 1; i <= steps; i++) {
                        await page.mouse.move(
                            startX + (endX - startX) * (i / steps),
                            startY + (endY - startY) * (i / steps),
                            { steps: 1 },
                        );
                    }
                    await page.mouse.up();
                }

                // Wait for the move to register
                await page.waitForTimeout(500);
            }

            // Dismiss any dialog the drag may have opened (dnd-kit can
            // interpret a zero-distance drag as a click, opening the
            // ClusterConfigDialog — a fullscreen dialog with a 225ms
            // slide transition).
            await page.keyboard.press('Escape');
            // Wait for the slide-out animation to complete and for
            // moveClusterToGroup's fetchClusterData to settle. The MUI
            // Slide transition is 225ms; we wait longer to cover the
            // async data refresh that follows the move.
            await page.waitForLoadState('networkidle');
            await page.waitForTimeout(500);
            // Ensure the dialog is fully gone from the DOM
            const dialogAfterDrag = page.getByRole('dialog');
            if (await dialogAfterDrag.count() > 0) {
                await dialogAfterDrag.first().waitFor({ state: 'detached', timeout: 5_000 });
            }

            // Only click the group row to expand it if the cluster is
            // not already visible underneath it.  An unconditional click
            // would collapse an already-expanded group.
            const clusterVisibleUnderGroup = await page
                .locator('.group-item-row')
                .filter({ hasText: GROUP_NAME })
                .locator('..')
                .getByText(CLUSTER_NAME)
                .first()
                .isVisible();
            if (!clusterVisibleUnderGroup) {
                const groupRow = page
                    .locator('.group-item-row')
                    .filter({ hasText: GROUP_NAME })
                    .first();
                await groupRow.click();
                await page.waitForTimeout(300);
            }

            // Assert the cluster now appears under the group
            await expect(
                page
                    .locator('.group-item-row')
                    .filter({ hasText: GROUP_NAME })
                    .locator('..')
                    .getByText(CLUSTER_NAME).first(),
                `Cluster "${CLUSTER_NAME}" should be nested under group "${GROUP_NAME}"`,
            ).toBeVisible({ timeout: 10_000 });
        });

        // -------------------------------------------------------
        // Steps 4-6 — Add three servers
        // -------------------------------------------------------
        await test.step('Add primary-node server', async () => {
            await addServer(page, SERVERS[0]);
        });

        await test.step('Add standby-node-1 server', async () => {
            await addServer(page, SERVERS[1]);
        });

        await test.step('Add standby-node-2 server', async () => {
            await addServer(page, SERVERS[2]);
        });

        // -------------------------------------------------------
        // Step 6.5 — Expand cluster to show server rows
        // -------------------------------------------------------
        await test.step('Expand cluster to reveal server rows', async () => {
            const primaryRow = page
                .locator('.server-item-row')
                .filter({ hasText: 'primary-node' })
                .first();
            const isExpanded = await primaryRow.isVisible();
            if (!isExpanded) {
                // Click the expand/collapse IconButton (chevron) inside the
                // cluster header — NOT the cluster row itself, which would
                // open the cluster config dialog instead of toggling expand.
                const clusterHeader = page
                    .locator('.draggable-cluster')
                    .filter({ hasText: CLUSTER_NAME })
                    .first()
                    .locator('.cluster-header');
                await clusterHeader
                    .getByRole('button')
                    .first()
                    .click();
            }
            await expect(
                primaryRow,
                'primary-node server row should be visible after expanding cluster',
            ).toBeVisible({ timeout: 10_000 });
        });

        // -------------------------------------------------------
        // Step 6.75 — Restart the collector so it picks up the
        // three new servers immediately (avoids the 5-minute
        // config-reload interval that causes Initializing timeout)
        // -------------------------------------------------------
        await test.step('Restart collector to pick up new servers', async () => {
            await replication.restartCollector(SERVERS.map(s => s.name));
        });

        // -------------------------------------------------------
        // Step 7 — Wait for initialization to complete
        // -------------------------------------------------------
        await test.step('Wait for all servers to finish initializing', async () => {
            // restartCollector() already confirmed via the API that the
            // servers have left 'initialising' state before returning, so
            // the DB is already up to date.  The navigator auto-refreshes
            // every 30 seconds; this loop gives it up to 60 seconds to
            // pick up the change.  We deliberately do NOT click the
            // auto-refresh toggle button here — the old
            // getByRole('button', { name: 'Refresh' }) selector used a
            // substring match that accidentally matched "Auto-refresh
            // enabled", toggling auto-refresh off on every iteration and
            // preventing the UI from ever updating.
            await expect(async () => {
                await page.waitForTimeout(1_000);

                const count = await page.getByText('Initializing').count();
                expect(
                    count,
                    'Expected all servers to finish initializing',
                ).toBe(0);
            }).toPass({ timeout: 60_000, intervals: [5_000] });
        });

        // -------------------------------------------------------
        // Step 8 — Assert final topology
        // -------------------------------------------------------
        await test.step('Verify final replication topology', async () => {
            // Primary node shows "Primary" pill
            const primaryRow = page
                .locator('.server-item-row')
                .filter({ hasText: 'primary-node' })
                .first();
            await expect(
                primaryRow.getByText('Primary', { exact: true }),
                'primary-node should display the Primary role pill',
            ).toBeVisible({ timeout: 10_000 });

            // Standby node 1 shows "Standby" pill
            const standby1Row = page
                .locator('.server-item-row')
                .filter({ hasText: 'standby-node-1' })
                .first();
            await expect(
                standby1Row.getByText('Standby', { exact: true }),
                'standby-node-1 should display the Standby role pill',
            ).toBeVisible({ timeout: 10_000 });

            // Standby node 2 shows "Standby" pill
            const standby2Row = page
                .locator('.server-item-row')
                .filter({ hasText: 'standby-node-2' })
                .first();
            await expect(
                standby2Row.getByText('Standby', { exact: true }),
                'standby-node-2 should display the Standby role pill',
            ).toBeVisible({ timeout: 10_000 });

            // No servers are still initializing
            const initCount = await page
                .getByText('Initializing')
                .count();
            expect(
                initCount,
                'No servers should be in Initializing state',
            ).toBe(0);
        });

    });
});
