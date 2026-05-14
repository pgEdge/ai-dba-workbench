# Changelog

All notable changes to the pgEdge AI DBA Workbench are
documented in this file.

The format is based on
[Keep a Changelog](https://keepachangelog.com/), and this
project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
