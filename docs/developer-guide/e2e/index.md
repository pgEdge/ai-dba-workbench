# End-to-End Smoke Tests

The pgEdge AI DBA Workbench includes a Playwright-based
end-to-end smoke-test suite that exercises the production client
bundle in a real browser. The suite catches the class of bug that
component-level unit tests cannot, such as ESM-resolution
differences between Vite's production build and Vitest's jsdom
environment.

## Overview

The suite lives under the top-level `e2e/` directory and runs on
every pull request through the `CI - E2E` workflow. Playwright
drives Chromium, Firefox, and WebKit against a `vite preview`
serving the production client bundle; the preview proxies API
traffic to a real `ai-dba-server` binary that talks to a real
Postgres database. The matrix runs all three browsers in parallel,
each in its own job.

The architecture deliberately mirrors production as closely as
possible. The server uses a cover-instrumented build so that
Go integration coverage flows back into the existing Codacy
pipeline. The client is the same `npm run build` output that
ships in the Docker image; tests do not use a development server.

## Coverage Scope

The v1 suite focuses on production-bundle smoke coverage and
omits broader functional testing. The smoke tests are not a
replacement for unit tests; they exist to catch the bugs that
unit tests structurally cannot.

The suite covers:

- The login flow renders correctly, accepts valid credentials,
  and rejects invalid credentials.
- The post-login application shell renders the header, cluster
  navigator, chat FAB, and admin-panel trigger without
  triggering the React error boundary.
- The admin panel opens each of its ten permission-gated
  sections (users, groups, permissions, token scopes, probes,
  alert rules, and the four notification-channel sections)
  without crashing or emitting console errors.

The suite does not cover:

- Real-data flows that depend on a populated datastore; no
  collector or target Postgres is wired up during the run.
- Admin CRUD form submission; the suite only verifies that
  sections open and render their initial content.
- Browser-side JavaScript coverage; only server-side Go
  integration coverage is collected.

The motivating example for the suite is commit `aa28aa8`, where
Vite's production ESM resolver disagreed with Vitest's jsdom
resolver. Unit tests passed; the production build crashed on
every icon click. A real browser running a production bundle is
the only environment that catches that bug class.

## Running Locally

The suite requires Docker for the ephemeral Postgres container,
plus Go and Node.js to build the server and client. Install the
Playwright browsers once with `make install-browsers`.

The simplest invocation runs everything from the repository
root:

```bash
make test-e2e
```

This target is intentionally not part of `make test-all` because
the suite is slow and requires Docker. For an interactive
workflow, work inside the `e2e/` directory:

```bash
cd e2e
./run-local.sh
```

To scope the run to a single browser, pass a Playwright project
filter to `run-local.sh`:

```bash
./run-local.sh --project=chromium
```

To keep the stack running after the suite finishes so the
running application can be inspected, set `E2E_KEEP_STACK=1`:

```bash
E2E_KEEP_STACK=1 ./run-local.sh
```

After the run finishes, the script prints the commands needed
to tear the stack down manually.

## Running in CI

The `.github/workflows/ci-e2e.yml` workflow runs the suite on
every pull request against `main` and on every push to `main`.
The workflow uses a Postgres service container and a three-way
browser matrix that runs Chromium, Firefox, and WebKit in
parallel.

Each leg checks out the repository, sets up Go and Node.js,
builds the server with coverage instrumentation, builds the
collector, builds the client, installs the Playwright browser
for the matrix value, and runs the suite. Failed runs upload
the Playwright HTML report and the server and preview logs as
artifacts; successful runs upload the Playwright report only.

The chromium leg has one extra responsibility: it uploads the
Go integration coverage collected from the running server as
the `server-coverage-e2e` artifact. On pushes to `main`, the
same leg converts the coverage to LCOV and submits it to
Codacy as a partial Go report. The existing
`coverage-finalize.yml` workflow merges the partial with the
unit-test partial so total Go coverage reflects both sources.

## Stack Bring-Up

The `e2e/scripts/start-stack.sh` script brings the test stack
up in the correct order; both `run-local.sh` and the CI
workflow delegate to it. The script performs the following
steps:

1. The script waits for Postgres to accept connections.
2. The script renders the server configuration and a random
   shared secret into `e2e/.runtime/`.
3. The script applies the collector datastore schema by
   building and briefly running the collector binary so its
   embedded schema manager creates the operational tables that
   the server reads.
