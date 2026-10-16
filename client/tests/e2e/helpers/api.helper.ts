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

/** A single content block of an MCP tool result. */
export interface McpContentBlock {
    type: string;
    text: string;
}

/**
 * A tool definition as returned by GET /api/v1/mcp/tools (mirrors the
 * server `mcp.Tool` struct). `inputSchema` is the raw JSON Schema for the
 * tool's arguments; the E2E suite only reads `name`, so the schema is
 * typed loosely.
 */
export interface McpToolDefinition {
    name: string;
    description?: string;
    inputSchema?: Record<string, unknown>;
}

/**
 * Parsed result of POST /api/v1/mcp/tools/call. RBAC denials arrive as
 * HTTP 200 with a JSON tool result rather than a 4xx status: an access
 * error sets `isError: true`, while an empty-scope listing returns
 * `isError: false` with an explanatory `text`. `text` is a convenience
 * concatenation of every content block for wording-independent leak
 * checks; assert exact denial strings on `content[0].text`.
 */
export interface McpToolResult {
    content: McpContentBlock[];
    isError: boolean;
    text: string;
}

/**
 * A probe configuration row as returned by GET /api/v1/probe-configs.
 * `name` carries the probe name (e.g. `pg_stat_statements`,
 * `pg_sys_cpu_usage_info`); `connection_id` is null for the global
 * default configuration that applies to every monitored connection.
 */
export interface ProbeConfig {
    id: number;
    connection_id: number | null;
    is_enabled: boolean;
    name: string;
    description: string;
    collection_interval_seconds: number;
    retention_days: number;
}

/** Partial update payload accepted by PUT /api/v1/probe-configs/{id}. */
export interface ProbeConfigUpdate {
    is_enabled?: boolean;
    collection_interval_seconds?: number;
    retention_days?: number;
}

/** A single top-query row from GET /api/v1/metrics/top-queries. */
export interface TopQueryApiRow {
    queryid: string;
    database_name: string;
    query: string;
    calls: number;
    total_exec_time: number;
    mean_exec_time: number;
    rows: number;
    shared_blks_hit: number;
    shared_blks_read: number;
}

/**
 * Result of a top-queries request: the response body is a bare JSON
 * array of rows, and the unpaginated total is carried in the
 * `X-Total-Count` header (null when the header is absent/unparseable).
 */
export interface TopQueriesResult {
    rows: TopQueryApiRow[];
    totalCount: number | null;
}

/** A single time-series data point from GET /api/v1/metrics/query. */
export interface MetricDataPoint {
    time: string;
    value: number;
}

/** A named metric series from GET /api/v1/metrics/query. */
export interface MetricSeries {
    name: string;
    metric: string;
    data: MetricDataPoint[];
    unit?: string;
}

/** Server capabilities from GET /api/v1/capabilities. */
export interface CapabilitiesResponse {
    ai_enabled: boolean;
    max_iterations?: number;
}

/**
 * A single alert row as returned by GET /api/v1/alerts (mirrors the
 * server `database.Alert` struct). Optional fields are omitted by the
 * server when null.
 */
export interface AlertApiRow {
    id: number;
    alert_type: string;
    rule_id?: number;
    connection_id: number;
    database_name?: string;
    probe_name?: string;
    metric_name?: string;
    metric_value?: number;
    metric_unit?: string;
    threshold_value?: number;
    operator?: string;
    severity: string;
    title: string;
    description: string;
    object_name?: string;
    status: string;
    triggered_at: string;
    cleared_at?: string;
    last_updated?: string;
    acknowledged_at?: string;
    acknowledged_by?: string;
    ack_message?: string;
    false_positive?: boolean;
    anomaly_score?: number;
    server_name?: string;
}

/** Response body of GET /api/v1/alerts. */
export interface AlertListResponse {
    alerts: AlertApiRow[];
    total: number;
}

/** Response body of GET /api/v1/alerts/counts. */
export interface AlertCountsResponse {
    total: number;
    by_server: Record<string, number>;
}

/** A single alert-rule catalog entry from GET /api/v1/alert-rules. */
export interface AlertRuleApiRow {
    id: number;
    name: string;
    description: string;
    category: string;
    metric_name: string;
    metric_unit?: string | null;
    default_operator: string;
    default_threshold: number;
    default_severity: string;
    default_enabled: boolean;
    required_extension?: string | null;
    is_built_in: boolean;
}

