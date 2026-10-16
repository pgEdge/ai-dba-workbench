/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect, type Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { label } from 'allure-js-commons';
import { ApiHelper, type McpToolResult } from '../../helpers/api.helper';
import { AuthHelper } from '../../helpers/auth.helper';
import { AlertSetupHelper } from '../../helpers/alert-setup.helper';
import { ReplicationHelper } from '../../helpers/replication.helper';
import { MonitoringPage } from '../../pages/MonitoringPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { ChatPage } from '../../pages/ChatPage';
import {
    ensureClusterAndPrimary,
    primeSystemMetrics,
    CLUSTER_NAME,
    PRIMARY_SERVER,
} from '../../helpers/cluster-setup.helper';
import {
    captureChatPanel,
    captureToolResponse,
    CAPTURE_WIDTH,
    SCREENSHOT_DIR,
} from '../../helpers/screenshot.helper';
import { LLM_CONFIG } from '../../fixtures/llm-config';
import { loadE2EConfig } from '../../fixtures/e2e-config';
import { API_URL } from '../../fixtures/test-data';
import { getChatResponseOrSkip } from '../../helpers/ai.helper';

/**
 * Comprehensive "Ask Ellie" MCP-tool coverage.
 *
 * For EVERY tool the deployment offers (enumerated at runtime from
 * GET /api/v1/mcp/tools with the superuser cookie), this suite runs a
 * hybrid check:
 *
 *   1. DETERMINISTIC contract check (authoritative): call the tool
 *      directly via POST /api/v1/mcp/tools/call and assert `isError` plus
 *      the tool-specific header/field/value strings emitted by its Go
 *      handler (server/src/internal/tools/<tool>.go). This is the strong,
 *      non-flaky regression assertion.
 *   2. ASK ELLIE chat flow: send a directive prompt naming the tool's
 *      intent, wait for the reply, and assert BOTH that the correct tool
 *      ran (its tool-activity chip, whose label is getToolDisplayName(name),
 *      appears in the panel transcript) AND that a tool-specific content
 *      signature is present.
 *   3. SCREENSHOT the chat response in a try/finally so it is captured on
 *      BOTH pass and fail (the finally never swallows the assertion error).
 *
 * A static coverage-guard test fails the run if the server offers a tool
 * with no TOOL_SPECS entry (retry-safe, deterministic). LLM tool selection
 * is non-deterministic, so chat assertions are structural (chip + signature,
 * or a per-tool expected-behaviour override), never exact strings.
 */

// ---------------------------------------------------------------------------
// getToolDisplayName — inlined from client/src/utils/toolDisplayNames.ts.
//
// The e2e tsconfig `rootDir` is the e2e package, so importing across the
// package boundary into `../../../src` is disallowed by the compiler. This
// map MUST stay in sync with the product source; the ChatPanel renders each
// tool-activity chip with exactly this label, and the transcript reader
// folds that label in. See .claude/react-expert/quality-checklist.md.
// ---------------------------------------------------------------------------

const TOOL_DISPLAY_NAMES: Record<string, string> = {
    query_metrics: 'Querying metrics',
    get_metric_baselines: 'Fetching metric baselines',
    get_alert_history: 'Reviewing alert history',
    get_alert_rules: 'Checking alert rules',
    query_database: 'Querying database',
    get_schema_info: 'Inspecting schema',
    list_probes: 'Listing probes',
    describe_probe: 'Examining probe details',
    get_blackouts: 'Checking blackouts',
    search_knowledgebase: 'Searching knowledgebase',
    list_connections: 'Listing connections',
    execute_explain: 'Running EXPLAIN',
    query_datastore: 'Querying datastore',
    read_resource: 'Reading resource',
    generate_embedding: 'Generating embedding',
    similarity_search: 'Searching similarities',
    test_query: 'Validating query',
    count_rows: 'Counting rows',
    store_memory: 'Storing memory',
    recall_memories: 'Recalling memories',
    delete_memory: 'Deleting memory',
    get_timeline_events: 'get_timeline_events',
};

function getToolDisplayName(toolName: string): string {
    return TOOL_DISPLAY_NAMES[toolName] || toolName;
}

// ---------------------------------------------------------------------------
// Fixtures shared by the per-tool contract + chat closures.
// ---------------------------------------------------------------------------

/** Resolved runtime context handed to every tool spec. */
interface ToolCtx {
    api: ApiHelper;
    adminCookie: string;
    /** The replication primary connection id (name matches /primary/i). */
    connId: number;
    /** A probe primed with data for the primary (pg_sys_memory_info). */
    metricsProbe: string;
    /** The seeded RAG database on the primary. */
    database: string;
    /** The seeded table in `database` (no vector column). */
    table: string;
}

/**
 * Per-test scratch state produced by a tool's optional `setup` and threaded
 * through its contract/chat/teardown steps (e.g. a temporary blackout id and
 * its unique marker reason for get_blackouts).
 */
type ToolState = Record<string, unknown>;

