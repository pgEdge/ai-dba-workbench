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

Execute an MCP tool with JSON arguments from stdin.

```bash
cat input.json | ./ai-cli run-tool <tool-name>
```

Example:

```bash
cat config_options.json | ./ai-cli run-tool set_config
```

The JSON input should contain the tool's required arguments.

#### 2. read-resource

Read an MCP resource by its URI.

```bash
./ai-cli read-resource <resource-uri>
```

Example:

```bash
./ai-cli read-resource system://stats
```

#### 3. ping

Test connectivity to the MCP server.

```bash
./ai-cli ping
```

## Examples

### Setting Configuration

Create a JSON file with configuration options:

```json
{
    "option1": "value1",
    "option2": "value2"
}
```

Execute the tool:

```bash
cat config.json | ./ai-cli run-tool set_config
```

### Reading System Statistics

```bash
./ai-cli read-resource system://stats
```

### Testing Server Connection

```bash
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
