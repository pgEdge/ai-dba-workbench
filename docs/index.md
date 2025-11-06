# pgEdge AI Workbench Documentation

Welcome to the pgEdge AI Workbench documentation. The AI Workbench is a
unified environment for interacting with pgEdge's distributed and
non-distributed PostgreSQL systems through artificial intelligence and
traditional methods.

## Overview

The pgEdge AI Workbench combines a Model Context Protocol (MCP) Server with a
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

The AI Workbench is composed of three main components that work together:

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
offers:

- JSON-RPC 2.0 over Server-Sent Events
- User and service token management
- Secure database access and operations
- Extensible tools and resources framework

### Interaction Layer

The [CLI](cli/index.md) provides command-line access to the MCP server for
testing, automation, and integration. It includes:

- Tool execution with JSON input
- Resource reading and listing
- Server connectivity testing
- Clean, simple interface for scripting

## Getting Started

### Quick Start

Each component can be deployed and operated independently:

1. **Start with the Collector** - Set up monitoring for your PostgreSQL
   servers
   - [Collector Quick Start Guide](collector/quickstart.md)
   - [Collector Configuration](collector/configuration.md)

2. **Deploy the MCP Server** - Enable AI-powered database interactions
   - [Server README](../server/README.md)
   - [Server Configuration](server/index.md#configuration)

3. **Use the CLI** - Test and interact with the MCP server
   - [CLI README](../cli/README.md)
   - [CLI Commands](cli/index.md#commands)

### System Requirements

- PostgreSQL 12 or higher
- Go 1.21 or higher (for building from source)
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

- [Architecture](server/index.md#architecture) - Server components
- [Configuration](server/index.md#configuration) - Setup options
- [Protocol](server/index.md#protocol) - MCP protocol details
- [Authentication](authentication.md) - User and service token management
- [MCP Resources](server/mcp-resources.md) - Available resources
- [MCP Tools](server/mcp-tools.md) - Available tools
- [Development](server/index.md#development) - Development guide

### CLI Documentation

The CLI provides command-line access to the MCP server.

**[Go to CLI Documentation →](cli/index.md)**

Key topics:

- [Architecture](cli/index.md#architecture) - CLI components
- [Commands](cli/index.md#commands) - Available commands
- [Authentication](authentication.md#cli-authentication) - Token and credential
  management
- [Error Handling](cli/index.md#error-handling) - Error messages
- [Development](cli/index.md#development) - Building and testing

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

For detailed authentication configuration and best practices, see the
[Authentication Guide](authentication.md).

See component-specific documentation for additional security configuration.

## Development

### Project Structure

```
ai-workbench/
├── collector/          # Data collector service
│   ├── src/           # Source code
│   ├── docs/          # Original documentation
│   └── README.md      # Component readme
├── server/            # MCP server
│   ├── src/           # Source code
│   ├── docs/          # Original documentation
│   └── README.md      # Component readme
├── cli/               # Command-line interface
│   ├── src/           # Source code
│   ├── docs/          # Original documentation
│   └── README.md      # Component readme
├── docs/              # Unified documentation (this directory)
│   ├── collector/     # Collector docs
│   ├── server/        # Server docs
│   └── cli/           # CLI docs
├── configs/           # Sample configurations
├── DESIGN.md          # Overall system design
└── README.md          # Project readme
```

### Building from Source

Each component has its own build process:

**Collector:**

```bash
cd collector/src
go mod tidy
go build -o collector
```

**Server:**

```bash
cd server/src
go mod tidy
go build -o mcp-server
```

**CLI:**

```bash
cd cli/src
go mod tidy
go build -o ai-cli
```

### Running Tests

All components include comprehensive test suites:

```bash
# Collector tests
cd collector/src
go test -v ./...

# Server tests
cd server/src
go test -v ./...

# CLI tests
cd cli/src
go test -v ./...
```

### Code Style

The project follows Go conventions:

- Four-space indentation
- Use `gofmt` for formatting
- Use `go vet` for static analysis
- Comprehensive unit tests
- Code coverage reporting

See [CLAUDE.md](../CLAUDE.md) for detailed coding standards.

## Integration Examples

### Basic Monitoring Setup

1. Deploy the collector to monitor PostgreSQL servers
2. Configure connection to datastore
3. Enable desired probes
4. Verify data collection

See [Collector Quick Start](collector/quickstart.md) for detailed steps.

### AI Assistant Integration

1. Deploy and configure the MCP server
2. Connect to the datastore with collected metrics
3. Configure MCP tools and resources
4. Connect your AI assistant to the server

See [Server Documentation](server/index.md) for detailed configuration.

### Automation and Scripting

1. Use the CLI to test MCP server connectivity
2. Create scripts to call tools programmatically
3. Parse JSON output for automation workflows
4. Integrate with existing monitoring systems

See [CLI Commands](cli/index.md#commands) for examples.

## Troubleshooting

### Common Issues

**Collector not connecting to PostgreSQL:**

- Verify connection parameters in configuration
- Check network connectivity and firewalls
- Ensure PostgreSQL user has required permissions
- Review collector logs for specific errors

**MCP server not responding:**

- Verify server is running on expected port
- Check TLS configuration if using HTTPS
- Ensure database connectivity
- Review server logs for initialization errors

**CLI connection failures:**

- Verify server URL is correct
- Check network connectivity to server
- Ensure server is initialized (use `/health` endpoint)
- Validate JSON input format for tool calls

### Getting Help

- Check component-specific documentation
- Review sample configuration files in `/configs`
- Examine test files for usage examples
- Consult [DESIGN.md](../DESIGN.md) for architecture details

## Version Information

This documentation corresponds to version 0.1.0 of the pgEdge AI Workbench.

## License

Copyright (c) 2025, pgEdge, Inc.

This software is released under The PostgreSQL License.

## Additional Resources

- [Project README](../README.md) - Quick overview
- [Design Document](../DESIGN.md) - System architecture
- [Sample Configurations](../configs/) - Example configs
- [Standing Instructions](../CLAUDE.md) - Development guidelines