4. The script launches the cover-instrumented server with
   `GOCOVERDIR` pointing at `.runtime/cov`.
5. The script waits for the server's `/health` endpoint.
6. The script bootstraps the initial admin user with
   `add-user` and `set-superuser`.
7. The script starts `vite preview` from the client directory,
   proxying `/api/v1/*` to the server.
8. The script waits for the preview server to respond, then
   prints `STACK_READY`.

The collector schema application in step three is mandatory.
Without the operational tables that the collector owns (such
as `cluster_groups`, `alerts`, and `blackouts`), the
post-login dashboard endpoints return HTTP 500 and Playwright
captures spurious console errors.

## Server Coverage Integration

The server binary used in the suite is built with
`go build -cover -covermode=atomic -coverpkg=./...`. The
`-coverpkg=./...` flag instruments every package in the server
module, not just the `cmd/mcp-server` entry point; without it
the integration-coverage signal is near-zero because handler
packages such as `internal/api` are not in the default
instrumentation set.

The server installs a SIGTERM and SIGINT handler in
`cmd/mcp-server` that calls `coverage.WriteCountersDir` before
the process exits. The flush is necessary because Go's cover
runtime writes counter data on normal exit but not on signal
termination; the stack-tear-down script sends SIGTERM, so
without the handler the per-run coverage data is lost.

The chromium leg of the CI matrix uploads the
`${GOCOVERDIR}` directory as the `server-coverage-e2e`
artifact. On pushes to `main`, the leg converts the coverage
into LCOV with `gcov2lcov` and uploads it to Codacy with the
`--partial` and `-l Go` flags. The downstream
`coverage-finalize.yml` workflow merges this partial with the
unit-test partial uploaded by `ci-server.yml`, producing a
single Go coverage figure that reflects both sources.

## Adding a New Test

The suite source lives in `e2e/tests/`. Each spec file
follows the existing pattern: import `test` and `expect` from
the shared `../fixtures/error-boundary` fixture, declare
`test.use({ storageState: '.auth/admin.json' })` if the test
requires a logged-in session, and exercise the application
through `data-testid` selectors.

The shared fixture provides two assertions that every test
should call:

- The `assertNoErrorBoundary` assertion verifies that the
  React error boundary has not rendered its fallback UI.
- The `assertNoConsoleErrors` assertion verifies that no
  unexpected `console.error` or `pageerror` events fired
  during the test.

The `auth.setup.ts` project writes `.auth/admin.json` once
per run; the chromium, firefox, and webkit projects depend on
the setup project so the storage state is always available.
New tests that need a logged-in session should reuse this
storage state rather than logging in inline.

In the following example, a new spec verifies that opening
the alert panel does not crash:

```typescript
import { test, expect } from '../fixtures/error-boundary';

test.use({ storageState: '.auth/admin.json' });

test('alert panel opens without crashing', async ({
    page,
    assertNoErrorBoundary,
    assertNoConsoleErrors,
}) => {
    await page.goto('/');
    await page.getByTestId('alert-panel-trigger').click();
    await expect(page.getByTestId('alert-panel'))
        .toBeVisible({ timeout: 10_000 });
    await assertNoErrorBoundary();
    assertNoConsoleErrors();
});
```

See `e2e/tests/admin-panel.spec.ts` for a parameterised
example that iterates over every admin section.

## Troubleshooting

The most common local-run failures stem from environment
collisions rather than test-logic bugs. The following
overrides resolve the typical causes.

Port conflicts on `4173`, `8080`, or `55432` produce
`EADDRINUSE` errors during stack bring-up. Override the
defaults to free ports before invoking the runner:

```bash
E2E_SERVER_PORT=18080 \
    E2E_PREVIEW_PORT=14173 \
    E2E_DB_PORT=15432 \
    ./run-local.sh
```

A missing Docker daemon causes the Postgres container step to
fail with a connection error to the Docker socket. Start
Docker Desktop or the equivalent platform service before
running the suite.

Missing Playwright browsers produce an "executable doesn't
exist" error on the first test step. Install the browsers
from the `e2e/` directory:

```bash
cd e2e
make install-browsers
```

A failed run leaves logs in `e2e/.runtime/logs/`; the
`server.log`, `preview.log`, and `collector-migrate.log`
files capture the most useful diagnostic context. Use
`E2E_KEEP_STACK=1` to keep the stack alive after the run for
interactive debugging.
