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
import {
    ensureSpockClusterAndNodes,
    openSpockClusterSettings,
    SPOCK_CLUSTER_NAME,
} from '../../helpers/spock-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * Foundation spec for the spock-replication suite: provisions the three-node
 * native Spock (multi-master mesh) cluster and asserts, both through the GUI
 * cluster settings and via the API, that its replication type is Spock. Every
 * other spock spec reuses ensureSpockClusterAndNodes, so this also doubles as
 * the setup smoke test.
 */
test.describe('Spock Replication Cluster', () => {
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
        // Idempotently (re)establish the Spock mesh across the three node
        // containers before any registration happens.
        await spock.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.afterAll(async () => {
        await spock.stop().catch(() => undefined);
    });

    test('three-node spock cluster is created with replication type Spock', async ({
        page,
    }) => {
        test.setTimeout(180_000);

        await test.step('Create the spock cluster and register all three nodes', async () => {
            await ensureSpockClusterAndNodes(page, api, adminCookie);
        });

        await test.step('Cluster settings show replication type "Spock" (GUI)', async () => {
            const dialog = await openSpockClusterSettings(page);
            await expect(
                dialog.getByRole('combobox', { name: 'Replication Type' }),
                'cluster settings should display replication type Spock',
            ).toContainText('Spock');
            await dialog
                .getByRole('button', { name: 'close cluster settings' })
                .click();
        });

        await test.step('Cluster replication_type is spock (API backstop)', async () => {
            const clusters = await api.listClusters(adminCookie);
            const cluster = clusters.find(
                (c) => c.name === SPOCK_CLUSTER_NAME,
            );
            expect(
                cluster,
                `cluster "${SPOCK_CLUSTER_NAME}" should exist`,
            ).toBeDefined();
            expect(
                (cluster?.replication_type ?? '').toLowerCase(),
                'cluster replication_type should be spock',
            ).toBe('spock');
        });
    });
});
