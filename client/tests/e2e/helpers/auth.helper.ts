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
     * First tries to reuse the session cookie saved by global
     * setup (either via the E2E_ADMIN_COOKIE env var or the
     * `.auth/admin.json` storage state file).  Falls back to a
     * fresh API login when neither source is available.
     *
     * @returns The raw session cookie string.
     */
    async loginAsAdmin(): Promise<string> {
        // 1. Check the env var set by global setup.
        const envCookie = process.env.E2E_ADMIN_COOKIE;
        if (envCookie) {
            return envCookie;
        }

        // 2. Try to extract the cookie from the saved storage
        //    state file that global setup writes.
        const saved = AuthHelper.loadCookieFromStorageState();
        if (saved) {
            return saved;
        }

        // 3. Fall back to a fresh login.
        await this.throttleLogin();
        const { cookie } = await this.api.login(
            ADMIN_USER.username,
            ADMIN_USER.password,
        );
        return cookie;
    }

    /**
     * Read the `.auth/admin.json` storage state file written by
     * global setup and extract the `session_token` cookie value.
     *
     * Returns the cookie in `session_token=<value>` format, or
     * `null` when the file is missing or unparseable.
     */
    private static loadCookieFromStorageState(): string | null {
        try {
            const statePath = path.resolve(
                __dirname, '..', '.auth', 'admin.json',
            );
            if (!fs.existsSync(statePath)) {
                return null;
            }
            const raw = fs.readFileSync(statePath, 'utf-8');
            const state = JSON.parse(raw) as {
                cookies?: Array<{ name: string; value: string }>;
            };
            const match = state.cookies?.find(
                (c) => c.name === 'session_token',
            );
            if (match) {
                return `session_token=${match.value}`;
            }
        } catch {
            // Silently fall through to fresh login.
        }
        return null;
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
