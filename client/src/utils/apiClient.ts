/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Centralized API client for the pgEdge AI DBA Workbench.
 *
 * This module provides typed helper functions for HTTP requests to
 * the backend API. It handles:
 *
 * - Automatic inclusion of credentials (httpOnly cookies)
 * - JSON serialization and Content-Type headers
 * - Consistent error handling with structured error responses
 * - Typed responses via generics
 *
 * Usage:
 *   import { apiGet, apiPost, apiPut, apiDelete } from '../utils/apiClient';
 *
 *   const data = await apiGet<MyType>('/api/v1/resources');
 *   const result = await apiPost<CreateResponse>('/api/v1/resources', body);
 *   await apiDelete('/api/v1/resources/1');
 */

/**
 * Error class for API responses that indicates a non-OK HTTP status.
 * Carries the HTTP status code and, when available, the parsed error
 * body from the server.
 */
export class ApiError extends Error {
    public readonly statusCode: number;
    public readonly errorBody: string;

    constructor(message: string, statusCode: number, errorBody: string = '') {
        super(message);
        this.name = 'ApiError';
        this.statusCode = statusCode;
        this.errorBody = errorBody;
    }
}

/**
 * Options for API requests.  All fields are optional; the defaults
 * match the conventions already established in the codebase.
 */
export interface ApiRequestOptions {
    /** Additional headers to merge with the defaults. */
    headers?: Record<string, string>;
    /**
     * When true the response body is returned as raw text instead of
     * being parsed as JSON.  Defaults to false.
     */
    rawResponse?: boolean;
    /** AbortSignal for cancellable requests. */
    signal?: AbortSignal;
}

// ---------------------------------------------------------------
// Connection health tracking
// ---------------------------------------------------------------

export type DisconnectReason = 'auth' | 'server' | 'network';

type DisconnectListener = (reason: DisconnectReason) => void;

const CONSECUTIVE_FAILURE_THRESHOLD = 3;

let consecutiveFailures = 0;
let disconnected = false;
let disconnectListener: DisconnectListener | null = null;

/**
 * Register a callback that fires when the server is considered
 * unreachable.  Returns an unsubscribe function.
 */
export function onDisconnect(listener: DisconnectListener): () => void {
    disconnectListener = listener;
    return () => {
        if (disconnectListener === listener) {
            disconnectListener = null;
        }
    };
}

/**
 * Reset all connection-health state so the next request starts
 * with a clean slate.
 */
export function resetConnectionHealth(): void {
    consecutiveFailures = 0;
    disconnected = false;
}

function recordSuccess(): void {
    consecutiveFailures = 0;
}

function recordFailure(reason: DisconnectReason): void {
    if (disconnected) {
        return;
    }

    if (reason === 'auth') {
        // Auth failures fire immediately.
        disconnected = true;
        disconnectListener?.(reason);
        return;
    }

    consecutiveFailures += 1;
    if (consecutiveFailures >= CONSECUTIVE_FAILURE_THRESHOLD) {
        disconnected = true;
        disconnectListener?.(reason);
    }
}

// ---------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------

/**
 * Build the standard headers object.  Every request includes
 * credentials: 'include' so the browser sends the httpOnly session
 * cookie.  Requests that carry a body also get Content-Type.
 */
function buildHeaders(
    hasBody: boolean,
    extra?: Record<string, string>,
): Record<string, string> {
    const headers: Record<string, string> = {};
    if (hasBody) {
        headers['Content-Type'] = 'application/json';
    }
    if (extra) {
        Object.assign(headers, extra);
    }
    return headers;
}

/**
 * Extract a human-readable error message from a failed response.
 *
 * The server may return JSON with an "error" or "message" field, or
 * it may return plain text.  This helper tries JSON first, then
 * falls back to the raw text, and finally to a generic message
 * derived from the HTTP status.
 */
async function extractErrorMessage(
    response: Response,
    fallback: string,
): Promise<{ message: string; body: string }> {
    let body = '';
    try {
        body = await response.text();
        const parsed = JSON.parse(body);
        const message = parsed.error || parsed.message || fallback;
        return { message, body };
    } catch {
        // Response was not valid JSON -- use raw text if available.
        return { message: body || fallback, body };
    }
}

