# Changelog

All notable changes to the pgEdge AI DBA Workbench will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Chat memory feature for Ask Ellie; Ellie can store,
  recall, and delete persistent memories across
  conversations using three new tools: `store_memory`,
  `recall_memories`, and `delete_memory`. Memories
  support categories, user and system scopes, and
  pinned memories that are automatically included in
  every conversation.
- RBAC permission `store_system_memory` controls which
  users can create system-scoped memories visible to
  all users; administrators assign this permission
  through the admin panel.
- Memories management panel in the settings UI; users
  can view, delete, and toggle the pinned status of
  their memories and system-scoped memories.
- Automatic user context injection; Ellie receives the
  current user's profile, group memberships, and
  effective permissions at the start of each
  conversation.
- REST API endpoints for memory management:
  `GET /api/v1/memories` lists visible memories,
  `DELETE /api/v1/memories/{id}` removes a memory, and
  `PATCH /api/v1/memories/{id}` toggles the pinned
  status.
- The `test_query` MCP tool validates SQL query correctness
  without executing the query; the LLM uses this tool to
  verify all generated SQL before presenting results.
- Optional `connection_id` and `database_name` parameters
  on all monitored-database tools (`query_database`,
  `get_schema_info`, `similarity_search`, `execute_explain`,
  `count_rows`, `test_query`); these parameters allow the
  LLM to target a specific database without changing the
  active connection.
- Query validation in alert analysis and server analysis
  reports; the LLM validates all SQL queries before
  including them in generated reports.
- Visual indicator on alert panels showing whether each
  alert was triggered by a threshold rule or by anomaly
  detection.
- Anomaly detection alerts now trigger correctly when
  metrics deviate from established baselines.
- Comprehensive authentication and RBAC test coverage
  for the server component.
- Support for custom base URLs on all LLM providers;
  the OpenAI provider API key is optional when using a
  custom base URL, enabling local OpenAI-compatible
  inference servers such as Docker Model Runner,
  llama.cpp, LM Studio, and EXO.
- Google Gemini as an LLM provider option for the server
  chat proxy and the alerter reasoning engine.
- Hierarchical monitoring dashboards with estate, cluster,
  server, database, and object levels; users drill down
  through the cluster navigator or dashboard elements.
- Cluster topology visualization showing servers as nodes
  with color-coded replication edges for physical, Spock,
  and logical replication types.
- Replication lag monitoring with KPI tiles and time-series
  charts tracking lag across cluster relationships.
- Comparative metrics section on the cluster dashboard for
  identifying performance disparities between members.
- Event timeline repositioned above performance summary
  tiles in the monitoring section.
- AI Overview panel on the status panel that displays
  LLM-generated summaries of database health and status.
- Context-aware scoped summaries that adapt to estate,
  cluster, server, and group selections in the navigator.
- Collapsible AI Overview panel with persistent collapse
  state across browser sessions.
- Automatic refresh of estate-wide summaries every 60
  seconds; scoped summaries refresh on demand.
- Stale summary indicator when the cached overview
  exceeds its five-minute expiry window.
- REST API endpoint `GET /api/v1/overview` for retrieving
  AI-generated overview summaries with optional scope
  filtering.
- Configurable probe settings via the REST API and admin panel;
  administrators can adjust frequency, retention, and enabled
  state for each probe.
- Configurable alert rule defaults via the REST API and admin
  panel; administrators can set threshold, operator, severity,
  and enabled state for each rule.
- Per-connection alert threshold overrides that allow fine-tuned
  alerting for individual monitored database connections.
- Edit alert override button on alert instances; users can edit
  threshold overrides directly from active alerts with a scope
  dropdown for server, cluster, or group targeting.
- REST API endpoint for override context
  (`GET /api/v1/alert-overrides/context/{connectionId}/{ruleId}`)
  that returns the connection hierarchy, rule defaults, and
  existing overrides at all applicable scopes.
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
- Session-based connection selection persisted in SQLite auth database
- Documentation for connection management in `docs/server/connections.md`
- New `pg://connection_info` resource that returns the currently selected
  database connection details without querying the database
