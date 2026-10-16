/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Page, expect } from '@playwright/test';
import { ApiHelper } from './api.helper';
import { StoreHelper } from './store.helper';
import { ReplicationHelper } from './replication.helper';

/**
 * Shared setup for specs that need the replication primary registered
 * in the Workbench. The names, host, and port mirror those used by
 * replication-cluster.spec.ts so both specs converge on the same tree
 * entities; every creation step is guarded by an existence check, so
 * the two specs compose regardless of execution order.
 *
 * This helper deliberately creates the cluster WITHOUT a cluster group
 * and WITHOUT the drag-and-drop nesting used by replication-cluster
 * (an ungrouped cluster is still selectable when adding a server),
 * keeping the setup lean and free of the flaky HTML5 drag interaction.
 */

export const CLUSTER_NAME = 'Test Binary Cluster';
export const CLUSTER_DESC = '3-node replication cluster';

/** The replication primary configuration (port 5440, database ragdb). */
export const PRIMARY_SERVER = {
    name: 'primary-node',
    host: process.env['E2E_REPLICATION_HOST'] ?? 'postgres',
    port: 5440,
    db: 'ragdb',
    user: 'postgres',
    pass: process.env['POSTGRES_PASSWORD'] ?? 'postgres',
    role: 'Primary' as const,
};

/**
 * Ensure the binary cluster exists and the primary server is
 * registered under it. Idempotent: re-running is a no-op once the
 * entities exist. Leaves the cluster expanded so the primary row is
 * visible for selection.
 */
export async function ensureClusterAndPrimary(
    page: Page,
    api: ApiHelper,
    cookie: string,
): Promise<void> {
    await page.goto('/');
    await expect(
        page.locator('header'),
        'Application header should be visible after load',
    ).toBeVisible({ timeout: 15_000 });
    await page.waitForLoadState('load');

    await ensureCluster(page, api, cookie);
    await addPrimaryServer(page, api, cookie);
    await expandClusterAndVerifyPrimary(page);
}

/**
 * The server-scoped system_stats probes whose data backs the System
 * Resources KPI tiles and CPU/memory graphs.
 */
const SYSTEM_PROBES = [
    'pg_sys_cpu_usage_info',
    'pg_sys_memory_info',
    'pg_sys_load_avg_info',
    'pg_sys_disk_info',
];

/** Fast collection interval (seconds) so a fresh sample lands promptly. */
const FAST_INTERVAL_SECONDS = 15;

/**
 * Ensure fresh system_stats samples exist for the primary connection so
 * the System Resources tiles/graphs resolve to real values instead of
 * "no data". The default pg_sys_* interval is 600s, so on a warm stack
 * the newest sample can be minutes old (or absent) — assertions on the
 * Memory/CPU tiles then flake. This enables the extension, lowers the
 * probe interval, restarts the collector, and waits until rows land in
 * the metrics tables (the same ground truth the UI reads).
 *
 * Idempotent and safe to call once per suite in `beforeAll`.
 */
export async function primeSystemMetrics(
    api: ApiHelper,
    cookie: string,
    connectionId: number,
): Promise<void> {
    const store = new StoreHelper();
    const replication = new ReplicationHelper();

    // Enable + speed up the pg_sys_* probes.
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

    // Ensure the extension is present (created as the postgres superuser;
    // the MCP query path runs read-only and cannot CREATE EXTENSION).
    await store
        .execPrimary('CREATE EXTENSION IF NOT EXISTS system_stats;')
        .catch(() => undefined);

    // Restart so the collector picks up the new config and collects now.
    await replication.restartCollector([PRIMARY_SERVER.name]);

    // Wait for at least one stored sample per core probe.
    await expect(async () => {
        for (const table of [
            'pg_sys_cpu_usage_info',
            'pg_sys_memory_info',
        ]) {
            const count = await store.queryScalar(
                `SELECT count(*) FROM metrics.${table} ` +
                `WHERE connection_id = ${connectionId}`,
            );
            expect(
                Number.parseInt(count ?? '0', 10),
                `A stored sample should exist for ${table}`,
            ).toBeGreaterThan(0);
        }
    }).toPass({ timeout: 180_000, intervals: [5_000] });
}

// ---------------------------------------------------------------
// Internal steps
// ---------------------------------------------------------------

async function ensureCluster(
    page: Page,
    api: ApiHelper,
    cookie: string,
): Promise<void> {
    // Race-free existence check. The navigator tree loads asynchronously,
    // so a DOM count can miss an already-created cluster and trigger a
    // duplicate create — which the server rejects with a unique-constraint
    // error (clusters_group_name_unique). Ask the API instead so the
    // helper is idempotent even against a reused, warm stack.
    const clusters = await api.listClusters(cookie);
    if (clusters.some((c) => c.name === CLUSTER_NAME)) {
        return;
    }

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

    await page.getByRole('textbox', { name: 'Name' }).fill(CLUSTER_NAME);
    await page
        .getByRole('textbox', { name: 'Description' })
        .fill(CLUSTER_DESC);

    await page
        .getByRole('combobox', { name: 'Replication Type' })
        .click();
    await page
        .getByRole('option', { name: 'Binary (Physical)' })
        .click();

    await page.getByRole('button', { name: 'Create' }).click();

    await expect(
        page.getByText('Cluster created successfully'),
        'Success toast should appear after cluster creation',
    ).toBeVisible({ timeout: 10_000 });

    await page
        .getByRole('button', { name: 'close cluster settings' })
        .click();

    await expect(
        page.getByText(CLUSTER_NAME).first(),
        `Cluster "${CLUSTER_NAME}" should appear in the tree`,
    ).toBeVisible({ timeout: 10_000 });
}

async function addPrimaryServer(
    page: Page,
    api: ApiHelper,
    cookie: string,
): Promise<void> {
    const cfg = PRIMARY_SERVER;
    // Race-free existence check via the API (see ensureCluster). Servers
    // surface as connections, so an existing primary is detectable here.
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
    // fetchClusterData re-render can otherwise swallow the click.
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

    // Use getByRole for Name to avoid matching Username (substring).
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
        .fill(CLUSTER_NAME);
    await page
        .getByRole('option', { name: CLUSTER_NAME })
        .first()
        .click();

    // Use the combobox role, not getByLabel('Role'): getByLabel substring-
    // matches and collides with Top Queries rows whose aria-label contains a
    // role-listing query (e.g. "SELECT rolname, rolsuper, rolcreaterole…").
    await page.getByRole('combobox', { name: 'Role' }).click();
    await page.getByRole('option', { name: cfg.role }).click();

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

async function expandClusterAndVerifyPrimary(page: Page): Promise<void> {
    const primaryRow = page
        .locator('.server-item-row')
        .filter({ hasText: PRIMARY_SERVER.name })
        .first();

    if (!(await primaryRow.isVisible().catch(() => false))) {
        const clusterHeader = page
            .locator('.draggable-cluster')
            .filter({ hasText: CLUSTER_NAME })
            .first()
            .locator('.cluster-header');
        await clusterHeader.getByRole('button').first().click();
    }

    await expect(
        primaryRow,
        'primary-node row should be visible after expanding cluster',
    ).toBeVisible({ timeout: 10_000 });
}
