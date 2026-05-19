/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// ---------------------------------------------------------------
// Types
// ---------------------------------------------------------------

export interface WireMockRequest {
    url: string;
    method: string;
    body: string;
    headers: Record<string, string>;
    loggedDate: string;
}

interface WireMockRequestsResponse {
    requests: Array<{
        request: WireMockRequest;
    }>;
}

export interface MockServerRequest {
    path: string;
    method: string;
    body?: { string?: string; type?: string };
    headers?: Record<string, string[]>;
}

// ---------------------------------------------------------------
// WireMockHelper
// ---------------------------------------------------------------

/**
 * Thin fetch-based wrapper around the WireMock admin API for
 * verifying webhook and Slack notification delivery in E2E tests.
 *
 * WireMock captures HTTP requests and exposes them via its admin
 * API, enabling assertions on request content without a real
 * external service.
 */
export class WireMockHelper {
    private readonly baseUrl: string;

    constructor(baseUrl?: string) {
        this.baseUrl = (
            baseUrl ?? process.env.WIREMOCK_URL ?? 'http://wiremock:8080'
        ).replace(/\/+$/, '');
    }

    /**
     * Fetch all recorded requests from WireMock.
     */
    async getRequests(): Promise<WireMockRequest[]> {
        const res = await fetch(
            `${this.baseUrl}/__admin/requests`,
        );
        if (!res.ok) {
            throw new Error(
                `WireMock GET /__admin/requests failed: ${res.status}`,
            );
        }
        const data = (await res.json()) as WireMockRequestsResponse;
        return (data.requests ?? []).map((r) => r.request);
    }

    /**
     * Return only requests whose URL matches the given path.
     */
    async getRequestsByPath(path: string): Promise<WireMockRequest[]> {
        const all = await this.getRequests();
        return all.filter((r) => r.url === path);
    }

    /**
     * Delete all recorded requests from WireMock.
     */
    async resetRequests(): Promise<void> {
        const res = await fetch(
            `${this.baseUrl}/__admin/requests`,
            { method: 'DELETE' },
        );
        if (!res.ok) {
            throw new Error(
                `WireMock DELETE /__admin/requests failed: ${res.status}`,
            );
        }
    }

    /**
     * Register a stub mapping that responds to any HTTP method on
     * the given path with HTTP 200 and body "ok".
     */
    async registerStub(path: string): Promise<void> {
        const res = await fetch(`${this.baseUrl}/__admin/mappings`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                request: { method: 'ANY', url: path },
                response: { status: 200, body: 'ok' },
            }),
        });
        if (!res.ok) {
            throw new Error(
                `WireMock POST /__admin/mappings failed for ${path}: ${res.status}`,
            );
        }
    }

    /**
     * Register a default 200 OK stub for each of the given paths.
     * Call this in beforeAll after WireMock is ready.
     */
    async setupDefaultStubs(paths: string[]): Promise<void> {
        await Promise.all(paths.map((p) => this.registerStub(p)));
    }

    /**
     * Poll WireMock until a request to `path` appears, or throw
     * after `timeout` milliseconds.
     */
    async waitForRequest(
        path: string,
        timeout: number = 30_000,
    ): Promise<WireMockRequest> {
        const start = Date.now();
        while (Date.now() - start < timeout) {
            const matches = await this.getRequestsByPath(path);
            if (matches.length > 0) {
                return matches[0];
            }
            await new Promise((r) => setTimeout(r, 500));
        }
        throw new Error(
            `WireMock: no request to ${path} within ${timeout}ms`,
        );
    }
}

// ---------------------------------------------------------------
// MockServerHelper
// ---------------------------------------------------------------

/**
 * Thin fetch-based wrapper around the MockServer admin API for
 * verifying Mattermost notification delivery in E2E tests.
 *
 * MockServer captures HTTP requests and exposes them via its
 * retrieval API, enabling assertions on request content without
 * a real Mattermost instance.
 */
export class MockServerHelper {
    private readonly baseUrl: string;

    constructor(baseUrl?: string) {
        this.baseUrl = (
            baseUrl ??
            process.env.MOCKSERVER_URL ??
            'http://mockserver:1080'
        ).replace(/\/+$/, '');
    }

    /**
     * Fetch all recorded requests from MockServer.
     */
    async getRequests(): Promise<MockServerRequest[]> {
        const res = await fetch(
            `${this.baseUrl}/mockserver/retrieve?type=REQUESTS`,
            {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({}),
            },
        );
        if (!res.ok) {
            throw new Error(
                `MockServer retrieve requests failed: ${res.status}`,
            );
        }
        return (await res.json()) as MockServerRequest[];
    }

    /**
     * Return only requests whose path matches the given value.
     */
    async getRequestsByPath(
        path: string,
    ): Promise<MockServerRequest[]> {
        const all = await this.getRequests();
        return all.filter((r) => r.path === path);
    }

    /**
     * Reset all expectations and recorded requests.
     */
    async resetRequests(): Promise<void> {
        const res = await fetch(
            `${this.baseUrl}/mockserver/reset`,
            { method: 'PUT' },
        );
        if (!res.ok) {
            throw new Error(
                `MockServer reset failed: ${res.status}`,
            );
        }
    }

    /**
     * Poll MockServer until a request to `path` appears, or throw
     * after `timeout` milliseconds.
     */
    async waitForRequest(
        path: string,
        timeout: number = 30_000,
    ): Promise<MockServerRequest> {
        const start = Date.now();
        while (Date.now() - start < timeout) {
            const matches = await this.getRequestsByPath(path);
            if (matches.length > 0) {
                return matches[0];
            }
            await new Promise((r) => setTimeout(r, 500));
        }
        throw new Error(
            `MockServer: no request to ${path} within ${timeout}ms`,
        );
    }
}
