/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import * as fs from 'fs';
import * as path from 'path';
import { ApiHelper } from './api.helper';
import { ADMIN_USER, TEST_USER_PREFIX, TEST_USER_PASSWORD } from '../fixtures/test-data';

// ---------------------------------------------------------------
// Types
// ---------------------------------------------------------------

export interface CreatedUser {
    userId: number;
    username: string;
    cookie: string;
}

export interface CreatedToken {
    tokenId: number;
    rawToken: string;
}

// ---------------------------------------------------------------
// AuthHelper
// ---------------------------------------------------------------

/**
 * Higher-level authentication helpers built on top of ApiHelper.
 * Simplifies common test workflows such as logging in as the
 * admin, creating a test user and logging in as that user, or
 * creating a bearer token.
 */
export class AuthHelper {
    private readonly api: ApiHelper;

    /**
     * Minimum delay (ms) between consecutive login calls to
     * avoid triggering the server's rate limiter.
     */
    private static readonly LOGIN_DELAY_MS = 500;
    private static lastLoginTime = 0;

    constructor(api: ApiHelper) {
        this.api = api;
    }

    /**
     * Wait if needed so that consecutive login calls are spaced
     * at least `LOGIN_DELAY_MS` apart.
     */
    private async throttleLogin(): Promise<void> {
        const now = Date.now();
        const elapsed = now - AuthHelper.lastLoginTime;
        if (elapsed < AuthHelper.LOGIN_DELAY_MS) {
            await new Promise((r) =>
                setTimeout(r, AuthHelper.LOGIN_DELAY_MS - elapsed),
            );
        }
        AuthHelper.lastLoginTime = Date.now();
    }

    /**
     * Log in as the admin user.
     *
     * Tries to reuse the session cookie cached in E2E_ADMIN_COOKIE,
     * validating it against an authenticated endpoint first.
     * Falls back to a fresh API login when the cached cookie is
     * absent, stale, or evicted from the server's in-memory store.
     *
     * NOTE: /api/v1/capabilities is a PUBLIC endpoint and always
     * returns 200 regardless of session validity.  Use an endpoint
     * that requires authentication so that an evicted session is
     * correctly detected as stale.
     *
     * @returns The raw session cookie string.
     */
    async loginAsAdmin(): Promise<string> {
        // 1. Check the env var set by global setup.  Validate it
        //    with an authenticated endpoint so that a session evicted
        //    from the server's in-memory store (maxSessionsPerUser)
        //    is correctly detected and a fresh login is performed.
        const envCookie = process.env.E2E_ADMIN_COOKIE;
        if (envCookie) {
            try {
                const { status } = await this.api.rawGet(
                    '/api/v1/rbac/users',
                    { Cookie: envCookie },
                );
                if (status >= 200 && status < 300) {
                    return envCookie;
                }
            } catch {
                // Network error or unexpected failure — fall through.
            }
            // Cookie is stale; clear it so other callers in this
            // process do not reuse it either.
            delete process.env.E2E_ADMIN_COOKIE;
        }

        // 2. Fresh login.  Do NOT fall back to the .auth/admin.json
        //    storage-state file: it contains the same session value
        //    as E2E_ADMIN_COOKIE and is equally stale when the env
        //    cookie failed validation above.
        await this.throttleLogin();
        const { cookie } = await this.api.login(
            ADMIN_USER.username,
            ADMIN_USER.password,
        );

        // Cache the fresh cookie so subsequent calls in the same
        // worker process reuse it without another login round-trip.
        process.env.E2E_ADMIN_COOKIE = cookie;

        // Also update .auth/admin.json so that browser-context tests
        // using `storageState: '.auth/admin.json'` also receive the
        // fresh session and are not redirected to the login page.
        AuthHelper.refreshStorageState(cookie);

        return cookie;
    }

    /**
     * Replace the `session_token` value in `.auth/admin.json` with
     * the freshly obtained cookie.
     *
     * Playwright UI tests that use `test.use({ storageState: '.auth/admin.json' })`
     * read this file when creating each browser context.  If the admin
     * session was evicted and a new one was obtained, the file must be
     * updated so those tests receive a valid cookie rather than being
     * redirected to the login page.
     *
     * Failures here are non-fatal: the update is best-effort.
     */
    private static refreshStorageState(cookie: string): void {
        try {
            const statePath = path.resolve(
                __dirname, '..', '.auth', 'admin.json',
            );
            if (!fs.existsSync(statePath)) {
                return;
            }
            const raw = fs.readFileSync(statePath, 'utf-8');
            const state = JSON.parse(raw) as {
                cookies?: Array<{ name: string; value: string; [k: string]: unknown }>;
                origins?: unknown[];
            };
            if (!Array.isArray(state.cookies)) {
                return;
            }
            // cookie format: "session_token=<value>"
            const sessionValue = cookie.split('=').slice(1).join('=');
            const entry = state.cookies.find((c) => c.name === 'session_token');
            if (entry) {
                entry.value = sessionValue;
                fs.writeFileSync(statePath, JSON.stringify(state, null, 2));
            }
        } catch {
            // Non-critical — best-effort only.
        }
    }

    /**
     * Create a new user via the admin API and log in as that user.
     *
     * The admin cookie is obtained automatically. The caller
     * receives the new user's ID, username, and a session cookie
     * for authenticated requests.
     */
    async createAndLoginUser(
        username: string,
        password: string = TEST_USER_PASSWORD,
    ): Promise<CreatedUser> {
        const adminCookie = await this.loginAsAdmin();

        await this.api.createUser(adminCookie, {
            username,
            password,
            display_name: `E2E ${username}`,
            email: `${username}@e2e.test`,
            annotation: `${TEST_USER_PREFIX}auto`,
        });

        // Retrieve the user's ID from the users list.
        const { users } = await this.api.listUsers(adminCookie);
        const user = users.find((u) => u.username === username);
        if (!user) {
            throw new Error(`User ${username} not found after creation`);
        }

        // Log in as the newly created user.
        await this.throttleLogin();
        const { cookie } = await this.api.login(username, password);

        return { userId: user.id, username, cookie };
    }

    /**
     * Create a bearer token for the given user.
     *
     * @param adminCookie - A valid admin session cookie.
     * @param ownerUsername - The username that will own the token.
     * @param annotation - An optional annotation (defaults to
     *   an E2E-prefixed string for teardown cleanup).
     * @returns The token ID and raw token string.
     */
    async createBearerToken(
        adminCookie: string,
        ownerUsername: string,
        annotation?: string,
    ): Promise<CreatedToken> {
        const tokenAnnotation =
            annotation ?? `${TEST_USER_PREFIX}token-${Date.now()}`;
        const result = await this.api.createToken(
            adminCookie,
            ownerUsername,
            tokenAnnotation,
        );
        return { tokenId: result.id, rawToken: result.token };
    }

    /**
     * Revoke (delete) a bearer token.
     */
    async revokeToken(adminCookie: string, tokenId: number): Promise<void> {
        await this.api.deleteToken(adminCookie, tokenId);
    }

    /**
     * Delete a test user by ID.
     */
    async cleanupUser(adminCookie: string, userId: number): Promise<void> {
        await this.api.deleteUser(adminCookie, userId);
    }
}
