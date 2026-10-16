/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Page, type Locator, expect, test } from '@playwright/test';
import { ApiHelper } from './api.helper';
import { StoreHelper } from './store.helper';
import { ReplicationHelper } from './replication.helper';

/**
 * Shared setup for the NATIVE Spock (multi-master) replication specs. Mirrors
 * logical-cluster-setup.helper.ts, but creates a cluster whose replication type
 * is "Spock" and registers three nodes, each with the "Node" role. Every
 * creation step is guarded by an API existence check, so the specs compose
 * regardless of execution order.
 *
 * The three nodes are separate containers provisioned by
 * start-spock-replication.sh (see SpockReplicationHelper); the workbench reaches
 * them over the e2e docker network as pg-spock-node1 / pg-spock-node2 /
 * pg-spock-node3 on port 5432.
 */

export const SPOCK_CLUSTER_NAME = 'Test Spock Cluster';
export const SPOCK_CLUSTER_DESC = '3-node Spock multi-master cluster';

/** Node configuration for a spock-cluster member. */
export interface SpockNode {
    name: string;
    host: string;
    port: number;
    db: string;
    user: string;
    pass: string;
    /**
     * Exact role option label in the Add Server dialog. A Spock cluster offers
     * "Node"/"Standby" (topologyHelpers.getRolesForType('spock')); every mesh
     * member takes the "Node" role.
     */
    role: 'Node';
}

const PG_PASSWORD = process.env['POSTGRES_PASSWORD'] ?? 'postgres';

/** Mesh node n1 (pg-spock-node1). */
export const SPOCK_NODE1: SpockNode = {
    name: 'spock-node1',
    host: process.env['E2E_SPOCK_NODE1_HOST'] ?? 'pg-spock-node1',
    port: 5432,
    db: 'ragdb',
    user: 'postgres',
    pass: PG_PASSWORD,
    role: 'Node',
};

/** Mesh node n2 (pg-spock-node2). */
export const SPOCK_NODE2: SpockNode = {
    name: 'spock-node2',
    host: process.env['E2E_SPOCK_NODE2_HOST'] ?? 'pg-spock-node2',
    port: 5432,
    db: 'ragdb',
    user: 'postgres',
    pass: PG_PASSWORD,
    role: 'Node',
};

/** Mesh node n3 (pg-spock-node3). */
export const SPOCK_NODE3: SpockNode = {
    name: 'spock-node3',
    host: process.env['E2E_SPOCK_NODE3_HOST'] ?? 'pg-spock-node3',
    port: 5432,
    db: 'ragdb',
    user: 'postgres',
    pass: PG_PASSWORD,
    role: 'Node',
};

/** All three mesh nodes in order, for convenient iteration. */
export const SPOCK_NODES: readonly SpockNode[] = [
    SPOCK_NODE1,
    SPOCK_NODE2,
    SPOCK_NODE3,
];

/**
 * Ensure the spock cluster exists and all three nodes are registered under it.
 * Idempotent. Leaves the cluster expanded so the node rows are selectable.
 */
export async function ensureSpockClusterAndNodes(
    page: Page,
    api: ApiHelper,
    cookie: string,
): Promise<void> {
    // The AI-recreate window (global.setup restarts server/alerter to apply the
    // LLM config) can leave the app shell briefly unrenderable; give the whole
    // GUI bring-up a generous budget.
    test.setTimeout(180_000);

    // Retry the initial load until the shell is up (harmless first-try pass when
    // nothing restarted — mirrors ensureLogicalClusterAndNodes).
    await expect(async () => {
        await page.goto('/');
        await expect(
            page.locator('header'),
            'Application header should be visible after load',
        ).toBeVisible({ timeout: 10_000 });
    }).toPass({ timeout: 90_000, intervals: [3_000] });
    await page.waitForLoadState('load');

    await ensureSpockCluster(page, api, cookie);
    for (const node of SPOCK_NODES) {
        await addSpockNode(page, api, cookie, node);
    }
    await expandClusterAndVerify(page);
}

