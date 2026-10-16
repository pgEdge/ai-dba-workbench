/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ApiHelper } from '../helpers/api.helper';
import { AuthHelper } from '../helpers/auth.helper';
import { ReplicationHelper } from '../helpers/replication.helper';
import { MonitoringPage } from '../pages/MonitoringPage';
import { ServerNavigatorPage } from '../pages/ServerNavigatorPage';
import {
    ensureClusterAndPrimary,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../helpers/cluster-setup.helper';
import { API_URL } from '../fixtures/test-data';

/**
 * GUI cleanup for the PHYSICAL (binary) cluster — removes its server and then
 * the cluster itself through the navigator's delete controls, asserting both
 * are gone from the navigator AND the API.
 *
 * Placed at the specs ROOT as `zz-` so it sorts AFTER every physical-cluster
 * spec (specs/monitoring/* plus the root replication-cluster / server-*
 * specs) but BEFORE the logical suite (specs/zzz-logical/*, which sorts after
 * `zz-`). This gives the full-run sequence: all physical specs → this cleanup
 * (which also STOPS the physical replication instances, see afterAll) → all
 * logical specs → logical cleanup. Running physical and logical back-to-back
 * rather than concurrently keeps resource use down.
 *
 * Self-contained (it first ensures the cluster exists), so it also works when
 * run on its own. The workbench registration is removed here; the replication
 * instances are stopped in afterAll.
 */
test.describe('Cluster Cleanup — Physical', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const replication = new ReplicationHelper();

    let adminCookie: string;

    test.beforeEach(async () => {
        await label('package', 'Cleanup');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.afterAll(async () => {
        // Stop the physical replication instances (5440/5441/5442) AND remove
        // their data directories to free BOTH RAM/CPU and disk for the logical
        // suite that runs after this spec (specs/zzz-logical/*). Unlike stop()
        // (a no-op), forceStop() halts the Postgres processes and deletes the
        // /data/repl-* dirs; the physical specs re-create them from scratch on
        // the next run via ReplicationHelper.start(). The ai_workbench
        // datastore (main Postgres, default PGDATA) is untouched.
        await replication.forceStop().catch(() => undefined);
    });

    test('removes the physical cluster and its server via the GUI, then asserts removal', async ({
        page,
    }) => {
        test.setTimeout(180_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        // Self-contained: ensure the cluster + primary exist so this test is
        // order-independent.
        await ensureClusterAndPrimary(page, api, adminCookie);

        await page.goto('/');
        await monitoring.waitForAppLoad();

        await test.step('Delete every server in the cluster via the GUI', async () => {
            // The shared physical cluster can hold more than one server (other
            // specs register servers under it), so remove them all.
            await serverNav.deleteAllServersInCluster(CLUSTER_NAME);
            const connections = await api.listConnections({
                cookie: adminCookie,
            });
            expect(
                connections.some((c) => c.name === PRIMARY_SERVER.name),
                'the primary server should be removed from the API',
            ).toBe(false);
        });

        await test.step('Delete the now-empty cluster via the GUI', async () => {
            await serverNav.deleteCluster(CLUSTER_NAME);
        });

        await test.step('Assert the cluster is gone from the navigator and the API', async () => {
            await expect(
                page
                    .locator('.draggable-cluster')
                    .filter({ hasText: CLUSTER_NAME }),
                'cluster should be gone from the navigator',
            ).toHaveCount(0, { timeout: 15_000 });

            const clusters = await api.listClusters(adminCookie);
            expect(
                clusters.some((c) => c.name === CLUSTER_NAME),
                'cluster should be removed from the API',
            ).toBe(false);
        });
    });
});
