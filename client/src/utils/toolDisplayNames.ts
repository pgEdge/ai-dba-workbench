/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Human-readable display names for MCP tools
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
};

/**
 * Return a human-readable label for the given MCP tool name.
 * Falls back to the raw tool name when no mapping exists.
 */
export function getToolDisplayName(toolName: string): string {
    return TOOL_DISPLAY_NAMES[toolName] || toolName;
}