/**
 * Open the spock cluster's settings dialog via the navigator gear icon and
 * return the dialog locator. The gear is an unlabeled IconButton containing a
 * `SettingsIcon`, revealed on hover over the cluster header.
 */
export async function openSpockClusterSettings(page: Page): Promise<Locator> {
    const clusterItem = page
        .locator('.draggable-cluster')
        .filter({ hasText: SPOCK_CLUSTER_NAME })
        .first();
    await expect(
        clusterItem,
        'spock cluster should appear in the navigator',
    ).toBeVisible({ timeout: 15_000 });

    await clusterItem.locator('.cluster-header').hover();
    await clusterItem
        .locator('button:has([data-testid="SettingsIcon"])')
        .first()
        .click({ force: true });

    const dialog = page.getByRole('dialog');
    await expect(dialog, 'cluster settings dialog should open').toBeVisible({
        timeout: 10_000,
    });
    await expect(dialog).toContainText('Cluster Settings');
    return dialog;
}

// ---------------------------------------------------------------
// Internal steps
// ---------------------------------------------------------------

async function ensureSpockCluster(
    page: Page,
    api: ApiHelper,
    cookie: string,
): Promise<void> {
    // Race-free existence check via the API (see cluster-setup.helper).
    const clusters = await api.listClusters(cookie);
    if (clusters.some((c) => c.name === SPOCK_CLUSTER_NAME)) {
        return;
    }

    await page.getByRole('button', { name: 'Add server or group' }).click();
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
        .fill(SPOCK_CLUSTER_NAME);
    await page
        .getByRole('textbox', { name: 'Description' })
        .fill(SPOCK_CLUSTER_DESC);

    await page.getByRole('combobox', { name: 'Replication Type' }).click();
    await page.getByRole('option', { name: 'Spock', exact: true }).click();

    await page.getByRole('button', { name: 'Create' }).click();

    await expect(
        page.getByText('Cluster created successfully'),
        'Success toast should appear after cluster creation',
    ).toBeVisible({ timeout: 10_000 });

    await page
        .getByRole('button', { name: 'close cluster settings' })
        .click();

    await expect(
        page.getByText(SPOCK_CLUSTER_NAME).first(),
        `Cluster "${SPOCK_CLUSTER_NAME}" should appear in the tree`,
    ).toBeVisible({ timeout: 10_000 });
}

async function addSpockNode(
    page: Page,
    api: ApiHelper,
    cookie: string,
    cfg: SpockNode,
): Promise<void> {
    // Race-free existence check via the API (servers surface as connections).
    const connections = await api.listConnections({ cookie });
    if (connections.some((c) => c.name === cfg.name)) {
        return;
    }

    await page.waitForLoadState('load');
    await expect(
        page.locator('header'),
        'App header should be visible before opening Add Server menu',
    ).toBeVisible({ timeout: 10_000 });

    // Retry the open-menu → Add Server → dialog sequence; a concurrent
    // re-render can otherwise swallow the click (see cluster-setup.helper).
    await expect(async () => {
        const menuOpen =
            (await page
                .getByRole('menuitem', { name: 'Add Server' })
                .count()) > 0;
        if (menuOpen) {
            await page.keyboard.press('Escape');
            await page.waitForTimeout(200);
        }

        await page
            .getByRole('button', { name: 'Add server or group' })
            .click();
        await page
            .getByRole('menuitem', { name: 'Add Server' })
            .waitFor({ state: 'visible', timeout: 5_000 });
        await page.getByRole('menuitem', { name: 'Add Server' }).click();

        await expect(page.getByRole('dialog')).toBeVisible({
            timeout: 5_000,
        });
    }).toPass({ timeout: 20_000, intervals: [1_000] });

    await page
        .getByRole('textbox', { name: 'Name', exact: true })
        .fill(cfg.name);
    await page.getByLabel('Host').fill(cfg.host);
    await page.getByLabel('Port').fill(String(cfg.port));
    await page.getByLabel('Maintenance Database').fill(cfg.db);
    await page.getByLabel('Username').fill(cfg.user);
    await page.getByLabel('Password').fill(cfg.pass);

    await page
        .getByRole('combobox', { name: 'Cluster' })
        .fill(SPOCK_CLUSTER_NAME);
    await page
        .getByRole('option', { name: SPOCK_CLUSTER_NAME })
        .first()
        .click();

    // Role options are derived from the selected cluster's replication type; a
    // Spock cluster offers Node/Standby, and every mesh member is a Node.
    await page.getByRole('combobox', { name: 'Role' }).click();
    await page.getByRole('option', { name: cfg.role, exact: true }).click();

    await page.getByRole('button', { name: 'Save' }).click();
    await expect(
        page.getByRole('dialog'),
        `Dialog should close after saving server "${cfg.name}"`,
    ).not.toBeVisible({ timeout: 15_000 });

    await expect(async () => {
        const count = await page.getByText(cfg.name).count();
        expect(
            count,
            `Server "${cfg.name}" should appear in the navigator`,
        ).toBeGreaterThan(0);
    }).toPass({ timeout: 10_000, intervals: [500] });
}

