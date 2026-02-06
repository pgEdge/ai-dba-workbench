# Changelog

All notable changes to the pgEdge AI DBA Workbench will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Configurable probe settings via the REST API and admin panel;
  administrators can adjust frequency, retention, and enabled
  state for each probe.
- Configurable alert rule defaults via the REST API and admin
  panel; administrators can set threshold, operator, severity,
  and enabled state for each rule.
- Per-connection alert threshold overrides that allow fine-tuned
  alerting for individual monitored database connections.
- New RBAC permissions `manage_probes` and `manage_alert_rules`
  for controlling access to probe and alert configuration.
- Probes and Alert Rules tabs in the administration panel for
  managing probe settings and alert rule defaults.
- Auth database migration v10 that adds the `manage_probes` and
  `manage_alert_rules` permissions to the role system.
- Blackout management for suppressing alerts during maintenance:
  - Management UI supports estate, group, cluster, and server scopes
  - REST API endpoints for CRUD operations on blackouts and schedules
  - Hierarchical cascading from estate to group to cluster to server
  - Cron-based recurring blackout schedules for regular maintenance
  - RBAC integration with the `manage_blackouts` permission
  - ClusterNavigator displays blackout status indicators
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

- Migrated the React client from JavaScript to TypeScript for
  improved type safety:
  - Converted all ~50 source, context, hook, and test files with
    proper type interfaces
  - Removed the `prop-types` dependency
- Service account support for non-interactive users that authenticate
  only via API tokens; service accounts cannot log in with a password.
- CLI flag `-add-service-account` for creating service accounts from
  the command line.
- CLI flag `-user` for the `-add-token` command to specify the token
  owner.
- Token scope system with three restriction types: connection
  access (with per-connection read or read/write levels), MCP
  privilege restrictions, and admin permission restrictions.
- Wildcard scope options for tokens: "All Connections",
  "All MCP Privileges", and "All Admin Permissions" allow
  broad scope without listing individual items.
- Per-connection access levels in token scopes; tokens can
  restrict access to read-only even when the owner has
  read/write access.
- Admin panel token management with create, edit scope, and
  delete operations; includes owner-based scope filtering.
- API usage examples section in the admin panel token tab
  showing sample `curl` commands for common operations.
- Expandable permission panels on all three admin panel tabs
  (Users, Groups, Tokens) for consistent privilege display.
- Notification channel management through the admin panel with
  support for Email, Slack, Mattermost, and Webhook channels.
- Webhook channels with configurable HTTP methods, custom headers,
  authentication (Basic, Bearer, API Key), and customizable JSON
  payload templates using Go template syntax.
- Test notification button for all channel types to verify
  configuration before use.
- Email channel recipient management with per-recipient enable
  and display name.
- REST API endpoints for notification channel CRUD, testing, and
  email recipient management.
- Hierarchical alert threshold overrides at group, cluster, and
  server levels; the alerter resolves thresholds using server
  first, then cluster, then group, then the global default.
- Hierarchical probe configuration overrides at group, cluster,
  and server levels; probe settings at lower levels take
  precedence over higher levels.
- Estate default flag for notification channels; channels marked
  as estate defaults are active for all servers unless
  overridden.
- Hierarchical notification channel overrides at group, cluster,
  and server levels; administrators can enable or disable
  individual channels at each level of the hierarchy.
- Notification Channels tab in server, cluster, and group edit
  dialogs for managing channel override settings.
- Alert Overrides and Probe Configuration tabs in server,
  cluster, and group edit dialogs for managing per-scope
  overrides.
- REST API endpoints for channel overrides
  (`/api/v1/channel-overrides/{scope}/{scopeId}`).
- Collector schema migration v10 that adds the estate default
  column and notification channel overrides table.

### Changed

- Unified the token model; all tokens now have a mandatory owner and
  inherit superuser status from the owning user instead of storing it
  on the token.
- Auth database migration v11 that consolidates service tokens and user
  tokens into a single tokens table, adds the `is_service_account`
  column to the users table, and removes the `token_type` and
  `is_superuser` columns from tokens.
- Replaced the `-superuser` flag on `-add-token` with user-level
  superuser status; tokens created for superuser accounts automatically
  inherit superuser privileges.
- Updated the admin panel token scopes view to display owner username
  with service account and superuser badges.
- Updated the admin panel users view to display account type
  and support creating service accounts.
- Admin panel permissions display unified across Users,
  Groups, and Tokens tabs using expandable rows with the
  shared EffectivePermissionsPanel component.
- Auth database migrations v12 and v13 add token admin
  permission scope and per-connection access levels
  respectively.

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
  - Removed `llm-` prefix: `/set provider`, `/show provider`
    (was `llm-provider`)
  - Removed `llm-` prefix: `/set model`, `/show model` (was `llm-model`)
  - Moved `/tools`, `/resources`, `/prompts` to `/list tools`,
    `/list resources`, `/list prompts`
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
- Scheduler now starts a new goroutine when probe interval changes; previously
  interval changes via the `probe_configs` table left probes orphaned with no
  active scheduler until collector restart
- pg_database probe type mismatch for `datlocprovider` column; schema now uses
  correct `"char"` type instead of TEXT

### Breaking Changes

- Collector schema completely redesigned. The datastore database must be
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
