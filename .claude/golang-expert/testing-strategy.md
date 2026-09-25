<!--
/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
-->

# Go Testing in This Repository

This file records how the Go test suites in `collector/`, `server/` and
`alerter/` are actually wired up. It deliberately omits general Go
testing advice; where a fact here disagrees with a Makefile, a workflow
or the code, the code wins and this file needs correcting.

## Makefile Targets

Each Go sub-project has the same target set; run them from the
sub-project directory, or `make test-all` from the repository root to
run every sub-project (including the client) in turn.

- `make test` runs `go test -race -v ./...` from `src/`.
- `make coverage` runs the same suite with `-coverprofile=coverage.out`,
  writes `src/coverage.html`, and fails when the total drops below
  `COVERAGE_MIN`, a ratchet pinned at the sub-project's current total.
  The 90% floor in `CLAUDE.md` applies to new and modified code; raise
  `COVERAGE_MIN` whenever a change lifts the total.
- `make lint` runs `golangci-lint run` from `src/`.
- `make fmt-check` fails if `gofmt -l .` lists any file.
- `make test-all` is `fmt-check coverage lint`; it does not also run
  `test`, because `coverage` already runs the full suite verbosely.
- `make killall` is a standalone, developer-invoked target that
  SIGTERMs every sub-project binary and `go run` process on the host,
  including a dev server started from `bin/`. It is not a prerequisite
  of `test` or `coverage` (issue #445): the test suites spawn no
  processes, so there is nothing for it to clean up.

The server and alerter pass `-p=1` to `go test` on both `test` and
`coverage`. Their integration tests in `internal/database`,
`internal/api` and `internal/tools` (server) share Postgres tables and
drop and recreate them, so concurrent per-package test binaries race.
The collector does not pass `-p=1`; each of its integration packages
creates its own uniquely named database instead.

Read the numeric coverage breakdown with
`cd <subproject>/src && go tool cover -func=coverage.out`.

## Environment Variables

Integration tests read their target from the environment and skip when
it is absent, so a bare `go test ./...` with nothing set exercises unit
tests only.

- `TEST_AI_WORKBENCH_SERVER` is the Postgres URL every sub-project
  honours. CI sets it to the job's service container.
- `TEST_DB_CONN` is a collector-only fallback, read when
  `TEST_AI_WORKBENCH_SERVER` is unset (see `integrationConnString` in
  `collector/src/probes/integration_helpers_test.go`).
- `SKIP_DB_TESTS`, when set to anything, skips database tests in the
  server and alerter even if a URL is configured.
- `TEST_AI_WORKBENCH_KEEP_DB=1` (or `true`) stops the collector
  integration suites dropping their per-run test database, and prints
  its name, for post-mortem inspection.

Both URL variables must point at `127.0.0.1`. The suites create, drop
and recreate tables including `connections`, `cluster_groups`,
`alerts`, `blackouts`, `metrics.*` and `schema_version` on whatever
they are pointed at. The alerter enforces this in both its
database-backed packages: `testdsn.Require` in
`alerter/src/internal/testdsn/testdsn.go` holds the only copy of the
rule, and the three-line `requireLocalTestDSN` wrappers in
`alerter/src/internal/database/test_dsn_guard_test.go` and
`alerter/src/internal/engine/test_dsn_guard_test.go` are the only places
either package reads `TEST_AI_WORKBENCH_SERVER`. It skips when
`SKIP_DB_TESTS` is set or the URL is empty, and fails hard, before
connecting, on a non-loopback host and on a database name outside
`ai_workbench`, `ai_workbench_pr<n>`, `ai_workbench_issue<n>`,
`ai_workbench_sess<n>` (the per-task databases that concurrent local
sessions use so their schema resets do not collide) and `postgres` (the
CI default). The loopback check is the real safety net and the name
check a convenience, so do not read more into the tag rule than that.
`Require` takes a small `TB` interface rather than `*testing.T`, which
is what lets `TestRequire` drive every skip and refusal through a
recorder; `TestAllowedTestDatabase` pins the name rule in both
directions, including numbered production-shaped names such as
`ai_workbench_prod2`. Route any new alerter integration entry point
through the helper rather than reading the variable again. The server
and collector suites still rely on the operator. Never derive a test URL from
`bin/ai-dba-server.yaml`.

## Getting a Database in a Test

Three patterns are in use; copy the one already used by the package you
are editing.

- Server packages connect with `pgxpool.New` to the configured URL,
  skip on ping failure, and wrap the pool in
  `database.NewTestDatastore(pool)` from
  `server/src/internal/database/test_helpers.go` (the `Datastore`
  fields are unexported, so tests in other packages cannot build one
  directly). They create only the tables the exercised path needs, for
  example `clusterGroupsTestSchema` in
  `server/src/internal/api/cluster_handlers_test.go`, rather than the
  full collector schema. `internal/metrics` tests build a fixture probe
  table under the `metrics` schema and drop it on cleanup.
- Collector packages create a fresh database per run named
  `ai_workbench_test_<yyyymmdd_hhmmss>_<micros>` (see
  `collector/src/database/schema_test.go` and the probes package
  `TestMain`), apply the real schema through `NewSchemaManager`, and
  drop the database at the end unless `TEST_AI_WORKBENCH_KEEP_DB` is
  set.
- Alerter tests build their environment in helpers such as
  `newDetectAnomaliesEnv` and `newFullTestDatastore`, which apply an
  integration schema with `DROP`/`CREATE` statements and therefore take
  their URL from `requireLocalTestDSN`.

`t.Parallel()` is not used in database-backed tests; the shared tables
make it unsafe.

## The SQLite Auth Store

`server/src/internal/auth` stores users, groups, tokens and permissions
in SQLite via `modernc.org/sqlite` and `database/sql`, so its SQL uses
`?` placeholders, not `$1`. Tests open a store in a temporary directory
with `auth.NewAuthStore` and must:

- Pass `auth.AuditKeyForTesting()` as the fourth argument. The store
  keys its audit hash chain (hash version 2) with a key production
  derives from the server secret via `auth.DeriveAuditKey`, and
  `NewAuthStore` refuses an empty one, because a store without a key
  cannot write a single audited change. The test helper returns a fixed
  non-secret key and skips the PBKDF2 work. It is an ordinary exported
  function rather than an `export_test.go` one, because callers span
  `api`, `tools`, `llmproxy` and `cmd/mcp-server`, so it ships in the
  production binary and panics via `testing.Testing()` if anything but
  a test binary calls it.
- Call `store.SetBcryptCostForTesting(t, bcrypt.MinCost)` immediately
  after construction. Production hashes at `DefaultBcryptCost` (12); a
  suite that creates hundreds of users at that cost has previously hit
  the ten-minute package timeout under `-race`. The helper also
  regenerates the store's timing-equaliser dummy hash at the lowered
  cost, so "user not found" paths stop costing a second each.
- Pair `NewAuthStore` with `store.Close()`, which stops the session
  cleanup goroutine as well as closing the database.
- Stop every `auth.NewRateLimiter` with `Stop()` (idempotent), or close
  the `AuthHandler` that owns it, to avoid leaking its cleanup
  goroutine.

A test that needs a database looking like one an older release left
behind calls `auth.SeedUnkeyedAuditLogForTesting`, which writes rows
hashed under the unkeyed version 1 rendering and optionally deletes one
to break the links. Nothing else in the tree writes such a row, by
design, and the helper carries the same `testing.Testing()` guard as
`AuditKeyForTesting` for the same reason. Assertions about the
append-only trigger must read `sqlite_master` through a raw
`sql.Open("sqlite", ...)` connection, because opening an `AuthStore`
runs `ensureAuditSchema`, which re-creates the trigger and would make
the assertion pass either way.

A test that expects `PurgeAuditEvents` to delete rows must build them
through the store (`recordAuditInOwnTx`, backdating `OccurredAt`), not
by raw INSERT: the purge verifies every row it removes and refuses a
prefix holding a forged row such as `insertAuditRowAtID` writes, or one
that does not start at genesis or the previous purge's recorded head.
From `cmd/mcp-server` tests, use `auth.SeedAuditEventForTesting`, which
does the same behind a `testing.Testing()` guard.

Tests in `server/src/cmd/mcp-server` that run a CLI command go through
`openAuthStoreCLI`, which resolves the audit key from the real server
secret file. `TestMain` in `main_test.go` replaces the `cliAuditKey`
function variable with a fixed key for the whole binary, so that the
suite does not pass or fail on whether the host happens to have
`/etc/pgedge/ai-dba-server.secret`; a test that exercises the
resolution itself restores `resolveCLIAuditKey` for its own scope.

## Handler Tests: Bearer Plus Context

Authenticated handlers in `server/src/internal/api` use two independent
surfaces: `getUserInfoCompat` validates the bearer token from the
`Authorization` header, and `rbacChecker.HasAdminPermission` reads the
user from the request context that middleware normally populates.
Handler-level tests bypass the middleware, so set both with the
existing helpers: `withBearer` (`cluster_handlers_test.go`) and
`withUser` or `withSuperuser` (`rbac_handlers_test.go`). Setting only
one produces 401s or 403s that look like permission bugs.
`setupUserWithPermission` in `rbac_integration_test.go` creates a user
holding a named permission. The gate-ordering tests for mutating
handlers are described in `rbac-patterns.md`.

## HTTP Handler Tests: Never Point at Unreachable URLs

The analysis paths build a real HTTP client through
`(*llmproxy.Config).BuildClientOptions`, which sets
`pgllm.Options.RequestTimeout` from `llm.timeout_seconds`; that setting
defaults to 120 seconds in `server/src/internal/config/config.go`. A
handler test that reaches a live provider endpoint, or an address that
accepts the connection without answering, therefore blocks for up to two
minutes instead of failing fast, and the cost lands on every `make test`
and `make coverage` run. A locally refused connection is the benign
case and returns at once.

Stub the endpoint with `httptest.NewServer` and return a minimal valid
body. The stubs in `server/src/internal/overview/generator_test.go` and
`server/src/internal/api/server_info_handlers_test.go` are the patterns
to copy; both decode the request body, which is also how the
`max_tokens` assertions described below are made.

When auditing a slow test, look for a base URL set to a literal
`http://localhost:...` or to a real provider address.

## AI Analysis Paths: Token Budget and Empty Responses

The non-streaming analysis paths, the estate and scoped overview in
`internal/overview/` and the server-info database analysis in
`internal/api/server_info_handlers.go`, share two conventions that
tests must uphold.

The first convention is that no caller hardcodes an output-token
budget. `(*llmproxy.Config).BuildClientOptions(temperature)` resolves
the budget internally from `llm.max_tokens` through
`AnalysisMaxTokens`, falling back to
`llmproxy.DefaultAnalysisMaxTokens`, which is 4096, when the setting is
unset, zero or negative. A caller that also stamps `MaxTokens` onto a
`pgllm.ChatRequest` must read `AnalysisMaxTokens` rather than a local
constant. Cover the configured, unset and negative cases, and assert
the value on the wire by decoding `max_tokens` from the request body
inside the `httptest` stub.

The second convention is that a response carrying no text block, or
whitespace-only text, is an error rather than an empty summary. Both
paths return `llmproxy.ErrNoTextContent`, which the server-info handler
maps to HTTP 502. Reasoning models charge their thinking blocks against
the same budget as the answer, so this is the shape a starved budget
produces; reproduce it with a content block of type `"thinking"`, for
which the library exposes no named constant, or with an empty assistant
message. The alerter mirrors both conventions in
`alerter/src/internal/llm/reasoning.go`, using
`config.DefaultLLMMaxTokens` and its own `ErrNoTextContent`.

## Linting

Every sub-project runs golangci-lint v2 (`version: "2"` configs; note
the server's lives at `server/src/.golangci.yml`, whilst the collector
and alerter keep theirs at `collector/.golangci.yml` and
`alerter/.golangci.yml`). All three enable `errcheck`, `gosec`,
`govet` (all analysers bar `fieldalignment` and `shadow`),
`ineffassign`, `misspell` (US locale), `staticcheck` and `unused`; the
server additionally enables `gocritic`. `gosec` is excluded for
`_test.go` files everywhere. Codacy's Opengrep runs separately from
golangci-lint; a false positive it raises is cleared with a
`// nosemgrep: <rule>` line directly above the flagged call, never
with `//nolint` (see `metrics-queries.md` and
`internal-query-markers.md`).

CI installs the linter with `go install
github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`, so the
version is unpinned and moves with upstream releases. A locally
installed older build can pass code that CI rejects: gosec's G7xx taint
rules, G704 (SSRF) among them, are absent from the gosec v2.22.8 that
golangci-lint v2.5.0 bundles and present in the gosec v2.28.0 that
v2.13.2 bundles. Match the CI version before trusting a clean local
`make lint` on new network or SQL code.

## Continuous Integration

`ci-collector.yml`, `ci-server.yml` and `ci-alerter.yml` run the
sub-project's `make build`, `make coverage` and `make lint` on a single
Go version against a matrix of Postgres 14 to 18 service containers
built from the `pgvector/pgvector` image, with
`TEST_AI_WORKBENCH_SERVER` pointed at the service. Coverage is
converted to lcov and uploaded to Codacy as a partial report per
sub-project; `coverage-finalize.yml` closes the set. `ci-client.yml`
runs `npm test`, `npm run test:coverage` and `npm run build`. The
remaining workflows (`ci-e2e.yml`, `ci-docker.yml`, `ci-docs.yml`,
`ci-walkthrough.yml`, `docker-publish.yml`, `release.yml`) do not run
the Go unit suites. Check the workflow file for the current Go version
rather than recording it here.

`make test-e2e` at the repository root runs the Playwright smoke suite
in `e2e/` against a Dockerised Postgres; it is intentionally not part
of `make test-all`.

## Testing Command-Line Flag Precedence

Configuration is layered as defaults, then configuration file, then
command-line flags. Whether a flag overrides the file is decided by
whether the operator actually passed it, never by comparing the
flag's value against its default; a value comparison silently drops
an explicitly passed default value and clobbers a file value that
happens to match a default.

The shared helper `pkg/flagutil` provides `Passed(fs *flag.FlagSet)
Set`, backed by `flag.FlagSet.Visit`, and `Set.Has(name)`. The
collector threads a `flagutil.Set` into `loadConfiguration` and
`(*Config).ApplyFlags`; the alerter carries one in the `Passed`
field of its `flagOverrides` struct, which `applyFlagOverrides` and
the SIGHUP reload path both consume. Both binaries declare their
flag names as constants, so registration and lookup cannot drift.

Tests construct the set directly rather than parsing a command
line, so both branches of every override are reachable:

```go
// The flag was passed, carrying the value that is also its default.
cfg.ApplyFlags(flagutil.Set{flagPGPort: true})

// Nothing was passed, so the config file value must survive.
cfg.ApplyFlags(nil)
```

Any new flag needs both cases covered: a configuration value that
coincides with the flag's default must survive when the flag is not
passed, and the flag passed with its default value must still win.

A server setting whose zero value is meaningful is held as a `*int`
or `*bool` with an accessor that applies the default, because
`mergeConfig` decides whether a later source overrides an earlier one
by testing the raw value: an omitted key arrives as the zero value and
a guard such as `>= 0` then overwrites the default with it. This is
what silently disabled account lockout (#473), and
`http.auth.audit_retention_days` and `http.auth.local.enabled` follow
the same shape. Tests cover the accessor's nil, negative and explicit
zero cases, and, where the value is consumed once at start-up, also
assert on the effective value observed at the consumer rather than on
the configuration field, since reading the raw pointer at the call
site still compiles (see
`TestInitAuthStoreAppliesEffectiveLockoutThreshold` in
`server/src/cmd/mcp-server/lockout_wiring_test.go`). Any such setting
also belongs in `logRestartRequiredSettings` in
`server/src/internal/config/reload.go`, which compares effective
values, so a SIGHUP reports a change it cannot apply instead of
reporting success.

Config-file loading has a second trap that shows up as dead test
coverage. `(*Config).LoadFromFile` wraps the `os.ReadFile` error with
`%w`, so `os.IsNotExist` never matches it; the missing-file branches
in the collector's `loadConfiguration` use `errors.Is(err,
fs.ErrNotExist)`, which unwraps. Prefer `errors.Is` over the
`os.IsNotExist` and `os.IsExist` predicates anywhere the error may
have passed through a `%w` wrap.

The "auto-discovered file vanished between discovery and load" branch
is only reachable deterministically through the package-level seam
`discoverDefaultConfigPath` in `collector/src/main.go`, which defaults
to `GetDefaultConfigPath`. A test replaces it with a function that
discovers the real path, removes the file, and returns the path, so
the load then fails with `fs.ErrNotExist` and the collector must fall
back to compiled-in defaults without error (see
`TestLoadConfiguration_AutoDiscoveredVanished`). Tests driving
discovery also need `t.Setenv` for `XDG_CONFIG_HOME`, `HOME` and
`AppData` plus `fileutil.SetSystemConfigDirForTest`, so no real
`/etc/pgedge` content leaks in.
