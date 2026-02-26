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
 * Shared SQL code-block rules included in every analysis system prompt.
 * These rules ensure the LLM produces correct, executable SQL that the
 * user can run directly from the UI.
 */
export const SQL_CODE_BLOCK_RULES = `
CRITICAL rules for code blocks - the user executes SQL directly from the UI so accuracy is essential:

1. SQL code blocks (\`\`\`sql) must ONLY contain executable SQL statements and SQL comments (lines starting with --). NEVER include any of the following in SQL code blocks:
   - Configuration file snippets (e.g. shared_buffers = 8GB, work_mem = 16MB)
   - File paths or filenames
   - Shell commands
   - Explanatory prose or notes
   Use \`\`\`conf for postgresql.conf snippets, \`\`\`bash for shell commands, and \`\`\`text for other content.

2. Place each SQL query in its own separate \`\`\`sql code block. NEVER combine multiple queries in one block.

3. Every SQL query MUST be correct and executable. The user will run these directly. Incorrect SQL wastes their time and erodes trust. You MUST verify all column names against the actual PostgreSQL system catalog. The correct column names are:
   - pg_stat_user_tables: schemaname, relname, seq_scan, seq_tup_read, idx_scan, idx_tup_fetch, n_tup_ins, n_tup_upd, n_tup_del, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum, last_analyze, last_autoanalyze, vacuum_count, autovacuum_count, analyze_count, autoanalyze_count
   - pg_statio_user_tables: schemaname, relname, heap_blks_read, heap_blks_hit, idx_blks_read, idx_blks_hit, toast_blks_read, toast_blks_hit, tidx_blks_read, tidx_blks_hit
   - pg_stat_activity: datid, datname, pid, leader_pid, usesysid, usename, application_name, client_addr, client_hostname, client_port, backend_start, xact_start, query_start, state_change, wait_event_type, wait_event, state, backend_xid, backend_xmin, query, backend_type
   - pg_stat_statements: userid, dbid, queryid, query, calls, total_exec_time, mean_exec_time, rows, shared_blks_hit, shared_blks_read, shared_blks_written, temp_blks_read, temp_blks_written
   - pg_stat_bgwriter: checkpoints_timed, checkpoints_req, buffers_checkpoint, buffers_clean, maxwritten_clean, buffers_backend, buffers_alloc
   - pg_class: oid, relname, relnamespace, reltype, relowner, relam, relfilenode, reltablespace, relpages, reltuples, relallvisible, reltoastrelid, relhasindex, relisshared, relpersistence, relkind, relnatts, relchecks, relhasrules, relhastriggers, relhassubclass
   - pg_stat_database: datid, datname, numbackends, xact_commit, xact_rollback, blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted, tup_updated, tup_deleted, conflicts, temp_files, temp_bytes, deadlocks
   NEVER use "tablename" - the column is always "relname" in PostgreSQL catalogs. When in doubt, keep queries simple and use only columns you are certain exist.

4. Ensure all SQL syntax, function names, and catalog column names are valid for the specific PostgreSQL version in use (provided in the server context below). Do not use features, functions, or columns introduced in newer versions. For example, pg_stat_statements column names changed between PostgreSQL 12 and 13.

5. When suggesting ALTER SYSTEM or other DDL statements, place them in separate code blocks from diagnostic SELECT queries.

QUERY VALIDATION:
If a test_query tool is available, you MUST validate every SQL query you generate by calling test_query with the appropriate connection_id before including it in your report. If validation fails, fix the query and re-validate. If you cannot validate a query (e.g., no connection available), clearly mark it as an unvalidated example by adding a SQL comment "-- NOTE: This query has not been validated against the target database" as the first line.`;

/**
 * Additional SQL code-block rules for server analysis prompts that
 * include schema-level recommendations (placeholder and index rules).
 */
export const SQL_PLACEHOLDER_RULES = `

6. NEVER use placeholder names like \`schema_name\`, \`table_name\`, \`your_table\`, \`my_table\`, \`your_database\`, or similar invented identifiers in SQL code blocks. Users execute SQL directly from the UI, and placeholders cause runtime errors. Instead:
   - If the server context or tool results provide specific object names, use those exact names in the SQL.
   - If remediation requires acting on specific database objects that are not yet known, first provide a diagnostic query that identifies the affected objects (e.g., tables with high dead tuple ratios), then provide the remediation SQL using the actual names returned by that diagnostic query.
   - If the specific objects cannot be determined, provide ONLY the diagnostic query and explain that the user should run the remediation command on the objects it identifies. Do NOT generate non-executable SQL containing placeholders.

7. NEVER suggest dropping indexes that implement PRIMARY KEY or UNIQUE constraints, even if they show zero scans in pg_stat_user_indexes. These indexes enforce data integrity constraints and cannot be removed without dropping the constraint itself. Low scan counts on constraint indexes are normal and expected; they serve a correctness purpose, not a performance purpose.`;