/** A single tool's hybrid coverage definition. */
interface ToolSpec {
    /**
     * Whether the tool exercises the EMBEDDING provider. Governs only the
     * screenshot filename provider slug (embedding vs reasoning).
     */
    isEmbeddingTool: boolean;
    /**
     * Optional per-test setup run BEFORE the contract check, whose returned
     * state is threaded into runContract/buildChatPrompt/chatAssert and the
     * matching teardown. Used to create fixture data the tool then reports on
     * (e.g. a temporary active blackout). Only get_blackouts uses it.
     */
    setup?: (ctx: ToolCtx) => Promise<ToolState>;
    /**
     * Optional teardown run in the test's outer `finally` — ALWAYS runs, even
     * when a contract or chat assertion fails, so fixture data (e.g. a
     * temporary blackout that would otherwise suppress alerts and corrupt
     * later specs) is always cleaned up.
     */
    teardown?: (ctx: ToolCtx, state: ToolState) => Promise<void>;
    /** Deterministic, authoritative contract check via the MCP API. */
    runContract: (ctx: ToolCtx, state: ToolState) => Promise<void>;
    /**
     * Build the directive chat prompt. May perform setup (e.g. pre-store a
     * memory) and embed dynamic values such as an id.
     */
    buildChatPrompt: (ctx: ToolCtx, state: ToolState) => Promise<string>;
    /** Tool-specific content signature expected in the chat transcript. */
    chatSignature: RegExp;
    /**
     * Optional override of the default chat assertion. When present it
     * REPLACES the default "chip label + chatSignature" check — used when
     * the correct Ask-Ellie behaviour is NOT to call the tool (e.g.
     * similarity_search on a table with no vector column, where Ellie
     * inspects the schema and explains the missing capability instead).
     */
    chatAssert?: (transcript: string, ctx: ToolCtx, state: ToolState) => void;
}

