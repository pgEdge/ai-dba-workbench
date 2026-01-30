# Changelog

All notable changes to the pgEdge AI DBA Workbench will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Grouped alert display on the dashboard:
  - Alerts of the same type are grouped into single panels
  - Each group shows individual instances by server, database, and table
  - Table names are now captured for table-specific alerts (bloat ratio,
    dead tuple ratio, autovacuum status)
  - Consistent capitalization of alert titles
- RFC 8631 compliant REST API with versioned endpoints (`/api/v1/`):
  - All JSON responses include Link header for API discovery
  - OpenAPI 3.0.3 specification at `/api/v1/openapi.json`
  - Interactive API browser in documentation using ReDoc
- Alert Analysis feature with LLM-powered remediation recommendations:
  - New MCP tools for alert context: `get_alert_history`, `get_alert_rules`,
    `get_metric_baselines`
  - AlertAnalysisDialog component with professional analytics design
  - Analyze button on each alert in the StatusPanel
  - Markdown report generation with download option
- Shared embedding package in `pkg/embedding` for reusable components.
- Documentation for the embedding package with provider details and usage
  examples.
- HTTP security headers middleware for enhanced server protection.
- Alerter engine for monitoring metrics with threshold and anomaly detection.
- Comprehensive test coverage for the alerter engine.
- Connection management REST APIs for selecting database connections
  (`/api/v1/connections`, `/api/v1/connections/current`)
- CLI slash commands for connection management (`/list connections`,
  `/set connection`, `/show connection`)
- Session-based connection selection persisted in SQLite auth database
- Documentation for connection management in `docs/server/connections.md`
- New `pg://connection_info` resource that returns the currently selected
  database connection details without querying the database
- Unified CI workflows for collector, server, CLI, and documentation
- Datastore metrics tools for querying collected metrics:
  - `list_probes`: List available metrics probes in the datastore
  - `describe_probe`: Get column details for a specific metrics probe
  - `query_metrics`: Query historical metrics with time-based aggregation
- Enhanced LLM system prompts with PostgreSQL DBA expertise and two-tier
  database architecture guidance
- Documentation for metrics tools in `docs/server/metrics.md`
- Alerter configuration reference documentation with all options, environment
  variables, and command-line flags in `docs/alerter/configuration.md`
- Cron expression documentation for blackout schedule configuration in
  `docs/alerter/cron-expressions.md`
- Standardized indexes on all collector metrics tables for improved query
  performance:
  - `(connection_id, collected_at DESC)` on every metrics table
  - `(connection_id, database_name, collected_at DESC)` on database-scoped
    tables
  - Object-specific indexes for tables with additional key columns
- New consolidated `pg_stat_connection_security` probe combining SSL and GSSAPI
  connection security metrics into a single collection

### Changed

- Probe consolidation reduces database round-trips by ~20%:
  - `pg_stat_replication_slots` merged into `pg_replication_slots`
  - `pg_stat_subscription_stats` merged into `pg_stat_subscription`
  - `pg_stat_bgwriter` merged into `pg_stat_checkpointer`
  - `pg_stat_archiver` merged into `pg_stat_wal`
  - `pg_stat_wal_receiver` merged into `pg_stat_replication`
  - `pg_statio_all_tables` merged into `pg_stat_all_tables`
  - `pg_statio_all_indexes` merged into `pg_stat_all_indexes`
  - `pg_stat_slru` merged into `pg_stat_io`
  - `pg_stat_ssl` and `pg_stat_gssapi` merged into new
    `pg_stat_connection_security`

- Collector schema migrations consolidated into single migration for simpler
  deployment and reduced complexity. **Breaking change**: Existing collector
  databases must be dropped and recreated.
- All timestamp columns in the collector database now use TIMESTAMPTZ
  (timestamp with timezone) for unambiguous time representation. **Breaking
  change**: Existing collector databases must be dropped and recreated.
- Server `main.go` refactored for improved code organization.
- Context propagation added to MCP tools for better request handling.
- Full 5-field cron parser implementation replaces the limited parser.
- CLI commands refactored for consistency:
  - Removed `llm-` prefix: `/set provider`, `/show provider` (was `llm-provider`)
  - Removed `llm-` prefix: `/set model`, `/show model` (was `llm-model`)
  - Moved `/tools`, `/resources`, `/prompts` to `/list tools`, `/list resources`,
    `/list prompts`
  - Added `/list providers` to list available LLM providers
  - Replaced `/connect` and `/disconnect` with `/set connection <id>`
    (use `/set connection none` to disconnect)
  - Reorganized help output into logical groups
- Resources (like `pg://system_info`) now use the selected monitored
  database connection instead of the datastore
- Error messages from database connections now show the root cause error
  for clearer diagnostics
- Server now requires both secret file and database configuration at startup

### Fixed

- SQL identifier validation prevents injection attacks via table and column
  names.
- X-Forwarded-For IP spoofing vulnerability addressed with trusted proxy
  configuration.
- Shared `pkg/crypto` package provides consistent password encryption across
  collector and server using random salts instead of username-based salts.
- Alerter standard deviation calculation corrected with proper `math.Sqrt`
  usage.
- URL encoding for passwords with special characters in connection strings
- Proper error propagation from database connection failures to clients
- PostgreSQL numeric types now display correctly in TSV output (previously
  showed internal struct representation)
- LLM now receives notification when database connection changes mid-session,
  preventing stale context from previous connections
- Comprehensive handling of PostgreSQL pgtype wrappers in query results
  (Float8, Float4, Int8, Int4, Int2, Text, Bool, Timestamp, Timestamptz, Date,
  Interval, UUID)
- pg_database probe type mismatch for `datlocprovider` column; schema now uses
  correct `"char"` type instead of TEXT

### Breaking Changes

- **Collector schema completely redesigned.** The datastore database must be
  dropped and recreated. All historical metrics data will be lost. Changes
  include: probe consolidations (43 probes reduced to 34), standardized indexes
  on all tables, and TIMESTAMPTZ for all timestamp columns.
- REST API paths have changed from `/api/` to `/api/v1/`. Update any custom
  integrations or scripts that call the API directly. The CLI and web client
  have been updated to use the new paths.
- Password encryption now uses random salts instead of username-based salts.
  Existing encrypted passwords will no longer decrypt correctly after
  upgrading. You must re-enter passwords for all monitored connections using
  the MCP server API.
