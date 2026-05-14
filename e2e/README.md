# pgEdge AI DBA Workbench E2E Smoke Tests

[![CI - E2E](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-e2e.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-e2e.yml)

The pgEdge AI DBA Workbench E2E suite boots the production
client bundle in a real browser against a real server and
Postgres instance. The suite catches bugs that unit tests
cannot detect, such as ESM-resolution differences between
Vite's production build and Vitest's jsdom environment.

For complete documentation, visit
[docs.pgedge.com](https://docs.pgedge.com).

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Documentation](#documentation)

## Features

The suite provides the following capabilities:

- The login spec exercises the authentication flow against a
  real server with both valid and invalid credentials.
- The application-shell spec verifies that the post-login
  layout renders without triggering the React error boundary.
- The admin-panel spec parameterises over each admin section
  and verifies that every section opens without console
  errors.
- The Playwright matrix runs the suite against Chromium,
  Firefox, and WebKit in parallel.
- The server runs under `GOCOVERDIR` so Go integration
  coverage flows back into the existing Codacy pipeline.

## Prerequisites

Before running the suite, ensure you have the following:

- [Docker](https://docs.docker.com/get-docker/) for the
  ephemeral Postgres container on port `55432`.
- [Go 1.24](https://go.dev/doc/install) or later to build
  the server and collector binaries.
- [Node.js 24](https://nodejs.org/) or later to build the
  client bundle and drive Playwright.

## Installation

Clone the repository and install the suite's dependencies:

```bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench/e2e
npm install
make install-browsers
```

The `make install-browsers` target downloads the Playwright
browsers with their OS dependencies; the step is required
once per workstation.

## Configuration

The suite reads its configuration from environment variables
with sensible defaults; the table below lists the most
common overrides.

| Variable             | Default            | Purpose                  |
|----------------------|--------------------|--------------------------|
| `E2E_DB_PORT`        | `55432`            | Ephemeral Postgres port. |
| `E2E_SERVER_PORT`    | `8080`             | The `ai-dba-server` port.|
| `E2E_PREVIEW_PORT`   | `4173`             | The `vite preview` port. |
| `E2E_KEEP_STACK`     | unset              | Keep the stack running.  |
| `E2E_COLLECTOR_BIN`  | `bin/ai-dba-collector` | Cached collector path. |

The `run-local.sh` script renders the server and collector
YAML files into `e2e/.runtime/` from a freshly generated
shared secret; the runtime directory is gitignored.

## Usage

The simplest invocation runs the full suite from the
repository root via the top-level Makefile:

```bash
make test-e2e
```

The target is intentionally absent from `make test-all`
because the suite is slow and requires Docker.

For an interactive workflow, work inside the `e2e/`
directory and invoke the runner directly:

```bash
cd e2e
./run-local.sh
```

To scope the run to a single browser, pass a Playwright
project filter:

```bash
./run-local.sh --project=chromium
```

To keep the stack running after the suite finishes so the
running application can be inspected, set `E2E_KEEP_STACK`:

```bash
E2E_KEEP_STACK=1 ./run-local.sh
```

After an interactive run, tear the stack down manually:

```bash
./scripts/stop-stack.sh
docker rm -f ai-dba-e2e-postgres
```

The runner writes diagnostic logs to
`e2e/.runtime/logs/server.log`, `preview.log`, and
`collector-migrate.log`; the files persist after the run
even on success.

## Documentation

The developer guide at
[docs/developer-guide/e2e/index.md](../docs/developer-guide/e2e/index.md)
covers the suite in detail.

The guide includes the following topics:

- The stack bring-up sequence and the rationale behind
  each step.
- The server coverage integration with Codacy.
- The procedure for adding a new spec, including the
  shared error-boundary fixture.
- The most common local-run failures and their
  resolutions.

---

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

We welcome your project contributions; for more information,
see [docs/developer-guide/contributing.md][contributing].

For more information, visit
[docs.pgedge.com](https://docs.pgedge.com).

This project is licensed under the [PostgreSQL License][license].

[contributing]: https://github.com/pgEdge/ai-dba-workbench/blob/main/docs/developer-guide/contributing.md
[license]: https://github.com/pgEdge/ai-dba-workbench/blob/main/LICENSE.md
