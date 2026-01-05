# pgEdge AI DBA Workbench MCP Server

The MCP (Model Context Protocol) Server provides AI assistants with standardized
access to PostgreSQL systems through HTTP/HTTPS endpoints with authentication.

## Features

- HTTP/HTTPS transport with JSON-RPC 2.0
- Token-based and user session authentication
- Multi-database support with access control
- MCP tools for database operations
- MCP resources for schema and data access
- MCP prompts for common workflows
- LLM proxy support for Anthropic, OpenAI, and Ollama

## Building

```bash
# Build the server
make build

# Run tests
make test

# Run linting
make lint
```

## Configuration

The server is configured via YAML configuration file and/or command line flags.

### Command Line Options

```
-config string     Path to configuration file
-addr string       HTTP server address
-tls               Enable TLS/HTTPS
-cert string       Path to TLS certificate file
-key string        Path to TLS key file
-chain string      Path to TLS certificate chain file
-token-file string Path to API token file
-user-file string  Path to user file
-debug             Enable debug logging
```

### Token Management

```bash
# Add a new API token
./mcp-server -add-token

# List all tokens
./mcp-server -list-tokens

# Remove a token
./mcp-server -remove-token <token-id>
```

### User Management

```bash
# Add a new user
./mcp-server -add-user -username <name>

# List all users
./mcp-server -list-users

# Enable/disable a user
./mcp-server -enable-user -username <name>
./mcp-server -disable-user -username <name>
```

## Documentation

See the [Server Documentation](../docs/server/index.md) for detailed information.
