/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { API_URL } from '../fixtures/test-data';

// ---------------------------------------------------------------
// Response types
// ---------------------------------------------------------------

export interface LoginResult {
    cookie: string;
    expiresAt: string;
}

export interface UserResponse {
    id: number;
    username: string;
    display_name: string;
    email: string;
    enabled: boolean;
    is_superuser: boolean;
    is_service_account: boolean;
    annotation?: string;
}

export interface TokenCreateResponse {
    id: number;
    token: string;
    owner: string;
    annotation: string;
    expires_at: string | null;
    message: string;
}

export interface TokenListItem {
    id: number;
    name: string;
    token_prefix: string;
    user_id: number;
    username?: string;
    is_service_account: boolean;
    is_superuser: boolean;
    expires_at: string | null;
    scope?: {
        scoped: boolean;
        connections?: Array<{ connection_id: number; access_level: string }>;
        mcp_privileges?: number[];
        admin_permissions?: string[];
    };
}

export interface TokenScopeResponse {
    token_id: number;
    scoped: boolean;
    connections?: Array<{ connection_id: number; access_level: string }>;
    mcp_privileges?: number[];
    admin_permissions?: string[];
}

export interface ConnectionResponse {
    id: number;
    name: string;
    host: string;
    port: number;
    database: string;
    username: string;
}

// ---------------------------------------------------------------
// ApiHelper
// ---------------------------------------------------------------

/**
 * Thin fetch-based wrapper around the AI DBA Workbench REST API.
 *
 * API tests call the server directly at `baseUrl` (default
 * `http://localhost:8080`), bypassing the nginx proxy.
 *
 * Authentication is handled in two ways:
 * - Session cookies: extracted from the `Set-Cookie` response header
 *   after login and passed as `Cookie: session_token=<value>`.
 * - Bearer tokens: passed as `Authorization: Bearer <rawToken>`.
 */
export class ApiHelper {
    private readonly baseUrl: string;

    constructor(baseUrl: string = API_URL) {
        this.baseUrl = baseUrl.replace(/\/+$/, '');
    }

    // -----------------------------------------------------------
    // Auth
    // -----------------------------------------------------------

