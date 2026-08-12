# Changelog

All notable changes to the pgEdge AI DBA Workbench are
documented in this file.

The format is based on
[Keep a Changelog](https://keepachangelog.com/), and this
project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add a `-group-description` CLI flag that sets a group's
  description when creating it with `-add-group`, matching the
  description support already available in the web console. (#301)

- Add a `-list-members` CLI flag to the server, which lists the
  members of the group named with `-group`, including both member
  users and nested member groups. (#303)

### Changed

- Extend the `-show-group-privileges` CLI command to also display a
  group's admin permissions, alongside the MCP and connection
  privileges it already reports. (#303)

- Drop the runtime dependency on a knowledgebase package from the
  `pgedge-ai-dba-server` package in both the Debian and RPM
  packaging. The knowledgebase is now published as co-installable
  `pgedge-ai-kb-<provider>-<model>` packages, one per embedding
  provider and model, and it remains opt-in and disabled by
  default. Operators who want the knowledgebase install the
  package matching their chosen embedding provider and model, and
  set `database_path` themselves. (#316)

- Update the knowledgebase `database_path` documentation and the
  example server configuration for the renamed `pgedge-ai-kb`
  package. The packages install one database per embedding
  provider and model under `/usr/share/pgedge/pgedge-ai-kb/`,
  named `kb-<provider>-<model>.db`, and the built-in default
  still points at the legacy `postgres-mcp-kb` path. (#293)

- Relabel the token scope options in the API token create and edit
  screens so they now read "All the owner's MCP privileges" and
  "All the owner's admin permissions" rather than "All MCP
  Privileges" and "All Admin Permissions"; the new wording clarifies
  that a token grants the owner's privileges rather than all system
  privileges. (#323)

- Ellie now gives correct, deployment-aware guidance for restarting
  the Workbench's own components (server, collector, alerter, and
  web client), and no longer suggests commands for unrelated
  third-party tooling.
  The web client sends its own copy of Ellie's system prompt on
  every request, which always takes precedence over the server's
  default; both copies now carry the same guidance, since fixing
  only the server default left the client-facing bug unchanged for
  real users. (#329)

### Fixed

- Roll every database transaction back on a non-cancelable context,
  across the server, collector, and alerter. The pgx v5 driver fails
  a rollback outright once its context is cancelled, and it then
  discards the pooled connection whilst the transaction is still
  open on the server, so a client that disconnected mid-request
  slowly leaked connections left in an aborted-transaction state.
  Two call sites already guarded against this; the remaining sites
  now follow the same rule, and a convention test enforces it for
  new transactions. (#381)

- Fix every chat request that included a tool list failing with
  `anthropic (400): tools.0.custom.input_schema: Input does not
  match the expected shape`, which broke Ask Ellie and the Server,
  Query, and Alert analysis panels for any interaction requiring a
  tool call. The client built each tool with a camelCase
  `inputSchema` field, but `POST /api/v1/llm/chat` decodes its
  request body via the vendored `pgedge-go-llm-lib` package, whose
  wire contract expects snake_case `input_schema`; the mismatched
  key silently unmarshalled to an empty schema for every tool,
  which Anthropic then rejected outright. The client now normalises
  tool definitions to the library's wire shape before sending them,
  mirroring the existing message normalisation added in the same
  migration. (#370)

- Connection-privilege chips in the Admin Groups and Users panels
  now show the connection name instead of the internal numeric
  connection id. (#309)

- Fix the metrics time-series API (`GET /api/v1/metrics/query`)
  silently returning an empty HTTP 200 response, which the web UI
  rendered as a generic "No data available" message, whenever a
  monitored server's metric sample contained a NaN or Infinity value.
  Go's JSON encoder cannot serialize non-finite floats, so the encode
  failed silently after the response status had already been sent. The
  query path now treats a non-finite sample the same way it already
  treats a missing value from an underlying LEFT JOIN gap; the code
  carries the last known good value forward when one exists and skips
  the sample when none does, so a single bad reading degrades
  gracefully instead of blanking the whole chart. A JSON-encoding
  failure on this path is now logged loudly rather than silently
  swallowed, so any future occurrence surfaces in the server logs.

- Fix blank overview tiles and a Maintenance Info panel stuck on
  "Never" in the Table and Index object-detail dashboards of the
  web client. These panels requested the single most recent row of
  stats through the `limit`, `order_by`, and `order` query
  parameters, but the metrics API ignored those parameters and
  returned time-bucketed chart data instead. The API now honors
  these parameters and returns the most recent raw stat rows as
  real column values, validating `order_by` against the table's
  known columns before use.

- Fix the end-to-end test suite intermittently failing at the "Start
  stack" step. The server logged "collector schema is at v2, server
  requires at least v4" and then timed out waiting for its health
  endpoint. The harness script that applies the collector schema
  stopped the collector as soon as it observed any recorded schema
  version. A poll that landed between two migrations killed the
  collector mid-migration, leaving the datastore below the version the
  server requires. The collector now accepts a
  `--print-latest-schema-version` flag that reports the newest known
  schema version without touching any database. The harness asks for
  that target up front and waits for the datastore to reach it. This
  change affects test infrastructure only and does not alter
  application behavior.

- Fix the Table Size and Index Size fields rendering permanently as
  "--" on the Table and Index object-detail dashboards of the web
  client. No collector probe ever gathered relation size, so the value
  genuinely never reached the datastore rather than failing at query
  time as the two prior dashboard fixes did. The `pg_stat_all_tables`
  probe now collects `table_size` via `pg_table_size`, which covers
  heap, TOAST, free space map, and visibility map storage whilst
  excluding indexes. The `pg_stat_all_indexes` probe now collects
  `index_size` via `pg_relation_size` for each index. Both values are
  stored as raw byte counts that the web client formats for display.
  Collector schema migration Version 7 adds the two new columns to the
  `metrics.pg_stat_all_tables` and `metrics.pg_stat_all_indexes`
  partitioned parent tables, and PostgreSQL propagates them to every
  existing and future partition automatically. The web client's
  existing latest-row request reads the new columns with no further
  server or client changes.

- Fix the Activity Charts on the Table and Index object-detail
  dashboards rendering permanently blank, with the messages "No tuple
  operation data available", "No scan data available", and "No dead
  tuple data available" appearing even when the monitored server had
  completely normal, healthy activity. Those charts request computed
  metrics, including per-second rates such as sequential scans, index
  scans, and inserted, updated, or deleted rows, together with a
  dead-tuple percentage. The metrics API understood only literal, raw
  stored columns and could not compute a rate or a ratio, so it
  rejected these requests with an error that the web client silently
  rendered as missing data. The metrics API now computes a per-second
  rate for any counter column a dashboard requests by name; a request
  for `seq_scan_per_sec` computes the rate of change of the underlying
  `seq_scan` counter between samples. The API also computes a
  dead-tuple ratio metric for tables, reusing the rate and ratio
  calculations already applied elsewhere in the product. (#342)

- Fix embedding storage rejecting non-OpenAI providers with a
  dimension-mismatch error. The chat memory and anomaly detection
  embedding columns were hardcoded to `vector(1536)`, OpenAI's
  dimension, which broke Gemini (3072), Voyage (1024 or 512), and
  Ollama (384, 768, or 1024). The columns are now `halfvec(4000)`,
  and the workbench zero-pads every embedding to 4000 dimensions, so
  embeddings from any supported provider store and compare correctly.
  This widening introduces a hard ceiling: an embedding model that
  produces vectors with more than 4000 dimensions is not supported,
  because 4000 is pgvector's HNSW index limit for the `halfvec` type.
  The workbench rejects such a model with a clear error rather than
  truncating the vector. (#294)

- Fix the Estate Overview dashboard's KPI tiles (XID Age, Cache Hit
  Ratio, Transactions, and Checkpoints) and Event Timeline getting
  stuck on "No data" indefinitely after a brief backend restart or
  any transient API failure. Recovering required the user to click the
  sidebar's Refresh button or reload the page, because nothing retried
  on its own. The client uses no React Query or SWR; every panel hook
  refetched only when a single shared `lastRefresh` timestamp changed,
  and that timestamp advanced only when the sidebar's
  `GET /api/v1/clusters` call succeeded. A transient failure in an
  individual panel's fetch, or in the shared refresh call itself, left
  the affected panels blank with no automatic recovery. A shared
  `useRetryingFetch` hook now provides capped exponential backoff (3s,
  6s, 12s, 24s, capped at 45s) for the Event Timeline, the
  performance-summary tiles, the Estate KPI tiles, and the alerts
  panel. A manual refresh always pre-empts any pending retry, and the
  Estate KPI tiles show a subtle "Reconnecting..." indicator while a
  retry is pending, distinguishing that state from genuinely empty
  data.

- Granting "All Connections" access no longer deletes a group's
  specific per-connection grants. Previously, adding an All
  Connections READ grant destructively removed existing
  per-connection grants, silently reducing a connection's
  READ/WRITE access to READ; specific and wildcard grants now
  coexist, and the effective access for each connection is the
  higher of the two. (#302)

- Fix the Table and Index object-detail dashboards showing stale,
  pre-maintenance values in their overview tiles and Maintenance Info
  panel indefinitely, even after the collector recorded fresh, correct
  data. Running ANALYZE on a table corrected its live-tuple-count
  estimate downward, yet the dashboard kept showing the old, larger
  count and a stale or missing Last Analyze value. The latest-row mode
  of the metrics API (`GET /api/v1/metrics/query` with `limit` and
  `order_by`) ranked the table's entire history by the requested
  `order_by` column and used `collected_at` only to break exact ties,
  so it returned whichever historical sample had the highest column
  value rather than the most recent sample. Any column that can
  decrease over time, such as a tuple count after ANALYZE, VACUUM, or
  DELETE, or a dead-tuple count after VACUUM, could therefore return a
  permanently stale historical row. The query now reduces to each
  monitored entity's newest sample by `collected_at` first, then ranks
  those already-latest rows by the requested `order_by` column, so a
  request filtered to one table or index always returns that entity's
  true latest sample regardless of the sort column.

- Fix the Index object-detail dashboard's Scan Activity chart always
  showing "No index scan data available" for every index. The web
  client sent the index's own name as the `table_name` query
  parameter, but the metrics time-series query filtered on `relname`,
  so no table ever matched an index name and the request returned no
  rows. The time-series query path also had no way to filter by index
  name, so a table with several indexes would have blended all of
  their scan activity together. The metrics API
  (`GET /api/v1/metrics/query`) now filters on `indexrelname`, the
  client's metrics-fetching hook accepts a new `indexName` parameter,
  and the Scan Activity chart sends it instead of misusing the
  table-name parameter.

- Fix several dashboard charts rendering blank whenever their time
  series was perfectly flat, most visibly the Cache Hit Ratio Over
  Time chart for a new, essentially read-only sample database whose
  ratio held steady at 100% for its whole history. A flat series gave
  the value axis a zero-height range, with `min` equal to `max`, so
  the line or area had nothing to draw against and the chart appeared
  empty. The value axis now pads a small visible range around a flat
  value so the series renders, and it does so in a baseline-aware way
  for zero-anchored charts, such as bars and stacked or area fills, so
  their fill still renders from zero. (#336)

- Fix the Cluster Group settings dialog so it shows and preserves a
  group's description and its "Share with all users" setting, which
  previously appeared empty or unchecked and were lost when editing;
  the share flag is now persisted by the API. (#304)

- Harden the collector against clean-exit restart loops that could
  stop the Dashboard from showing Top Queries. The collector now
  logs which signal triggered its shutdown, rather than exiting
  silently, and the default monitored probe and pool wait timeout
  (`monitored_max_wait_seconds`) was raised from 60 to 120 seconds
  to give slow probes such as `pg_stat_statements` on large
  monitored databases more headroom before timing out. (#308)

- Harden the shipped Docker Compose collector service by running an
  init process as PID 1, adding a stop grace period for graceful
  shutdown, and raising the memory limit modestly from 256M to
  512M; the collector connection-pool and timeout options are now
  documented in the sample configuration. (#308)

### Security

- Ignore a blank password when updating a database connection, so an
  empty or whitespace-only password can no longer overwrite the stored
  credential. The datastore now enforces the "leave blank to keep
  unchanged" rule server-side as defence-in-depth, rather than relying
  on the web client alone. The create path, which still requires a
  non-empty password, is unaffected. (#332)

## [1.0.0] - 2026-06-08

This release is the first general-availability release of the
pgEdge AI DBA Workbench, graduating the project from beta to a
stable, supported 1.0 line.

### Added

- Add a `password_file` option to the server's `database:` YAML
  block, allowing the server to read the datastore password from
  a file. The server uses this file only when no inline
  `password` value and no CLI password flag are set, bringing the
  server in line with the collector and alerter. (#267)

### Changed

- Unify how the server, collector, and alerter read secrets from
  files onto a single hardened helper. The change covers database
  and user passwords, service tokens, LLM and embedding API keys,
  server and notification secrets, proxy header values, and binary
  encryption keys. Several behaviours below are
  backward-incompatible, so operators upgrading the collector or
  alerter should review them before rolling out the new binaries.
  (#267)

- Trim only the trailing newline from a secret file, rather than
  stripping all surrounding whitespace. This applies to passwords,
  tokens, API keys, server and notification secrets, and proxy
  header values. Secrets that legitimately contain leading,
  trailing, or interior spaces are now preserved verbatim.
  Operational: operators who relied on the old
  whitespace-stripping behaviour may need to re-create affected
  secret files so the stored value matches the intended secret.
  (#267)

- Treat a configured-but-empty secret or password file as a hard
  startup error, instead of silently treating the file as "no
  value". Previously an empty file fell through to `.pgpass` or a
  passwordless connection, which could mask a misconfigured
  deployment. Operators must ensure every
  configured secret file contains a value before starting the new
  binaries. (#267)

- Accept any owner-only permission mode, such as `0400` or `0600`,
  for the binary key loaders that read the server encryption key
  and the alerter notification secret key, instead of requiring
  exactly `0600`. Group- or world-accessible modes such as `0640`
  and `0644` are still rejected. (#267)

- Expand a leading `~` in a secret or server-secret file path to
  the user's home directory consistently across all components.
  (#267)

- Allow the server's `-db-password-file` flag to accept paths
  containing `..`, so legitimate relative paths now work. The flag
  previously rejected any path containing `..`. (#267)

- Raise the web client's minimum Node.js version to 20.19 and
  enforce it through an `engines` field in the client
  `package.json` (`^20.19.0 || >=22.12.0`), with a matching
  `.nvmrc`. The client's build tooling, including Vite 8 and the
  `@csstools` packages, requires Node.js 20.19 or later, so older
  releases such as Node.js 18 failed the build with a cryptic
  error. The documented prerequisite now states the supported
  range (Node.js 20.19 or later on the 20.x line, or 22.12 or
  later) everywhere it appears. (#272)

### Fixed

- Fix the AI assistant "Ellie" and the client-side analysis prompts
  generating SQL against the old combined `pg_stat_bgwriter` columns
  such as `checkpoints_timed`, `checkpoints_req`, and
  `buffers_checkpoint`, which fail on PostgreSQL 17 and later. Those
  checkpoint statistics moved into the new `pg_stat_checkpointer` view
  in PostgreSQL 17. The system prompts now instruct the model to query
  `pg_stat_checkpointer` on PostgreSQL 17 and later and the combined
  `pg_stat_bgwriter` view on PostgreSQL 16 and earlier, choosing the
  view based on the target server's version. (#286)

- Fix the `get_metric_baselines` MCP tool returning no baselines
  when the `metric_name` filter used an unqualified shorthand such
  as `cache_hit_ratio`, because the tool matched the stored
  fully-qualified name such as `pg_stat_database.cache_hit_ratio`
  only on an exact match. The filter now applies a parameterised,
  case-insensitive containment match, with literal `%` and `_`
  matched literally, and a filter that matches nothing returns the
  available fully-qualified metric names within the caller's RBAC
  scope so the model can retry. (#287)

- Fix the alerter failing to load its server secret when
  `secret_file` was unset, with no fallback search. The alerter now
  resolves a default secret file the way the collector and server
  already do; when `secret_file` is not set, it searches the
  per-user config directory first and then `/etc/pgedge`. This
  brings the three services to parity and makes the search order
  documented in `examples/ai-dba-alerter.yaml` accurate. (#291)

- Fix the alerter's Gemini provider lacking a configurable embedding
  model. The `llm.gemini` section now exposes an `embedding_model`
  option that defaults to `gemini-embedding-001`; the alerter
  previously offered no embedding model field for Gemini. When Gemini
  is the embedding provider, the configured model must match the model
  that the KB Builder used to produce the knowledgebase. (#284)

- Fix the documented Gemini embedding model list, which referenced the
  deprecated `text-embedding-004` and `embedding-001` models that
  Google has retired. The supported models are now
  `gemini-embedding-001` (the default), `gemini-embedding-2`, and
  `gemini-embedding-2-preview`, all of which output 3072 dimensions
  rather than the previously documented 768. The server's
  knowledgebase status log now reports the Gemini API key as loaded.
  (#285)

- Fix the Admin Panel's Create Group, Edit Group, and Create
  Token dialogs accepting unsupported special characters and
  over-long Name values. RBAC group and token names now reuse the
  app's shared name validator on both the client and the server:
  after trimming, a name must be non-empty, at most 255
  characters, and may contain only letters, numbers, spaces, and
  the characters `. _ - ( )`. Creating a group with a name that
  already exists now returns a clear "A group with this name
  already exists" conflict (HTTP 409) instead of a generic server
  error. (#273)

- Fix the Admin Panel's Create User and Edit User dialogs
  accepting invalid usernames and email addresses and surfacing
  unhelpful errors. A username with special characters, an
  invalid email such as `notanemail` or `test@`, or a duplicate
  username previously returned a generic server error or stored
  silently. The client and server now apply matching rules: the
  server validates the username, validates the email format, and
  returns HTTP 400 with the specific rule, or HTTP 409 "Username
  already taken" for a duplicate. The dialogs show inline,
  field-level errors and block the save until the input is
  valid. (#271)

- Resolve two moderate-severity npm vulnerabilities reported in
  the web client's dependency tree by updating the affected
  transitive dependencies. (#272)

- Fix the server silently swallowing read errors for configured
  LLM, embedding, and knowledgebase API-key files. The server
  previously proceeded with no key when such a file was unreadable
  or empty; an unreadable or empty configured key file now fails
  loudly at startup. Leaving a key-file path unset remains valid
  and is not an error. (#267)

- Fix name fields for server groups, clusters, and servers
  accepting special characters such as `<>!@#$%`. Validation
  previously rejected only empty input, so disallowed
  characters were stored unchanged. The server and client now
  apply the same rule: after trimming, a name must be
  non-empty, at most 255 characters, and may contain only
  letters, numbers, spaces, and the characters `. _ - ( )`.
  The server returns HTTP 400 for invalid names across all
  cluster-group, cluster, and server-connection create and
  update endpoints, and the client shows an inline validation
  error and blocks the save. (#269)

- Fix the alerter intermittently and silently suppressing
  anomaly alerts that should have fired. The Tier 3
  LLM-based classifier parsed model responses
  non-deterministically, so the same response could resolve
  to either "alert" or "suppress" from one run to the next.
  The parser now strips markdown code fences and extracts an
  embedded JSON object before falling back to keyword
  matching, and the keyword fallback applies a deterministic,
  fail-safe precedence (alert before suppress, keep before
  clear) instead of iterating a randomly ordered map. An
  over-broad suppress keyword that matched phrases such as
  "deviation from normal behavior" was also tightened. The
  same anomaly condition now produces the same decision every
  time, so genuine alerts fire reliably. (#264)

- Fix the Add Cluster Group, Add Cluster, and Add Server
  dialogs accepting Name, Host, Maintenance Database, and
  Username values longer than 255 characters, which the
  database rejected through its `VARCHAR(255)` columns and
  surfaced as a generic, unhelpful server error. The web
  client now caps those fields at 255 characters so
  over-length input cannot be submitted, and the server
  validates their length and returns a clear 400 response (for
  example "Name must be 255 characters or less") instead of a
  generic 500. The server counts characters rather than bytes
  to match the `VARCHAR(255)` limit. (#270)
- Fix Ellie, the AI chat assistant, hanging in an effectively
  infinite tool-validation loop for regular, non-superuser
  users. When a regular user selected a connection shared with
  them and asked Ellie to describe it, the query-validation
  tool kept succeeding while the tools that execute the query
  failed with an access error. The assistant retried slightly
  different queries indefinitely, repeatedly showing
  `Validating query` and never responding. The chat loop now
  applies a repeated-failure circuit breaker: when the same
  tool fails with the same error three times, the assistant
  stops retrying and returns a clear message. The message names
  the failing tool, shows the underlying error, and suggests a
  likely permissions or connection-access problem to raise with
  an administrator. (#268)

### Security

- Log a warning recommending `chmod 600` when reading any secret
  file from a group- or world-readable path. The read still
  succeeds, so existing deployments continue to work while
  operators tighten the affected file permissions. (#267)

## [1.0.0-beta3] - 2026-05-26

### Added

- Add a hybrid variance floor and a per-`period_type` warmup
  gate to the alerter's Tier 1 anomaly detector, plus a
  symmetric z-score cap as defence in depth. Three new YAML
  blocks under `anomaly.tier1` expose the controls:
  `max_z_score` (default `100.0`, set to `0` to disable the
  clamp), `variance_floor` with `relative_pct` (default
  `0.05`) and `absolute_floor` (default `0.001`), and `warmup`
  with `min_samples` and `min_span_hours` pairs for `all`,
  `hourly`, and `daily` baselines. A new
  `metric_baselines.earliest_sample_at` column is added by an
  idempotent `ALTER TABLE` in the consolidated collector
  schema migration; the column is populated automatically by
  the alerter's baseline builder at the next refresh, and the
  warmup gate fails closed on NULL values until the column is
  populated. The `anomaly_candidates` and `alerts` tables are
  deliberately left untouched to preserve audit history.
  **Operational:** operators upgrading an existing deployment
  must `TRUNCATE TABLE metric_baselines` after stopping the
  alerter and before starting the new binary, so the detector
  rebuilds baselines under the new logic from a clean slate.

- Add a startup datastore schema health check to the MCP
  server. The server now reads the collector-owned
  `schema_version` table and probes a small set of critical
  dashboard tables before wiring any handlers; a missing or
  pre-v4 collector schema, or a partial drop of any probed
  relation, surfaces an operator-actionable error that
  names the affected datastore and the recommended action,
  then exits non-zero. This replaces the previous silent
  failure mode where the server would come up against an
  empty database and 500 on every dashboard endpoint.

- Add the `get_timeline_events` MCP tool, which exposes
  the unified incident-investigation timeline that was
  previously only reachable through the
  `/api/v1/timeline/events` REST endpoint. The tool
  returns configuration changes, HBA and ident edits,
  server restarts, extension changes, alert
  fired/cleared/acknowledged events, and blackout
  start/end markers in a single TSV result ordered most
  recent first. Ellie and other MCP clients can now
  correlate alerts with the underlying changes that may
  have caused them without falling back to direct REST
  calls. The tool gates results through the same RBAC
  checks as `get_alert_history`. (#250)

### Security

- Fix the server creating the SQLite `auth.db` file with
  world-readable mode `0644` on first start, exposing bcrypt
  password hashes and token hashes to other local users on
  the host. The previous `os.Chmod(0600)` call ran before the
  `modernc.org/sqlite` driver had created the file on disk,
  so the chmod silently failed with ENOENT and the file was
  left at the operating system's umask default. The fix moves
  the chmod after the `PRAGMA foreign_keys` query, which
  forces the driver to round-trip and create the file, and
  adds a post-chmod `os.Stat` verification that logs a
  warning if the resulting mode is not `0600`. The server
  applies the chmod unconditionally on every startup, so an
  existing `auth.db` left at `0644` by an earlier release is
  re-narrowed automatically and no manual operator action is
  required. On filesystems that silently ignore chmod (for
  example FAT or some FUSE mounts), the server now refuses to
  start and operators should consult the WARNING log line that
  reports the observed mode. (#249)

### Added

- Add Gemini as a supported embedding provider in the
  `embedding` and `knowledgebase` server configurations;
  the provider supports `text-embedding-004` (default)
  and `embedding-001`, both at 768 dimensions. (#246)

### Changed

- Update the default Gemini chat model in the alerter
  and server configurations from `gemini-2.0-flash`,
  which is no longer available to new API users, to
  `gemini-2.5-flash`. (#246)

### Fixed

- Fix the `get_timeline_events` MCP tool emitting a
  flat "Alert condition no longer active" summary on
  `alert_cleared` rows, which left a reviewer
  scanning the timeline to read the vivid
  `alert_fired` summary (carrying the alert's frozen
  `description` text) and risk misreading an already
  resolved alert as still active. Cleared rows now
  carry a self-contained summary of the form
  `Resolved after <duration>. Fired: <original alert
  description>`, where `<duration>` renders as
  `Ns`, `Nm Ms`, or `Nh Mm`; the underlying
  `alerts.description` column is preserved unchanged,
  so this is a presentation change in the timeline
  tool rather than a rewrite of historical alert
  text. The tool description visible to MCP clients
  now also states that `alert_fired.summary`
  describes the firing condition and not the current
  state, and instructs clients to pair each
  `alert_fired` row with its corresponding
  `alert_cleared` row (matching title prefix
  `'Alert Cleared: '`) before treating an alert as
  ongoing.

- Fix the collector entering a restart loop when its
  consolidated schema migration ran against a partially
  populated datastore. PostgreSQL has no
  `ALTER TABLE ... ADD CONSTRAINT IF NOT EXISTS`, so a
  re-run that found a pre-existing foreign key (such as
  `fk_anomaly_candidates_embedding` on
  `anomaly_candidates`) raised `duplicate_object`, aborted
  the surrounding `pgx.Tx`, and the next statement
  returned SQLSTATE 25P02 ("current transaction is
  aborted, commands ignored until end of transaction
  block") with a misleading error pointing at an
  unrelated table. The migration now guards every
  `ADD CONSTRAINT` with a catalog check against
  `pg_constraint`, runs the optional pgvector blocks
  inside SAVEPOINTs so a missing extension or schema
  drift cannot poison the parent transaction, and
  surfaces real failures with a `return` instead of
  swallowing them through `logger.Infof`. Fresh installs
  are unaffected; the change matters only for recovery
  cases.

- Fix the web client crashing into the "Something
  went wrong" error boundary after the user deleted
  an empty cluster. The server marshaled an empty
  `clusters` list (and the nested `servers` list
  inside each cluster) as JSON `null` rather than
  `[]`, and `MainLayout`'s selection `useMemo` then
  called `.some` on the null value without a guard.
  The server's `GetClusterTopology` now normalises
  both lists to a non-nil empty array, and the
  client's selection logic guards every read of
  `group.clusters` through a new `buildSelection`
  helper. The `ClusterGroup.clusters` TypeScript
  type is also tightened to `ClusterEntry[] | null`
  so this regression class is caught by the type
  system. (#242)

## [1.0.0-beta2] - 2026-05-14

### Added

- Document the TLS and reverse-proxy requirements for
  any network-accessible deployment in a new
  Administrator's Guide page at
  `docs/admin-guide/tls-and-reverse-proxy.md`; the
  page states explicitly that TLS termination, HTTP
  to HTTPS redirection, and HSTS are operator
  responsibilities at the reverse proxy layer, calls
  out the Vite dev server on port 5173 as
  localhost-only and unsupported for any
  network-accessible use, notes that the server's
  built-in TLS support remains available for
  operators who choose to terminate at the
  application, and enumerates the credential-exposure
  risks of running the workbench over plain HTTP.
  Cross-reference callouts now appear in the
  installation, quick-start, Docker, and web-client
  configuration pages. (#234)
- Add a Playwright-based end-to-end smoke-test suite
  that drives the production client bundle in a real
  browser against a real server and Postgres on every
  pull request; the suite runs across a
  Chromium/Firefox/WebKit matrix and is invoked
  locally with `make test-e2e`. See
  `docs/developer-guide/e2e/index.md` for details.
  (#236)
- Capture Go integration coverage from the running
  server during the end-to-end suite and merge it into
  the existing Codacy partial-upload pipeline, so unit
  and integration coverage combine into a single
  reported figure. (#236)
- Add the `spock_exception_log` and `spock_resolutions`
  collector probes; both probes capture a rolling
  15-minute window of the Spock extension's
  exception and conflict-resolution catalogs and
  no-op cleanly on databases without Spock installed.
  (#200)
- Add six built-in alert rules in the `replication`
  category: `spock_recent_exceptions_present`,
  `spock_recent_exceptions_high`,
  `spock_recent_resolutions_present`,
  `spock_recent_resolutions_high`,
  `replication_slot_retention_warn`, and
  `replication_slot_retention_high`; the Spock rules
  require the `spock` extension and the slot
  retention rules apply to every PostgreSQL
  deployment. (#200)

### Changed

- **Breaking change:** the web client container now
  runs as a non-root user and listens on port
  **8080** instead of port 80. The base image in
  `client/Dockerfile` switched from
  `nginx:stable-alpine` to
  `nginxinc/nginx-unprivileged:stable-alpine`, with
  an explicit `USER nginx` directive. Host-side port
  mappings in `docker-compose.yml`,
  `docker-compose.prod.yml`, and the walkthrough
  compose file are unchanged, so
  `http://localhost:3000` continues to work with the
  default `CLIENT_PORT`. Operators running custom
  reverse-proxy configurations, Kubernetes
  manifests, or external `proxy_pass` upstreams that
  target container port 80 must update those
  references to 8080; this includes Service
  `targetPort` values, health probes, and any
  direct container-to-container references.
- **Breaking change:** the collector, alerter, and
  server no longer auto-discover configuration or
  secret files in the binary directory or the current
  working directory; review the migration steps below
  before upgrading. (#195)

    - The new lookup order is the `--config` flag, the
      per-user config directory, and `/etc/pgedge/`;
      the first match wins, and missing files fall
      through to compiled-in defaults.
    - The per-user path resolves to
      `~/.config/pgedge/<binary>.yaml` on Linux
      (honouring `$XDG_CONFIG_HOME`),
      `~/Library/Application Support/pgedge/<binary>.yaml`
      on macOS, and `%AppData%\pgedge\<binary>.yaml`
      on Windows.
    - The same precedence applies to the collector and
      server secret files (`ai-dba-collector.secret`
      and `ai-dba-server.secret`); the alerter does not
      use a secret file.
    - Production deployments that already use
      `/etc/pgedge/` are unaffected.
    - Development setups that drop a YAML file next to
      the binary or in the current working directory
      will silently fall through to compiled-in
      defaults; move the file to `/etc/pgedge/` or the
      per-user directory, or pass `--config` with an
      explicit path.
    - The alerter's `SIGHUP` handler re-runs discovery
      on each reload, so installing a config at a
      default location after startup is picked up on
      the next signal.
- Replace the composition-rule password validator
  with a policy aligned to NIST SP 800-63B; the
  server now requires a minimum of 12 characters,
  enforces the 72-byte bcrypt upper bound, drops
  uppercase, lowercase, digit, and special-character
  requirements, and rejects passwords found in a
  built-in dictionary of approximately 10,000 common
  and breached entries. The web client shows live
  password-strength feedback as the user types, and
  the server remains the authoritative validator.
  (#177)
- Document installation paths for each deployment
  method (GitHub release, Docker, RPM/DEB) in the
  installation guide with a reference table. Add
  cross-reference notes to the quick start, Docker,
  and sub-project README files. Align manual-install
  systemd service names to `pgedge-ai-dba-*` to
  match RPM/DEB package conventions. (#173)
- Reject `cors_origin: "*"` at server startup when
  authentication is enabled. Browsers discard
  credentialed responses that combine
  `Access-Control-Allow-Origin: *` with
  `Access-Control-Allow-Credentials: true` per the
  Fetch spec. Operators should configure an explicit
  origin or leave the option empty for same-origin
  deployments. (#81)
- Migrate the `collector`, `alerter`, and `server`
  `.golangci.yml` configurations to the golangci-lint v2
  format, and update the CI workflows to install
  `golangci-lint/v2`; `make test-all` now works again on
  developer machines that have golangci-lint v2
  installed locally. (#66)
- Apply a Biome and ESLint auto-fix pass across
  `client/src/`, clearing roughly 600 Codacy findings
  across 294 files; the change is a mechanical refactor
  with no behavior changes, and existing lint and test
  baselines remain unchanged.
- Clear all `@typescript-eslint/no-confusing-void-expression`
  findings in `client/src/` across 80 files; ESLint's
  auto-fixer resolved 279 sites and 19 remaining cases were
  rewritten manually by expanding
  `() => cond && voidFn()` into explicit `if` blocks. No
  behavior changes, and all 2,604 Vitest tests pass.
- Raise line coverage of `server/internal/crypto` from
  86.8% to 100%. New tests cover four previously uncovered
  error branches. The branches are random source failure,
  `ReadFile` failure, `WriteFile` failure, and the GCM
  encrypt failure path. (#78)
- Add integration tests for `server/internal/memory.Store`
  against PostgreSQL. The tests cover all nine public
  methods: `NewStore`, `Store`, `Search`, `GetPinned`,
  `ListByUser`, `GetByID`, `Delete`, `DeleteByID`, and
  `UpdatePinned`. They also exercise pgvector similarity
  ordering, scope visibility, and ownership checks.
  Package line coverage for the memory store now reaches
  92.5%. (#78)
- Add the `-race` flag to the `test` and `coverage`
  Makefile targets in the `server`, `collector`, and
  `alerter` sub-projects. The race detector now runs in
  CI and on developer machines. (#78)
- Auto-collapse the Server Dashboard's "System
  Resources" section when its data is unavailable,
  typically because the `system_stats` PostgreSQL
  extension is not installed on the connected server;
  the section previously stayed expanded and rendered
  five empty CPU, Memory, Disk, Load, and Network IO
  panels that pushed the "PostgreSQL Overview"
  section far down the page. The collapsed header now
  shows the italic message "No data available. Is the
  system_stats extension installed?" next to the
  title, and the user can still expand the section
  manually to inspect the empty panels. The manual
  override is intentionally not persisted to
  `localStorage`, so the section returns to the
  user's previous expand or collapse preference once
  the extension is installed. The shared
  `CollapsibleSection` component gained two new
  props, `forceCollapsed` and `forceCollapsedMessage`,
  which temporarily override the persisted state
  without mutating storage and render the italic
  header message; an anti-flicker guard delays the
  force-collapsed state until the initial KPI fetch
  completes, so the section does not briefly collapse
  during loading.
- Bump the Go toolchain from 1.26.1 to 1.26.2
  across the server, collector, alerter, and `pkg`
  modules and the dev-container image; the upgrade
  picks up upstream fixes for seven Go security
  advisories listed in the Security section.
- Bump `github.com/jackc/pgx/v5` from 5.7.6 to
  5.9.2 in the server, collector, and alerter; the
  upgrade picks up the memory-safety and
  dollar-quoted-string fixes listed in the Security
  section.
- Add a `.codacy.yaml` configuration that suppresses
  confirmed false-positive findings from Codacy's
  Semgrep and ESLint8 engines; suppressions are
  scoped to specific files or to `__tests__/**`
  globs, were independently reviewed by the
  security-auditor agent, and mask no real
  vulnerabilities.
- Reduce the web client's default `body1` and `body2`
  typography sizes to the MUI standards of 16px and 14px,
  and remove the roughly 80 inline `fontSize` overrides
  that previously compensated for the oversized defaults;
  body text across the application now renders smaller and
  more consistently, while headings and subtitles are
  unchanged.
- Add a `CHART_AXIS_LABEL_FONTSIZE` token in
  `client/src/theme/tokens.ts` so chart axis labels render
  at a consistent 14px, and add a `MONO_CAPTION_SX` token
  that deduplicates the monospace caption styling shared
  by alert thresholds, Spock node info, and cron
  schedules.
- Consolidate four duplicated patterns in `server/src` as
  part of the codebase cleanup tracked in #77. The
  copy-pasted `getClient()` helper in
  `internal/tools/context_aware_provider.go` and
  `internal/resources/context_aware_registry.go` now
  delegates to a new
  `(*database.ClientResolver).ResolveOrError` method that
  returns the canonical "no database connection
  configured" error. The 14-field
  `chat.NewClientFromConfig` invocation repeated in
  `internal/llmproxy/proxy.go` (`HandleModels` and
  `HandleChat`) and `internal/overview/generator.go`
  (`createLLMClient`) collapses into a new
  `chat.NewClientFromLLMConfig` factory that takes an
  `LLMOptions` parameter for per-call overrides such as
  `Model`, `MaxTokens`, `Temperature`, `Debug`, and
  `Headers`, removing roughly 40 lines of boilerplate per
  call site. The two `auth.ConnectionVisibilityLister`
  adapters in `internal/api/helpers.go` and
  `internal/database/visibility_lister.go` share a single
  projection; the slice-based adapter moves into the
  `database` package as `database.NewSliceVisibilityLister`
  and the projection is exported as
  `database.ConnectionsToVisibilityInfo`. The five-line
  closure that wired
  `(*database.Datastore).GetConnectionSharingInfo` into an
  `auth.RBACChecker` from
  `internal/tools/context_aware_provider.go`,
  `internal/resources/context_aware_registry.go`, and
  `cmd/mcp-server/handlers.go` now flows through a new
  `auth.NewRBACCheckerForDatastore` constructor that
  accepts the datastore through a small
  `DatastoreSharingLookup` interface and handles nil and
  typed-nil cases internally. The change is internal-only
  and behavior-preserving; no public HTTP API, MCP tool,
  or configuration surface changes. (#77)

### Security

- Pick up upstream fixes for seven Go security
  advisories by bumping the toolchain to 1.26.2;
  the advisories are CVE-2026-32280 (certificate
  chain validation denial of service), CVE-2026-33810
  (DNS-constraint certificate validation bypass),
  CVE-2026-32281 (certificate chain validation denial
  of service), CVE-2026-32283 (TLS 1.3 key-update
  denial of service), CVE-2026-32289 (`html/template`
  cross-site scripting), CVE-2026-32288
  (`archive/tar` denial of service), and
  CVE-2026-32282 (`Root.Chmod` symlink escape).
- Pick up upstream fixes in `github.com/jackc/pgx/v5`
  by bumping to 5.9.2; the advisories are
  CVE-2026-33816 (Critical, memory safety) and
  GHSA-j88v-2chj-qfwx (Low, SQL injection through
  dollar-quoted-string and `$N` placeholder
  confusion). A code audit confirmed that no query
  in this project mixes `$$...$$` literals with `$N`
  placeholders, so the second advisory was
  theoretical for our code base; the bump is still
  warranted as a defence-in-depth measure.
- Bump the web client container's base images to
  pick up upstream fixes for high-severity CVEs
  flagged by Docker Scout. The builder stage moves
  from `node:22-slim` to `node:22-trixie-slim`
  (Debian 13), which closes CVE-2026-33845 and
  CVE-2026-33846 in `gnutls28`. The runtime stage
  moves from `nginxinc/nginx-unprivileged:stable-alpine`
  to `nginxinc/nginx-unprivileged:stable-alpine-slim`,
  which closes CVE-2026-3805 in `curl` on Alpine
  3.23; the slim variant omits `curl`, which the
  runtime does not need. One residual high finding,
  CVE-2026-33671 in the `picomatch` package bundled
  inside npm, persists across all Node 22 and 24
  tags pending an npm release; the residual lives
  only in the builder stage and never reaches the
  shipped image. The non-root UID 101 nginx user,
  port 8080, and other hardening from the earlier
  base-image change are preserved, and
  `docker scout cves` reports no vulnerable
  packages in the final image.
- Redact notification channel secrets from API responses;
  `GET /api/v1/notification-channels` and
  `GET /api/v1/notification-channels/{id}` no longer return
  `smtp_username`, `smtp_password`, `webhook_url`, or
  `auth_credentials`, all of which were previously emitted in
  plaintext after server-side decryption. Each response now
  includes the boolean indicators `smtp_username_set`,
  `smtp_password_set`, `webhook_url_set`, and
  `auth_credentials_set` so clients can show whether a secret
  is configured without ever reading the value. The
  `PUT /api/v1/notification-channels/{id}` endpoint applies a
  three-way merge to the four secret fields; omit a field to
  preserve the stored value, send an empty string to clear
  it, or send a new value to replace it. The web admin UI for
  the Email, Slack and Mattermost, and Webhook channel
  editors now leaves secret form fields blank when editing an
  existing channel and preserves the stored value unless the
  user types a replacement. (#187)

- Fix log, SQL, and SMTP injection findings surfaced by
  the golangci-lint v2 upgrade; the knowledgebase search
  now binds its filter values through `?` placeholders
  instead of string concatenation, the email test sender
  sanitizes envelope and header fields before writing
  them to the SMTP connection, and user-tainted values
  are escaped through a new `logging.SanitizeForLog`
  helper at log sites across the api, auth, and config
  packages. (#66)

- Hoist RBAC access-control checks above all
  datastore calls in the alert-counts, alert
  acknowledgement, alert unacknowledgement, alert
  analysis, and cluster-topology handlers; zero-grant
  callers now short-circuit to an empty response
  without touching the database, and HTTP-level
  regression tests cover every affected handler.
  (#67)

- Extend the `manage_connections` gate to the remaining
  connection paths missed by the earlier sweep under
  #207; the server's `createConnection` handler now
  requires the permission for every new connection
  rather than only for shared connections, the
  `handleUpdateConnectionCluster` handler behind
  `PUT /api/v1/connections/{id}/cluster` now also
  requires the permission so that a user with only
  read-visibility on a connection can no longer re-home
  it between clusters, and the web client's Add menu
  hides the "Add Server" entry from users who lack the
  permission. The new server-side gate sits after the
  existing visibility check and before any body decode
  or datastore mutation. Unauthorized callers receive a
  `403 Forbidden` response with a clear authorization
  error, and the OpenAPI specification and the static
  `docs/admin-guide/api/openapi.json` document the new
  response on the affected paths; the previously
  undocumented `/connections/{id}/cluster` path is now
  present in the specification with both its GET and
  PUT methods. (#233)

- Require the `manage_connections` permission on all
  cluster and cluster-group mutating endpoints; users
  without the permission previously could create
  cluster groups and clusters through the REST API,
  and the server silently committed the rows and
  returned a success status even though the resulting
  records were invisible to the creator while remaining
  visible to administrators. A follow-up audit found
  the same gap on additional mutating routes, so the
  fix now gates eleven endpoints in total: creating
  and deleting cluster groups, adding clusters to a
  group, creating, updating, and deleting clusters,
  adding and removing cluster servers, and creating,
  updating, and deleting cluster relationships.
  Unauthorized callers now receive a `403 Forbidden`
  response with a clear authorization error instead of
  a misleading success, and the group owner can still
  delete a group they own even without the permission.
  The web client's Add menu now hides the "Add Cluster
  Group" and "Add Cluster" entries from users who lack
  `manage_connections`, and the OpenAPI specification
  and static `docs/admin-guide/api/openapi.json`
  document the `403 Forbidden` response on the
  affected paths. Administrators see no functional
  change. (#207)

### Fixed

- Fix the Replication Type select in the Cluster Settings
  dialog rendering blank for auto-detected clusters whose
  `replication_type` column is NULL; the dialog now derives
  the value from the cluster's `auto_cluster_key` prefix when
  no explicit replication type is set, mirroring the existing
  behaviour of the Topology panel. (#235)
- Fix auto-detected clusters lingering in the cluster
  autocomplete dropdown after their last member connection
  was deleted; `DeleteConnection` now runs in a single
  transaction that marks the parent cluster `dismissed=TRUE`
  when the cluster was auto-detected (`auto_cluster_key IS
  NOT NULL`) and no connections still reference it. The
  delete itself uses `DELETE ... RETURNING cluster_id` to
  close a TOCTOU window between the lookup and the delete,
  and the rollback path uses `context.Background()` to avoid
  a pgx v5 close-of-closed-channel panic when the request
  context is canceled mid-transaction. User-created clusters
  and connections without a cluster assignment are
  unaffected. (#245)
- Fix anomaly alerts displaying raw dotted metric names such
  as `pg_replication_slots.max_retained_bytes` in the Status
  Panel, and six alert rules seeded by collector migration v3
  rendering as auto-title-cased fallbacks in the Admin Alert
  Rules panel; the `FRIENDLY_ALERT_TITLES` map now includes
  curated labels for 15 dotted anomaly metric names and the 6
  v3 rule names, with full test coverage of the map. (#244)
- Fix long, non-wrapping SQL queries flowing underneath the
  copy and run icons in the Remediation Steps panel of the
  alert AI analysis view; the shared markdown styles now
  reserve right-side padding on each code block sized to the
  number of overlaid action buttons, so the SQL text never
  collides with the icons. (#221)
- Fix the Active Alerts Restore button returning HTTP 500
  "Failed to unacknowledge alert" for alerts that were
  already non-acknowledged (for example after the alerter
  reactivated them following a severity change); the server
  now maps a missing alert to 404, an alert that is not
  currently acknowledged to 409 Conflict, and wraps every
  failure path with the alert ID for actionable logs. The
  alerter's auto-reactivation path is also hardened against
  panicking on alerts with a NULL `metric_value` column and
  captures the previous severity before the database write
  so the in-memory comparison cannot drift from the
  acknowledged state. (#227)

- Fix the alerter's `replication_slot_inactive` critical
  alert never firing because its metric query selected
  from a non-existent `metrics.pg_stat_replication_slots`
  table; the `pg_replication_slots.inactive` metric now
  reads directly from `metrics.pg_replication_slots` (the
  table the collector probe writes to) and derives the
  inactive state from the `active` column. New integration
  tests cover the happy path, the no-row case when every
  slot is active, slot deduplication per connection, and
  the 5-minute freshness cutoff. (#224)

- Fix Ask Ellie incorrectly reporting missing Spock
  replication slots on healthy Spock 6.x clusters; the
  assistant previously generated
  `WHERE plugin = 'spock'` against
  `pg_replication_slots`, but current Spock releases name
  the output plugin `spock_output`. The chat system prompt
  in `server/src/internal/chat/llm.go` now instructs Ellie
  to use `plugin LIKE 'spock%'` for cross-version
  compatibility. (#220)

- Fix three datastore schema and probe inefficiencies
  identified during a production performance review.
  Collector migration v4 adds a partial index on
  `anomaly_candidates(detected_at)` filtered to
  `processed_at IS NULL AND tier1_pass = TRUE` so the
  alerter sweeper stops sequential-scanning the full
  table on every poll, and drops the redundant
  `idx_pg_stat_all_indexes_conn_db_time` and
  `idx_pg_stat_statements_conn_db_time` indexes (along
  with their attached child indexes on every existing
  weekly partition) which were duplicated by the more
  specific `_object` indexes. The change-detection probes
  (`pg_settings`, `pg_extension`, `pg_hba_file_rules`,
  `pg_ident_file_mappings`) now strip the
  `ai_dba_wb_probe` marker column injected by
  `WrapQuery` before hashing, so the live snapshot hash
  matches the stored snapshot hash; previously the marker
  caused every hourly collection to look "changed" and
  write a fresh snapshot, inflating the `pg_settings`
  partitions by roughly an order of magnitude. (#219)

- Fix the Admin panels showing a success toast alongside a
  page-level refresh error when a save succeeded but the
  follow-on reload failed; the shared `useCrudPanel` hook
  now suppresses the success toast when the post-mutation
  refresh fails, so the user sees only the actionable
  refresh error. (#215)

- Fix the `AdminMessagingChannels` panel flipping the
  `deleteLoading` busy flag when a user toggled a
  channel's enabled state; the shared
  `useCrudPanel.runMutation` helper gained an
  independent `busyTarget` option
  (`'save' | 'delete' | 'inline'`) that decouples the
  busy-state flag from the `errorTarget`, and a new
  `'inline'` error target routes errors only to the
  caller through the returned result rather than to
  `crud.error` or `crud.dialogError`. Defaults preserve
  the previous behavior so existing call sites are
  unchanged, and `handleToggleEnabled` now uses
  `busyTarget: 'inline'`. (#216)
- Fix the divergent error fallback wording shown by the
  Admin panels when a thrown value is not an `Error`
  instance. The `AdminUsers`, `AdminMemories`,
  `AdminTokenScopes`, `AdminPermissions`, `AdminProbes`,
  `AdminEmailChannels`, `AdminWebhookChannels`,
  `AdminMessagingChannels`, and `AdminGroups` panels, along
  with the `useChannelCRUD` hook, now route non-`Error`
  throws through the shared `extractErrorMessage` helper in
  [`client/src/components/AdminPanel/_shared/errors.ts`](https://github.com/pgEdge/ai-dba-workbench/blob/main/client/src/components/AdminPanel/_shared/errors.ts).
  The helper returns the generic `'An unexpected error
  occurred'` message instead of leaking output such as
  `[object Object]` produced by `String(err)`. Panels that
  pass a contextual fallback (for example, "Failed to add
  recipient") to the helper's second argument retain that
  context-specific wording. (#212)
- Fix the `Chart.test.tsx` regression introduced in
  commit `aa28aa8` that has been failing the CI -
  Client workflow on every commit since; the
  vitest mock specifier `echarts-for-react/lib/core`
  was not updated to `echarts-for-react/esm/core`
  when production code switched to the ESM path,
  leaving the real `ReactEChartsCore` running in
  tests and tripping the deliberately-narrow
  `echarts/core` mock. The change updates only the
  test mock specifier; no production source code
  was modified.
- Fix the npm-install branch in
  `start_dev_web_client.sh` never firing because an
  intervening `echo` clobbered `$?` before the
  `if [ $? -eq 0 ]` check; the script now uses a
  direct `&&`/`||` pattern that tests the previous
  command's exit status without an intermediate
  command.
- Fix Ask Ellie entering a long retry loop ("Joining the
  relations..." / "Validating query") when the signed-in
  user has no MCP privileges; the chat now surfaces a
  clear permission-denied message immediately instead of
  cycling through planning steps. (#188)
- Fix wide markdown tables overflowing and clipping the
  right-side columns inside the Ask Ellie chat panel;
  tables returned by MCP tools such as
  `get_alert_history`, `get_alert_rules`, and
  `query_datastore` now sit inside a horizontally
  scrollable wrapper, so narrow tables still fill the
  bubble while wide tables scroll within it instead of
  spilling outside. The shared `MarkdownContent`
  helper used by other dialogs received the same
  treatment. (#185)
- Fix the web client rendering a blank screen on every
  navigation when the LLM proxy was enabled with a
  reasoning model that returns a structured `summary`
  object; `AIOverview` now coerces non-string summaries
  to text before rendering, removing the React error
  that triggered the blank screen. The top-level
  `<ErrorBoundary>` has also been rewritten to always
  show the error message and component stack in a
  collapsible details block and to expose a "Reload"
  button, so users can recover from a crash and file
  actionable bug reports without rebuilding the
  container. (#182)
- Fix stale auto-detected edges remaining in
  `cluster_node_relationships` after the cluster
  topology changed through failover, subscriber
  removal, or a new parent in a binary chain;
  `SyncAutoDetectedRelationships` now replaces the
  auto-detected set transactionally, deleting all
  existing `is_auto_detected = TRUE` rows for the
  cluster before inserting the freshly detected set,
  and the `syncRelationshipsFromTopology` caller no
  longer short-circuits on an empty detected slice.
  Manual relationships and auto-detected rows for
  other clusters are preserved, and a failure during
  the delete or insert rolls the transaction back to
  the prior state. (#152)
- Fix the cluster Topology tab dropping cascading
  standbys and marking empty auto-detected nodes as
  expandable; persisted and manual chains such as
  primary -> standby -> cascading standby now render
  every level regardless of input order, and nodes
  whose children are filtered out no longer display a
  disclosure arrow. (#153)
- Fix the collector probe config loader ignoring scope
  and silently re-enabling disabled parent overrides;
  `LoadProbeConfigs` now restricts its query to
  `scope IN ('global', 'server')` so cluster- and
  group-scoped rows no longer collapse into the
  `connection_id = 0` bucket and get misapplied as
  global defaults, and `EnsureProbeConfig` now inherits
  the parent config's `is_enabled` value when
  materializing a server-level row instead of
  hard-coding it to true. The SQL is extracted into a
  `loadProbeConfigsQuery` constant and the value
  resolution moves into a pure
  `resolveProbeConfigDefaults` helper, both covered by
  new unit tests. (#151)

- Fix MCP and admin scope privileges granted through a
  wildcard group grant (`"*"`) being silently dropped
  during token scope intersection; the intersection
  logic now recognises wildcard grants and preserves
  the scoped privileges. The fix also introduces an
  explicit `AccessLevelNone` constant to replace raw
  empty strings for "no access" semantics, improving
  code clarity and reducing error-prone comparisons.
  (#96)

- Fix the ClusterNavigator group-editing flow
  round-tripping string group ids (`"group-{x}"`)
  through numeric parses; the string id now travels
  unchanged through `handleConfigureGroup`,
  `handleSaveGroup`, and the cluster actions context,
  and the one `strconv.Atoi`-compatible conversion
  happens at the `GroupDialog` override-panel
  boundary. Auto-detected groups without a numeric
  backing row now display an info alert explaining
  that alert, probe, and channel overrides are
  unavailable instead of silently passing `NaN` to
  the override panels. This removes the root cause
  patched symptomatically in #59. (#63)

## [1.0.0-beta1] - 2026-04-21

### Added

- Add the `llm.timeout_seconds` server configuration
  option to control the HTTP client timeout for
  requests to the configured LLM provider; the
  default remains 120 seconds. (#60)
- Add a guided walkthrough example with pre-seeded
  demo data and an in-browser Driver.js tour covering
  the workbench's major features.

### Changed

- Default the knowledgebase `database_path` to the
  pgEdge package install path at
  `/usr/share/pgedge/postgres-mcp-kb/kb.db`. (#52)

### Fixed

- Remove misleading raw API key options from the
  example server configuration files; the server
  only accepts API keys through the corresponding
  `*_file` variants. (#54)
- Fix spurious "partition would overlap" errors from
  the collector on non-UTC hosts when the weekly
  partition rolled over. (#55)
- Fix foreign key violations during alerter baseline
  calculation when metric rows outlive their
  connection; historical metric queries now filter
  through the connections table. (#56)
- Fix MCP tool invocations failing with TLS
  certificate verification errors against servers
  that require a custom `sslrootcert`, `sslcert`, or
  `sslkey`; the server now forwards these fields on
  the database connection string. (#57)
- Fix reactivated alerts continuing to appear as
  acknowledged in the GUI by clearing stale
  `alert_acknowledgments` rows when an alert leaves
  the acknowledged state; the alerts API now also
  exposes a nullable `last_updated` RFC3339 timestamp
  and the StatusPanel surfaces it alongside
  "Triggered" when the two differ. (#64)
- Fix the cluster Topology tab "Add server" dropdown
  silently excluding servers that had been re-claimed
  by an auto-detected Spock cluster; the connections
  API now returns the `membership_source` field the
  client filter requires. (#25, #46)
- Fix dismissed auto-detected clusters reappearing
  after the collector's next auto-detection run;
  `UpsertAutoDetectedCluster` no longer clears the
  `dismissed` flag on rediscovery, and `GetCluster`
  now filters dismissed rows from single-cluster
  lookups. (#36)
- Fix dismissed auto-detected clusters reappearing in
  the Server creation dialog's cluster dropdown after
  alert or connection context was fetched; the
  connection hierarchy resolver now skips dismissed
  rows and no longer resurrects them through its
  upsert fallback. (#36)
- Fix partitions not being dropped at the appropriate
  time by the collector. (#62)
- Fix the copy-to-clipboard button in the Admin Tokens
  "Token created" dialog; the button now shows a check
  mark and "Copied!" tooltip on success and surfaces
  clipboard failures through the error alert. (#71)
- Fix the StatusPanel "Restore to active" action
  silently failing on error; the alert now leaves the
  acknowledged list optimistically, rolls back and
  surfaces a Snackbar error on API failure, and
  guards against double-click submissions. (#72)
- Fix servers assigned to a manually created cluster
  continuing to appear under a re-created
  auto-detected cluster after the next topology
  refresh; auto-detected Spock, binary-replication,
  and logical-replication grouping now skip
  connections with `membership_source = 'manual'`.
  (#74)
- Fix `GET /api/v1/connections` returning an empty
  array for scoped API tokens when the token owner's
  read access came from a wildcard group grant; the
  scoped connections are now returned as expected,
  and token scopes continue to restrict but not
  elevate the owner's privileges. (#83)

### Security

- Fix several REST endpoints and MCP tools leaking
  unshared connections owned by other users; the
  connection detail, database listing, connection
  context, cluster topology, alerts, alert
  acknowledgement, alert analysis, timeline, and
  current-connection endpoints now apply the same
  ownership, sharing, group, and token-scope checks
  as `GET /api/v1/connections`, returning
  `403 Forbidden` for single-resource requests and
  filtering list responses to the caller's visible
  connections. The `get_alert_history`,
  `get_metric_baselines`, and `get_blackouts` MCP
  tools applied no connection filter for callers
  with zero explicit grants; all three now restrict
  results to connections the caller is permitted to
  see. The OpenAPI specification and the static
  `docs/admin-guide/api/openapi.json` now document
  the `403 Forbidden` response on the affected
  single-resource endpoints. (#35)
- Fix server and cluster visibility leaks through the
  cluster list, cluster group, and cluster-membership
  REST endpoints; `GET /api/v1/clusters/list`,
  `GET /api/v1/clusters/{id}`,
  `GET /api/v1/clusters/{id}/servers`,
  `GET /api/v1/cluster-groups`, and
  `GET /api/v1/cluster-groups/{id}` now filter clusters,
  groups, and member servers to the caller's visible
  connections and return `404 Not Found` for clusters or
  groups that contain no visible members. (#35)
- Fix additional RBAC leaks surfaced by the follow-up
  security audit; the overview REST and SSE endpoints
  (`GET /api/v1/overview` and `GET /api/v1/overview/stream`)
  now verify scope visibility for scoped requests and
  filter `connection_ids` lists to the caller's visible
  set; the blackout list and get endpoints
  (`GET /api/v1/blackouts` and
  `GET /api/v1/blackouts/{id}`) hide blackouts whose
  referenced connection, cluster, or group is not
  visible to the caller; the alert, probe, and channel
  override list endpoints
  (`GET /api/v1/alert-overrides/{scope}/{scopeId}`,
  `GET /api/v1/probe-overrides/{scope}/{scopeId}`, and
  `GET /api/v1/channel-overrides/{scope}/{scopeId}`)
  return `404 Not Found` when the caller cannot see the
  requested scope; and the MCP connection resolver now
  checks RBAC before loading credentials and returns a
  generic "connection not found or not accessible"
  message for both missing and denied cases. (#35)
- Fix remaining RBAC leaks flagged by the third-round
  security audit; the `query_metrics` and `get_alert_rules`
  MCP tools now check `CanAccessConnection` before any
  datastore read and return a generic "connection not
  found or not accessible" error for both missing and
  denied connections, closing the ID/name enumeration
  that the previous error path exposed. The overview
  scoped-snapshot path
  (`GET /api/v1/overview?scope_type=cluster|group`) now
  intersects the scope's member connection IDs with the
  caller's visible set and generates the summary from
  the intersection through the existing connections-
  summary path, so two callers with different visibility
  never share a cache entry; scope denial now returns
  `404 Not Found` to match sibling handlers. The blackout
  schedule list and get endpoints
  (`GET /api/v1/blackout-schedules` and
  `GET /api/v1/blackout-schedules/{id}`) apply the same
  visibility filter as the blackout endpoints, and the
  alert override context endpoint
  (`GET /api/v1/alert-overrides/context/{connectionId}/{ruleId}`)
  now gates on `scopeVisibleToCaller` before reading
  override hierarchy. The MCP connection resolver logs
  RBAC denials so operators can correlate without
  widening the caller-visible surface. (#35)

## [1.0.0-alpha3] - 2026-04-08

### Added

- Add a Docker publish workflow that builds and pushes
  multi-platform images to GitHub Container Registry
  on version tags and main branch pushes.
- Add a production Docker Compose configuration using
  pre-built images with resource limits and log
  rotation.
- Add a Docker deployment guide to the documentation.
- Add a favicon to the web client.
- Replace the SQLite driver with a pure-Go
  implementation to support CGO-free builds.

### Changed

- Improve blackout status indicators and require
  confirmation before deleting a blackout. (#34)
- Limit blackout scope options to relevant entries
  only. (#33)
- Allow servers from auto-detected clusters to be
  reassigned to manual clusters. (#46)
- Hide alert threshold links for users who lack the
  required permission. (#40)
- Display errors to users when fetching unassigned
  servers fails.

### Fixed

- Fix the blackout dialog refreshing unnecessarily
  when underlying components update. (#47)
- Fix missing browser refresh after certain
  navigation actions and prevent title wrapping. (#45)
- Fix the replication type not carrying through to
  the edit dialog. (#44)
- Fix multiple potential crashes after a network
  failure and recovery. (#43)
- Fix a serialization error when updating server
  details. (#42)
- Fix a crash when clicking an empty cluster. (#39)
- Fix inconsistent alert operator values. (#38)
- Fix auto-detected clusters reappearing after
  deletion. (#36)
- Fix the is_shared flag for servers not being
  respected in all cases. (#35)
- Fix connection error alerts ignoring active
  blackouts. (#32)
- Fix MCP write access to databases. (#29)
- Fix SSL settings being silently dropped when
  creating or updating a server.
- Fix various issues with the database summary popup.
- Fix various TypeScript type safety issues in the
  web client.

### Security

- Fix MCP memory tools bypassing RBAC checks; all
  authenticated users could access the datastore
  without proper authorization.

## [1.0.0-alpha2] - 2026-03-04

### Fixed

- Fix a crash in the Add Server dialog when no clusters
  exist in the database.

## [1.0.0-alpha1] - 2026-03-02

Initial release.
