# pgEdge AI Workbench MCP Server Documentation

Welcome to the pgEdge AI Workbench MCP Server documentation.

## Overview

The MCP Server implements the Model Context Protocol (MCP), a standardized
interface for AI assistants to interact with external systems. This server
provides secure access to PostgreSQL databases and enables AI-powered
database operations.

## Quick Links

- [README](../README.md) - Getting started guide
- [Architecture](#architecture) - Server architecture and components
- [Configuration](#configuration) - Configuration reference
- [Protocol](#protocol) - MCP protocol details
- [Development](#development) - Development guide

## Architecture

The MCP server is organized into several packages:

### Package Structure

```
server/
├── src/
│   ├── main.go           # Application entry point
│   ├── config/           # Configuration handling
│   │   ├── config.go     # Configuration struct and methods
│   │   └── config_test.go
│   ├── logger/           # Logging functionality
│   │   ├── logger.go     # Logger implementation
│   │   └── logger_test.go
│   ├── mcp/              # MCP protocol implementation
│   │   ├── protocol.go   # MCP data structures
│   │   ├── protocol_test.go
│   │   ├── handler.go    # Request handler
│   │   └── handler_test.go
│   └── server/           # HTTP/HTTPS server
│       └── server.go     # Server implementation
└── docs/
    └── index.md          # This file
```

### Components

#### Main Application

The [main.go](../src/main.go) file serves as the application entry point and
handles:

- Command-line flag parsing
- Configuration loading and validation
- Server initialization
- Signal handling for graceful shutdown
- Lifecycle management

#### Configuration Package

The [config](../src/config/) package provides configuration management:

- Loading from configuration files
- Command-line flag overrides
- Validation of all settings
- Secure handling of credentials

#### Logger Package

The [logger](../src/logger/) package provides structured logging with:

- Verbose mode support
- Different log levels (Error, Info, Startup, Fatal)
- Standard output formatting
- Thread-safe operations

#### MCP Package

The [mcp](../src/mcp/) package implements the Model Context Protocol:

- JSON-RPC 2.0 request/response structures
- Protocol error codes
- Request handlers for MCP methods
- Session state management

#### Server Package

The [server](../src/server/) package implements the HTTP/HTTPS server:

- Server-Sent Events (SSE) support
- TLS/SSL configuration
- Health check endpoint
- Connection management
- Graceful shutdown

## Configuration

### Configuration File Format

The server uses a simple key-value configuration file format:

```
# Comments start with #
key = value

# Quoted values for strings with spaces
description = "My Server"

# Unquoted values for simple strings and numbers
port = 8080
tls = false
```

### Available Settings

#### Server Settings

- `port` (int) - HTTP/HTTPS server port (default: 8080)
- `tls` (bool) - Enable TLS/SSL (default: false)
- `tls_cert` (string) - Path to TLS certificate file
- `tls_key` (string) - Path to TLS key file
- `tls_chain` (string) - Path to TLS certificate chain file

#### Database Settings

- `pg_host` (string) - PostgreSQL server hostname
- `pg_hostaddr` (string) - PostgreSQL server IP address
- `pg_database` (string) - PostgreSQL database name
- `pg_username` (string) - PostgreSQL username
- `pg_password_file` (string) - Path to password file
- `pg_port` (int) - PostgreSQL port (default: 5432)
- `pg_sslmode` (string) - PostgreSQL SSL mode (default: "prefer")
- `pg_sslcert` (string) - PostgreSQL client certificate
- `pg_sslkey` (string) - PostgreSQL client key
- `pg_sslrootcert` (string) - PostgreSQL root certificate

#### Security Settings

- `server_secret` (string) - Server secret for encryption (REQUIRED)

### Configuration Precedence

Settings are applied in the following order (later sources override earlier):

1. Default values
2. Configuration file settings
3. Command-line flags

## Protocol

### JSON-RPC 2.0

All MCP communication uses JSON-RPC 2.0 over Server-Sent Events.

#### Request Format

```json
{
    "jsonrpc": "2.0",
    "id": <request-id>,
    "method": "<method-name>",
    "params": <parameters>
}
```

#### Response Format

Success response:

```json
{
    "jsonrpc": "2.0",
    "id": <request-id>,
    "result": <result-data>
}
```

Error response:

```json
{
    "jsonrpc": "2.0",
    "id": <request-id>,
    "error": {
        "code": <error-code>,
        "message": "<error-message>",
        "data": <optional-error-data>
    }
}
```

### Error Codes

Standard JSON-RPC 2.0 error codes:

- `-32700` - Parse error (invalid JSON)
- `-32600` - Invalid request
- `-32601` - Method not found
- `-32602` - Invalid parameters
- `-32603` - Internal error

### MCP Methods

#### initialize

Initializes an MCP session.

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {
            "name": "ClientName",
            "version": "1.0.0"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "serverInfo": {
            "name": "pgEdge AI Workbench MCP Server",
            "version": "0.1.0"
        }
    }
}
```

#### ping

Health check method.

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "ping"
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "result": {
        "status": "ok"
    }
}
```

## Development

### Building

```bash
cd src
go mod tidy
go build -o mcp-server
```

### Testing

Run all tests:

```bash
go test -v ./...
```

Run tests for a specific package:

```bash
go test -v ./src/mcp
go test -v ./src/config
go test -v ./src/logger
```

### Code Style

The project follows standard Go conventions:

- Use `gofmt` for formatting
- Use `go vet` for static analysis
- Four-space indentation
- Comprehensive unit tests

### Adding New MCP Methods

To add a new MCP method:

1. Add method handler in [src/mcp/handler.go](../src/mcp/handler.go):

```go
func (h *Handler) handleNewMethod(req Request) (*Response, error) {
    // Parse parameters
    // Perform operation
    // Return response
}
```

2. Add method routing in `HandleRequest`:

```go
case "newMethod":
    return h.handleNewMethod(req)
```

3. Add tests in [src/mcp/handler_test.go](../src/mcp/handler_test.go)

## License

This software is released under The PostgreSQL License. See
[LICENSE.md](../../LICENSE.md) for details.
