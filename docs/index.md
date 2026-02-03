# pgEdge AI DBA Workbench Documentation

Welcome to the pgEdge AI DBA Workbench documentation. The AI DBA Workbench is a
unified environment for interacting with pgEdge's distributed and
non-distributed PostgreSQL systems through artificial intelligence and
traditional methods.

## Overview

The pgEdge AI DBA Workbench combines a Model Context Protocol (MCP) Server with a
web-based user interface and data collector, enabling users to query, analyze,
and manage distributed clusters using natural language and intelligent
automation. The Workbench exposes pgEdge tools and data sources — such as
Spock replication status, cluster configuration, and operational metrics — to
either hosted or both hosted and locally running language models.

Its architecture supports seamless switching between cloud-connected LLMs like
Claude and locally hosted models from Ollama, ensuring the similar levels of
functionality in air-gapped or secure environments. In essence, the pgEdge AI
Workbench bridges the gap between database administration and AI reasoning,
offering an extensible foundation for observability, troubleshooting, and
intelligent workflow creation across the pgEdge ecosystem.

## Architecture

The AI DBA Workbench is composed of several components that work together:

### Data Collection Layer

The [Collector](collector/index.md) continuously monitors PostgreSQL servers
and collects metrics into a centralized datastore. It provides:

- Multi-server monitoring with independent connection pools
- 24 built-in probes covering PostgreSQL system views
- Automated data management with partitioning and retention
- Secure connections with encryption and SSL/TLS support

### Intelligence Layer

The [MCP Server](server/index.md) implements the Model Context Protocol,
providing AI assistants with standardized access to PostgreSQL systems. It
provides:

- HTTP/HTTPS transport with JSON-RPC 2.0
- SQLite-based authentication with users and tokens
- Database tools for queries, schema introspection, and analysis
- LLM proxy support for Anthropic, OpenAI, and Ollama
- Conversation history management

### Interaction Layer

The [CLI](cli/index.md) (Natural Language Agent) provides an interactive
command-line interface for chatting with your PostgreSQL database using natural
language. It provides:

- Multiple LLM provider support (Anthropic, OpenAI, Ollama)
- Username/password and service token authentication
- Runtime configuration with slash commands
- Conversation history and prompt caching

## Getting Started

### Quick Start

Start with the Collector to set up monitoring for your PostgreSQL servers:

- [Collector Quick Start Guide](collector/quickstart.md)
- [Collector Configuration](collector/configuration.md)

### System Requirements

- PostgreSQL 14 or higher
- Go 1.23 or higher (for building from source)
- Network connectivity between components
- Appropriate database credentials and permissions

## Component Documentation

### Collector Documentation

The Collector monitors PostgreSQL servers and collects performance metrics.

**[Go to Collector Documentation →](collector/index.md)**

Key topics:

- [Overview](collector/overview.md) - Architecture and concepts
- [Quick Start](collector/quickstart.md) - Get running quickly
- [Configuration](collector/configuration.md) - Setup and options
- [Architecture](collector/architecture.md) - Detailed design
- [Probes](collector/probes.md) - How data collection works
- [Development](collector/development.md) - Contributing guide

### Server Documentation

The MCP Server provides AI assistants with access to PostgreSQL systems.

**[Go to Server Documentation →](server/index.md)**

Key topics:

- [Overview](server/index.md) - Architecture and features
- [Authentication](server/authentication.md) - Users, tokens, and security
- [Configuration](server/configuration.md) - Setup and options

### CLI Documentation

The CLI provides natural language access to PostgreSQL databases.

**[Go to CLI Documentation →](cli/index.md)**

Key topics:

- [Overview](cli/index.md) - Features and usage

## Common Configuration

### Database Connection

All components connect to PostgreSQL databases. Common connection parameters:

```
pg_host = localhost
pg_port = 5432
pg_database = ai_workbench
pg_username = postgres
pg_password_file = /path/to/password.txt
pg_sslmode = prefer
```

