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

The CLI tool supports three main commands:

### Basic Syntax

```bash
./ai-cli [--server URL] <command> [arguments]
```

### Options

- `--server URL`: MCP server URL (default: `http://localhost:8080`)
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

## Examples

### Viewing Data with Resources

Resources provide read-only access to system data:

```bash
# List all users
./ai-cli read-resource ai-workbench://users

# List all service tokens
./ai-cli read-resource ai-workbench://service-tokens
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

### Testing Server Connection

```bash
./ai-cli ping

# Using a different server
./ai-cli --server http://myserver:8080 ping
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
