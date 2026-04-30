/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import * as crypto from 'crypto';

/**
 * Default admin credentials used for E2E tests. Override with
 * E2E_ADMIN_USER and E2E_ADMIN_PASS environment variables.
 */
export const ADMIN_USER = {
    username: process.env.E2E_ADMIN_USER || 'admin',
    password: process.env.E2E_ADMIN_PASS || 'E2ETestPass123!',
};

/**
 * Base URL for the web client (served by nginx).
 * Browser-based tests navigate to this origin.
 */
export const BASE_URL = process.env.E2E_BASE_URL || 'http://localhost:3000';

/**
 * Base URL for the API server. API tests call this directly,
 * bypassing the nginx proxy for simpler request handling.
 */
export const API_URL = process.env.E2E_API_URL || 'http://localhost:8080';

/**
 * Prefix applied to all usernames created by the E2E suite.
 * The global teardown uses this prefix to identify and clean
 * up test-created resources.
 */
export const TEST_USER_PREFIX = 'e2e-test-';

/**
 * Default password assigned to test users created during E2E
 * runs. Meets the server password complexity requirements.
 */
export const TEST_USER_PASSWORD = 'E2EUserPass456!';

/**
 * Admin permission string constants matching the server's
 * auth.Perm* values.
 */
export const PERMISSIONS = {
    MANAGE_CONNECTIONS: 'manage_connections',
    MANAGE_GROUPS: 'manage_groups',
    MANAGE_PERMISSIONS: 'manage_permissions',
    MANAGE_USERS: 'manage_users',
    MANAGE_TOKEN_SCOPES: 'manage_token_scopes',
    MANAGE_BLACKOUTS: 'manage_blackouts',
    MANAGE_PROBES: 'manage_probes',
    MANAGE_ALERT_RULES: 'manage_alert_rules',
    MANAGE_NOTIFICATION_CHANNELS: 'manage_notification_channels',
    STORE_SYSTEM_MEMORY: 'store_system_memory',
} as const;

/**
 * Generate a unique test username with the E2E prefix and a
 * random suffix to avoid collisions across parallel runs.
 *
 * @param suffix - A human-readable label for the test context.
 * @returns A username like `e2e-test-create-a1b2c3d4`.
 */
export function makeTestUsername(suffix: string): string {
    return `${TEST_USER_PREFIX}${suffix}-${crypto.randomUUID().slice(0, 8)}`;
}
