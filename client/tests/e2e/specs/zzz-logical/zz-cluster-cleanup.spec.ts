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
import { LogicalReplicationHelper } from '../../helpers/logical-replication.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import {
    ensureLogicalClusterAndNodes,
    LOGICAL_CLUSTER_NAME,
    LOGICAL_NODE1,
    LOGICAL_NODE2,
} from '../../helpers/logical-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * GUI cleanup for the LOGICAL cluster: removes both Publisher/Subscriber
 * nodes and then the cluster itself through the navigator's delete controls,
 * asserting all are gone from the navigator AND the API. Named `zz-` so it
 * runs last in specs/monitoring/logical/; self-contained (ensures the cluster
 * exists first). As the final logical spec, its afterAll also STOPS AND
 * REMOVES the two dedicated logical-node containers (pg-logical-node1 /
 * pg-logical-node2) to free their resources — the logical mirror of the
 * physical cleanup's ReplicationHelper.forceStop().
 */
test.describe('Cluster Cleanup — Logical', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const logical = new LogicalReplicationHelper();

    let adminCookie: string;

    test.beforeEach(async () => {
        await label('package', 'Monitoring (Logical)');
    });

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await logical.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.afterAll(async () => {
        // This is the last logical spec: stop and remove the dedicated logical
        // node containers so they do not linger after the run. Best-effort —
        // the stack teardown is the final backstop.
        await logical.remove().catch(() => undefined);
    });

    test('removes the logical cluster and both nodes via the GUI, then asserts removal', async ({
        page,
    }) => {
        test.setTimeout(180_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);

        await ensureLogicalClusterAndNodes(page, api, adminCookie);

        await test.step('Delete every node in the cluster via the GUI', async () => {
            await page.goto('/');
            await monitoring.waitForAppLoad();
            await serverNav.deleteAllServersInCluster(LOGICAL_CLUSTER_NAME);

            const connections = await api.listConnections({
                cookie: adminCookie,
            });
            for (const nodeName of [LOGICAL_NODE1.name, LOGICAL_NODE2.name]) {
                expect(
                    connections.some((c) => c.name === nodeName),
                    `node "${nodeName}" should be removed from the API`,
                ).toBe(false);
            }
        });

        await test.step('Delete the now-empty cluster via the GUI', async () => {
            await page.goto('/');
            await monitoring.waitForAppLoad();
            await serverNav.deleteCluster(LOGICAL_CLUSTER_NAME);
        });

        await test.step('Assert the cluster is gone from the navigator and the API', async () => {
            await expect(
                page
                    .locator('.draggable-cluster')
                    .filter({ hasText: LOGICAL_CLUSTER_NAME }),
                'cluster should be gone from the navigator',
            ).toHaveCount(0, { timeout: 15_000 });

            const clusters = await api.listClusters(adminCookie);
            expect(
                clusters.some((c) => c.name === LOGICAL_CLUSTER_NAME),
                'cluster should be removed from the API',
            ).toBe(false);
        });
    });
});
