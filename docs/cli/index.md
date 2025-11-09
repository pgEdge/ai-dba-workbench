# pgEdge AI Workbench CLI Documentation

## Quick Links

- [Main Documentation](../index.md) - Return to main documentation index
- [CLI README](../../cli/README.md) - Getting started guide

## Overview

The pgEdge AI Workbench CLI (`ai-cli`) is a command-line interface tool for
interacting with the MCP (Model Context Protocol) server. It provides a simple
way to call MCP tools, read resources, test server connectivity, and interact
with LLMs that have access to MCP capabilities.

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
- Bearer token authentication

#### 3. LLM Integration ([llm.go](../../cli/src/llm.go))

The LLM integration provides:

- Support for Anthropic Claude and Ollama providers
- Conversation history management for interactive mode
- Automatic resource data fetching for context
- Tool and resource schema passing to LLMs
- Visual feedback with animated spinner during processing

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

Executes an MCP tool with optional JSON arguments from stdin.

**Usage:**

```bash
# With JSON input
cat input.json | ./ai-cli run-tool <tool-name>

# Without JSON input (uses empty object {})
./ai-cli run-tool <tool-name>
```

**Flow:**

1. Parse command-line arguments to get tool name
2. Check if stdin has piped data
3. If no data, use empty JSON object `{}`
4. If data present, parse JSON from stdin
5. Call MCP server with `tools/call` method
6. Output pretty-printed result

**Examples:**

```bash
# With arguments (creating a user)
echo '{"username": "alice", "email": "alice@example.com", "fullName": "Alice Smith", "password": "secret"}' | ./ai-cli run-tool create_user

# Without arguments (sends empty object {})
./ai-cli run-tool some_tool
```

where `config.json` contains:

```json
{
    "key": "value",
    "option": "setting"
}
```

### read-resource

Reads an MCP resource by its URI. Resources provide read-only access to system
data like user lists, service tokens, and other information.

**Usage:**

```bash
./ai-cli read-resource <resource-uri>
```

**Flow:**

1. Parse command-line arguments to get resource URI
2. Call MCP server with `resources/read` method
3. Output pretty-printed result

**Examples:**

```bash
# List all users
./ai-cli read-resource ai-workbench://users

# List all service tokens
./ai-cli read-resource ai-workbench://service-tokens
```

**Note:** Use `read-resource` for viewing/listing data, and `run-tool` for
actions (create, update, delete).

### list-resources

Lists all available MCP resources from the server.

**Usage:**

```bash
./ai-cli list-resources
```

**Example Output:**

```
ai-workbench://users (User Accounts) - List of all user accounts
ai-workbench://service-tokens (Service Tokens) - List of all service tokens
ai-workbench://connections (Database Connections) - List of database connections
ai-workbench://groups (User Groups) - List of all user groups
...
```

### list-tools

Lists all available MCP tools from the server.

**Usage:**

```bash
./ai-cli list-tools
```

**Example Output:**

```
authenticate_user - Authenticate a user and obtain a session token
create_user - Create a new user account
create_connection - Create a new database connection
update_connection - Update an existing database connection
delete_connection - Delete a database connection
...
```

### list-prompts

Lists all available MCP prompts from the server.

**Usage:**

```bash
./ai-cli list-prompts
```

### ask-llm

Interacts with an LLM (Large Language Model) that has access to all MCP tools,
resources, and prompts. The LLM can help understand the system, query data, and
suggest actions.

**Usage:**

```bash
./ai-cli ask-llm [query]
```

**Features:**

- **Interactive Mode**: If no query is provided, enters conversation mode
- **Conversation History**: Maintains context across multiple turns
- **Visual Feedback**: Animated spinner displays while waiting for responses
- **Resource Context**: LLM has access to actual data from static resources
- **Multiple Providers**: Supports Anthropic Claude (preferred) and Ollama

**Environment Variables:**

- `ANTHROPIC_API_KEY` - API key for Anthropic Claude (if set, used as default)
- `ANTHROPIC_MODEL` - Model to use (default: `claude-sonnet-4-5`)
- `OLLAMA_URL` - Ollama server URL (default: `http://localhost:11434`)
- `OLLAMA_MODEL` - Ollama model to use (default: `llama2`)

**Examples:**

```bash
# Single question mode
./ai-cli ask-llm "List all database connections"

# Interactive mode - have a conversation
./ai-cli ask-llm
# You: What users are in the system?
# ⠋ Thinking...
# [LLM responds]
# You: Tell me more about the first user
# ⠋ Thinking...
# [LLM responds with context from previous answer]
# Ctrl+C to exit

# Ask about specific capabilities
./ai-cli ask-llm "What tools are available for managing database connections?"
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

The CLI uses both Go standard library packages and external dependencies:

### Standard Library

- `flag` - Command-line flag parsing
- `encoding/json` - JSON serialization
- `net/http` - HTTP client
- `io` - I/O utilities
- `os` - Operating system interface
- `fmt` - Formatted I/O
- `bytes` - Byte buffer utilities
- `context` - Context handling for cancellation
- `sync` - Synchronization primitives for spinner
- `time` - Time operations for spinner animation
- `syscall` - System calls for terminal operations

### External Packages

- `golang.org/x/term` - Terminal operations for password input
- `github.com/anthropics/anthropic-sdk-go` - Anthropic Claude API (optional)
- `github.com/ollama/ollama` - Ollama API client (optional)

**Note**: External packages are only required if using the `ask-llm` command.
Basic CLI functionality (run-tool, read-resource, ping, list commands) works
without external dependencies.

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

## Features

### Implemented

- ✅ **LLM Integration** - Interactive conversations with AI assistants
- ✅ **Visual Feedback** - Animated spinner for LLM operations
- ✅ **Authentication** - Bearer token support with automatic credential
  prompting
- ✅ **Connection Management** - Full CRUD operations for database connections
- ✅ **Resource Listing** - Browse available resources, tools, and prompts
- ✅ **Interactive Mode** - Conversational interface with LLMs

### Future Enhancements

Potential improvements for future versions:

1. **Configuration File** - Support for configuration file (~/.ai-cli.yaml)
2. **Output Formats** - Support for YAML, table, and raw output formats
3. **Command History** - Save and recall previous commands
4. **Shell Completion** - Bash/Zsh completion scripts
5. **Verbose Mode** - Debug logging for troubleshooting
6. **Batch Mode** - Execute multiple commands from a file
7. **Tool Execution in LLM Mode** - Allow LLMs to directly execute tools
8. **Connection Testing** - Validate database connection parameters

## License

Copyright (c) 2025, pgEdge, Inc.
This software is released under The PostgreSQL License.
