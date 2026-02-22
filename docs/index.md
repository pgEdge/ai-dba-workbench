# pgEdge AI DBA Workbench Documentation

Welcome to the pgEdge AI DBA Workbench documentation. The AI DBA Workbench is a
unified environment for interacting with pgEdge's distributed and
non-distributed PostgreSQL systems through artificial intelligence and
traditional methods.

## Overview

The pgEdge AI DBA Workbench combines a Model Context Protocol (MCP) Server
with a web-based user interface and data collector, enabling users to query,
analyze, and manage distributed clusters using natural language and intelligent
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

The AI DBA Workbench consists of four components that
work together to provide monitoring, alerting, and
AI-powered database management.

### Data Collection Layer

The [Collector](collector/index.md) continuously monitors
PostgreSQL servers and collects metrics into a centralized
datastore. The collector provides the following features:

- Multi-server monitoring with independent connection pools.
- 34 built-in probes covering PostgreSQL system views.
- Automated data management with partitioning and retention.
- Secure connections with encryption and SSL/TLS support.

### Intelligence Layer

The [MCP Server](server/index.md) implements the Model
Context Protocol, providing AI assistants with standardized
access to PostgreSQL systems. The server provides the
following features:

- HTTP/HTTPS transport with JSON-RPC 2.0.
- SQLite-based authentication with users and tokens.
- Role-based access control with groups and privileges.
- Database tools for queries, schema introspection, and
  analysis.
- LLM proxy support for Anthropic, OpenAI, Gemini, and
  Ollama.
- Conversation history management.

### Alert Monitoring Layer

The [Alerter](alerter/index.md) evaluates collected metrics
against thresholds and uses AI-powered anomaly detection to
generate alerts. The alerter provides the following features:

- Threshold-based alerting with configurable rules.
- Tiered anomaly detection using statistical analysis,
  embeddings, and LLM classification.
- Blackout scheduling for maintenance windows.
- Notification delivery through email, Slack, Mattermost,
  and webhooks.

### Presentation Layer

The [Client](client/index.md) provides a web-based user
interface for cluster monitoring and management. The client
provides the following features:

- Hierarchical dashboards for estate, cluster, and server
  monitoring.
- Cluster topology visualization with replication edges.
- AI-powered chat interface for natural language queries.
- Administration panel for users, groups, and tokens.

## Getting Started

### Quick Start

Start with the Collector to set up monitoring for your PostgreSQL servers:

- [Collector Quick Start Guide](collector/quickstart.md)
- [Collector Configuration](collector/configuration.md)

### System Requirements

- [PostgreSQL 14](https://www.postgresql.org/download/) or
  higher for the datastore.
- [Go 1.24](https://go.dev/doc/install) or higher for
  building server-side components from source.
- [Node.js 18](https://nodejs.org/) or higher for building
  the web client from source.
- Network connectivity between all components.
- Database credentials with appropriate permissions.

## Component Documentation

### Collector Documentation

The Collector monitors PostgreSQL servers and collects
performance metrics.

**[Go to Collector Documentation](collector/index.md)**

Key topics:

- [Overview](collector/overview.md) covers architecture and
  concepts.
- [Quick Start](collector/quickstart.md) helps you get
  running quickly.
- [Configuration](collector/configuration.md) describes
  setup and options.
- [Architecture](collector/architecture.md) explains the
  detailed design.
- [Probes](collector/probes.md) describes how data
  collection works.
- [Development](collector/development.md) provides the
  contributing guide.

### Server Documentation

The MCP Server provides AI assistants with access to
PostgreSQL systems.

**[Go to Server Documentation](server/index.md)**

Key topics:

- [Overview](server/index.md) covers architecture and
  features.
- [API Reference](server/api-reference.md) documents all
  REST API endpoints.
- [Authentication](server/authentication.md) covers users,
  tokens, and security.
- [Configuration](server/configuration.md) describes setup
  and options.

### Alerter Documentation

The Alerter evaluates collected metrics and generates
alerts using threshold rules and anomaly detection.

**[Go to Alerter Documentation](alerter/index.md)**

Key topics:

- [Overview](alerter/overview.md) covers architecture and
  concepts.
- [Quick Start](alerter/quickstart.md) helps you get
  running quickly.
- [Configuration](alerter/configuration.md) describes all
  configuration options.
- [Alert Rules](alerter/alert-rules.md) explains the
  threshold rule system.
- [Anomaly Detection](alerter/anomaly-detection.md)
  covers AI-powered alerting.

### Client Documentation

The Client provides a web-based user interface for
monitoring and management.

**[Go to Client Documentation](client/index.md)**

Key topics:

- [Overview](client/index.md) covers features and
  architecture.
- [Dashboards](client/dashboards.md) describes the
  monitoring dashboards.

## Deployment

The following guides cover deployment for each component.

### Collector Deployment

The Collector Quick Start guide includes systemd service
configuration for Linux and launchd for macOS. See the
[Collector Quick Start](collector/quickstart.md) for
installation and service setup instructions.

### Server Deployment

The Server Configuration guide covers YAML configuration,
environment variables, and TLS setup. See the
[Server Configuration](server/configuration.md) for
all server options.

### Alerter Deployment

The Alerter Quick Start guide includes a systemd service
file and environment variable configuration. See the
[Alerter Quick Start](alerter/quickstart.md) for
installation and service setup instructions.

### Client Deployment

Build the web client for production using `npm run build`.
The build output in the `dist` directory can be served by
any static file server or reverse proxy.

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

- The system uses AES-256-GCM encryption for password storage.
- Full support for SSL/TLS encrypted connections is available.
- User and service token management handles authentication.
- Session and connection isolation protects user data.

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
├── alerter/           # Alert evaluation service
│   ├── src/           # Source code
│   └── README.md      # Component readme
├── client/            # Web-based user interface
│   ├── src/           # Source code
│   └── README.md      # Component readme
├── collector/         # Data collector service
│   ├── src/           # Source code
│   └── README.md      # Component readme
├── server/            # MCP server
│   ├── src/           # Source code
│   └── README.md      # Component readme
├── docs/              # Unified documentation
│   ├── alerter/       # Alerter docs
│   ├── collector/     # Collector docs
│   └── server/        # Server docs
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
- Consult the [DESIGN.md][design] document for architecture details

[design]: https://github.com/pgEdge/ai-dba-workbench/blob/main/DESIGN.md

## Version Information

This documentation corresponds to version 0.1.0 of the pgEdge AI DBA Workbench.

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.

This software is released under The PostgreSQL License.

## Additional Resources

- [Project README][readme] provides a quick overview of the project.
- [Design Document][design] describes the system architecture.
- [Example Configurations][examples] provides sample configuration files.
- [Standing Instructions][claude] contains the development guidelines.

[readme]: https://github.com/pgEdge/ai-dba-workbench/blob/main/README.md
[examples]: https://github.com/pgEdge/ai-dba-workbench/tree/main/examples
[claude]: https://github.com/pgEdge/ai-dba-workbench/blob/main/CLAUDE.md