/** Escape a string for safe embedding inside a RegExp. */
function escapeRegExp(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Read a PNG's pixel dimensions straight from its IHDR chunk: an 8-byte
 * signature, then length(4) + "IHDR"(4), so width is the big-endian uint32
 * at byte 16 and height at byte 20. Inlined to avoid adding an image
 * dependency; used only by the screenshot-framing verification test.
 */
function readPngSize(buffer: Buffer): { width: number; height: number } {
    return { width: buffer.readUInt32BE(16), height: buffer.readUInt32BE(20) };
}

const DATABASE = 'ragdb';
const TABLE = 'documents';
/** Primed in beforeEach via primeSystemMetrics; guaranteed rows for primary. */
const METRICS_PROBE = 'pg_sys_memory_info';

// ---------------------------------------------------------------------------
// Contract assertion helpers.
// ---------------------------------------------------------------------------

/**
 * Assert a tool result succeeded (isError=false) and its text contains
 * every required substring. Each failure names the offending substring.
 */
function expectSuccessContains(
    result: McpToolResult,
    name: string,
    substrings: string[],
): void {
    expect(
        result.isError,
        `${name}: tool must not report isError — tool text: ${result.text}`,
    ).toBe(false);
    for (const needle of substrings) {
        expect(
            result.text,
            `${name}: response must contain ${JSON.stringify(needle)}`,
        ).toContain(needle);
    }
}

/**
 * True when a tool result is a transient provider rate-limit (HTTP 429),
 * e.g. the embedding provider throttling many calls in one run. This is a
 * throttle, not a defect, so callers may wait and retry.
 */
function isRateLimited(result: McpToolResult): boolean {
    return (
        result.isError &&
        /\b429\b|rate.?limit|too many requests/i.test(result.text)
    );
}

/**
 * Call an MCP tool, retrying ONLY on a transient rate-limit (429) with a wait
 * between attempts (embedding/LLM providers throttle per-minute). A success or
 * any NON-rate-limit error returns immediately so real failures are never
 * hidden; if the rate limit never clears, the last (still-failing) result is
 * returned so the caller's assertion fails clearly.
 */
async function callMcpToolWithRetry(
    api: ApiHelper,
    name: string,
    args: Record<string, unknown>,
    cookie: string,
    attempts = 4,
    delayMs = 20_000,
): Promise<McpToolResult> {
    let result = await api.callMcpTool(name, args, { cookie });
    for (let attempt = 1; attempt < attempts; attempt += 1) {
        if (!isRateLimited(result)) {
            return result;
        }
        // eslint-disable-next-line no-console -- diagnostic for a slow retry
        console.log(
            `[E2E] ${name}: rate-limited (429), waiting ${delayMs}ms then ` +
            `retrying (attempt ${attempt + 1}/${attempts})`,
        );
        await new Promise<void>((resolve) => setTimeout(resolve, delayMs));
        result = await api.callMcpTool(name, args, { cookie });
    }
    return result;
}

/**
 * Store a memory via the MCP API as the current (admin) user and return the
 * new memory id and the unique content marker. Used to seed recall/delete so
 * their contract checks are self-contained and deterministic.
 */
async function seedMemory(
    ctx: ToolCtx,
    category: string,
): Promise<{ id: number; content: string }> {
    const content =
        `e2e-memory-${category}-${Date.now()}-` +
        Math.random().toString(36).slice(2, 8);
    const res = await ctx.api.callMcpTool(
        'store_memory',
        { content, category, scope: 'user' },
        { cookie: ctx.adminCookie },
    );
    expect(res.isError, 'seedMemory: store_memory must succeed').toBe(false);
    const idMatch = /ID:\s*(\d+)/.exec(res.text);
    expect(idMatch, 'seedMemory: store_memory response must include an ID')
        .not.toBeNull();
    const id = Number(idMatch?.[1]);
    expect(
        Number.isInteger(id) && id > 0,
        'seedMemory: parsed memory id must be a positive integer',
    ).toBe(true);
    return { id, content };
}

/**
 * Shared alert-store seeder for the get_alert_history fixture below. Like the
 * module-scope helpers above (escapeRegExp, seedMemory) it is captured by the
 * per-tool closures in TOOL_SPECS. Construction is side-effect-free; it only
 * runs `docker exec psql` when a seed/delete method is actually called.
 */
const alertSeeder = new AlertSetupHelper();

// ---------------------------------------------------------------------------
// TOOL_SPECS — one entry per registered MCP tool. Assertions are grounded in
// each tool's Go handler in server/src/internal/tools/<tool>.go.
// ---------------------------------------------------------------------------

const TOOL_SPECS: Record<string, ToolSpec> = {
    // -- Database tools (per-connection) ------------------------------------
    query_database: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'query_database',
                { connection_id: ctx.connId, query: 'SELECT 1 AS one' },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'query_database', [
                'Database:',
                'SQL Query:',
                'Results (1 rows):',
                'one',
            ]);
        },
        buildChatPrompt: async (ctx) =>
            `Using database connection ${ctx.connId}, run the SQL query ` +
            `"SELECT 1 AS one" and show me the result.`,
        chatSignature: /\bone\b/i,
    },

    get_schema_info: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'get_schema_info',
                {
                    connection_id: ctx.connId,
                    database_name: ctx.database,
                    schema_name: 'public',
                    table_name: ctx.table,
                },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'get_schema_info', [
                'Database:',
                'schema\ttable\ttype',
                ctx.table,
                'content',
                'metadata',
            ]);
        },
        buildChatPrompt: async (ctx) =>
            `Inspect the schema of the "${ctx.table}" table in the ` +
            `${ctx.database} database on connection ${ctx.connId} and ` +
            'describe its columns.',
        chatSignature: /documents|content|metadata/i,
    },

    execute_explain: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'execute_explain',
                {
                    connection_id: ctx.connId,
                    database_name: ctx.database,
                    query: `SELECT * FROM ${ctx.table}`,
                    analyze: false,
                },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'execute_explain', [
                'Database:',
                'Query:',
                'Execution Plan:',
                ctx.table,
            ]);
        },
        buildChatPrompt: async (ctx) =>
            `Run an EXPLAIN execution plan for the query ` +
            `"SELECT * FROM ${ctx.table}" on connection ${ctx.connId} ` +
            `(database ${ctx.database}).`,
        chatSignature: /plan|scan|documents/i,
    },

    count_rows: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'count_rows',
                {
                    connection_id: ctx.connId,
                    database_name: ctx.database,
                    schema: 'public',
                    table: ctx.table,
                },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'count_rows', [
                'Database:',
                'SQL Query:',
                'Count:',
            ]);
            const countMatch = /Count:\s*(\d+)/.exec(res.text);
            expect(countMatch, 'count_rows: response must carry a Count value')
                .not.toBeNull();
            // The seed inserts three rows into documents; other specs never
            // delete from it, so the count is a stable floor of 3.
            expect(
                Number(countMatch?.[1]),
                'count_rows: documents count must be at least the seeded 3',
            ).toBeGreaterThanOrEqual(3);
        },
        buildChatPrompt: async (ctx) =>
            `Count the number of rows in the public.${ctx.table} table on ` +
            `connection ${ctx.connId} (database ${ctx.database}).`,
        chatSignature: /count|rows|\b3\b/i,
    },

    test_query: {
        // RBAC-exempt: always offered and always callable.
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'test_query',
                { connection_id: ctx.connId, query: 'SELECT 1' },
                { cookie: ctx.adminCookie },
            );
            expect(res.isError, 'test_query: must not report isError').toBe(false);
            expect(
                res.text.trim(),
                'test_query: a valid query must return exactly "Query is valid."',
            ).toBe('Query is valid.');
        },
        buildChatPrompt: async (ctx) =>
            'Use the query validation tool to check whether the SQL ' +
            `"SELECT 1" is valid on connection ${ctx.connId}, without ` +
            'executing it.',
        chatSignature: /valid/i,
    },

    similarity_search: {
        // Embedding tool. The seeded documents table has NO vector column,
        // so the handler returns its no-vector-column error BEFORE ever
        // invoking the embedding provider. That error is the real, expected
        // contract for this input (see similarity_search.go).
        isEmbeddingTool: true,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'similarity_search',
                {
                    connection_id: ctx.connId,
                    database_name: ctx.database,
                    table_name: ctx.table,
                    query_text: 'PostgreSQL',
                },
                { cookie: ctx.adminCookie },
            );
            expect(
                res.isError,
                'similarity_search: a table with no vector column must error',
            ).toBe(true);
            expect(
                res.text,
                'similarity_search: must name the missing vector column',
            ).toContain(`No vector columns found in table '${ctx.table}'.`);
        },
        buildChatPrompt: async (ctx) =>
            `Run a vector similarity search for "PostgreSQL" against the ` +
            `${ctx.table} table on connection ${ctx.connId} ` +
            `(database ${ctx.database}).`,
        chatSignature: /vector|documents|similarit/i,
        // The documents table has no vector column, so the CORRECT Ask-Ellie
        // behaviour is NOT to call similarity_search: Ellie inspects the
        // schema (get_schema_info) and explains that a vector/embedding
        // column is missing. So do not require the similarity_search chip;
        // assert Ellie handled it meaningfully by referencing the missing
        // vector/embedding capability and naming the documents table. The
        // deterministic contract check above remains authoritative.
        chatAssert: (transcript) => {
            expect(
                transcript,
                'similarity_search: Ellie must reference the missing ' +
                'vector/embedding capability',
            ).toMatch(/vector|embedding|pgvector/i);
            expect(
                transcript,
                'similarity_search: Ellie must name the documents table',
            ).toMatch(/documents/i);
        },
    },

    // -- Datastore tools ----------------------------------------------------
    list_connections: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'list_connections',
                {},
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'list_connections', [
                'connections',
                'id\tname\thost\tport',
                PRIMARY_SERVER.name,
            ]);
        },
        buildChatPrompt: async () =>
            'List all database connections registered in the workbench.',
        chatSignature: new RegExp(PRIMARY_SERVER.name, 'i'),
    },

    list_probes: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'list_probes',
                {},
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'list_probes', [
                'metrics probes',
                'name\tdescription\trow_count\tscope',
                ctx.metricsProbe,
            ]);
        },
        buildChatPrompt: async () =>
            'List the available metrics probes the collector has created.',
        chatSignature: /probe/i,
    },

    describe_probe: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'describe_probe',
                { probe_name: ctx.metricsProbe },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'describe_probe', [
                `Probe: ${ctx.metricsProbe}`,
                'Columns:',
                'column_name\tdata_type\tdescription\tcolumn_type',
            ]);
        },
        buildChatPrompt: async (ctx) =>
            `Describe the "${ctx.metricsProbe}" metrics probe, including ` +
            'its columns.',
        chatSignature: /column|probe/i,
    },

    query_metrics: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'query_metrics',
                {
                    probe_name: ctx.metricsProbe,
                    connection_id: ctx.connId,
                    time_start: '30d',
                    buckets: 20,
                },
                { cookie: ctx.adminCookie },
            );
            // primeSystemMetrics guarantees rows for this probe/connection,
            // so the populated header + "(N rows)" trailer must appear.
            expectSuccessContains(res, 'query_metrics', [
                `Probe: ${ctx.metricsProbe} | Connection: ${ctx.connId}`,
                'bucket_time',
            ]);
            expect(
                res.text,
                'query_metrics: must report a row-count trailer for primed data',
            ).toMatch(/\(\d+ rows\)/);
        },
        buildChatPrompt: async (ctx) =>
            `Query the "${ctx.metricsProbe}" metric for connection ` +
            `${ctx.connId} over the last 30 days.`,
        chatSignature: /metric|memory|bucket/i,
    },

    get_alert_history: {
        // get_alert_history reports REAL data. In an isolated Ask-Ellie run no
        // alert has ever fired (the alert-lifecycle spec did not run), so the
        // response — and its screenshot — would show "No alerts ... The slate
        // is clean!". Mirroring get_blackouts, seed one ACTIVE alert for the
        // primary connection directly in the store (fast + deterministic; no
        // alerter eval-cycle wait), assert the tool returns it, then ALWAYS
        // delete exactly that row in teardown so it never leaks into later
        // tools/specs. The seed is scoped to ctx.connId — the SAME connection
        // the contract and chat prompt query — so the row actually appears.
        isEmbeddingTool: false,
        setup: async (ctx) => {
            const marker =
                `e2e-askellie-alerthist-${Date.now()}-` +
                Math.random().toString(36).slice(2, 8);
            const alertId = await alertSeeder.seedActiveAlert(ctx.connId, marker);
            return { alertId, marker };
        },
        teardown: async (ctx, state) => {
            const alertId = state.alertId as number | undefined;
            if (typeof alertId === 'number') {
                // Delete must always run; swallow only its OWN error (logged)
                // so it can never mask a failing assertion in the caller.
                await alertSeeder.deleteAlert(alertId).catch((err: unknown) => {
                    const reason =
                        err instanceof Error ? err.message : String(err);
                    // eslint-disable-next-line no-console -- cleanup diag
                    console.warn(
                        `[E2E] get_alert_history teardown failed to delete ` +
                        `alert ${alertId}: ${reason}`,
                    );
                });
            }
        },
        runContract: async (ctx, state) => {
            const marker = state.marker as string;
            const res = await ctx.api.callMcpTool(
                'get_alert_history',
                { connection_id: ctx.connId, status: 'all', limit: 5 },
                { cookie: ctx.adminCookie },
            );
            expect(res.isError, 'get_alert_history: must not error').toBe(false);
            // The seed guarantees the POPULATED shape, so assert it directly
            // (no empty-result fallback): the connection header AND the seeded
            // alert row identified by its unique marker title. Asserting on the
            // contract result — not brittle NL — keeps this robust.
            expect(
                res.text,
                'get_alert_history: must return the populated shape for ' +
                `connection ${ctx.connId}`,
            ).toMatch(new RegExp(`Alerts \\| Connection: ${ctx.connId}\\b`));
            expect(
                res.text,
                'get_alert_history: the seeded active alert (marker title) ' +
                'must appear in the response',
            ).toContain(marker);
        },
        buildChatPrompt: async (ctx) =>
            `Show me the alert history for connection ${ctx.connId}.`,
        chatSignature: /alert/i,
    },

    get_alert_rules: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            // The alert-rule catalogue is seeded, so this is fully
            // deterministic.
            const res = await ctx.api.callMcpTool(
                'get_alert_rules',
                { enabled_only: false },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'get_alert_rules', [
                'Alert Rules |',
                'rule_id\tname\tcategory\tmetric_name\toperator\t' +
                'threshold\tseverity\tenabled\tdescription',
            ]);
            expect(
                res.text,
                'get_alert_rules: must report a rule-count trailer',
            ).toMatch(/\(\d+ rules\)/);
        },
        buildChatPrompt: async () =>
            'List the alert rules and their default thresholds.',
        chatSignature: /alert|rule|threshold/i,
    },

    get_metric_baselines: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'get_metric_baselines',
                { connection_id: ctx.connId },
                { cookie: ctx.adminCookie },
            );
            expect(res.isError, 'get_metric_baselines: must not error').toBe(false);
            expect(
                res.text,
                'get_metric_baselines: must return a populated or empty-result ' +
                `shape for connection ${ctx.connId}`,
            ).toMatch(
                new RegExp(
                    `Metric Baselines \\| Connection: ${ctx.connId}\\b` +
                    `|No metric baselines found for connection ${ctx.connId}\\b` +
                    '|No metric baselines matched',
                ),
            );
        },
        buildChatPrompt: async (ctx) =>
            `Show the metric baselines for connection ${ctx.connId}.`,
        chatSignature: /baseline/i,
    },

    query_datastore: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'query_datastore',
                { query: 'SELECT 1 AS one' },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'query_datastore', [
                'Datastore Query',
                'SQL Query:',
                'Results (1 rows):',
                'one',
            ]);
        },
        buildChatPrompt: async () =>
            'Run the SQL query "SELECT 1 AS one" against the monitoring ' +
            'datastore (the ai_workbench store) and show the result.',
        chatSignature: /\bone\b|datastore/i,
    },

    get_blackouts: {
        // Superuser (admin) can always see blackouts. To assert real data
        // (not just the header) a temporary ACTIVE server-scoped blackout is
        // created on the primary in setup, asserted, then ALWAYS deleted in
        // teardown — a leftover active blackout would suppress alerts and
        // corrupt later specs (alert-lifecycle, etc.).
        isEmbeddingTool: false,
        setup: async (ctx) => {
            const reason =
                `e2e-askellie-blackout-${Date.now()}-` +
                Math.random().toString(36).slice(2, 8);
            const now = Date.now();
            // Active window: started a minute ago, ends 10 minutes out, so
            // NOW() is inside it and is_active renders true.
            const blackout = await ctx.api.createBlackout(ctx.adminCookie, {
                scope: 'server',
                connection_id: ctx.connId,
                reason,
                start_time: new Date(now - 60_000).toISOString(),
                end_time: new Date(now + 10 * 60_000).toISOString(),
            });
            return { blackoutId: blackout.id, reason };
        },
        teardown: async (ctx, state) => {
            const blackoutId = state.blackoutId as number | undefined;
            if (typeof blackoutId === 'number') {
                // Delete must always run; swallow only its OWN error (logged)
                // so it can never mask a failing assertion in the caller.
                await ctx.api
                    .deleteBlackout(ctx.adminCookie, blackoutId)
                    .catch((err: unknown) => {
                        const reason =
                            err instanceof Error ? err.message : String(err);
                        // eslint-disable-next-line no-console -- cleanup diag
                        console.warn(
                            `[E2E] get_blackouts teardown failed to delete ` +
                            `blackout ${blackoutId}: ${reason}`,
                        );
                    });
            }
        },
        runContract: async (ctx, state) => {
            const reason = state.reason as string;
            // active_only=false returns all matching blackouts (including the
            // active one); limit 50 guarantees ours is within the page.
            const res = await ctx.api.callMcpTool(
                'get_blackouts',
                { connection_id: ctx.connId, active_only: false, limit: 50 },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'get_blackouts', [
                `Blackouts | Connection: ${ctx.connId}`,
                'id\tscope\treason\tstart_time\tend_time\tis_active',
                reason,
            ]);
            // The marker row must show the blackout as active: reason column,
            // then start/end, then is_active rendered by the handler as `%t`
            // (true). Assert the marker's row ends in a `true` is_active cell.
            expect(
                res.text,
                'get_blackouts: the created blackout must appear as active ' +
                '(is_active true) on its marker row',
            ).toMatch(new RegExp(`${escapeRegExp(reason)}[^\\n]*\\ttrue`));
        },
        buildChatPrompt: async (ctx) =>
            `Are there any blackout windows configured for connection ` +
            `${ctx.connId}?`,
        chatSignature: /blackout/i,
    },

    get_timeline_events: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'get_timeline_events',
                { connection_id: ctx.connId, start_time: '7d', limit: 10 },
                { cookie: ctx.adminCookie },
            );
            expect(res.isError, 'get_timeline_events: must not error').toBe(false);
            expect(
                res.text,
                'get_timeline_events: must return a populated or empty-result ' +
                `shape for connection ${ctx.connId}`,
            ).toMatch(
                new RegExp(
                    `Timeline Events \\| Connection: ${ctx.connId}\\b` +
                    `|No timeline events for connection ${ctx.connId}\\b`,
                ),
            );
        },
        buildChatPrompt: async (ctx) =>
            `Show the timeline of events for connection ${ctx.connId} over ` +
            'the last 7 days.',
        chatSignature: /timeline|event/i,
    },

    // -- Stateless / resource / embedding tools -----------------------------
    read_resource: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'read_resource',
                { list: true },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'read_resource', [
                'Available Resources:',
                'URI:',
                'MIME Type:',
            ]);
        },
        buildChatPrompt: async () =>
            'List the MCP resources available to you.',
        chatSignature: /resource/i,
    },

    generate_embedding: {
        // Embedding tool; offered only when server-side embedding is enabled.
        isEmbeddingTool: true,
        runContract: async (ctx) => {
            // Embedding calls can hit the provider's per-minute rate limit
            // (Voyage returns 429) when a run makes many of them; wait and
            // retry rather than switch providers. A non-429 error still fails.
            const res = await callMcpToolWithRetry(
                ctx.api,
                'generate_embedding',
                { text: 'PostgreSQL is a powerful open-source database' },
                ctx.adminCookie,
            );
            expectSuccessContains(res, 'generate_embedding', [
                'Embedding Generated Successfully',
                'Provider:',
                'Model:',
                'Embedding Vector (',
            ]);
            // The handler appends the vector as a pretty-printed JSON array of
            // floats. Parse it and assert it is a non-empty numeric array.
            const start = res.text.indexOf('[');
            const end = res.text.lastIndexOf(']');
            expect(
                start >= 0 && end > start,
                'generate_embedding: response must include a JSON vector array',
            ).toBe(true);
            const parsed: unknown = JSON.parse(res.text.slice(start, end + 1));
            expect(
                Array.isArray(parsed),
                'generate_embedding: the vector must parse to an array',
            ).toBe(true);
            const vector = parsed as unknown[];
            expect(
                vector.length,
                'generate_embedding: the vector must not be empty',
            ).toBeGreaterThan(0);
            expect(
                vector.every((v) => typeof v === 'number'),
                'generate_embedding: every vector element must be a number',
            ).toBe(true);
        },
        buildChatPrompt: async () =>
            'Generate an embedding vector for the text ' +
            '"PostgreSQL is a powerful open-source database".',
        chatSignature: /embedding|dimension|vector/i,
    },

    search_knowledgebase: {
        // Embedding tool; offered only when a knowledgebase DB is configured
        // (RPM installs). list_products mode is deterministic and needs no
        // query embedding.
        isEmbeddingTool: true,
        runContract: async (ctx) => {
            const res = await ctx.api.callMcpTool(
                'search_knowledgebase',
                { list_products: true },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'search_knowledgebase', [
                'Available Products in Knowledgebase',
                'Total:',
                'chunks',
            ]);
        },
        buildChatPrompt: async () =>
            'Search the knowledgebase and list the available products.',
        chatSignature: /product|knowledgebase|chunk/i,
    },

    // -- Memory tools (offered only when a memory store is configured) ------
    store_memory: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const marker =
                `e2e-store-${Date.now()}-` +
                Math.random().toString(36).slice(2, 8);
            const res = await ctx.api.callMcpTool(
                'store_memory',
                { content: marker, category: 'e2e-contract', scope: 'user' },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'store_memory', [
                'Memory Stored Successfully',
                'ID:',
                'Category: e2e-contract',
            ]);
        },
        buildChatPrompt: async () => {
            const marker =
                `e2e-remember-${Date.now()}-` +
                Math.random().toString(36).slice(2, 8);
            return (
                `Please remember this for later: "${marker}". Store it as a ` +
                'memory in the "e2e-chat" category.'
            );
        },
        chatSignature: /stored|memory|remember/i,
    },

    recall_memories: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const seeded = await seedMemory(ctx, 'e2e-recall');
            const res = await ctx.api.callMcpTool(
                'recall_memories',
                { query: seeded.content, limit: 5 },
                { cookie: ctx.adminCookie },
            );
            expectSuccessContains(res, 'recall_memories', [
                'Memory Recall Results',
                'Total Results:',
                seeded.content,
            ]);
        },
        buildChatPrompt: async (ctx) => {
            const seeded = await seedMemory(ctx, 'e2e-recall-chat');
            return `Recall any memories related to "${seeded.content}".`;
        },
        chatSignature: /recall|memor/i,
    },

    delete_memory: {
        isEmbeddingTool: false,
        runContract: async (ctx) => {
            const seeded = await seedMemory(ctx, 'e2e-delete');
            const res = await ctx.api.callMcpTool(
                'delete_memory',
                { id: seeded.id },
                { cookie: ctx.adminCookie },
            );
            expect(res.isError, 'delete_memory: must not error').toBe(false);
            expect(
                res.text.trim(),
                'delete_memory: must confirm deletion of the exact id',
            ).toBe(`Memory with ID ${seeded.id} has been deleted`);
        },
        buildChatPrompt: async (ctx) => {
            const seeded = await seedMemory(ctx, 'e2e-delete-chat');
            return `Delete the memory with ID ${seeded.id}.`;
        },
        chatSignature: /delet/i,
    },
};

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