- Unified CI workflows for collector, server, and documentation
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
- Status panel alerts now refresh in sync with the cluster
  navigator refresh cycle, including both manual and automatic
  refresh.
- Event timeline now refreshes in sync with the cluster
  navigator instead of using a separate 60-second polling
  interval.
- Server-Sent Events for AI Overview updates; the client
  receives instant push notifications instead of polling.
- Compact tool descriptions for chat requests that reduce
  prompt token count by approximately 54 percent; the server
  sends shorter tool summaries to the LLM when processing
  Ellie chat requests.
- New `compact_tool_descriptions` configuration option with
  three modes: `"auto"` detects localhost LLM endpoints and
  uses compact descriptions, `"true"` always uses compact
  descriptions, and `"false"` always uses verbose descriptions.
- Configurable `max_iterations` option for LLM agentic
  tool-calling loops; controls the maximum number of
  round-trips the LLM can perform during analysis reports
  and chat conversations (default: 50).
- Human-readable tool display names in the Ellie chat
  interface; tool usage indicators now show labels such as
  "Querying metrics" instead of raw tool names.
- CORS middleware with configurable `cors_origin` option
  for cross-origin deployments where the client and
  server run on different origins.
- Critical alert for disconnected standby servers; the
  alert fires when a standby is in recovery mode but has
  no active WAL receiver process.
- Critical alert for logical replication subscription
  workers that are not running; the alert covers both
  native PostgreSQL logical replication and Spock
  subscriptions.

### Removed

- The CLI sub-project (`/cli`); the web client now provides all
  interactive functionality previously available through the
  command-line interface.

### Changed

- Authentication is now always required; the no-auth mode
  has been removed to enforce security best practices.
- Metrics charts now use `generate_series` for full time-series
  coverage; gaps in collected data no longer cause missing
  chart segments.
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
- Server invalidates sessions on logout to prevent reuse
  of expired session tokens.
- HTML email templates use `html/template` instead of
  `text/template` for automatic XSS prevention.
- Error messages no longer expose internal Go error
  details to users; the server returns generic messages.
- TLS certificate chain files are now properly
  PEM-decoded before use.
- Default Anthropic model aligned between the server
  configuration defaults and the application code.
- Extracted helper functions to reduce code duplication
  in metric value queries across the alerter service.
- Consolidated raw `fetch()` calls in the client to use
  the centralized API client with connection health
  tracking.
- Split the `MarkdownContent` component into focused
  modules for improved maintainability.
- Created a shared `useAnalysisState` hook to reduce
  duplication across the client analysis hooks.
- Removed redundant conversation management from the
  `useChat` hook in the client.
- Eliminated `mode` prop drilling in the client;
  components now use `useTheme()` directly.
- Fixed `PaletteMode` typing with proper `localStorage`
  validation in the client theme system.
- Server `main.go` refactored for improved code organization.
- Context propagation added to MCP tools for better request handling.
- Full 5-field cron parser implementation replaces the limited parser.
- Resources (like `pg://system_info`) now use the selected monitored
  database connection instead of the datastore
- Error messages from database connections now show the root cause error
  for clearer diagnostics
- Server now requires both secret file and database configuration at startup

### Fixed

- NULL interval values in replication lag metrics are now
  treated as zero to prevent chart rendering errors.
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
- Alert override edit dialog scope dropdown now shows cluster
  and group options for auto-detected clusters by resolving
  the hierarchy through the topology system.
- Alerter now treats a NULL `database_name` in alert threshold
  overrides as a wildcard matching any database.
- Alerter now updates threshold value, operator, and severity
  on active alert records when overrides change.
- Server-level alert threshold unique index now uses COALESCE
  to handle NULL `database_name` values, preventing duplicate
  rows on upsert.
- Fixed nil pointer dereference in alert reminder processing
  when no prior alert state exists.
- Fixed race condition on engine configuration in the
  alerter service.
- Added missing `rows.Err()` checks after database row
  iterations in the alerter to detect incomplete reads.
- Fixed password escaping in alerter database connection
  strings for passwords containing special characters.
- Added "high" severity to the notification color and
  emoji mapping for alert notifications.
- Fixed error swallowing in `GetActiveConnectionAlert`
  that silently ignored database query failures.

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
