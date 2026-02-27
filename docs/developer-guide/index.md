# Developer Guide

The Developer Guide provides architecture documentation,
development workflows, and contribution guidelines for the
pgEdge AI DBA Workbench project.

## Project Overview

The pgEdge AI DBA Workbench consists of four components
that work together to monitor and manage PostgreSQL
database estates.

- The collector is a Go service that gathers metrics from
  monitored PostgreSQL instances and stores the data in a
  central datastore.
- The alerter is a Go service that evaluates alert rules
  against collected metrics and sends notifications
  through configured channels.
- The server is a Go service that implements the Model
  Context Protocol (MCP) and provides REST APIs for the
  web client.
- The client is a React/TypeScript web application that
  displays dashboards, alerts, and AI-generated insights.

## Development Prerequisites

Install the following tools before starting development:

- [Go 1.24](https://go.dev/doc/install) or later for
  building server-side components.
- [Node.js 18](https://nodejs.org/) or later for building
  the web client.
- [PostgreSQL 14](https://www.postgresql.org/download/)
  or later for running database tests.
- [Git](https://git-scm.com/) for version control.
- [Make](https://www.gnu.org/software/make/) for build
  automation.

Install the Go linter with the following command:

```bash
go install \
  github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Getting Started

Clone the repository from
[GitHub](https://github.com/pgEdge/ai-dba-workbench):

```bash
git clone \
  https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench
```

Build all components from the top-level directory:

```bash
make all
```

Run the full test suite to verify the setup:

```bash
make test-all
```

## Project Structure

The repository follows this directory layout:

```
ai-dba-workbench/
  alerter/           Alert monitoring service
    src/             Source code
  client/            Web client (React/TypeScript)
    src/             Source code
  collector/         Data collector service
    src/             Source code
  server/            MCP server
    src/             Source code
  pkg/               Shared Go packages
  docs/              Unified documentation
  examples/          Example configurations
```

Each Go component has its own `go.mod` under the `src/`
subdirectory. Build commands must run from within the
`src/` directory of each component.

## Component Documentation

### Collector

- [Testing Guide](collector/testing.md) covers the
  collector architecture, workflows, and test strategy.
- [Adding Probes](collector/adding-probes.md) explains
  how to create new metric probes.

### Alerter

- [Testing Guide](alerter/testing.md) covers the
  alerter architecture, workflows, and test strategy.
- [Adding Rules](alerter/adding-rules.md) explains
  how to create new alert rules.

### Server

- [Server Architecture](server/architecture.md) describes
  the MCP server internals, transport layer, and
  extension points.

### Client

- [Client Architecture](client/architecture.md) describes
  the React component structure, state management, and
  build workflow.

## Design Documents

- [Node Role Probe Design](design/node-role-probe.md)
  documents the design for detecting PostgreSQL node
  roles within cluster topologies.

## Contributing

See the [Contributing Guide](contributing.md) for
instructions on submitting code, running quality checks,
and following the project coding standards.

## Additional Resources

- [CLAUDE.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/CLAUDE.md)
  contains the detailed coding standards for the project.
- [Changelog](../changelog.md) tracks notable changes
  by release.