test.describe('Ask Ellie — MCP tool coverage', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const replication = new ReplicationHelper();
    const installMode = loadE2EConfig().installMode;
    const runDate = new Date().toISOString().slice(0, 10);

    let adminCookie: string;
    let connId: number;
    let primed = false;
    const listedTools = new Set<string>();

    test.beforeAll(async () => {
        test.setTimeout(240_000);
        await replication.start();
        adminCookie = await auth.loginAsAdmin();

        // Locked decision: this suite requires AI enabled — never skip.
        const caps = await api.getCapabilities(adminCookie);
        expect(
            caps.ai_enabled,
            'AI must be enabled (E2E_AI_ENABLED=true + LLM key) for the ' +
            'Ask Ellie tool suite',
        ).toBe(true);

        // Authoritative, RBAC-filtered tool set for the superuser: exactly
        // the tools this deployment offers.
        const tools = await api.listMcpTools({ cookie: adminCookie });
        for (const tool of tools) {
            listedTools.add(tool.name);
        }
        expect(
            listedTools.size,
            'the server must offer at least one MCP tool to the superuser',
        ).toBeGreaterThan(0);
    });

    test.beforeEach(async ({ page }) => {
        test.setTimeout(360_000);
        await label('package', 'Ask Ellie Tools');
        await ensureClusterAndPrimary(page, api, adminCookie);

        const connections = await api.listConnections({ cookie: adminCookie });
        const primary = connections.find((c) => /primary/i.test(c.name));
        expect(primary, 'a connection matching /primary/i must exist')
            .toBeTruthy();
        connId = primary?.id ?? -1;

        // Prime pg_sys_* samples once so list_probes/describe_probe/
        // query_metrics have real data for the primary connection.
        if (!primed) {
            await primeSystemMetrics(api, adminCookie, connId);
            primed = true;
        }
    });

    test.afterAll(async () => {
        await replication.stop().catch(() => undefined);
        // NOTE: a runtime "every offered+specced tool was exercised" guard was
        // deliberately removed. Under CI `retries` the module-level state is
        // reset per retry, so a retried run would see a partial set and fail
        // spuriously (mis-attributed across tests). The STATIC coverage-guard
        // test below (every offered tool has a TOOL_SPECS entry) is the
        // deterministic, retry-safe completeness check.
    });

    // -----------------------------------------------------------------------
    // Coverage guard
    // -----------------------------------------------------------------------

    test('coverage guard: every offered tool has a TOOL_SPECS entry', async () => {
        const uncovered = [...listedTools].filter(
            (name) => TOOL_SPECS[name] === undefined,
        );
        expect(
            uncovered,
            'server tools with no TOOL_SPECS entry (add coverage for these): ' +
            uncovered.join(', '),
        ).toEqual([]);

        // RPM installs ship a knowledgebase DB, so search_knowledgebase MUST
        // be offered there. Docker installs ship none, so it is naturally
        // absent — that is the intended Docker skip, not a silent one.
        if (installMode === 'rpm') {
            expect(
                listedTools.has('search_knowledgebase'),
                'RPM deployments must offer search_knowledgebase',
            ).toBe(true);
        }
    });

    // -----------------------------------------------------------------------
    // Per-tool hybrid tests
    // -----------------------------------------------------------------------

    for (const [name, spec] of Object.entries(TOOL_SPECS)) {
        test(`${name}: contract + Ask Ellie`, async ({ page }) => {
            test.skip(
                !listedTools.has(name),
                `${name} is not offered by this deployment`,
            );

            const ctx: ToolCtx = {
                api,
                adminCookie,
                connId,
                metricsProbe: METRICS_PROBE,
                database: DATABASE,
                table: TABLE,
            };

            // Optional per-tool setup (e.g. create a temporary blackout). Its
            // teardown runs in the outer finally so it ALWAYS executes, even
            // when a contract or chat assertion below fails.
            const state: ToolState = spec.setup ? await spec.setup(ctx) : {};

            try {
                // 1. Deterministic contract check (authoritative).
                await test.step('deterministic contract check', async () => {
                    await spec.runContract(ctx, state);
                });

                // 2 + 3. Ask Ellie chat flow + screenshot on pass AND fail.
                const monitoring = new MonitoringPage(page);
                const serverNav = new ServerNavigatorPage(page);
                await selectPrimaryServer(page, monitoring, serverNav);

                const chat = new ChatPage(page);
                await chat.openChat();

                const prompt = await spec.buildChatPrompt(ctx, state);
                await chat.sendMessage(prompt);
                // Provider-limit-aware wait: a stuck response caused purely by
                // an AI provider usage/rate limit (e.g. store_memory when the
                // account has hit its quota) skips this test rather than
                // failing on the never-re-enabled input; a genuine hang or a
                // wrong-but-complete response still fails the assertions below.
                const transcript = await getChatResponseOrSkip(chat, 75_000);

                const display = getToolDisplayName(name);
                const provider = spec.isEmbeddingTool
                    ? LLM_CONFIG.embedding.provider
                    : LLM_CONFIG.reasoning.provider;

                try {
                    if (spec.chatAssert) {
                        // Tool-specific expected-behaviour override (e.g. the
                        // tool is correctly NOT called for this input).
                        spec.chatAssert(transcript, ctx, state);
                    } else {
                        expect(
                            transcript,
                            `${name}: the "${display}" tool-activity chip must ` +
                            'appear in the chat transcript (correct tool ran)',
                        ).toContain(display);
                        expect(
                            transcript,
                            `${name}: the reply must carry a ${name}-specific ` +
                            'signature',
                        ).toMatch(spec.chatSignature);
                    }
                } finally {
                    await captureToolResponse(page, {
                        tool: name,
                        provider,
                        date: runDate,
                    });
                }
            } finally {
                // Teardown ALWAYS runs — even if an assertion above failed —
                // so temporary fixture data never leaks into later serial
                // tests. It does not swallow the caller's assertion failure.
                if (spec.teardown) {
                    await spec.teardown(ctx, state);
                }
            }
        });
    }

    // -----------------------------------------------------------------------
    // Screenshot framing verification (required-area-visible + no-truncation)
    // -----------------------------------------------------------------------

    test('screenshot capture: panel is framed and long content is not truncated', async ({
        page,
    }) => {
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        await selectPrimaryServer(page, monitoring, serverNav);

        const chat = new ChatPage(page);
        await chat.openChat();

        // Build a transcript taller than the viewport with table-heavy,
        // deterministic tool responses — no reliance on any single tool's
        // exact output length. Stop as soon as the panel content clearly
        // exceeds the viewport; capped so a terse model cannot loop forever.
        const prompts = [
            'List all alert rules with their thresholds.',
            'List all monitored connections.',
            'Show the schema of the documents table.',
            'List all alert rules again, grouped by category.',
        ];
        let contentHeight = 0;
        let panelViewportHeight = 0;
        for (const prompt of prompts) {
            await chat.sendMessage(prompt);
            await chat.waitForResponse(75_000);
            const measured = await page.evaluate(() => {
                const panel = document.querySelector(
                    '[aria-label="AI Chat Panel"]',
                );
                let max = 0;
                panel?.querySelectorAll('*').forEach((el) => {
                    if (el instanceof HTMLElement) {
                        max = Math.max(max, el.scrollHeight);
                    }
                });
                return { content: max, viewport: window.innerHeight };
            });
            contentHeight = measured.content;
            panelViewportHeight = measured.viewport;
            if (contentHeight > panelViewportHeight * 1.3) {
                break;
            }
        }

        expect(
            contentHeight,
            'the transcript must exceed the viewport so the long-content ' +
            'case is genuinely exercised',
        ).toBeGreaterThan(panelViewportHeight);

        // Capture the focused, fully-expanded panel and measure the produced
        // PNG against the panel geometry — no hardcoded pixel sizes.
        const filePath = path.join(SCREENSHOT_DIR, `_verify-panel_${runDate}.png`);
        const geometry = await captureChatPanel(page, filePath);
        expect(
            geometry,
            'captureChatPanel must return panel geometry (panel present)',
        ).not.toBeNull();
        if (geometry === null) {
            throw new Error('panel geometry unexpectedly null');
        }

        const png = readPngSize(fs.readFileSync(filePath));
        const tol = Math.ceil(4 * geometry.dpr);
        const expectedWidthPx = Math.round(geometry.width * geometry.dpr);
        const expectedHeightPx = Math.round(geometry.height * geometry.dpr);

        // The intended framing captures the TRANSCRIPT (message list) rather
        // than the whole panel, so the shot excludes the header and the input
        // composer and is sized to the messages alone — consistent for short
        // and tall responses. Prove that path ran (not the panel-root
        // fallback), so the phantom-empty-space / input-composer inconsistency
        // cannot regress.
        expect(
            geometry.capturedTranscript,
            'captureChatPanel must capture the transcript element (header and ' +
            'input composer excluded), not the panel-root fallback',
        ).toBe(true);

        // (a) FULL RESPONSE WIDTH CAPTURED, FOCUSED ON THE TRANSCRIPT — the
        //     shot spans exactly the measured (widened) transcript box, so wide
        //     tool output is not truncated horizontally, and the frame is the
        //     message-list element's own box rather than the full page.
        expect(
            Math.abs(png.width - expectedWidthPx),
            `screenshot width (${png.width}px) must match the expanded ` +
            `transcript width (${expectedWidthPx}px) — the full response ` +
            'width, no horizontal truncation',
        ).toBeLessThanOrEqual(tol);
        // The narrow live sidebar (≤800px) is deliberately widened to a
        // comfortable reading width for capture; the transcript fills that
        // width less the panel's 1px left border and its own padding (a few px),
        // and a wider response only grows it further. Assert it is essentially
        // the full CAPTURE_WIDTH (small chrome tolerance), which is far above
        // the ≤800px sidebar — proving the widening ran (no thin strip, wide
        // output has room) and the frame is the widened transcript, not the
        // un-expanded sidebar.
        expect(
            geometry.width,
            `the captured transcript must be widened to about ${CAPTURE_WIDTH}px ` +
            '(short replies are not thin strips; wide output has room)',
        ).toBeGreaterThanOrEqual(CAPTURE_WIDTH - 16);

        // (b) LONG CONTENT NOT TRUNCATED — the shot is exactly as tall as the
        //     full transcript content box, which we already proved exceeds the
        //     viewport. `>=` catches truncation (the old tall-response cut);
        //     `<=` catches an empty tail or capturing an unrelated larger
        //     portion of the page (requirement: exactly the messages' area).
        expect(
            png.height,
            `screenshot height (${png.height}px) must cover the full ` +
            `transcript content (${expectedHeightPx}px) — nothing truncated`,
        ).toBeGreaterThanOrEqual(expectedHeightPx - tol);
        expect(
            png.height,
            `screenshot height (${png.height}px) must not exceed the ` +
            `transcript content box (${expectedHeightPx}px) — no empty tail, ` +
            'focused, not the whole page',
        ).toBeLessThanOrEqual(expectedHeightPx + tol);
        expect(
            geometry.height,
            'the captured transcript must be taller than the viewport (the ' +
            'long-response path was exercised, and ancestor clipping at the ' +
            'viewport boundary was neutralized)',
        ).toBeGreaterThan(geometry.viewportHeight);
    });

    // -----------------------------------------------------------------------
    // Navigation helper (mirrors cluster-monitoring-ai.spec.ts)
    // -----------------------------------------------------------------------

    async function selectPrimaryServer(
        page: Page,
        monitoring: MonitoringPage,
        serverNav: ServerNavigatorPage,
    ): Promise<void> {
        await page.goto('/');
        await monitoring.waitForAppLoad();
        await expect(async () => {
            const row = serverNav.getServerRow(PRIMARY_SERVER.name).first();
            if (!(await row.isVisible().catch(() => false))) {
                await serverNav.expandCluster(CLUSTER_NAME);
            }
            await row.click({ force: true });
            await expect(
                monitoring.getSectionHeader('Monitoring'),
                'Monitoring section should render after selecting the primary',
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 60_000, intervals: [3_000] });
    }
});
