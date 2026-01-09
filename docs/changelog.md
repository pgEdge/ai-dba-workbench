# Changelog

All notable changes to the pgEdge AI DBA Workbench will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Connection management REST APIs for selecting database connections
  (`/api/connections`, `/api/connections/current`)
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

### Changed

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

- URL encoding for passwords with special characters in connection strings
- Proper error propagation from database connection failures to clients
- PostgreSQL numeric types now display correctly in TSV output (previously
  showed internal struct representation)
- LLM now receives notification when database connection changes mid-session,
  preventing stale context from previous connections
- Comprehensive handling of PostgreSQL pgtype wrappers in query results
  (Float8, Float4, Int8, Int4, Int2, Text, Bool, Timestamp, Timestamptz, Date,
  Interval, UUID)
