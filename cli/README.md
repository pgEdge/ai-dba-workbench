# pgEdge AI Workbench CLI

[![Build CLI](https://github.com/pgEdge/ai-workbench/actions/workflows/build-cli.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/build-cli.yml)
[![Test CLI](https://github.com/pgEdge/ai-workbench/actions/workflows/test-cli.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/test-cli.yml)
[![Lint CLI](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-cli.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-cli.yml)

A command-line interface tool for interacting with the pgEdge AI Workbench MCP
(Model Context Protocol) server.

## Installation

Build the CLI tool:

```bash
make build
```

This will create the `ai-cli` binary in the `cli` directory.

## Usage

The CLI tool supports several commands for interacting with the MCP server and
LLMs.

### Basic Syntax

```bash
./ai-cli [--server URL] <command> [arguments]
```

### Options

- `--server URL`: MCP server URL (default: `http://localhost:8080`)
- `--token TOKEN`: Bearer token for authentication
- `--version`: Show version information

### Commands

#### 1. run-tool

Execute an MCP tool with optional JSON arguments from stdin.

```bash
# With JSON input
cat input.json | ./ai-cli run-tool <tool-name>

# Without JSON input (uses empty object {})
./ai-cli run-tool <tool-name>
```

Example:

```bash
# With arguments (e.g., creating a user)
echo '{"username": "alice", "email": "alice@example.com", "fullName": "Alice Smith", "password": "secret"}' | ./ai-cli run-tool create_user

# Without arguments (sends empty object {})
# Note: Most tools require arguments, but the CLI will send {} if none provided
./ai-cli run-tool some_tool
```

The JSON input should contain the tool's required arguments. If no input is
provided, an empty JSON object `{}` will be sent to the tool.

#### 2. read-resource

Read an MCP resource by its URI. Resources provide read-only access to data
like lists of users, tokens, and other system information.

```bash
./ai-cli read-resource <resource-uri>
```

Example:

```bash
# List all users
./ai-cli read-resource ai-workbench://users

# List all service tokens
./ai-cli read-resource ai-workbench://service-tokens
```

**Note:** Use `read-resource` for viewing/listing data, and `run-tool` for
performing actions (create, update, delete).

#### 3. ping

Test connectivity to the MCP server.

```bash
./ai-cli ping
```

#### 4. list-resources

List all available MCP resources.

```bash
./ai-cli list-resources
```

#### 5. list-tools

List all available MCP tools.

```bash
./ai-cli list-tools
```

#### 6. list-prompts

List all available MCP prompts.

```bash
./ai-cli list-prompts
```

#### 7. ask-llm

Ask a question to an LLM (Large Language Model) that has access to all MCP
tools, resources, and prompts. The LLM can help you understand the system,
query data, and suggest actions.

```bash
./ai-cli ask-llm [query]
```

**Interactive Mode**: If no query is provided, the CLI enters interactive mode
where you can have a multi-turn conversation with the LLM. The conversation
history is maintained, so the LLM remembers previous questions and answers.
Press Ctrl+C to exit.

**Visual Feedback**: While waiting for the LLM to respond, an animated spinner
is displayed to indicate that processing is in progress.

The CLI supports two LLM providers:

1. **Anthropic Claude** (preferred if configured)
2. **Ollama** (local models)

Example:

```bash
# Single question mode
./ai-cli ask-llm "List all users in the system"

# Interactive mode - have a conversation
./ai-cli ask-llm
# You: What users are in the system?
# [LLM responds]
# You: Tell me more about the first user
# [LLM responds with context from previous answer]
# Ctrl+C to exit

# Ask about available tools
./ai-cli ask-llm "What tools are available for managing users?"

# Get help with a specific task
./ai-cli ask-llm "How do I create a new service token?"
```

**Configuration:**

The LLM integration is configured through environment variables:

- `ANTHROPIC_API_KEY`: API key for Anthropic Claude (if set, Anthropic is
  preferred)
- `ANTHROPIC_MODEL`: Model to use (default: `claude-sonnet-4-5`)
- `OLLAMA_URL`: Ollama server URL (default: `http://localhost:11434`)
- `OLLAMA_MODEL`: Ollama model to use (default: `llama2`)

**Note:** The LLM will have access to all MCP tools and resources, including
the actual data from static resources (like users, groups, service tokens). It
can use this data to answer questions directly. Tool execution is not yet
implemented in this basic version, so the LLM can only describe what actions
it would take.

## Examples

### Viewing Data with Resources

Resources provide read-only access to system data:

```bash
# List all users
./ai-cli read-resource ai-workbench://users

# List all service tokens
./ai-cli read-resource ai-workbench://service-tokens

# List all database connections
./ai-cli read-resource ai-workbench://connections
```

### Performing Actions with Tools

Tools are used for create, update, delete, and other actions:

```bash
# Create a new user
echo '{
  "username": "alice",
  "email": "alice@example.com",
  "fullName": "Alice Smith",
  "password": "secret123"
}' | ./ai-cli run-tool create_user

# Update a user
echo '{
  "username": "alice",
  "email": "newemail@example.com"
}' | ./ai-cli run-tool update_user

# Delete a user
echo '{"username": "alice"}' | ./ai-cli run-tool delete_user
```

### Managing Database Connections

Database connections can be created, updated, and deleted:

```bash
# Create a new database connection
echo '{
  "name": "Production DB",
  "host": "prod.example.com",
  "port": 5432,
  "databaseName": "myapp",
  "username": "dbuser",
  "password": "dbpass123",
  "sslmode": "require",
  "isMonitored": true
}' | ./ai-cli run-tool create_connection

# Update a connection
echo '{
  "id": 1,
  "isMonitored": false,
  "sslmode": "verify-full"
}' | ./ai-cli run-tool update_connection

# Delete a connection
echo '{"id": 1}' | ./ai-cli run-tool delete_connection
```

### Testing Server Connection

```bash
./ai-cli ping

# Using a different server
./ai-cli --server http://myserver:8080 ping
```

### Using the LLM Integration

The ask-llm command allows you to interact with an LLM that has access to all
MCP tools and resources:

```bash
# Set up Anthropic (preferred)
export ANTHROPIC_API_KEY="your-api-key"
./ai-cli ask-llm "What users are in the system?"

# Or use Ollama (local)
# Make sure Ollama is running with a model installed
./ai-cli ask-llm "How do I create a new user?"

# The LLM has access to all tools and resources
./ai-cli ask-llm "What service tokens exist and what are they used for?"
```

## Output

All commands output pretty-printed JSON for easy inspection:

```json
{
    "status": "success",
    "data": {
        "key": "value"
    }
}
```

## Error Handling

The CLI provides clear error messages for:

- Connection failures
- Invalid JSON input
- MCP protocol errors
- HTTP errors

Exit codes:
- 0: Success
- 1: Error

## Development

See the [documentation](../docs/cli/index.md) for detailed information about
the CLI architecture and development guidelines.

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Linting

```bash
make lint
```

### All Checks

Run all checks before committing:

```bash
make check
```

## License

Copyright (c) 2025, pgEdge, Inc.
This software is released under The PostgreSQL License.
