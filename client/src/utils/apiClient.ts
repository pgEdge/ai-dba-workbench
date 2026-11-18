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
 * Carries the HTTP status code, the parsed error body from the server
 * when available, and the number of seconds a `Retry-After` header
 * asked the client to wait (null when the header was absent).
 */
export class ApiError extends Error {
    public readonly statusCode: number;
    public readonly errorBody: string;
    public readonly retryAfterSeconds: number | null;

    constructor(
        message: string,
        statusCode: number,
        errorBody = '',
        retryAfterSeconds: number | null = null,
    ) {
        super(message);
        this.name = 'ApiError';
        this.statusCode = statusCode;
        this.errorBody = errorBody;
        this.retryAfterSeconds = retryAfterSeconds;
    }
}

/**
 * Read a `Retry-After` header given in delta-seconds.  The HTTP-date
 * form is not used by this server, so it is treated as absent, as is
 * anything that is not a non-negative whole number.
 */
function parseRetryAfter(response: Response): number | null {
    const raw = response.headers?.get('Retry-After')?.trim();
    if (!raw || !/^\d+$/.test(raw)) {
        return null;
    }
    return Number(raw);
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
    /**
     * When true this request is left out of the connection-health
     * accounting below: neither its failures nor its successes move
     * the consecutive-failure counter.
     *
     * Reserved for requests that are not evidence about the session's
     * connection, such as the login screen's capabilities probe, which
     * runs before anyone has signed in and imposes its own timeout. A
     * probe like that would otherwise spend most of the failure budget
     * at page load and latch `disconnected`, after which the overlay
     * can never fire again for the rest of the session.
     */
    skipHealthTracking?: boolean;
}

// ---------------------------------------------------------------
// Connection health tracking
// ---------------------------------------------------------------

export type DisconnectReason = 'auth' | 'server' | 'network';

type DisconnectListener = (reason: DisconnectReason) => void;

const CONSECUTIVE_FAILURE_THRESHOLD = 3;

/**
 * The request used to confirm an outage before the client declares
 * itself disconnected.
 *
 * A run of consecutive failures does not by itself show that the
 * server is down: one broken endpoint retried by a dashboard panel
 * (see `useRetryingFetch`) fails over and over on a healthy server.
 * So when the threshold is reached the client asks the server
 * something cheap and independent of that endpoint. The capabilities
 * endpoint is public, is served from memory, and sits under `/api`,
 * which every deployment (the Vite proxy and the nginx
 * configurations) forwards to the server. The server's own `/health`
 * route is not used because not every front end forwards it; where it
 * is not forwarded, the single-page fallback would answer 200 and
 * hide a real outage.
 */
export const OUTAGE_PROBE_URL = '/api/v1/capabilities';

/**
 * How long the outage probe may take before the server is treated as
 * unreachable. The endpoint normally answers in milliseconds, so this
 * only bounds a request that never settles.
 */
export const OUTAGE_PROBE_TIMEOUT_MS = 5000;

let consecutiveFailures = 0;
let disconnected = false;
let disconnectListener: DisconnectListener | null = null;
let probeInFlight = false;
// Bumped by resetConnectionHealth so that a probe started before a
// reset cannot act on the state after it.
let healthGeneration = 0;

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
    probeInFlight = false;
    healthGeneration += 1;
}

function recordSuccess(): void {
    consecutiveFailures = 0;
    // A success proves the server reachable, so a probe still pending for
    // the failure run it ended is stale. Invalidate it, so that a new run
    // of failures starts a fresh probe instead of acting on this one's
    // late answer.
    if (probeInFlight) {
        probeInFlight = false;
        healthGeneration += 1;
    }
}

function declareDisconnected(reason: DisconnectReason): void {
    disconnected = true;
    disconnectListener?.(reason);
}

/**
 * Ask the server whether it is reachable. Any answer below 500 means
 * something on the server side responded, so the session is not cut
 * off; a network error, a timeout or a 5xx (which a reverse proxy
 * returns when the server behind it is down) means it is.
 *
 * This calls `fetch` directly rather than going through the helpers
 * below, so the probe never feeds back into the failure accounting.
 */
async function serverIsReachable(): Promise<boolean> {
    const controller = new AbortController();
    const timer = setTimeout(() => {
        controller.abort();
    }, OUTAGE_PROBE_TIMEOUT_MS);
    try {
        const response = await fetch(OUTAGE_PROBE_URL, {
            method: 'GET',
            credentials: 'include',
            signal: controller.signal,
        });
        return response.status < 500;
    } catch {
        return false;
    } finally {
        clearTimeout(timer);
    }
}

/**
 * Confirm a suspected outage with one probe, then disconnect only if
 * the server really is unreachable. If the server answers, the run
 * of failures came from the endpoints being called rather than from
 * the connection, so the counter starts again and those callers
 * report their own errors.
 */
async function confirmOutage(reason: DisconnectReason): Promise<void> {
    probeInFlight = true;
    const generation = healthGeneration;
    const reachable = await serverIsReachable();

    // A reset while the probe was pending started a new session of
    // accounting; this probe's answer no longer applies to it.
    if (generation !== healthGeneration) {
        return;
    }
    probeInFlight = false;

    // Something else (a 401) may already have disconnected, and a
    // request that succeeded while the probe was pending has already
    // shown the server to be reachable.
    if (disconnected || consecutiveFailures < CONSECUTIVE_FAILURE_THRESHOLD) {
        return;
    }

    if (reachable) {
        consecutiveFailures = 0;
        return;
    }

    declareDisconnected(reason);
}

function recordFailure(reason: DisconnectReason): void {
    if (disconnected) {
        return;
    }

    if (reason === 'auth') {
        // Auth failures fire immediately.
        declareDisconnected(reason);
        return;
    }

    consecutiveFailures += 1;
    if (consecutiveFailures >= CONSECUTIVE_FAILURE_THRESHOLD && !probeInFlight) {
        void confirmOutage(reason);
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
    const trackHealth = options?.skipHealthTracking !== true;

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
        if (trackHealth) {
            recordFailure('network');
        }
        throw error;
    }

    if (!response.ok) {
        if (trackHealth) {
            if (response.status === 401) {
                recordFailure('auth');
            } else if (response.status >= 500) {
                recordFailure('server');
            }
        }

        const { message, body: errorBody } = await extractErrorMessage(
            response,
            `Request failed: ${method} ${url}`,
        );
        throw new ApiError(
            message,
            response.status,
            errorBody,
            parseRetryAfter(response),
        );
    }

    if (trackHealth) {
        recordSuccess();
    }

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
