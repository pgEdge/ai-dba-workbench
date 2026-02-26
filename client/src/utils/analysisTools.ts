/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { AnalysisTool } from './mcpTools';

/**
 * Tool definitions shared across analysis hooks. Each constant is a
 * single MCP tool descriptor that can be composed into the tools array
 * for any analysis prompt.
 */

export const TOOL_GET_ALERT_HISTORY: AnalysisTool = {
    name: "get_alert_history",
    description: "Get historical alerts for the same rule or metric on a connection",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID to query" },
            rule_id: { type: "integer", description: "Filter by alert rule ID" },
            metric_name: { type: "string", description: "Filter by metric name" },
            time_start: { type: "string", description: "Start of time range (e.g., '7d', '24h')" },
            limit: { type: "integer", description: "Max results (default 50)" },
        },
        required: ["connection_id"],
    },
};

export const TOOL_GET_ALERT_RULES: AnalysisTool = {
    name: "get_alert_rules",
    description: "Get current alerting rules and thresholds configuration",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID for specific thresholds" },
            category: { type: "string", description: "Filter by category" },
            enabled_only: { type: "boolean", description: "Only enabled rules" },
        },
        required: [],
    },
};

export const TOOL_GET_METRIC_BASELINES: AnalysisTool = {
    name: "get_metric_baselines",
    description: "Get statistical baselines for metrics (mean, stddev, min, max)",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID to query" },
            metric_name: { type: "string", description: "Filter to specific metric" },
        },
        required: ["connection_id"],
    },
};

export const TOOL_QUERY_METRICS: AnalysisTool = {
    name: "query_metrics",
    description: "Query historical metric values with time-based aggregation",
    inputSchema: {
        type: "object",
        properties: {
            probe_name: { type: "string", description: "Name of the probe to query" },
            connection_id: { type: "integer", description: "Connection ID" },
            time_start: { type: "string", description: "Start of time range" },
            metrics: { type: "string", description: "Comma-separated metric columns" },
            buckets: { type: "integer", description: "Number of time buckets" },
        },
        required: ["probe_name"],
    },
};

export const TOOL_QUERY_DATABASE: AnalysisTool = {
    name: "query_database",
    description: "Execute a read-only SQL query against a PostgreSQL connection",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID to query" },
            query: { type: "string", description: "SQL query to execute (read-only)" },
            database_name: { type: "string", description: "Optional database name to connect to" },
        },
        required: ["connection_id", "query"],
    },
};

export const TOOL_GET_SCHEMA_INFO: AnalysisTool = {
    name: "get_schema_info",
    description: "Get schema information for a database including tables, indexes, and constraints",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID to query" },
            database_name: { type: "string", description: "Database name" },
            schema_name: { type: "string", description: "Schema name (default: public)" },
        },
        required: ["connection_id"],
    },
};

export const TOOL_LIST_PROBES: AnalysisTool = {
    name: "list_probes",
    description: "List available monitoring probes and their descriptions",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID (optional, for connection-specific probes)" },
        },
        required: [],
    },
};

export const TOOL_DESCRIBE_PROBE: AnalysisTool = {
    name: "describe_probe",
    description: "Get detailed information about a specific monitoring probe including its metrics",
    inputSchema: {
        type: "object",
        properties: {
            probe_name: { type: "string", description: "Name of the probe to describe" },
        },
        required: ["probe_name"],
    },
};

export const TOOL_TEST_QUERY: AnalysisTool = {
    name: "test_query",
    description: "Validate SQL query correctness without executing it. Uses EXPLAIN in a read-only transaction.",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID to validate against" },
            query: { type: "string", description: "SQL query to validate" },
            database_name: { type: "string", description: "Optional database name" },
        },
        required: ["connection_id", "query"],
    },
};

export const TOOL_GET_BLACKOUTS: AnalysisTool = {
    name: "get_blackouts",
    description: "Get active and recent blackout (maintenance window) periods for a connection",
    inputSchema: {
        type: "object",
        properties: {
            connection_id: { type: "integer", description: "Connection ID to query" },
            active_only: { type: "boolean", description: "Only return currently active blackouts" },
            include_schedules: { type: "boolean", description: "Also return recurring blackout schedules" },
        },
        required: [],
    },
};

/** Tools used by the server analysis hook. */
export const SERVER_ANALYSIS_TOOLS: AnalysisTool[] = [
    TOOL_QUERY_METRICS,
    TOOL_GET_METRIC_BASELINES,
    TOOL_GET_ALERT_HISTORY,
    TOOL_GET_ALERT_RULES,
    TOOL_QUERY_DATABASE,
    TOOL_GET_SCHEMA_INFO,
    TOOL_LIST_PROBES,
    TOOL_DESCRIBE_PROBE,
    TOOL_GET_BLACKOUTS,
    TOOL_TEST_QUERY,
];

/** Tools used by the alert analysis hook. */
export const ALERT_ANALYSIS_TOOLS: AnalysisTool[] = [
    TOOL_GET_ALERT_HISTORY,
    TOOL_GET_ALERT_RULES,
    TOOL_GET_METRIC_BASELINES,
    TOOL_QUERY_METRICS,
    TOOL_GET_BLACKOUTS,
    TOOL_TEST_QUERY,
];