### Security

The system uses several security mechanisms:

- **Encryption**: AES-256-GCM for password storage
- **SSL/TLS**: Full support for encrypted connections
- **Authentication**: User and service token management
- **Isolation**: Session and connection isolation

See component-specific documentation for additional security configuration.

## Development

### Prerequisites

Before developing any component, install the required tools:

```bash
# Install golangci-lint for linting
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

Ensure `$(go env GOPATH)/bin` is in your PATH:

```bash
# Add to your ~/.bashrc, ~/.zshrc, or ~/.zprofile
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Project Structure

```
ai-workbench/
├── collector/          # Data collector service
│   ├── src/           # Source code
│   └── README.md      # Component readme
├── server/            # MCP server
│   ├── src/           # Source code
│   └── README.md      # Component readme
├── cli/               # Natural language agent CLI
│   ├── src/           # Source code
│   └── README.md      # Component readme
├── docs/              # Unified documentation (this directory)
│   ├── collector/     # Collector docs
│   ├── server/        # Server docs
│   └── cli/           # CLI docs
├── examples/          # Example configurations
├── DESIGN.md          # Overall system design
└── README.md          # Project readme
```

### Building from Source

The project uses Makefiles for building and testing. You can build all
components from the top-level directory:

```bash
# Build all components
make all

# Build individual components
cd collector && make build
```

Or build directly with Go:

```bash
# Collector
cd collector/src && go build -o ../collector
```

### Running Tests

The project includes comprehensive unit tests for each component.

#### Using Make (Recommended)

```bash
# Run all sub-project tests
make test

# Run tests with coverage reports
make coverage

# Run linting
make lint

# Run everything (tests, coverage, and linting)
make test-all
```

#### Using Go Directly

```bash
# Collector tests
cd collector/src && go test -v ./...
```

#### Test Environment Variables

- `TEST_AI_WORKBENCH_SERVER`: PostgreSQL connection string for test database
  (default: `postgres://postgres@localhost:5432/postgres`)
- `TEST_AI_WORKBENCH_KEEP_DB=1`: Keep test database after tests complete for
  inspection
- `SKIP_DB_TESTS=1`: Skip database tests in collector

See the [Collector Testing Guide](collector/testing.md) for collector-specific
testing details.

### Code Style

The project follows Go conventions:

- Four-space indentation
- Use `gofmt` for formatting
- Use `go vet` for static analysis
- Comprehensive unit tests
- Code coverage reporting

See [CLAUDE.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/CLAUDE.md)
for detailed coding standards.

## Integration Examples

### Basic Monitoring Setup

1. Deploy the collector to monitor PostgreSQL servers
2. Configure connection to datastore
3. Enable desired probes
4. Verify data collection

See [Collector Quick Start](collector/quickstart.md) for detailed steps.

## Troubleshooting

### Common Issues

**Collector not connecting to PostgreSQL:**

- Verify connection parameters in configuration
- Check network connectivity and firewalls
- Ensure PostgreSQL user has required permissions
- Review collector logs for specific errors

### Getting Help

- Check component-specific documentation
- Review example configuration files in `/examples`
- Examine test files for usage examples
- Consult [DESIGN.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/DESIGN.md)
  for architecture details

## Version Information

This documentation corresponds to version 0.1.0 of the pgEdge AI DBA Workbench.

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.

This software is released under The PostgreSQL License.

## Additional Resources

- [Project README](https://github.com/pgEdge/ai-dba-workbench/blob/main/README.md) -
  Quick overview
- [Design Document](https://github.com/pgEdge/ai-dba-workbench/blob/main/DESIGN.md) -
  System architecture
- [Example Configurations](https://github.com/pgEdge/ai-dba-workbench/tree/main/examples) -
  Example configs
- [Standing Instructions](https://github.com/pgEdge/ai-dba-workbench/blob/main/CLAUDE.md) -
  Development guidelines
