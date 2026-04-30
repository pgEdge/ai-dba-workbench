/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

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

    constructor(api: ApiHelper) {
        this.api = api;
    }

    /**
     * Log in as the admin user using the E2E environment
     * variables (or their defaults).
     *
     * @returns The raw session cookie string.
     */
    async loginAsAdmin(): Promise<string> {
        const { cookie } = await this.api.login(
            ADMIN_USER.username,
            ADMIN_USER.password,
        );
        return cookie;
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
