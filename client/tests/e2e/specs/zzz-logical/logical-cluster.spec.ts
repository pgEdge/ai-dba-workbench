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
import {
    ensureLogicalClusterAndNodes,
    openLogicalClusterSettings,
    LOGICAL_CLUSTER_NAME,
} from '../../helpers/logical-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

/**
 * Foundation spec for the logical-replication suite: provisions the two-node
 * native logical cluster (Publisher/Subscriber) and asserts, both through the
 * GUI cluster settings and via the API, that its replication type is Logical.
 * Every other logical spec reuses ensureLogicalClusterAndNodes, so this also
 * doubles as the setup smoke test.
 */
test.describe('Logical Replication Cluster', () => {
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
        // Idempotently (re)establish the publication/subscription between the
        // two logical-node containers before any registration happens.
        await logical.start();
        adminCookie = await auth.loginAsAdmin();
    });

    test.afterAll(async () => {
        await logical.stop().catch(() => undefined);
    });

    test('two-node logical cluster is created with replication type Logical', async ({
        page,
    }) => {
        test.setTimeout(180_000);

        await test.step('Create the logical cluster and register both nodes', async () => {
            await ensureLogicalClusterAndNodes(page, api, adminCookie);
        });

        await test.step('Cluster settings show replication type "Logical" (GUI)', async () => {
            const dialog = await openLogicalClusterSettings(page);
            await expect(
                dialog.getByRole('combobox', { name: 'Replication Type' }),
                'cluster settings should display replication type Logical',
            ).toContainText('Logical');
            await dialog
                .getByRole('button', { name: 'close cluster settings' })
                .click();
        });

        await test.step('Cluster replication_type is logical (API backstop)', async () => {
            const clusters = await api.listClusters(adminCookie);
            const cluster = clusters.find(
                (c) => c.name === LOGICAL_CLUSTER_NAME,
            );
            expect(
                cluster,
                `cluster "${LOGICAL_CLUSTER_NAME}" should exist`,
            ).toBeDefined();
            expect(
                (cluster?.replication_type ?? '').toLowerCase(),
                'cluster replication_type should be logical',
            ).toBe('logical');
        });
    });
});