/**
 * A blackout window as returned by the blackout endpoints (mirrors the
 * server `database.Blackout` struct). Optional scope-id fields are
 * omitted by the server when null. `is_active` is true while
 * `start_time <= now <= end_time`.
 */
export interface Blackout {
    id: number;
    scope: 'estate' | 'group' | 'cluster' | 'server';
    group_id?: number;
    cluster_id?: number;
    connection_id?: number;
    database_name?: string;
    reason: string;
    start_time: string;
    end_time: string;
    created_by: string;
    created_at: string;
    is_active: boolean;
}

/** Response body of GET /api/v1/blackouts. */
export interface BlackoutListResponse {
    blackouts: Blackout[];
    total_count: number;
}

/** A single timeline event from GET /api/v1/timeline/events. */
export interface TimelineEventApi {
    id: string;
    event_type: string;
    connection_id: number;
    server_name: string;
    occurred_at: string;
    severity: string;
    title: string;
    summary: string;
    details?: unknown;
}

/** Response body of GET /api/v1/timeline/events. */
export interface TimelineEventsResponse {
    events: TimelineEventApi[];
    total_count: number;
}

/**
 * Response body of GET /api/v1/overview. Only the fields the E2E suite
 * asserts on are typed; the server may return additional fields.
 */
export interface OverviewApiResponse {
    summary: string | null;
    status?: string;
    generated_at?: string;
    stale_at?: string;
    restart_detected?: boolean;
    [key: string]: unknown;
}

/**
 * Response body of GET /api/v1/metrics/latest. `rows` is a bare list of
 * column-name → value maps; the shape depends on the requested probe.
 */
export interface LatestSnapshotResponse {
    rows: Array<Record<string, unknown>>;
    total_count: number;
}

/**
 * Result of executing a single SQL statement via POST
 * /api/v1/connections/{id}/query. Mirrors the server `statementResult`
 * struct: `columns`/`rows`/`row_count`/`truncated` are present for
 * successful statements, `error` for failed ones.
 */
export interface ConnectionQueryStatementResult {
    query: string;
    columns?: string[];
    rows?: string[][];
    row_count?: number;
    truncated?: boolean;
    error?: string;
}

/**
 * Response body of POST /api/v1/connections/{id}/query (mirrors the
 * server `multiQueryResponse`). When the payload contains write
 * statements and `confirmed` was not set, `requires_confirmation` is
 * true and `write_statements` lists the statements awaiting
 * confirmation.
 */
