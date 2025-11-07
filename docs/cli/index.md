# pgEdge AI Workbench CLI Documentation

## Quick Links

- [Main Documentation](../index.md) - Return to main documentation index
- [CLI README](../../cli/README.md) - Getting started guide

## Overview

The pgEdge AI Workbench CLI (`ai-cli`) is a command-line interface tool for
interacting with the MCP (Model Context Protocol) server. It provides a simple
way to call MCP tools, read resources, and test server connectivity.

## Architecture

### Components

The CLI tool consists of two main components:

#### 1. Main Application ([main.go](../../cli/src/main.go))

The main application handles:

- Command-line argument parsing
- Command routing (run-tool, read-resource, ping)
- Stdin detection and JSON input handling
- Pretty-printed JSON output
- Usage information and examples
- Error handling and reporting

#### 2. MCP Client ([client.go](../../cli/src/client.go))

The MCP client implements:

- JSON-RPC 2.0 protocol communication
- HTTP transport layer
- Request/response serialization
- Error handling for HTTP and MCP protocol errors
- Automatic request ID management

### Protocol

The CLI communicates with the MCP server using JSON-RPC 2.0 over HTTP.

#### Request Format

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "tool_name",
        "arguments": {
            "arg1": "value1"
        }
    }
}
```

#### Response Format

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "status": "success",
        "data": {}
    }
}
```

#### Error Format

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "error": {
        "code": -32600,
        "message": "Invalid Request"
    }
}
```

## Commands

### run-tool

Executes an MCP tool with JSON arguments from stdin.

**Usage:**

```bash
cat input.json | ./ai-cli run-tool <tool-name>
```

**Flow:**

1. Parse command-line arguments to get tool name
2. Check if stdin has piped data
3. If no data, show example and exit with error
4. Parse JSON from stdin
5. Call MCP server with `tools/call` method
6. Output pretty-printed result

**Example:**

```bash
cat config.json | ./ai-cli run-tool set_config
```

where `config.json` contains:

```json
{
    "key": "value",
    "option": "setting"
}
```

### read-resource

Reads an MCP resource by its URI.

**Usage:**

```bash
./ai-cli read-resource <resource-uri>
```

**Flow:**

1. Parse command-line arguments to get resource URI
2. Call MCP server with `resources/read` method
3. Output pretty-printed result

**Example:**

```bash
./ai-cli read-resource system://stats
```

### ping

Tests connectivity to the MCP server.

**Usage:**

```bash
./ai-cli ping
```

**Flow:**

1. Call MCP server with `ping` method
2. Output pretty-printed result

**Example:**

```bash
./ai-cli --server http://myserver:8080 ping
```

## Error Handling

The CLI provides comprehensive error handling:

### Connection Errors

When the server is unreachable:

```
Error: failed to execute request: dial tcp: connection refused
```

### JSON Parsing Errors

When input JSON is invalid:

```
Error: failed to parse JSON input: unexpected end of JSON input
```

### MCP Protocol Errors

When the server returns an error:

```
Error: MCP error -32601: Method not found
```

### HTTP Errors

When the server returns a non-200 status:

```
Error: HTTP error 404: Not Found
```

## Development

### Prerequisites

Before developing, install the required tools:

```bash
# Install golangci-lint for linting
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

Ensure `$(go env GOPATH)/bin` is in your PATH:

```bash
# Add to your ~/.bashrc, ~/.zshrc, or ~/.zprofile
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Building

Build the CLI:

```bash
make build
```

This creates the `ai-cli` binary in the `cli` directory.

### Testing

Run tests:

```bash
make test
```

Run tests with coverage:

```bash
make coverage
```

Run tests, coverage, and linting:

```bash
make test-all
```

### Code Formatting

Format code:

```bash
make fmt
```

### Static Analysis

Run go vet:

```bash
make vet
```

### Linting

Run golangci-lint:

```bash
make lint
```

### Pre-commit Checks

Run all checks (fmt, vet, test, lint):

```bash
make check
```

This runs: fmt, vet, test, and lint.

## Adding New Commands

To add a new command:

1. Add a new case to the switch statement in
   [main.go](../../cli/src/main.go):

```go
case "new-command":
    if len(commandArgs) < 1 {
        return fmt.Errorf("usage: ai-cli new-command <arg>")
    }
    err = newCommand(client, commandArgs)
```

2. Implement the command function:

```go
func newCommand(client *MCPClient, args []string) error {
    // Implementation
    result, err := client.CallTool("tool_name", params)
    if err != nil {
        return err
    }
    return printJSON(result)
}
```

3. Add a method to the MCPClient if needed:

```go
func (c *MCPClient) NewMethod(params map[string]interface{}) (interface{}, error) {
    return c.makeRequest("new/method", params)
}
```

4. Update the usage message in `printUsage()` function

5. Add tests for the new command

## Dependencies

The CLI uses only Go standard library packages:

- `flag` - Command-line flag parsing
- `encoding/json` - JSON serialization
- `net/http` - HTTP client
- `io` - I/O utilities
- `os` - Operating system interface
- `fmt` - Formatted I/O
- `bytes` - Byte buffer utilities

No external dependencies are required.

## Configuration

The CLI accepts configuration through command-line flags:

- `--server <URL>` - MCP server URL (default: `http://localhost:8080`)
- `--version` - Show version information

## Security Considerations

### Input Validation

The CLI validates:

- Command names against known commands
- JSON input format
- Required arguments for each command

### Output Safety

The CLI:

- Pretty-prints JSON to prevent injection attacks
- Uses standard error for error messages
- Exits with appropriate status codes

### Connection Security

The CLI:

- Supports both HTTP and HTTPS
- Uses standard HTTP client with reasonable timeouts
- Does not store credentials or sensitive data

## Troubleshooting

### Server Not Running

```
Error: failed to execute request: dial tcp [::1]:8080: connect: connection
refused
```

**Solution:** Ensure the MCP server is running on the specified host and port.

### Invalid JSON Input

```
Error: failed to parse JSON input: invalid character '}' looking for beginning
of object key string
```

**Solution:** Validate your JSON input using a JSON validator.

### Tool Not Found

```
Error: MCP error -32601: Method not found
```

**Solution:** Check the tool name and ensure it's supported by the MCP server.

## Future Enhancements

Potential improvements for future versions:

1. **Interactive Mode** - Prompt for inputs when not provided
2. **Configuration File** - Support for configuration file (~/.ai-cli.yaml)
3. **Output Formats** - Support for YAML, table, and raw output formats
4. **Command History** - Save and recall previous commands
5. **Shell Completion** - Bash/Zsh completion scripts
6. **Verbose Mode** - Debug logging for troubleshooting
7. **Batch Mode** - Execute multiple commands from a file
8. **Authentication** - Support for API keys or tokens

## License

Copyright (c) 2025, pgEdge, Inc.
This software is released under The PostgreSQL License.
