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
they are pointed at. The alerter engine tests enforce this with
`assertLocalTestDSN` in `alerter/src/internal/engine/baselines_test.go`,
which fails hard on a non-local host; the other suites rely on the
operator. Never derive a test URL from `bin/ai-dba-server.yaml`.

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
- Alerter engine tests build their environment in helpers such as
  `newDetectAnomaliesEnv`, which apply an integration schema with
  `DROP`/`CREATE` statements and therefore guard with
  `assertLocalTestDSN`.

`t.Parallel()` is not used in database-backed tests; the shared tables
make it unsafe.

## The SQLite Auth Store

`server/src/internal/auth` stores users, groups, tokens and permissions
in SQLite via `modernc.org/sqlite` and `database/sql`, so its SQL uses
`?` placeholders, not `$1`. Tests open a store in a temporary directory
with `auth.NewAuthStore` and must:

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
