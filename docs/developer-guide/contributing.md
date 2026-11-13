# Contributing

This guide provides instructions for developers who want
to contribute to the pgEdge AI DBA Workbench project. The
guide covers setting up a development environment,
building and testing the project, and submitting
contributions.

## Prerequisites

Before starting development, install the following tools:

- [Go 1.26.11](https://go.dev/doc/install) or a later 1.26 release, or
  Go 1.27.4 or later, for building server-side components.
- [Node.js 22.12](https://nodejs.org/) or later for building the web
  client.
- [PostgreSQL 15](https://www.postgresql.org/download/)
  or later for running tests.
- [Git](https://git-scm.com/) for version control.
- [Make](https://www.gnu.org/software/make/) for build
  automation.

Install the Go linter with the following command:

```bash
go install \
  github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

Add the Go bin directory to your PATH:

```bash
# Add to your ~/.bashrc, ~/.zshrc, or ~/.zprofile
export PATH="$PATH:$(go env GOPATH)/bin"
```

## Getting Started

Clone the repository from
[GitHub](https://github.com/pgEdge/ai-dba-workbench):

```bash
git clone \
  https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench
```

## Building

Build all components from the top-level directory:

```bash
make all
```

The build process compiles all Go binaries and places
them in the `bin/` directory. Build individual components
by changing to the component directory:

```bash
cd collector && make build
cd server && make build
```

Build the web client from the `ai-dba-workbench` repository root. In the
following example, the `cd client` command enters the client directory;
the `npm install` command installs dependencies; the `npm run build`
command builds the client:

```bash
cd client && npm install && npm run build
```

## Testing

The project uses comprehensive unit tests for all
components. Run all tests from the top-level directory:

```bash
make test-all
```

The `test-all` target runs the `test-all` target of each
sub-project in turn: `pkg`, `collector`, `server`, `alerter`,
and `client`. The collector, server, and alerter targets
check formatting, run the tests once with the coverage gate,
and run the linter. The client target runs the tests with the
coverage gate, the type checker, and the linter. The `pkg`
target checks formatting and runs the tests and the linter
without a coverage gate. Run individual test targets as
needed:

```bash
# Run tests only
make test

# Run coverage analysis
make coverage

# Run linting
make lint
```

While you develop, run only the tests that cover your change;
for example, the following commands run one Go test and the
Vitest tests related to the named client files:

```bash
cd server/src && go test -race -run TestName ./internal/api/...
cd client && npx vitest related src/components/Login.tsx
```

### Testing Only Changed Sub-Projects

The `test-changed` target runs `test-all` only in the
sub-projects that the current branch touches. The target
compares the branch with the point at which the branch
forked from `origin/main`, and also includes staged,
unstaged, and untracked changes in the working tree. The
selected sub-projects run in the same order as `test-all`,
and the first failure stops the run.

A change to some paths also tests the sub-projects that
depend on them. The following table shows which
sub-projects each change selects:

| Changed path                                  | Sub-projects tested                       |
|-----------------------------------------------|-------------------------------------------|
| `pkg/`                                        | pkg, collector, server, and alerter       |
| `collector/go.mod`, `collector/src/database/` | collector and server                      |
| Any other path under `collector/`             | collector                                 |
| `server/`, `alerter/`, or `client/`           | That sub-project only                     |
| `e2e/`                                        | None; prints a reminder to run test-e2e   |

Run the target from the repository root:

```bash
make test-changed
```

Set `BASE` to compare against a different reference, and set
`DRY_RUN=1` to print the selection without running any tests.
In the following example, a dry run on a branch that changes
the server, alerter, and client prints the plan:

```bash
DRY_RUN=1 make test-changed
```

The command produces output similar to the following:

```text
test-changed: comparing against origin/main (merge base e6bb269828c9) plus uncommitted changes
test-changed: selected sub-projects:
  server     server/ changed
  alerter    alerter/ changed
  client     client/ changed
test-changed: DRY_RUN set; not running any tests.
```

The full `make test-all` remains available, and CI runs the
complete test matrix on every push to `main`.

### Continuous Integration

The GitHub Actions workflows run a reduced matrix on pull
requests and the full matrix on `main`. In the workflows:

- pull requests run the collector, server, and alerter test
  suites on two combinations: Go 1.26.11 with PostgreSQL 18,
  and Go 1.27.4 with PostgreSQL 15.
- pushes to `main`, a nightly schedule, and manual runs test
  both Go releases against PostgreSQL 15, 16, 17, and 18.
- the shared `pkg` module needs no database, so every run
  tests the module with both Go releases.
- the client test suite runs on Node.js 22 only; Node.js 24
  and 26 type-check and build the client.
- a newer push to a pull request cancels the earlier run.
- the Docker and E2E workflows skip changes that touch only
  test files.
- the Docker Publish workflow builds pull request images for
  `linux/amd64` only, and skips pull requests that cannot
  change an image.

### Test Database

Tests that require a database create a temporary database
with a timestamp in the name. Set the connection string
using an environment variable:

```bash
export TEST_AI_WORKBENCH_SERVER=\
  "postgres://user:pass@localhost/postgres"
```

The server and alerter test packages run in parallel. Each
package that uses the database creates a database of its own,
named after the database in `TEST_AI_WORKBENCH_SERVER` with
`_pkg_` and the package name appended (for example,
`ai_workbench_pkg_api`). The package drops the database when
its tests finish.

The test database drops automatically after tests
complete. Set the keep flag to preserve the database for
inspection:

```bash
export TEST_AI_WORKBENCH_KEEP_DB=1
```

### Component-Specific Tests

Run tests for a specific component by changing to the
component directory:

```bash
cd collector && make test
cd server && make test
```

See the component development guides for detailed
testing information:

- [Collector Testing Guide](collector/testing.md)
- [Alerter Testing Guide](alerter/testing.md)

## Code Style

The project follows these coding standards:

- Use four spaces for indentation in all source files.
- Run `gofmt` on all Go files before committing.
- Follow Go conventions for naming and code organization.
- Write readable, modular, and well-documented code.
- Include unit tests for all new functions and features.

### Copyright Header

Include the following copyright header at the top of
every source file:

```go
/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
```

Adjust the comment style for non-Go languages. Do not
include the header in configuration files.

### Go Code

Follow these Go-specific guidelines:

- Export types and functions using PascalCase naming.
- Use camelCase for private functions and variables.
- Add doc comments to all exported types and functions.
- Handle all errors and provide context using
  `fmt.Errorf` with `%w`.
- Run `gofmt` and `go vet` before committing.

#### Transaction Rollbacks

Roll every pgx transaction back through the shared
`github.com/pgedge/ai-workbench/pkg/rollback` helper,
never by calling `Rollback` on the transaction directly:

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return fmt.Errorf("begin transaction: %w", err)
}
defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit
```

Use `rollback.ToSavepoint(ctx, tx, name)` in place of a
hand-written `ROLLBACK TO SAVEPOINT` statement. Both
functions run the rollback on a context derived from
`ctx` with `context.WithoutCancel` and a
`rollback.Timeout` deadline of five seconds. When a
rollback is given a request context that the client has
already cancelled, the pgx v5 driver fails the rollback
and closes the connection; the server then ends the
transaction itself, so the cost is connection churn and
a lost pool slot whilst the pool reconnects. A plain
`context.Background()` avoids that but has no deadline,
so a rollback to a hung server could pin a pool slot and
the goroutine indefinitely; the helper bounds the wait
and keeps the request's tracing values. The statements
inside the transaction still use the request context;
only the rollback differs. A convention test in each Go
module rejects direct `Rollback` calls and hand-written
`ROLLBACK` SQL outside the helper package; the package
doc comment in `pkg/rollback` gives the full reasoning.

### Documentation

Follow the documentation style guide in
[CLAUDE.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/CLAUDE.md):

- Use active voice in all documentation.
- Write sentences between 7 and 20 words.
- Wrap markdown files at 79 characters.
- Use one first-level heading per file.
- Include an introductory sentence after each heading.

## Submitting Contributions

We welcome contributions from the community. Follow
these steps to submit a contribution.

### 1. Create a Feature Branch

Create a branch for your changes:

```bash
git checkout -b feature/your-feature-name
```

### 2. Make Your Changes

Implement your changes following the code style
guidelines. Add tests for any new functionality and
update documentation as needed.

### 3. Run Quality Checks

Run the tests for the sub-projects your branch changes
before committing:

```bash
make test-changed
```

Run `make test-all` instead to test every sub-project.

The test suite must pass without errors or warnings
before you submit a pull request.

### 4. Commit Your Changes

Write clear commit messages that explain what changed
and why:

```
Short summary (50 characters or less)

More detailed explanation if needed. Wrap at 72
characters. Explain what changed and why, not how
(the code shows how).

- Use bullet points for multiple changes
- Use present tense ("Add feature" not "Added feature")
- Reference issues: "Fixes #123"
```

### 5. Submit a Pull Request

Push your branch to GitHub and open a pull request:

```bash
git push origin feature/your-feature-name
```

Provide a clear description of your changes in the pull
request. Reference any related issues and describe how
you tested your changes.

### 6. Address Review Feedback

Respond to code review comments and make requested
changes. Push additional commits to your branch to
update the pull request.

## Reporting Issues

To report a bug or request a feature, create an issue
on GitHub:

[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

Include the following information in bug reports:

- A clear description of the problem.
- Steps to reproduce the issue.
- Expected and actual behavior.
- Component version and environment details.

## License

This project is licensed under the
[PostgreSQL License](https://github.com/pgEdge/ai-dba-workbench/blob/main/LICENSE.md).

By contributing to the project, you agree that your
contributions will be licensed under the same license.
