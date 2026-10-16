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
import { ApiHelper } from '../../helpers/api.helper';
import { AuthHelper } from '../../helpers/auth.helper';
import { SpockReplicationHelper } from '../../helpers/spock-replication.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import {
    ensureSpockClusterAndNodes,
    SPOCK_CLUSTER_NAME,
    SPOCK_NODE1,
    SPOCK_NODE2,
    SPOCK_NODE3,
} from '../../helpers/spock-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * GUI cleanup for the SPOCK cluster: removes all three mesh nodes and then the
 * cluster itself through the navigator's delete controls, asserting all are
 * gone from the navigator AND the API. Named `zz-` so it runs last in
 * specs/zzz-spock/; self-contained (ensures the cluster exists first). As the
 * final spock spec, its afterAll also STOPS AND REMOVES the three dedicated
 * spock-node containers (pg-spock-node1 / pg-spock-node2 / pg-spock-node3) to
 * free their resources — the spock mirror of the logical cleanup's
 * LogicalReplicationHelper.remove().
 */
test.describe('Cluster Cleanup — Spock', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const spock = new SpockReplicationHelper();

    let adminCookie: string;

    test.beforeEach(async () => {
        await label('package', 'Monitoring (Spock)');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await spock.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.afterAll(async () => {
        // This is the last spock spec: stop and remove the dedicated spock
        // node containers so they do not linger after the run. Best-effort —
        // the stack teardown is the final backstop.
        await spock.remove().catch(() => undefined);
    });

    test('removes the spock cluster and all three nodes via the GUI, then asserts removal', async ({
        page,
    }) => {
        test.setTimeout(180_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await ensureSpockClusterAndNodes(page, api, adminCookie);

        await test.step('Delete every node in the cluster via the GUI', async () => {
            await page.goto('/');
            await monitoring.waitForAppLoad();
            await serverNav.deleteAllServersInCluster(SPOCK_CLUSTER_NAME);

            const connections = await api.listConnections({
                cookie: adminCookie,
            });
            for (const nodeName of [
                SPOCK_NODE1.name,
                SPOCK_NODE2.name,
                SPOCK_NODE3.name,
            ]) {
                expect(
                    connections.some((c) => c.name === nodeName),
                    `node "${nodeName}" should be removed from the API`,
                ).toBe(false);
            }
        });

        await test.step('Delete the now-empty cluster via the GUI', async () => {
            await page.goto('/');
            await monitoring.waitForAppLoad();
            await serverNav.deleteCluster(SPOCK_CLUSTER_NAME);
        });

        await test.step('Assert the cluster is gone from the navigator and the API', async () => {
            await expect(
                page
                    .locator('.draggable-cluster')
                    .filter({ hasText: SPOCK_CLUSTER_NAME }),
                'cluster should be gone from the navigator',
            ).toHaveCount(0, { timeout: 15_000 });

            const clusters = await api.listClusters(adminCookie);
            expect(
                clusters.some((c) => c.name === SPOCK_CLUSTER_NAME),
                'cluster should be removed from the API',
            ).toBe(false);
        });
    });
});
