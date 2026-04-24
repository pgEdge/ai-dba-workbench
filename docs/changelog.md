# Changelog

All notable changes to the pgEdge AI DBA Workbench are
documented in this file.

The format is based on
[Keep a Changelog](https://keepachangelog.com/), and this
project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

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

### Security

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

### Fixed

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
