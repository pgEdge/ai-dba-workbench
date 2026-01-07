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
  `/connect`, `/disconnect`)
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

- Resources (like `pg://system_info`) now use the selected monitored
  database connection instead of the datastore
- Error messages from database connections now show the root cause error
  for clearer diagnostics
- Server now requires both secret file and database configuration at startup

### Fixed

- URL encoding for passwords with special characters in connection strings
- Proper error propagation from database connection failures to clients
