# pgEdge AI DBA Workbench

[![CI - Alerter](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml)
[![CI - Client](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml)
[![CI - Collector](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml)
[![CI - Docs](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml)
[![CI - Server](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml)

The pgEdge AI DBA Workbench is a unified environment for
interacting with pgEdge's distributed and non-distributed PostgreSQL
systems through artificial intelligence and traditional methods.

The Workbench combines a Model Context Protocol (MCP) Server with a
web-based user interface and data collector. Users can query,
analyze, and manage distributed clusters using natural language and
intelligent automation. The Workbench exposes pgEdge tools and data
sources such as Spock replication status, cluster configuration, and
operational metrics to language models.

The architecture supports switching between cloud-connected LLMs
like Claude and locally hosted models from Ollama. This design
ensures similar levels of functionality in air-gapped or secure
environments. The pgEdge AI Workbench bridges database
administration and AI reasoning; it offers an extensible foundation
for observability, troubleshooting, and intelligent workflow
creation across the pgEdge ecosystem.

## Components

The pgEdge AI DBA Workbench consists of four main components:

- The [Collector](collector/README.md) monitors PostgreSQL
  servers and stores metrics in a centralized datastore.
- The [Server](server/README.md) provides MCP tools and
  resources for interacting with PostgreSQL systems.
- The [Alerter](alerter/README.md) evaluates collected
  metrics against thresholds and AI-powered anomaly
  detection to generate alerts.
- The [Client](client/README.md) provides a web-based user
  interface for the AI Workbench.

## Documentation

Comprehensive documentation is available in the [docs](docs/index.md)
directory:

- The [Documentation Index](docs/index.md) serves as the
  main entry point for all project documentation.
- The [Server Documentation](docs/server/index.md)
  describes MCP server configuration and authentication.
- The [Collector Documentation](docs/collector/index.md)
  explains data collection and monitoring setup.
- The [Alerter Documentation](docs/alerter/index.md)
  covers alert generation and anomaly detection.

## Prerequisites

Before building the project, install the following tools:

- [Go 1.24](https://go.dev/doc/install) or later for
  building server-side components.
- [Node.js 18](https://nodejs.org/) or later for building
  the web client.
- [PostgreSQL 14](https://www.postgresql.org/download/) or
  later for the datastore.
- [Make](https://www.gnu.org/software/make/) for build
  automation.

## Building

The project uses Makefiles for building and testing. All
components can be built from the top-level directory:

```bash
# Build all components
make all

# Build individual components
cd collector && make build
```

## Testing

The project includes comprehensive unit tests for each component.

### Run All Tests

```bash
# Run all sub-project tests
make test

# Run all sub-project tests with coverage
make coverage

# Run all sub-project tests with linting
make lint

# Run everything (all sub-project test-all)
make test-all
```

### Run Tests for Individual Components

```bash
cd collector && make test
```

### Environment Variables for Testing

- `TEST_AI_WORKBENCH_SERVER` - PostgreSQL connection string for test database
  (default: `postgres://postgres@localhost:5432/postgres`)
- `TEST_AI_WORKBENCH_KEEP_DB=1` - Keep test database after tests complete

### Available Make Targets

Each sub-project and the top-level Makefile support these targets:

- `all` - Build the project (default)
- `test` - Run tests
- `coverage` - Run tests with coverage report
- `lint` - Run linter
- `test-all` - Run tests, coverage, and linter
- `clean` - Remove build artifacts
- `killall` - Kill any running processes
- `help` - Show available targets

## Getting Started

For information on getting started with each component,
refer to the following guides:

- [Collector Quick Start](docs/collector/quickstart.md)
  covers monitoring setup.
- [Server Configuration](docs/server/configuration.md)
  covers server configuration.
- [Alerter Quick Start](docs/alerter/quickstart.md)
  covers alert setup.

## Deployment

For detailed installation, configuration, and usage
instructions, see the following documentation:

- [Collector Configuration](docs/collector/configuration.md)
  covers all collector options.
- [Server Configuration](docs/server/configuration.md)
  covers all server options.
- [Alerter Configuration](docs/alerter/configuration.md)
  covers all alerter options.

## Issues

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

## Contributing

We welcome your project contributions; for more information, see
[docs/developers.md](docs/developers.md).

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](LICENSE.md).