/**
 * The server-scoped system_stats probes whose data backs the System Resources
 * tiles/graphs (mirrors cluster-setup.helper).
 */
const SYSTEM_PROBES = [
    'pg_sys_cpu_usage_info',
    'pg_sys_memory_info',
    'pg_sys_load_avg_info',
    'pg_sys_disk_info',
];
const FAST_INTERVAL_SECONDS = 15;

/**
 * Ensure fresh system_stats samples exist for a spock node so the System
 * Resources tiles/graphs resolve to real values. Mirrors
 * primeLogicalNodeMetrics: restarts the collector waiting on node1 and skips
 * the CREATE EXTENSION step — start-spock-replication.sh already created
 * system_stats on all three nodes.
 */
export async function primeSpockNodeMetrics(
    api: ApiHelper,
    cookie: string,
    connectionId: number,
): Promise<void> {
    const store = new StoreHelper();
    const replication = new ReplicationHelper();

    const configs = await api.getProbeConfigs(cookie);
    for (const probeName of SYSTEM_PROBES) {
        const cfg = configs.find((c) => c.name === probeName);
        if (!cfg) {
            continue;
        }
        await api.updateProbeConfig(cookie, cfg.id, {
            is_enabled: true,
            collection_interval_seconds: FAST_INTERVAL_SECONDS,
        });
    }

    // Restart so the collector picks up the new interval and collects now,
    // waiting until node1 leaves the 'initialising' state.
    await replication.restartCollector([SPOCK_NODE1.name]);

    await expect(async () => {
        for (const table of ['pg_sys_cpu_usage_info', 'pg_sys_memory_info']) {
            const count = await store.queryScalar(
                `SELECT count(*) FROM metrics.${table} `
                + `WHERE connection_id = ${connectionId}`,
            );
            expect(
                Number.parseInt(count ?? '0', 10),
                `A stored sample should exist for ${table}`,
            ).toBeGreaterThan(0);
        }
    }).toPass({ timeout: 180_000, intervals: [5_000] });
}

export async function expandClusterAndVerify(page: Page): Promise<void> {
    const nodeRow = page
        .locator('.server-item-row')
        .filter({ hasText: SPOCK_NODE1.name })
        .first();

    if (!(await nodeRow.isVisible().catch(() => false))) {
        const clusterHeader = page
            .locator('.draggable-cluster')
            .filter({ hasText: SPOCK_CLUSTER_NAME })
            .first()
            .locator('.cluster-header');
        await clusterHeader.getByRole('button').first().click();
    }

    await expect(
        nodeRow,
        `${SPOCK_NODE1.name} row should be visible after expanding cluster`,
    ).toBeVisible({ timeout: 10_000 });
}