export interface ConnectionQueryResponse {
    results?: ConnectionQueryStatementResult[];
    total_statements?: number;
    requires_confirmation?: boolean;
    write_statements?: string[];
    confirmation_message?: string;
    error?: string;
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
     *
     * Automatically retries with exponential backoff when the
     * server responds with 429 (Too Many Requests).
     */
    async login(username: string, password: string): Promise<LoginResult> {
        const maxRetries = 4;
        let lastError: Error | null = null;

        for (let attempt = 0; attempt <= maxRetries; attempt++) {
            const res = await fetch(`${this.baseUrl}/api/v1/auth/login`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password }),
                redirect: 'manual',
            });

            if (res.status === 429) {
                const delayMs = 1000 * Math.pow(2, attempt); // 1s, 2s, 4s, 8s, 16s
                console.warn(
                    `[E2E] Login rate-limited (429) for ${username}, ` +
                    `retrying in ${delayMs}ms (attempt ${attempt + 1}/${maxRetries + 1})`,
                );
                await new Promise((r) => setTimeout(r, delayMs));
                lastError = new Error(
                    `Login rate-limited for ${username}: 429 Too Many Requests`,
                );
                continue;
            }

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

        throw lastError ?? new Error(`Login failed for ${username} after ${maxRetries + 1} attempts`);
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
    // Groups
    // -----------------------------------------------------------

    async createGroup(
        cookie: string,
        payload: { name: string; description?: string },
    ): Promise<{ id: number; name: string; description?: string }> {
        return this.request<{
            id: number;
            name: string;
            description?: string;
        }>('POST', '/api/v1/rbac/groups', payload, { cookie });
    }

    async listGroups(
        cookie: string,
    ): Promise<{
        groups: Array<{
            id: number;
            name: string;
            description?: string;
            member_count?: number;
        }>;
    }> {
        return this.request<{
            groups: Array<{
                id: number;
                name: string;
                description?: string;
                member_count?: number;
            }>;
        }>('GET', '/api/v1/rbac/groups', undefined, { cookie });
    }

    async deleteGroup(
        cookie: string,
        groupId: number,
    ): Promise<void> {
        return this.request<void>(
            'DELETE',
            `/api/v1/rbac/groups/${groupId}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Group membership
    // -----------------------------------------------------------

    async addGroupMember(
        cookie: string,
        groupId: number,
        userId: number,
    ): Promise<void> {
        return this.request<void>(
            'POST',
            `/api/v1/rbac/groups/${groupId}/members`,
            { user_id: userId },
            { cookie },
        );
    }

    async removeGroupMember(
        cookie: string,
        groupId: number,
        userId: number,
    ): Promise<void> {
        return this.request<void>(
            'DELETE',
            `/api/v1/rbac/groups/${groupId}/members/user/${userId}`,
            undefined,
            { cookie },
        );
    }

    async addGroupMemberGroup(
        cookie: string,
        parentGroupId: number,
        childGroupId: number,
    ): Promise<void> {
        return this.request<void>(
            'POST',
            `/api/v1/rbac/groups/${parentGroupId}/members`,
            { group_id: childGroupId },
            { cookie },
        );
    }

    async removeGroupMemberGroup(
        cookie: string,
        parentGroupId: number,
        childGroupId: number,
    ): Promise<void> {
        return this.request<void>(
            'DELETE',
            `/api/v1/rbac/groups/${parentGroupId}/members/group/${childGroupId}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Group admin permissions
    // -----------------------------------------------------------

    async grantGroupAdminPermission(
        cookie: string,
        groupId: number,
        permission: string,
    ): Promise<void> {
        return this.request<void>(
            'POST',
            `/api/v1/rbac/groups/${groupId}/permissions`,
            { permission },
            { cookie },
        );
    }

    async listGroupAdminPermissions(
        cookie: string,
        groupId: number,
    ): Promise<{ permissions: string[] }> {
        return this.request<{ permissions: string[] }>(
            'GET',
            `/api/v1/rbac/groups/${groupId}/permissions`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Group MCP privileges
    // -----------------------------------------------------------

    async grantGroupMcpPrivilege(
        cookie: string,
        groupId: number,
        privilegeName: string,
    ): Promise<void> {
        return this.request<void>(
            'POST',
            `/api/v1/rbac/groups/${groupId}/privileges/mcp`,
            { privilege: privilegeName },
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Group connection privileges
    // -----------------------------------------------------------

    async grantGroupConnectionPrivilege(
        cookie: string,
        groupId: number,
        connectionId: number,
        accessLevel: string,
    ): Promise<void> {
        return this.request<void>(
            'POST',
            `/api/v1/rbac/groups/${groupId}/privileges/connections`,
            { connection_id: connectionId, access_level: accessLevel },
            { cookie },
        );
    }

    async listGroupConnectionPrivileges(
        cookie: string,
        groupId: number,
    ): Promise<{
        connection_privileges: Array<{
            connection_id: number;
            access_level: string;
        }>;
    }> {
        const detail = await this.request<{
            connection_privileges?: Array<{
                connection_id: number;
                access_level: string;
            }>;
        }>(
            'GET',
            `/api/v1/rbac/groups/${groupId}`,
            undefined,
            { cookie },
        );
        return {
            connection_privileges: detail.connection_privileges ?? [],
        };
    }

    // -----------------------------------------------------------
    // Connections (for RBAC scope tests)
    // -----------------------------------------------------------

    async listConnections(
        authHeader?: { cookie?: string; bearerToken?: string },
    ): Promise<ConnectionResponse[]> {
        const headers: Record<string, string> = {};
        if (authHeader?.cookie) {
            headers['Cookie'] = authHeader.cookie;
        }
        if (authHeader?.bearerToken) {
            headers['Authorization'] = `Bearer ${authHeader.bearerToken}`;
        }
        return this.requestRaw<ConnectionResponse[]>(
            'GET',
            '/api/v1/connections',
            undefined,
            headers,
        );
    }

    /**
     * List clusters (autocomplete summaries) via GET
     * /api/v1/clusters/list. Returns a bare array of {id, name}; used for
     * a race-free existence check in cluster setup (the navigator tree
     * loads asynchronously, so a DOM-based check can miss an existing
     * cluster).
     */
    async listClusters(
        cookie: string,
    ): Promise<Array<{
        id: number;
        name: string;
        replication_type?: string | null;
    }>> {
        return this.requestRaw<Array<{
            id: number;
            name: string;
            replication_type?: string | null;
        }>>(
            'GET',
            '/api/v1/clusters/list',
            undefined,
            { Cookie: cookie },
        );
    }

    // -----------------------------------------------------------
    // User privileges (for RBAC scope tests)
    // -----------------------------------------------------------

    async getUserPrivileges(
        cookie: string,
        userId: number,
    ): Promise<{
        username: string;
        is_superuser: boolean;
        groups: string[];
        mcp_privileges: string[];
        connection_privileges: Record<string, string>;
        admin_permissions: string[];
    }> {
        return this.request<{
            username: string;
            is_superuser: boolean;
            groups: string[];
            mcp_privileges: string[];
            connection_privileges: Record<string, string>;
            admin_permissions: string[];
        }>(
            'GET',
            `/api/v1/rbac/users/${userId}/privileges`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // MCP tool listing & invocation (for scope enforcement tests)
    // -----------------------------------------------------------

    /**
     * List the MCP tools available to the caller via
     * GET /api/v1/mcp/tools. The server RBAC-filters the catalogue to the
     * tools the authenticated principal may use (`ListForContext`), so the
     * returned set depends on the supplied cookie/token: a superuser sees
     * every registered tool the deployment supports. Returns the bare
     * `tools` array (name + description + input schema per entry).
     */
    async listMcpTools(
        auth?: { cookie?: string; bearerToken?: string },
    ): Promise<McpToolDefinition[]> {
        const headers: Record<string, string> = {};
        if (auth?.cookie) {
            headers['Cookie'] = auth.cookie;
        }
        if (auth?.bearerToken) {
            headers['Authorization'] = `Bearer ${auth.bearerToken}`;
        }
        const raw = await this.requestRaw<{ tools?: McpToolDefinition[] }>(
            'GET',
            '/api/v1/mcp/tools',
            undefined,
            headers,
        );
        return raw.tools ?? [];
    }

    /**
     * Invoke an MCP tool via POST /api/v1/mcp/tools/call and return the
     * parsed tool result. Authenticate with either a session cookie or a
     * bearer token (the RBAC specs log the non-privileged user in and
     * hold their cookie; bearer remains supported for token-scope tests).
     *
     * RBAC denials are HTTP 200 with `isError` set or an explanatory
     * `text`, so this method does not throw on a denial — callers assert
     * on `isError` and `content[0].text`.
     */
    async callMcpTool(
        toolName: string,
        args: Record<string, unknown>,
        auth?: { cookie?: string; bearerToken?: string },
    ): Promise<McpToolResult> {
        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
        };
        if (auth?.cookie) {
            headers['Cookie'] = auth.cookie;
        }
        if (auth?.bearerToken) {
            headers['Authorization'] = `Bearer ${auth.bearerToken}`;
        }
        const raw = await this.requestRaw<{
            content?: McpContentBlock[];
            isError?: boolean;
        }>(
            'POST',
            '/api/v1/mcp/tools/call',
            { name: toolName, arguments: args },
            headers,
        );
        const content = raw.content ?? [];
        return {
            content,
            isError: raw.isError === true,
            text: content.map((block) => block.text ?? '').join('\n'),
        };
    }

    // -----------------------------------------------------------
    // Monitoring: probe configs & metrics (for stats verification)
    // -----------------------------------------------------------

    /**
     * List probe configurations. Without `connectionId` the global
     * default configurations (connection_id === null) are returned.
     */
    async getProbeConfigs(
        cookie: string,
        connectionId?: number,
    ): Promise<ProbeConfig[]> {
        const suffix = connectionId !== undefined
            ? `?connection_id=${connectionId}`
            : '';
        return this.request<ProbeConfig[]>(
            'GET',
            `/api/v1/probe-configs${suffix}`,
            undefined,
            { cookie },
        );
    }

    /**
     * Update a probe configuration (enable/disable, change the
     * collection interval, or adjust retention).
     */
    async updateProbeConfig(
        cookie: string,
        id: number,
        update: ProbeConfigUpdate,
    ): Promise<ProbeConfig> {
        return this.request<ProbeConfig>(
            'PUT',
            `/api/v1/probe-configs/${id}`,
            update,
            { cookie },
        );
    }

    /**
     * Convenience wrapper around the query_database MCP tool used to
     * run arbitrary SQL against a monitored connection's database.
     */
    async queryDatabase(
        cookie: string,
        connectionId: number,
        query: string,
    ): Promise<{ status: number; body: unknown }> {
        return this.rawPost(
            '/api/v1/mcp/tools/call',
            {
                name: 'query_database',
                arguments: { connection_id: connectionId, query },
            },
            { Cookie: cookie },
        );
    }

    /**
     * Fetch the Top Queries panel data for a connection. Returns the
     * bare row array plus the `X-Total-Count` header value. A dedicated
     * fetch is used (rather than `request`) so the response header can
     * be read.
     */
    async getTopQueries(
        cookie: string,
        params: {
            connectionId: number;
            databaseName?: string;
            limit?: number;
            offset?: number;
            orderBy?: string;
            order?: 'asc' | 'desc';
            excludeCollector?: boolean;
        },
    ): Promise<TopQueriesResult> {
        const sp = new URLSearchParams();
        sp.set('connection_id', params.connectionId.toString());
        sp.set('limit', (params.limit ?? 100).toString());
        sp.set('offset', (params.offset ?? 0).toString());
        sp.set('order_by', params.orderBy ?? 'total_exec_time');
        sp.set('order', params.order ?? 'desc');
        if (params.excludeCollector) {
            sp.set('exclude_collector', 'true');
        }
        if (params.databaseName) {
            sp.set('database_name', params.databaseName);
        }

        const res = await fetch(
            `${this.baseUrl}/api/v1/metrics/top-queries?${sp.toString()}`,
            {
                method: 'GET',
                headers: { Cookie: cookie, Accept: 'application/json' },
                redirect: 'manual',
            },
        );
        if (!res.ok) {
            const text = await res.text().catch(() => res.statusText);
            throw new Error(
                `getTopQueries failed: ${res.status} ${text}`,
            );
        }
        const rows = (await res.json()) as TopQueryApiRow[];
        const rawTotal = res.headers.get('X-Total-Count');
        let totalCount: number | null = null;
        if (rawTotal !== null) {
            const parsed = Number.parseInt(rawTotal, 10);
            if (Number.isFinite(parsed) && parsed >= 0) {
                totalCount = parsed;
            }
        }
        return { rows, totalCount };
    }

    /**
     * Fetch a bucketed metric time series via GET /api/v1/metrics/query.
     * Mirrors the request the SystemResourcesSection KPI tiles make so
     * the served data can be compared against the rendered tile values.
     */
    async getMetricsQuery(
        cookie: string,
        params: {
            probeName: string;
            connectionId: number;
            metrics: string[];
            timeRange?: string;
            buckets?: number;
            aggregation?: 'avg' | 'sum' | 'min' | 'max' | 'last';
        },
    ): Promise<MetricSeries[]> {
        const sp = new URLSearchParams();
        sp.set('probe_name', params.probeName);
        sp.set('connection_id', params.connectionId.toString());
        sp.set('time_range', params.timeRange ?? '1h');
        if (params.buckets !== undefined) {
            sp.set('buckets', params.buckets.toString());
        }
        if (params.aggregation) {
            sp.set('aggregation', params.aggregation);
        }
        if (params.metrics.length > 0) {
            sp.set('metrics', params.metrics.join(','));
        }
        return this.request<MetricSeries[]>(
            'GET',
            `/api/v1/metrics/query?${sp.toString()}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Capabilities
    // -----------------------------------------------------------

    /**
     * Fetch the server capabilities (whether AI features are enabled and
     * the agentic-loop iteration cap) via GET /api/v1/capabilities.
     */
    async getCapabilities(cookie: string): Promise<CapabilitiesResponse> {
        return this.request<CapabilitiesResponse>(
            'GET',
            '/api/v1/capabilities',
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Alerts
    // -----------------------------------------------------------

    /**
     * List alerts via GET /api/v1/alerts. `connectionIds` is serialised
     * as a comma-separated `connection_ids` parameter, and
     * `excludeCleared` maps to `exclude_cleared=true`.
     */
    async getAlerts(
        cookie: string,
        params: {
            connectionId?: number;
            connectionIds?: number[];
            status?: string;
            excludeCleared?: boolean;
            severity?: string;
            alertType?: string;
            startTime?: string;
            endTime?: string;
            limit?: number;
            offset?: number;
        } = {},
    ): Promise<AlertListResponse> {
        const qs = this.buildAlertQuery(params);
        return this.request<AlertListResponse>(
            'GET',
            `/api/v1/alerts${qs}`,
            undefined,
            { cookie },
        );
    }

    /**
     * Fetch alert counts (total and per-server) via
     * GET /api/v1/alerts/counts. The server derives the visible
     * connection set from the caller's RBAC scope; the optional
     * connection filters are forwarded for forward compatibility.
     */
    async getAlertCounts(
        cookie: string,
        params: {
            connectionId?: number;
            connectionIds?: number[];
        } = {},
    ): Promise<AlertCountsResponse> {
        const qs = this.buildAlertQuery(params);
        return this.request<AlertCountsResponse>(
            'GET',
            `/api/v1/alerts/counts${qs}`,
            undefined,
            { cookie },
        );
    }

    /**
     * Acknowledge an alert via POST /api/v1/alerts/acknowledge. The
     * acknowledging user is resolved from the session on the server.
     */
    async acknowledgeAlert(
        cookie: string,
        params: { alertId: number; message?: string; falsePositive?: boolean },
    ): Promise<{ status: string }> {
        return this.request<{ status: string }>(
            'POST',
            '/api/v1/alerts/acknowledge',
            {
                alert_id: params.alertId,
                message: params.message ?? '',
                false_positive: params.falsePositive ?? false,
            },
            { cookie },
        );
    }

    /**
     * Restore an acknowledged alert to active via
     * DELETE /api/v1/alerts/acknowledge?alert_id=<id>.
     */
    async unacknowledgeAlert(
        cookie: string,
        alertId: number,
    ): Promise<{ status: string }> {
        return this.request<{ status: string }>(
            'DELETE',
            `/api/v1/alerts/acknowledge?alert_id=${alertId}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Alert rules & overrides
    // -----------------------------------------------------------

    /**
     * List the alert-rule catalog via GET /api/v1/alert-rules. Returns a
     * bare array of rules.
     */
    async getAlertRules(cookie: string): Promise<AlertRuleApiRow[]> {
        return this.request<AlertRuleApiRow[]>(
            'GET',
            '/api/v1/alert-rules',
            undefined,
            { cookie },
        );
    }

    /**
     * Convenience wrapper returning the first alert rule whose name
     * matches exactly, or null when no rule matches.
     */
    async getAlertRuleByName(
        cookie: string,
        name: string,
    ): Promise<AlertRuleApiRow | null> {
        const rules = await this.getAlertRules(cookie);
        return rules.find((rule) => rule.name === name) ?? null;
    }

    /**
     * Upsert an alert-rule threshold override for a scope via
     * PUT /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}.
     */
    async upsertAlertOverride(
        cookie: string,
        scope: 'server' | 'cluster' | 'group',
        scopeId: number,
        ruleId: number,
        body: {
            operator: string;
            threshold: number;
            severity: string;
            enabled: boolean;
        },
    ): Promise<{ status: string }> {
        return this.request<{ status: string }>(
            'PUT',
            `/api/v1/alert-overrides/${scope}/${scopeId}/${ruleId}`,
            body,
            { cookie },
        );
    }

    /**
     * Delete an alert-rule threshold override for a scope via
     * DELETE /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}.
     */
    async deleteAlertOverride(
        cookie: string,
        scope: 'server' | 'cluster' | 'group',
        scopeId: number,
        ruleId: number,
    ): Promise<{ status: string }> {
        return this.request<{ status: string }>(
            'DELETE',
            `/api/v1/alert-overrides/${scope}/${scopeId}/${ruleId}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Blackouts
    // -----------------------------------------------------------

    /**
     * Create a blackout window via POST /api/v1/blackouts. `start_time`
     * and `end_time` are RFC3339 strings. Requires the `manage_blackouts`
     * permission, which the default admin/superuser holds. Provided for
     * setup, teardown, and cross-checks; the GUI-driven specs create
     * blackouts through the UI rather than this method.
     */
    async createBlackout(
        cookie: string,
        payload: {
            scope: 'estate' | 'group' | 'cluster' | 'server';
            connection_id?: number;
            cluster_id?: number;
            group_id?: number;
            reason: string;
            start_time: string;
            end_time: string;
        },
    ): Promise<Blackout> {
        return this.request<Blackout>(
            'POST',
            '/api/v1/blackouts',
            payload,
            { cookie },
        );
    }

    /**
     * List blackouts via GET /api/v1/blackouts. `active` maps to the
     * server's `active` filter (true → only currently-active windows,
     * false → only non-active); omitting it returns every blackout the
     * caller can see. `connectionId` maps to the `connection_id` filter.
     */
    async listBlackouts(
        cookie: string,
        params: { active?: boolean; connectionId?: number } = {},
    ): Promise<BlackoutListResponse> {
        const sp = new URLSearchParams();
        if (params.active !== undefined) {
            sp.set('active', params.active ? 'true' : 'false');
        }
        if (params.connectionId !== undefined) {
            sp.set('connection_id', params.connectionId.toString());
        }
        const qs = sp.toString();
        return this.request<BlackoutListResponse>(
            'GET',
            `/api/v1/blackouts${qs ? `?${qs}` : ''}`,
            undefined,
            { cookie },
        );
    }

    /**
     * Stop an active blackout via POST /api/v1/blackouts/{id}/stop. The
     * server sets its end time to now, flipping `is_active` to false.
     */
    async stopBlackout(cookie: string, id: number): Promise<Blackout> {
        return this.request<Blackout>(
            'POST',
            `/api/v1/blackouts/${id}/stop`,
            undefined,
            { cookie },
        );
    }

    /** Delete a blackout via DELETE /api/v1/blackouts/{id}. */
    async deleteBlackout(cookie: string, id: number): Promise<void> {
        return this.request<void>(
            'DELETE',
            `/api/v1/blackouts/${id}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Timeline & overview
    // -----------------------------------------------------------

    /**
     * Fetch timeline events via GET /api/v1/timeline/events. `startTime`
     * and `endTime` are required ISO strings; `eventTypes` is serialised
     * as a comma-separated `event_types` parameter when supplied.
     */
    async getTimelineEvents(
        cookie: string,
        params: {
            connectionId?: number;
            connectionIds?: number[];
            startTime: string;
            endTime: string;
            eventTypes?: string[];
            limit?: number;
        },
    ): Promise<TimelineEventsResponse> {
        const sp = new URLSearchParams();
        sp.set('start_time', params.startTime);
        sp.set('end_time', params.endTime);
        if (params.connectionId !== undefined) {
            sp.set('connection_id', params.connectionId.toString());
        }
        if (params.connectionIds && params.connectionIds.length > 0) {
            sp.set('connection_ids', params.connectionIds.join(','));
        }
        if (params.eventTypes && params.eventTypes.length > 0) {
            sp.set('event_types', params.eventTypes.join(','));
        }
        if (params.limit !== undefined) {
            sp.set('limit', params.limit.toString());
        }
        return this.request<TimelineEventsResponse>(
            'GET',
            `/api/v1/timeline/events?${sp.toString()}`,
            undefined,
            { cookie },
        );
    }

    /**
     * Fetch the AI overview for a scope via GET /api/v1/overview. When no
     * scope is given the estate-wide overview is returned. Set `refresh`
     * to force regeneration.
     */
    async getOverview(
        cookie: string,
        params: {
            scopeType?: 'server' | 'cluster';
            scopeId?: number;
            connectionIds?: number[];
            refresh?: boolean;
        } = {},
    ): Promise<OverviewApiResponse> {
        const sp = new URLSearchParams();
        if (params.scopeType) {
            sp.set('scope_type', params.scopeType);
        }
        if (params.scopeId !== undefined) {
            sp.set('scope_id', params.scopeId.toString());
        }
        if (params.connectionIds && params.connectionIds.length > 0) {
            sp.set('connection_ids', params.connectionIds.join(','));
        }
        if (params.refresh) {
            sp.set('refresh', 'true');
        }
        const query = sp.toString();
        return this.request<OverviewApiResponse>(
            'GET',
            `/api/v1/overview${query ? `?${query}` : ''}`,
            undefined,
            { cookie },
        );
    }

    // -----------------------------------------------------------
    // Latest metric snapshot & ad-hoc query
    // -----------------------------------------------------------

    /**
     * Fetch the latest metric snapshot rows for a probe via
     * GET /api/v1/metrics/latest. Server-scoped `pg_sys_*` probes require
     * an explicit `orderBy` dimension column, otherwise the server
     * responds 400 ("no dimension columns found").
     */
    async getMetricsLatest(
        cookie: string,
        probeName: string,
        connectionId: number,
        orderBy?: string,
    ): Promise<LatestSnapshotResponse> {
        const sp = new URLSearchParams();
        sp.set('probe_name', probeName);
        sp.set('connection_id', connectionId.toString());
        if (orderBy) {
            sp.set('order_by', orderBy);
        }
        return this.request<LatestSnapshotResponse>(
            'GET',
            `/api/v1/metrics/latest?${sp.toString()}`,
            undefined,
            { cookie },
        );
    }

    /**
     * Execute ad-hoc SQL against a monitored connection's database via
     * POST /api/v1/connections/{connectionId}/query. This backs the AI
     * "Run query" / RunnableCodeBlock flow. When the payload contains
     * write statements and `confirmed` is not set, the response carries
     * `requires_confirmation: true` and `write_statements`.
     */
    async executeConnectionQuery(
        cookie: string,
        connectionId: number,
        params: { query: string; databaseName?: string; confirmed?: boolean },
    ): Promise<ConnectionQueryResponse> {
        const body: Record<string, unknown> = { query: params.query };
        if (params.databaseName) {
            body.database_name = params.databaseName;
        }
        if (params.confirmed) {
            body.confirmed = true;
        }
        return this.request<ConnectionQueryResponse>(
            'POST',
            `/api/v1/connections/${connectionId}/query`,
            body,
            { cookie },
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
        const body = await this.parseResponseBody(res);
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
        const body = await this.parseResponseBody(res);
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
     * Build the query-string suffix shared by the alert list and count
     * endpoints. Returns an empty string when no parameters are set,
     * otherwise a string beginning with `?`.
     */
    private buildAlertQuery(params: {
        connectionId?: number;
        connectionIds?: number[];
        status?: string;
        excludeCleared?: boolean;
        severity?: string;
        alertType?: string;
        startTime?: string;
        endTime?: string;
        limit?: number;
        offset?: number;
    }): string {
        const sp = new URLSearchParams();
        if (params.connectionId !== undefined) {
            sp.set('connection_id', params.connectionId.toString());
        }
        if (params.connectionIds && params.connectionIds.length > 0) {
            sp.set('connection_ids', params.connectionIds.join(','));
        }
        if (params.status) {
            sp.set('status', params.status);
        }
        if (params.excludeCleared) {
            sp.set('exclude_cleared', 'true');
        }
        if (params.severity) {
            sp.set('severity', params.severity);
        }
        if (params.alertType) {
            sp.set('alert_type', params.alertType);
        }
        if (params.startTime) {
            sp.set('start_time', params.startTime);
        }
        if (params.endTime) {
            sp.set('end_time', params.endTime);
        }
        if (params.limit !== undefined) {
            sp.set('limit', params.limit.toString());
        }
        if (params.offset !== undefined) {
            sp.set('offset', params.offset.toString());
        }
        const query = sp.toString();
        return query ? `?${query}` : '';
    }

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
     * Safely parse a response body as JSON, falling back to plain
     * text. Uses `res.text()` first to avoid the "body already
     * read" error that occurs when `res.json()` fails after the
     * body stream has been consumed.
     */
    private async parseResponseBody(res: Response): Promise<unknown> {
        const text = await res.text();
        try {
            return JSON.parse(text);
        } catch {
            return text;
        }
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
