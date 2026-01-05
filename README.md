# pgEdge AI DBA Workbench

[![Build Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/build-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/build-collector.yml)
[![Test Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/test-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/test-collector.yml)
[![Lint Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-collector.yml)

The pgEdge AI DBA Workbench is a unified environment for interacting with pgEdge's
distributed and non-distributed PostgreSQL systems through artificial
intelligence and traditional methods. It combines a Model Context Protocol
(MCP) Server with a web-based user interface and data collector, enabling users
 to query, analyze, and manage distributed clusters using natural language and
 intelligent automation. The Workbench exposes pgEdge tools and data sources
 — such as Spock replication status, cluster configuration, and operational
 metrics — to either hosted or both hosted and locally running language models.
 Its architecture supports seamless switching between cloud-connected LLMs like
 Claude and locally hosted models from Ollama, ensuring the similar levels of
 functionality in air-gapped or secure environments. In essence, the pgEdge AI
 Workbench bridges the gap between database administration and AI reasoning,
 offering an extensible foundation for observability, troubleshooting, and
 intelligent workflow creation across the pgEdge ecosystem.

## Components

The pgEdge AI DBA Workbench consists of three main components:

- **[Collector](collector/README.md)** - A monitoring service that collects
  metrics from PostgreSQL servers and stores them in a centralized datastore
  for analysis
- **[Server](server/README.md)** - An MCP server that provides tools and
  resources for interacting with PostgreSQL systems
- **CLI** - A command-line interface for interacting with the MCP server
  (coming soon)
- **Client** - A web-based user interface for interacting with the AI
  Workbench (coming soon)

## Documentation

Comprehensive documentation is available in the [docs](docs/index.md)
directory:

- **[Documentation Index](docs/index.md)** - Main documentation entry point
- **[Collector Documentation](docs/collector/index.md)** - Data collection
  and monitoring

## Building

The project uses Makefiles for building and testing. All components can be
built from the top-level directory:

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

For information on getting started with each component, please refer to:

- [Collector Quick Start](docs/collector/quickstart.md) - Set up monitoring