/**
 * Core request function shared by all public helpers.
 */
async function request<T>(
    url: string,
    method: string,
    body?: unknown,
    options?: ApiRequestOptions,
): Promise<T> {
    const hasBody = body !== undefined;

    const fetchOptions: RequestInit = {
        method,
        credentials: 'include',
        headers: buildHeaders(hasBody, options?.headers),
    };

    if (hasBody) {
        fetchOptions.body = JSON.stringify(body);
    }

    if (options?.signal) {
        fetchOptions.signal = options.signal;
    }

    let response: Response;
    try {
        response = await fetch(url, fetchOptions);
    } catch (error) {
        // Network errors surface as TypeError when the server is
        // unreachable.
        recordFailure('network');
        throw error;
    }

    if (!response.ok) {
        if (response.status === 401) {
            recordFailure('auth');
        } else if (response.status >= 500) {
            recordFailure('server');
        }

        const { message, body: errorBody } = await extractErrorMessage(
            response,
            `Request failed: ${method} ${url}`,
        );
        throw new ApiError(message, response.status, errorBody);
    }

    recordSuccess();

    // 204 No Content -- nothing to parse.
    if (response.status === 204) {
        return undefined as T;
    }

    if (options?.rawResponse) {
        const text = await response.text();
        return text as unknown as T;
    }

    return (await response.json()) as T;
}

// ---------------------------------------------------------------
// Public API
// ---------------------------------------------------------------

/**
 * Perform a GET request and return the parsed JSON response.
 *
 * @example
 *   const data = await apiGet<AlertRule[]>('/api/v1/alert-rules');
 */
export async function apiGet<T>(
    url: string,
    options?: ApiRequestOptions,
): Promise<T> {
    return request<T>(url, 'GET', undefined, options);
}

/**
 * Perform a POST request with an optional JSON body.
 *
 * @example
 *   const result = await apiPost<Blackout>('/api/v1/blackouts', payload);
 *   await apiPost('/api/v1/blackouts/1/stop');
 */
export async function apiPost<T>(
    url: string,
    body?: unknown,
    options?: ApiRequestOptions,
): Promise<T> {
    return request<T>(url, 'POST', body, options);
}

/**
 * Perform a PUT request with an optional JSON body.
 *
 * @example
 *   await apiPut('/api/v1/alert-rules/1', { default_enabled: true });
 */
export async function apiPut<T>(
    url: string,
    body?: unknown,
    options?: ApiRequestOptions,
): Promise<T> {
    return request<T>(url, 'PUT', body, options);
}

/**
 * Perform a PATCH request with an optional JSON body.
 *
 * @example
 *   await apiPatch('/api/v1/conversations/1', { title: 'New Title' });
 */
export async function apiPatch<T>(
    url: string,
    body?: unknown,
    options?: ApiRequestOptions,
): Promise<T> {
    return request<T>(url, 'PATCH', body, options);
}

/**
 * Perform a DELETE request.
 *
 * @example
 *   await apiDelete('/api/v1/blackouts/1');
 */
export async function apiDelete<T>(
    url: string,
    options?: ApiRequestOptions,
): Promise<T> {
    return request<T>(url, 'DELETE', undefined, options);
}

/**
 * Perform a fetch request with credentials and connection health
 * tracking, returning the raw Response object.  Use this for
 * endpoints that require streaming, manual response handling, or
 * other scenarios where the typed helpers above are insufficient.
 *
 * @example
 *   const response = await apiFetch('/api/v1/llm/chat', {
 *       method: 'POST',
 *       body: JSON.stringify(payload),
 *       signal: abortController.signal,
 *   });
 */
export async function apiFetch(
    url: string,
    init?: RequestInit,
): Promise<Response> {
    const fetchOptions: RequestInit = {
        ...init,
        credentials: 'include',
    };

    let response: Response;
    try {
        response = await fetch(url, fetchOptions);
    } catch (error) {
        recordFailure('network');
        throw error;
    }

    if (!response.ok) {
        if (response.status === 401) {
            recordFailure('auth');
        } else if (response.status >= 500) {
            recordFailure('server');
        }
    } else {
        recordSuccess();
    }

    return response;
}
