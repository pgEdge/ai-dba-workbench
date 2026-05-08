# E2E Smoke Tests

End-to-end smoke tests for the AI DBA Workbench web client. These
tests boot the **production client bundle** in a real browser
against a real server + Postgres so they catch the class of bug
that unit tests cannot — for example, ESM resolution differences
between Vitest and Vite's production build.

See `.github/workflows/ci-e2e.yml` for the CI invocation and
`.claude/specs/2026-05-08-e2e-smoke-tests-design.md` for the design
rationale.

## Running locally

You need Docker (for an ephemeral Postgres on port 55432), Go, and
Node.js. Browsers are installed via `make install-browsers` once.

```sh
cd e2e
npm install
make install-browsers   # one-time
./run-local.sh          # builds + brings up stack + runs all tests
```

To run a single browser only:

```sh
./run-local.sh --project=chromium
```

To keep the stack up after the run for debugging:

```sh
E2E_KEEP_STACK=1 ./run-local.sh
# explore the running stack
e2e/scripts/stop-stack.sh && docker rm -f ai-dba-e2e-postgres
```

Logs are written to `e2e/.runtime/logs/{server,preview}.log`.

## Running in CI

The `.github/workflows/ci-e2e.yml` workflow runs the suite on every
PR and push to main, with a Postgres service container and a
matrix of Chromium/Firefox/WebKit. Server integration coverage from
the chromium leg is uploaded as the `server-coverage-e2e` artifact
and merged into the existing Codacy partial-upload pipeline by
`coverage-finalize.yml`.
