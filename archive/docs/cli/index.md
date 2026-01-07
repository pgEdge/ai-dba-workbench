# pgEdge AI DBA Workbench CLI Documentation

## Quick Links

- [Main Documentation](../index.md) - Return to main documentation index
- [CLI README](../../cli/README.md) - Getting started guide

## Overview

The pgEdge AI DBA Workbench CLI (`ai-cli`) is a command-line interface tool for
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
- **Agentic tool execution** - LLMs can call MCP tools and receive results
  (both Anthropic Claude and Ollama)
- Conversation history management for interactive mode
- Automatic resource data fetching for context
- Tool and resource schema passing to LLMs
- Visual feedback with animated spinner during processing
- Multi-turn tool execution loop (up to 10 iterations)

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

Primary (AI_CLI_* prefix):
- `AI_CLI_ANTHROPIC_API_KEY` - API key for Anthropic Claude
- `AI_CLI_ANTHROPIC_MODEL` - Model to use (default: `claude-sonnet-4-5`)
- `AI_CLI_OLLAMA_URL` - Ollama server URL (default: `http://localhost:11434`)
- `AI_CLI_OLLAMA_MODEL` - Ollama model to use (default: `gpt-oss:20b`)

Legacy (fallback):
- `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL`, `OLLAMA_URL`, `OLLAMA_MODEL`

**Note:** For agentic tool execution with Ollama, use models with reliable
function calling support such as `gpt-oss:20b`, `llama3.1`, `llama3.2`,
`mistral`, or `mixtral`. qwen models (qwen3-coder, qwen2.5-coder) do not work
reliably with Ollama's function calling implementation. Older models like
`llama2` will receive tool descriptions but cannot execute them.

**Output Formatting:**

The CLI provides consistent, human-readable output regardless of the LLM
provider:

- **Automatic JSON Detection**: The CLI automatically detects when Ollama
  returns JSON-formatted responses and converts them to readable tables
- **Query Result Tables**: Database query results are formatted as ASCII tables
  with column headers and proper alignment
- **Consistent Experience**: Both Anthropic Claude and Ollama produce
  similarly-formatted output, ensuring a uniform user experience
- **Embedded JSON Handling**: JSON embedded within text responses is extracted,
  formatted, and displayed alongside the surrounding text

This formatting ensures that tool execution results (like database queries) are
presented clearly regardless of which LLM provider you use.

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

## LLM Configuration Commands

The CLI provides commands to manage LLM provider and model preferences. Settings
are stored in `~/.ai-workbench-cli.json`.

### set-llm

Sets the preferred LLM provider. The CLI will validate that the provider is
properly configured before allowing the switch.

**Usage:**

```bash
./ai-cli set-llm <provider>
```

**Parameters:**

- `provider` - Either `anthropic` or `ollama`

**Examples:**

```bash
# Switch to Ollama (always available)
./ai-cli set-llm ollama

# Switch to Anthropic (requires API key)
export AI_CLI_ANTHROPIC_API_KEY="your-key-here"
./ai-cli set-llm anthropic
```

**Validation:**

- For `anthropic`: Checks that Anthropic API key is configured (via `AI_CLI_ANTHROPIC_API_KEY`, config file, or legacy `ANTHROPIC_API_KEY`)
- For `ollama`: No validation (assumes Ollama is available at configured URL)

### show-llm

Displays the current LLM provider and related configuration.

**Usage:**

```bash
./ai-cli show-llm
```

**Example Output:**

```
LLM Provider: ollama
Model: llama3.1
URL: http://localhost:11434
```

Or for Anthropic:

```
LLM Provider: anthropic
Model: claude-sonnet-4-5
API Key: configured
```

### set-model

Sets the model name for the current LLM provider. The CLI tracks separate model
preferences for Anthropic and Ollama.

**Usage:**

```bash
./ai-cli set-model <model-name>
```

**Parameters:**

- `model-name` - Model name appropriate for the current provider

**Examples:**

```bash
# For Ollama (after setting provider to ollama)
./ai-cli set-model gpt-oss:20b
./ai-cli set-model llama3.1
./ai-cli set-model mistral

# For Anthropic (after setting provider to anthropic)
./ai-cli set-model claude-sonnet-4-5
./ai-cli set-model claude-opus-4
```

**Note:** The CLI does not validate model names - it's your responsibility to
ensure the model exists and is available.

### show-model

Displays the model name for the current LLM provider.

**Usage:**

```bash
./ai-cli show-model
```

**Example Output:**

```
Current LLM Provider: ollama
Ollama Model: llama3.1 (configured)
```

Or if using defaults:

```
Current LLM Provider: anthropic
Anthropic Model: claude-sonnet-4-5 (default)
```

### Configuration File

The CLI stores user preferences in `~/.ai-workbench-cli.json`:

```json
{
    "server_url": "http://localhost:8080",
    "preferred_llm": "ollama",
    "anthropic_api_key": "sk-ant-...",
    "anthropic_model": "claude-opus-4",
    "ollama_url": "http://localhost:11434",
    "ollama_model": "llama3.1"
}
```

**Fields:**

- `server_url` - AI DBA Workbench MCP server URL
- `preferred_llm` - User's preferred LLM provider (`anthropic` or `ollama`). If
  empty, auto-detects based on API key availability.
- `anthropic_api_key` - Anthropic API key (stored in plain text with 0600
  permissions)
- `anthropic_model` - Model name for Anthropic Claude
- `ollama_url` - Ollama server URL
- `ollama_model` - Model name for Ollama

**Configuration Priority:**

Settings are applied in this priority order (highest to lowest):

1. Command-line flags (e.g., `--server`)
2. `AI_CLI_*` environment variables
3. Configuration file settings
4. Legacy environment variables (`ANTHROPIC_*`, `OLLAMA_*`)
5. Built-in defaults

### Environment Variables

**AI_CLI_* Variables (Override config file):**

- `AI_CLI_SERVER_URL` - AI DBA Workbench MCP server URL
- `AI_CLI_ANTHROPIC_API_KEY` - Anthropic API key
- `AI_CLI_ANTHROPIC_MODEL` - Anthropic model name
- `AI_CLI_OLLAMA_URL` - Ollama server URL
- `AI_CLI_OLLAMA_MODEL` - Ollama model name

**Legacy Variables (Fallback):**

- `ANTHROPIC_API_KEY` - Anthropic API key (used if `AI_CLI_ANTHROPIC_API_KEY`
  and config file not set)
- `ANTHROPIC_MODEL` - Anthropic model (used if `AI_CLI_ANTHROPIC_MODEL` and
  config file not set)
- `OLLAMA_URL` - Ollama URL (used if `AI_CLI_OLLAMA_URL` and config file not
  set)
- `OLLAMA_MODEL` - Ollama model (used if `AI_CLI_OLLAMA_MODEL` and config file
  not set)

### Additional Configuration Commands

#### set-server

Sets the AI DBA Workbench MCP server URL in the config file.

**Usage:**

```bash
./ai-cli set-server <url>
```

**Example:**

```bash
./ai-cli set-server http://myserver:8080
```

#### set-anthropic-key

Sets the Anthropic API key in the config file.

**Usage:**

```bash
./ai-cli set-anthropic-key <api-key>
```

**Example:**

```bash
./ai-cli set-anthropic-key sk-ant-api03-xxx
```

**Security Note:** The API key is stored in plain text in
`~/.ai-workbench-cli.json` with 0600 file permissions. For better security,
consider using the `AI_CLI_ANTHROPIC_API_KEY` environment variable instead.

#### set-ollama-url

Sets the Ollama server URL in the config file.

**Usage:**

```bash
./ai-cli set-ollama-url <url>
```

**Example:**

```bash
./ai-cli set-ollama-url http://ollama.local:11434
```

#### show-config

Displays all configuration settings and their sources.

**Usage:**

```bash
./ai-cli show-config
```

**Example Output:**

```
Configuration File: ~/.ai-workbench-cli.json

Server Settings:
  Server URL: http://localhost:8080 (configured)
  Server URL Override: http://prod:8080 (AI_CLI_SERVER_URL env var)

LLM Provider:
  Provider: ollama (configured)

Anthropic Settings:
  API Key: configured (config file)
  Model: claude-opus-4 (configured)

Ollama Settings:
  URL: http://localhost:11434 (configured)
  Model: llama3.1 (configured)

Priority Order:
  1. Command-line flags (highest)
  2. AI_CLI_* environment variables
  3. Configuration file settings
  4. Legacy environment variables (ANTHROPIC_*, OLLAMA_*)
  5. Built-in defaults (lowest)
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
- ✅ **Agentic Tool Execution** - LLMs can directly execute MCP tools
  (Anthropic Claude and Ollama)
- ✅ **Visual Feedback** - Animated spinner with PostgreSQL-themed animations
- ✅ **Authentication** - Bearer token support with automatic credential
  prompting
- ✅ **Connection Management** - Full CRUD operations for database connections
- ✅ **Resource Listing** - Browse available resources, tools, and prompts
- ✅ **Interactive Mode** - Conversational interface with LLMs
- ✅ **Color-coded Output** - Blue for user input, red for system notices

### Future Enhancements

Potential improvements for future versions:

1. **Configuration File** - Support for configuration file (~/.ai-cli.yaml)
2. **Output Formats** - Support for YAML, table, and raw output formats
3. **Command History** - Save and recall previous commands
4. **Shell Completion** - Bash/Zsh completion scripts
5. **Verbose Mode** - Debug logging for troubleshooting
6. **Batch Mode** - Execute multiple commands from a file
7. **Connection Testing** - Validate database connection parameters

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.
This software is released under The PostgreSQL License.
