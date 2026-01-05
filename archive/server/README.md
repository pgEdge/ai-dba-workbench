# pgEdge AI DBA Workbench MCP Server

[![Build Server](https://github.com/pgEdge/ai-workbench/actions/workflows/build-server.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/build-server.yml)
[![Test Server](https://github.com/pgEdge/ai-workbench/actions/workflows/test-server.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/test-server.yml)
[![Lint Server](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-server.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-server.yml)

The pgEdge AI DBA Workbench MCP Server implements the Model Context Protocol,
enabling AI assistants to interact with PostgreSQL databases through
a standardized interface.

## Overview

The MCP server is a standalone Go application that:

- Implements the Model Context Protocol (MCP) specification
- Provides HTTP and HTTPS endpoints with Server-Sent Events (SSE)
- Supports secure TLS/SSL connections
- Manages multiple PostgreSQL database connections
- Handles graceful shutdown and signal management

## Getting Started

### Prerequisites

- Go 1.23 or later
- PostgreSQL 12 or later (for database operations)
- Network access to PostgreSQL servers

### Building

```bash
cd src
go mod tidy
go build -o mcp-server
```

### Configuration

The MCP server can be configured using a configuration file, command line
flags, or a combination of both. Command line flags take precedence over
configuration file settings.

The configuration file is shared with the collector component and uses a
common format.

#### Configuration File

By default, the MCP server looks for `server.conf` in the same directory
as the executable. You can specify a different path using the `-config`
flag.

A sample configuration file is provided at
[../configs/ai-workbench.conf.sample](../configs/ai-workbench.conf.sample).
Copy this file to `server.conf` and customize it for your environment.

Key configuration options:

```
# Server settings
port = 8080
tls = false

# TLS settings (if tls = true)
tls_cert = /path/to/cert.pem
tls_key = /path/to/key.pem
tls_chain = /path/to/chain.pem

# Datastore connection settings
pg_host = localhost
pg_database = ai_workbench
pg_username = mcp_server
pg_password_file = /path/to/password.txt
pg_port = 5432
pg_sslmode = prefer

# Server secret for encryption (REQUIRED)
server_secret = your-secret-here
```

See the sample configuration file for all available options.

#### Command Line Flags

```
-config string
    Path to configuration file
-v
    Enable verbose logging (shows detailed operational information)
-port int
    Server listening port (default 8080)
-tls
    Enable HTTPS mode
-tls-cert string
    Path to TLS certificate
-tls-key string
    Path to TLS key
-tls-chain string
    Path to TLS certificate chain
-pg-host string
    PostgreSQL server hostname or IP address
-pg-database string
    PostgreSQL database name
-pg-username string
    PostgreSQL username
-pg-password-file string
    Path to file containing PostgreSQL password
-pg-port int
    PostgreSQL server port (default 5432)
-pg-sslmode string
    PostgreSQL SSL mode (default "prefer")
```

### Running

```bash
./mcp-server -config /path/to/server.conf
```

Or, if you place the configuration file in the same directory as the
server binary:

```bash
./mcp-server
```

To enable verbose logging for troubleshooting or development:

```bash
./mcp-server -v -config /path/to/server.conf
```

Verbose mode displays detailed operational information including request
handling, connection management, and internal operations. Without the `-v`
flag, only startup messages, shutdown messages, and errors are displayed.

To run with HTTPS enabled:

```bash
./mcp-server -tls -tls-cert /path/to/cert.pem -tls-key /path/to/key.pem
```

## API Endpoints

The MCP server exposes the following HTTP endpoints:

### `/sse` - Server-Sent Events Endpoint

This endpoint implements the MCP protocol using Server-Sent Events for
real-time bidirectional communication.

Supported MCP methods:

- `initialize` - Initialize the MCP session
- `ping` - Health check ping/pong

### `/health` - Health Check Endpoint

Returns the server health status and initialization state in JSON format:

```json
{
    "status": "ok",
    "initialized": true
}
```

## Protocol

The MCP server implements JSON-RPC 2.0 over Server-Sent Events. All
requests and responses follow the JSON-RPC 2.0 specification.

Example request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {
            "name": "MyClient",
            "version": "1.0.0"
        }
    }
}
```

Example response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "serverInfo": {
            "name": "pgEdge AI DBA Workbench MCP Server",
            "version": "0.1.0"
        }
    }
}
```

## Documentation

For detailed documentation, see [../docs/server/index.md](../docs/server/index.md).

## Testing

### Run tests

```bash
cd /Users/dpage/git/ai-workbench/server
go test -v ./...
```

This will run all unit tests for the MCP server, including:

- Configuration handling tests
- Logger functionality tests
- MCP protocol tests
- Handler tests

### Run specific package tests

```bash
go test -v ./src/mcp        # Test MCP protocol and handlers
go test -v ./src/config     # Test configuration
go test -v ./src/logger     # Test logger
```

## License

This software is released under The PostgreSQL License. See
[LICENSE.md](../LICENSE.md) for details.
