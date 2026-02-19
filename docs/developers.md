# Developer Guide

This guide provides information for developers who want to contribute to the
pgEdge AI DBA Workbench project. The guide covers setting up a development
environment, building and testing the project, and submitting contributions.

## Prerequisites

Before starting development, install the following tools:

- [Go 1.23+](https://go.dev/doc/install) for building server-side components.
- [Node.js 18+](https://nodejs.org/) for building the web client.
- [PostgreSQL 14+](https://www.postgresql.org/download/) for running tests.
- [Git](https://git-scm.com/) for version control.
- [Make](https://www.gnu.org/software/make/) for build automation.

Install the Go linter:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

Add the Go bin directory to your PATH:

```bash
# Add to your ~/.bashrc, ~/.zshrc, or ~/.zprofile
export PATH="$PATH:$(go env GOPATH)/bin"
```

## Getting Started

Clone the repository from GitHub:

```bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench
```

The project consists of four main components:

- The collector is a data collection service written in Go.
- The server is an MCP server written in Go.
- The alerter is an alert monitoring service written in Go.
- The client is a web client written in React/TypeScript.

Each component has its own directory with source code, tests, and a Makefile.

## Building

Build all components from the top-level directory:

```bash
make all
```

The build process compiles all Go binaries and places them in the `bin/`
directory. Build individual components by changing to the component directory:

```bash
cd collector && make build
cd server && make build
```

Build the web client:

```bash
cd client && npm install && npm run build
```

## Testing

The project uses comprehensive unit tests for all components. Run all tests
from the top-level directory:

```bash
make test-all
```

The `test-all` target runs tests, coverage analysis, and linting for all Go
components. Run individual test targets as needed:

```bash
# Run tests only
make test

# Run coverage analysis
make coverage

# Run linting
make lint
```

### Test Database

Tests that require a database create a temporary database with a timestamp in
the name. Set the connection string using an environment variable:

```bash
export TEST_AI_WORKBENCH_SERVER="postgres://user:pass@localhost/postgres"
```

The test database is dropped automatically after tests complete. To preserve
the database for inspection, set the keep flag:

```bash
export TEST_AI_WORKBENCH_KEEP_DB=1
```

### Component-Specific Tests

Run tests for a specific component by changing to the component directory:

```bash
cd collector && make test
cd server && make test
```

See the component development guides for detailed testing information:

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

Include the following copyright header at the top of every source file:

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

Adjust the comment style for non-Go languages. Do not include the header in
configuration files.

### Go Code

Follow these Go-specific guidelines:

- Export types and functions using PascalCase naming.
- Use camelCase for private functions and variables.
- Add doc comments to all exported types and functions.
- Handle all errors and provide context using `fmt.Errorf` with `%w`.
- Run `gofmt` and `go vet` before committing.

### Documentation

Follow the documentation style guide in CLAUDE.md:

- Use active voice in all documentation.
- Write sentences between 7 and 20 words.
- Wrap markdown files at 79 characters.
- Use one first-level heading per file.
- Include an introductory sentence after each heading.

## Project Structure

The repository follows this structure:

```
ai-dba-workbench/
├── alerter/           # Alert monitoring service
│   ├── src/          # Source code
│   └── README.md     # Component readme
├── client/           # Web client (React/TypeScript)
│   ├── src/          # Source code
│   └── README.md     # Component readme
├── collector/        # Data collector service
│   ├── src/          # Source code
│   └── README.md     # Component readme
├── server/           # MCP server
│   ├── src/          # Source code
│   └── README.md     # Component readme
├── pkg/              # Shared Go packages
├── docs/             # Unified documentation
├── examples/         # Example configurations
├── DESIGN.md         # System architecture
├── CLAUDE.md         # Development guidelines
└── README.md         # Project overview
```

## Contributing

We welcome contributions from the community. Follow these steps to submit
a contribution:

### 1. Create a Feature Branch

Create a branch for your changes:

```bash
git checkout -b feature/your-feature-name
```

### 2. Make Your Changes

Implement your changes following the code style guidelines. Add tests for
any new functionality and update documentation as needed.

### 3. Run Quality Checks

Run the full test suite before committing:

```bash
make test-all
```

The test suite must pass without errors or warnings before you submit a
pull request.

### 4. Commit Your Changes

Write clear commit messages that explain what changed and why:

```
Short summary (50 characters or less)

More detailed explanation if needed. Wrap at 72 characters. Explain
what changed and why, not how (the code shows how).

- Use bullet points for multiple changes
- Use present tense ("Add feature" not "Added feature")
- Reference issues: "Fixes #123"
```

### 5. Submit a Pull Request

Push your branch to GitHub and open a pull request:

```bash
git push origin feature/your-feature-name
```

Provide a clear description of your changes in the pull request. Reference
any related issues and describe how you tested your changes.

### 6. Address Review Feedback

Respond to code review comments and make requested changes. Push additional
commits to your branch to update the pull request.

## Development Guides

Each component has detailed development documentation:

- [Collector Development Guide](collector/development.md)
- [Alerter Development Guide](alerter/development.md)

These guides cover component-specific setup, architecture, and contribution
guidelines.

## Additional Resources

Consult these resources for more information:

- [DESIGN.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/DESIGN.md)
  describes the system architecture.
- [CLAUDE.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/CLAUDE.md)
  contains detailed coding standards.
- [docs/index.md](index.md) provides the main documentation entry point.

## Reporting Issues

To report a bug or request a feature, create an issue on GitHub:

[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

Include the following information in bug reports:

- A clear description of the problem.
- Steps to reproduce the issue.
- Expected and actual behavior.
- Component version and environment details.

## License

This project is licensed under the [PostgreSQL License][license].

[license]: https://github.com/pgEdge/ai-dba-workbench/blob/main/LICENSE.md

By contributing to the project, you agree that your contributions will be
licensed under the same license.
