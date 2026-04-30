/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { FullConfig } from '@playwright/test';
import { ApiHelper } from '../helpers/api.helper';
import { ADMIN_USER, API_URL, TEST_USER_PREFIX } from './test-data';

/**
 * Playwright global teardown function.
 *
 * This runs once after all test files have completed. It removes
 * any test-created users and tokens whose names start with the
 * E2E prefix so the environment is left clean.
 */
async function globalTeardown(_config: FullConfig): Promise<void> {
    const api = new ApiHelper(API_URL);

    let cookie: string;
    try {
        const result = await api.login(
            ADMIN_USER.username,
            ADMIN_USER.password,
        );
        cookie = result.cookie;
    } catch {
        // If we cannot log in (e.g. server already down), skip
        // cleanup silently.
        console.warn('[E2E teardown] Could not log in as admin; skipping cleanup.');
        return;
    }

    // -------------------------------------------------------
    // Clean up test tokens
    // -------------------------------------------------------
    try {
        const { tokens } = await api.listTokens(cookie);
        for (const token of tokens) {
            if (
                token.name &&
                token.name.startsWith(TEST_USER_PREFIX)
            ) {
                try {
                    await api.deleteToken(cookie, token.id);
                } catch {
                    console.warn(
                        `[E2E teardown] Failed to delete token ${token.id}`,
                    );
                }
            }
        }
    } catch {
        console.warn('[E2E teardown] Could not list tokens for cleanup.');
    }

    // -------------------------------------------------------
    // Clean up test users
    // -------------------------------------------------------
    try {
        const { users } = await api.listUsers(cookie);
        for (const user of users) {
            if (user.username.startsWith(TEST_USER_PREFIX)) {
                try {
                    await api.deleteUser(cookie, user.id);
                } catch {
                    console.warn(
                        `[E2E teardown] Failed to delete user ${user.username}`,
                    );
                }
            }
        }
    } catch {
        console.warn('[E2E teardown] Could not list users for cleanup.');
    }
}

export default globalTeardown;