    /**
     * Authenticate via username and password. Returns the raw
     * session cookie string and the server-reported expiry.
     */
    async login(username: string, password: string): Promise<LoginResult> {
        const res = await fetch(`${this.baseUrl}/api/v1/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password }),
            redirect: 'manual',
        });

        if (!res.ok) {
            const text = await res.text();
            throw new Error(
                `Login failed for ${username}: ${res.status} ${text}`,
            );
        }

        const cookie = this.extractSessionCookie(res);
        if (!cookie) {
            throw new Error('Login succeeded but no session_token cookie was returned');
        }

        const body = (await res.json()) as { expires_at: string };
        return { cookie, expiresAt: body.expires_at };
    }

    /**
     * Invalidate the current session.
     */
    async logout(cookie: string): Promise<void> {
        await this.request<void>('POST', '/api/v1/auth/logout', undefined, {
            cookie,
        });
    }

    // -----------------------------------------------------------
    // Users
    // -----------------------------------------------------------

    async createUser(
        cookie: string,
        payload: {
            username: string;
            password: string;
            display_name?: string;
            email?: string;
            annotation?: string;
            enabled?: boolean;
            is_superuser?: boolean;
            is_service_account?: boolean;
        },
    ): Promise<{ message: string }> {
        return this.request<{ message: string }>(
            'POST',
            '/api/v1/rbac/users',
            payload,
            { cookie },
        );
    }

    async listUsers(
        cookie: string,
    ): Promise<{ users: UserResponse[] }> {
        return this.request<{ users: UserResponse[] }>(
            'GET',
            '/api/v1/rbac/users',
            undefined,
            { cookie },
        );
    }

    async updateUser(
        cookie: string,
        userId: number,
        payload: {
            password?: string;
            display_name?: string;
            email?: string;
            annotation?: string;
            enabled?: boolean;
            is_superuser?: boolean;
        },
    ): Promise<{ message: string }> {
        return this.request<{ message: string }>(
            'PUT',
            `/api/v1/rbac/users/${userId}`,
            payload,
            { cookie },
        );
    }

    async deleteUser(
        cookie: string,
        userId: number,
    ): Promise<void> {
        return this.request<void>(
            'DELETE',
            `/api/v1/rbac/users/${userId}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Tokens
    // -----------------------------------------------------------

    async createToken(
        cookie: string,
        ownerUsername: string,
        annotation: string,
    ): Promise<TokenCreateResponse> {
        return this.request<TokenCreateResponse>(
            'POST',
            '/api/v1/rbac/tokens',
            { owner_username: ownerUsername, annotation },
            { cookie },
        );
    }

    async listTokens(
        cookie: string,
    ): Promise<{ tokens: TokenListItem[] }> {
        return this.request<{ tokens: TokenListItem[] }>(
            'GET',
            '/api/v1/rbac/tokens',
            undefined,
            { cookie },
        );
    }

    async deleteToken(
        cookie: string,
        tokenId: number,
    ): Promise<void> {
        return this.request<void>(
            'DELETE',
            `/api/v1/rbac/tokens/${tokenId}`,
            undefined,
            { cookie },
        );
    }

    async setTokenScope(
        cookie: string,
        tokenId: number,
        scope: {
            connections?: Array<{ connection_id: number; access_level: string }>;
            mcp_privileges?: string[];
            admin_permissions?: string[];
        },
    ): Promise<void> {
        return this.request<void>(
            'PUT',
            `/api/v1/rbac/tokens/${tokenId}/scope`,
            scope,
            { cookie },
        );
    }

    async getTokenScope(
        cookie: string,
        tokenId: number,
    ): Promise<TokenScopeResponse> {
        return this.request<TokenScopeResponse>(
            'GET',
            `/api/v1/rbac/tokens/${tokenId}/scope`,
            undefined,
            { cookie },
        );
    }

    async clearTokenScope(
        cookie: string,
        tokenId: number,
    ): Promise<void> {
        return this.request<void>(
            'DELETE',
            `/api/v1/rbac/tokens/${tokenId}/scope`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Connections (for RBAC scope tests)
    // -----------------------------------------------------------

    async listConnections(
        authHeader?: { cookie?: string; bearerToken?: string },
    ): Promise<{ connections: ConnectionResponse[] }> {
        const headers: Record<string, string> = {};
        if (authHeader?.cookie) {
            headers['Cookie'] = authHeader.cookie;
        }
        if (authHeader?.bearerToken) {
            headers['Authorization'] = `Bearer ${authHeader.bearerToken}`;
        }
        return this.requestRaw<{ connections: ConnectionResponse[] }>(
            'GET',
            '/api/v1/connections',
            undefined,
            headers,
        );
    }

    // -----------------------------------------------------------
    // MCP tool invocation (for scope enforcement tests)
    // -----------------------------------------------------------

    async callMcpTool(
        toolName: string,
        args: Record<string, unknown>,
        bearerToken?: string,
    ): Promise<unknown> {
        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
        };
        if (bearerToken) {
            headers['Authorization'] = `Bearer ${bearerToken}`;
        }
        return this.requestRaw<unknown>(
            'POST',
            '/api/v1/mcp/tools/call',
            { name: toolName, arguments: args },
            headers,
        );
    }

    // -----------------------------------------------------------
    // Raw unauthenticated request (for 401 tests)
    // -----------------------------------------------------------

    async rawGet(
        path: string,
        headers?: Record<string, string>,
    ): Promise<{ status: number; body: unknown }> {
        const res = await fetch(`${this.baseUrl}${path}`, {
            method: 'GET',
            headers: headers ?? {},
            redirect: 'manual',
        });
        let body: unknown;
        try {
            body = await res.json();
        } catch {
            body = await res.text();
        }
        return { status: res.status, body };
    }

    async rawPost(
        path: string,
        payload?: unknown,
        headers?: Record<string, string>,
    ): Promise<{ status: number; body: unknown }> {
        const res = await fetch(`${this.baseUrl}${path}`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                ...(headers ?? {}),
            },
            body: payload !== undefined ? JSON.stringify(payload) : undefined,
            redirect: 'manual',
        });
        let body: unknown;
        try {
            body = await res.json();
        } catch {
            body = await res.text();
        }
        return { status: res.status, body };
    }

    // -----------------------------------------------------------
    // Health check
    // -----------------------------------------------------------

    async healthCheck(): Promise<boolean> {
        try {
            const res = await fetch(`${this.baseUrl}/health`);
            return res.ok;
        } catch {
            return false;
        }
    }

    // -----------------------------------------------------------
    // Internal helpers
    // -----------------------------------------------------------

    /**
     * Issue an authenticated request using cookie or bearer token.
     */
    private async request<T>(
        method: string,
        path: string,
        body?: unknown,
        auth?: { cookie?: string; bearerToken?: string },
    ): Promise<T> {
        const headers: Record<string, string> = {};
        if (body !== undefined) {
            headers['Content-Type'] = 'application/json';
        }
        if (auth?.cookie) {
            headers['Cookie'] = auth.cookie;
        }
        if (auth?.bearerToken) {
            headers['Authorization'] = `Bearer ${auth.bearerToken}`;
        }
        return this.requestRaw<T>(method, path, body, headers);
    }

    /**
     * Low-level fetch wrapper with JSON serialisation and error
     * handling.
     */
    private async requestRaw<T>(
        method: string,
        path: string,
        body?: unknown,
        headers?: Record<string, string>,
    ): Promise<T> {
        const url = `${this.baseUrl}${path}`;
        const init: RequestInit = {
            method,
            headers: headers ?? {},
            redirect: 'manual',
        };
        if (body !== undefined) {
            init.body = JSON.stringify(body);
        }

        const res = await fetch(url, init);

        if (!res.ok) {
            let errorText: string;
            try {
                errorText = await res.text();
            } catch {
                errorText = res.statusText;
            }
            throw new Error(
                `API ${method} ${path} failed: ${res.status} ${errorText}`,
            );
        }

        // 204 No Content
        if (res.status === 204) {
            return undefined as T;
        }

        return (await res.json()) as T;
    }

    /**
     * Extract the `session_token` cookie value from a response's
     * `Set-Cookie` header.
     *
     * The header value looks like:
     *   session_token=abc123; Path=/; HttpOnly; SameSite=Lax
     *
     * We return `session_token=abc123` so it can be passed directly
     * as the `Cookie` header on subsequent requests.
     */
    private extractSessionCookie(res: Response): string | null {
        const setCookie = res.headers.get('set-cookie');
        if (!setCookie) {
            return null;
        }

        // The header may contain multiple cookies separated by commas
        // (though the server only sets one). Split on commas that are
        // followed by a cookie name to be safe.
        const parts = setCookie.split(/,(?=\s*\w+=)/);
        for (const part of parts) {
            const trimmed = part.trim();
            if (trimmed.startsWith('session_token=')) {
                // Return only the name=value portion, strip attributes.
                const nameValue = trimmed.split(';')[0].trim();
                return nameValue;
            }
        }

        return null;
    }
}
