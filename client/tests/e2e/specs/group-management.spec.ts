/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ApiHelper } from '../helpers/api.helper';
import { AuthHelper } from '../helpers/auth.helper';
import { AdminPage } from '../pages/AdminPage';
import { GroupManagementPage } from '../pages/GroupManagementPage';
import {
    ADMIN_USER,
    API_URL,
    makeTestUsername,
} from '../fixtures/test-data';

// ---------------------------------------------------------------
// Shared state
// ---------------------------------------------------------------

const api = new ApiHelper(API_URL);
const auth = new AuthHelper(api);

// ---------------------------------------------------------------
// Group Management Tests
// ---------------------------------------------------------------

test.describe('Group Management', () => {
    // Apply admin storage state to all browser-based tests in this
    // describe block so the UI tests skip the manual login flow.
    test.use({ storageState: '.auth/admin.json' });

    let adminCookie: string;

    test.beforeEach(async () => {
        await label('package', 'Group Management');
    });

    test.beforeAll(async () => {
        adminCookie = await auth.loginAsAdmin();
    });

    // -------------------------------------------------------
    // 1. Create group via UI
    // -------------------------------------------------------
    test('create group via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const groupPage = new GroupManagementPage(page);
        const groupName = makeTestUsername('grp-create');
        const description = 'E2E test group description';

        await test.step('Navigate to Admin > Groups', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToGroups();
        });

        await test.step('Create group via dialog', async () => {
            await groupPage.createGroup(groupName, description);
        });

        await test.step('Verify new group in table', async () => {
            await groupPage.expectGroupInTable(groupName);
            await groupPage.expectDescriptionInTable(
                groupName,
                description,
            );
        });
    });

    // -------------------------------------------------------
    // 2. Update group via UI
    // -------------------------------------------------------
    test('update group via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const groupPage = new GroupManagementPage(page);
        const groupName = makeTestUsername('grp-update');
        const newGroupName = makeTestUsername('grp-updated');
        const newDescription = 'Updated description';

        await test.step('Create test group via API', async () => {
            await api.createGroup(adminCookie, {
                name: groupName,
                description: 'Original description',
            });
        });

        await test.step('Navigate to Admin > Groups', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToGroups();
        });

        await test.step('Edit group via dialog', async () => {
            await groupPage.editGroup(
                groupName,
                newGroupName,
                newDescription,
            );
        });

        await test.step('Verify updated group in table', async () => {
            await groupPage.expectGroupInTable(newGroupName);
            await groupPage.expectGroupNotInTable(groupName);
            await groupPage.expectDescriptionInTable(
                newGroupName,
                newDescription,
            );
        });
    });

    // -------------------------------------------------------
    // 3. Delete group via UI
    // -------------------------------------------------------
    test('delete group via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const groupPage = new GroupManagementPage(page);
        const groupName = makeTestUsername('grp-delete');

        await test.step('Create test group via API', async () => {
            await api.createGroup(adminCookie, {
                name: groupName,
                description: 'To be deleted',
            });
        });

        await test.step('Navigate to Admin > Groups', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToGroups();
        });

        await test.step('Delete group via confirmation dialog', async () => {
            await groupPage.deleteGroup(groupName);
        });

        await test.step('Verify group is removed from table', async () => {
            await groupPage.expectGroupNotInTable(groupName);
        });
    });

    // -------------------------------------------------------
    // 4. Add member to group via UI
    // -------------------------------------------------------
    test('add member to group via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const groupPage = new GroupManagementPage(page);
        const groupName = makeTestUsername('grp-addmem');

        await test.step('Create test group via API', async () => {
            await api.createGroup(adminCookie, {
                name: groupName,
                description: 'Member test group',
            });
        });

        await test.step('Navigate to Admin > Groups', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToGroups();
        });

        await test.step('Expand group row', async () => {
            await groupPage.expandGroupRow(groupName);
        });

        await test.step('Add admin as member', async () => {
            await groupPage.addMember(ADMIN_USER.username);
        });

        await test.step('Verify member in list', async () => {
            await groupPage.expectMemberInList(
                ADMIN_USER.username,
            );
        });
    });

    // -------------------------------------------------------
    // 5. Remove member from group via UI
    // -------------------------------------------------------
    test('remove member from group via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const groupPage = new GroupManagementPage(page);
        const groupName = makeTestUsername('grp-rmmem');

        await test.step('Create test group via API', async () => {
            await api.createGroup(adminCookie, {
                name: groupName,
                description: 'Remove member test group',
            });
        });

        await test.step('Navigate to Admin > Groups', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToGroups();
        });

        await test.step('Expand group row', async () => {
            await groupPage.expandGroupRow(groupName);
        });

        await test.step('Add admin as member via UI', async () => {
            await groupPage.addMember(ADMIN_USER.username);
        });

        await test.step('Verify member was added', async () => {
            await groupPage.expectMemberInList(
                ADMIN_USER.username,
            );
        });

        await test.step('Remove admin member', async () => {
            await groupPage.removeMember(ADMIN_USER.username);
        });

        await test.step('Verify member was removed', async () => {
            await groupPage.expectMemberNotInList(
                ADMIN_USER.username,
            );
        });
    });
});
