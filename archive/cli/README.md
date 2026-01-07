# pgEdge AI DBA Workbench CLI

[![Build CLI](https://github.com/pgEdge/ai-workbench/actions/workflows/build-cli.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/build-cli.yml)
[![Test CLI](https://github.com/pgEdge/ai-workbench/actions/workflows/test-cli.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/test-cli.yml)
[![Lint CLI](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-cli.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-cli.yml)

A command-line interface tool for interacting with the pgEdge AI DBA Workbench MCP
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

**Interactive Shell Mode**: If you run the CLI without any command arguments,
it will enter an interactive shell mode with a command prompt. Shell mode
provides persistent command history and is ideal for exploratory work.

```bash
# Enter interactive shell mode
./ai-cli

# You'll see a prompt where you can enter commands
ai-workbench> help
ai-workbench> list-tools
ai-workbench> show-config
ai-workbench> quit
```

### Options

- `--server URL`: MCP server URL (default: `http://localhost:8080`)
- `--token TOKEN`: Bearer token for authentication
- `--version`: Show version information

### Shell Mode Features

When running in interactive shell mode (no command arguments):

- **Persistent Command History**: Shell commands and LLM conversations are
  stored separately in `~/.ai-workbench-cli-shell-history` and
  `~/.ai-workbench-cli-llm-history`
- **Interactive Tool Prompting**: When executing tools with `run-tool`, the CLI
  will interactively prompt you for each required parameter based on the tool's
  schema
- **LLM Conversation Mode**: Use the `ask` command to enter an interactive LLM
  conversation with persistent history
- **Tab Completion**: Use arrow keys to navigate history and edit commands
- **Color Output**: Commands and prompts are color-coded for better readability

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

The CLI supports two LLM providers with full agentic tool execution:

1. **Anthropic Claude** (preferred if configured) - All models support tool
   calling
2. **Ollama** (local models) - Requires models with reliable function calling
   support (e.g., gpt-oss:20b, llama3.1, llama3.2, mistral, mixtral). Note:
   qwen models do not work reliably with Ollama's function calling.

**Output Formatting:** Both providers return responses in a consistent,
human-readable format. The CLI automatically detects and formats JSON responses
from Ollama into readable tables, matching Anthropic's natural output style.
This ensures a consistent user experience regardless of the LLM provider.

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

The LLM integration is configured through environment variables or config file:

**Primary (AI_CLI_* variables):**
- `AI_CLI_ANTHROPIC_API_KEY`: API key for Anthropic Claude (if set, Anthropic is
  preferred)
- `AI_CLI_ANTHROPIC_MODEL`: Model to use (default: `claude-sonnet-4-5`)
- `AI_CLI_OLLAMA_URL`: Ollama server URL (default: `http://localhost:11434`)
- `AI_CLI_OLLAMA_MODEL`: Ollama model to use (default: `gpt-oss:20b`)

**Legacy (fallback if AI_CLI_* not set):**
- `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL`, `OLLAMA_URL`, `OLLAMA_MODEL`

**Config File:**
- `~/.ai-workbench-cli.json` - Persistent storage for all settings (see
  Configuration Commands below)

**Features:**

- **Agentic Tool Execution**: The LLM can directly execute MCP tools and
  receive results, enabling it to perform actions on your behalf
- **Resource Access**: The LLM has access to all MCP resources, including
  actual data from static resources (like users, groups, service tokens)
- **Multi-turn Conversations**: The LLM can make multiple tool calls in
  sequence to complete complex tasks (up to 10 iterations)
- **Visual Feedback**: Animated spinner with PostgreSQL-themed messages while
  processing
- **Color-coded Output**: Blue for user input, red for system notices

#### 8. LLM Configuration Commands

The CLI provides commands to manage LLM provider and model preferences. Settings
are stored in `~/.ai-workbench-cli.json`.

##### set-llm

Set the preferred LLM provider:

```bash
./ai-cli set-llm <provider>
```

Available providers: `anthropic`, `ollama`

Example:

```bash
# Switch to Ollama
./ai-cli set-llm ollama

# Switch to Anthropic (requires API key to be configured)
export AI_CLI_ANTHROPIC_API_KEY="your-key"
./ai-cli set-llm anthropic
```

##### show-llm

Display current LLM provider and configuration:

```bash
./ai-cli show-llm
```

##### set-model

Set the model name for the current LLM provider:

```bash
./ai-cli set-model <model-name>
```

Examples:

```bash
# For Ollama
./ai-cli set-model llama3.1

# For Anthropic
./ai-cli set-model claude-opus-4
```

##### show-model

Display the model name for the current LLM provider:

```bash
./ai-cli show-model
```

## Configuration File

The CLI stores persistent settings in `~/.ai-workbench-cli.json`. This file is
automatically created when you use configuration commands like `set-llm`,
`set-model`, or `set-anthropic-key`.

### File Location and Permissions

- **Path**: `~/.ai-workbench-cli.json` (in your home directory)
- **Permissions**: `0600` (read/write for owner only)
- **Format**: JSON

### Configuration Structure

Here's an example configuration file with all available options:

```json
{
    "server_url": "http://localhost:8080",
    "preferred_llm": "anthropic",
    "anthropic_api_key": "sk-ant-...",
    "anthropic_model": "claude-sonnet-4-5",
    "ollama_url": "http://localhost:11434",
    "ollama_model": "gpt-oss:20b"
}
```

### Configuration Options

#### Server Settings

- **`server_url`** (string, optional): The AI DBA Workbench MCP server URL
  - Default: `http://localhost:8080`
  - Example: `"http://myserver:8080"`
  - Can be overridden by: `--server` flag or `AI_CLI_SERVER_URL` environment
    variable

#### LLM Provider Settings

- **`preferred_llm`** (string, optional): Preferred LLM provider
  - Values: `"anthropic"` or `"ollama"`
  - Default: Auto-detect (prefers Anthropic if API key is set)
  - Example: `"anthropic"`
  - Set using: `./ai-cli set-llm <provider>`

#### Anthropic Settings

- **`anthropic_api_key`** (string, optional): Anthropic API key
  - Format: `"sk-ant-..."`
  - Can be overridden by: `AI_CLI_ANTHROPIC_API_KEY` environment variable
  - Set using: `./ai-cli set-anthropic-key <key>`
  - **Security Note**: Only store keys in this file if your home directory is
    secure

- **`anthropic_model`** (string, optional): Anthropic model name
  - Default: `"claude-sonnet-4-5"`
  - Examples: `"claude-opus-4"`, `"claude-sonnet-4-5"`
  - Can be overridden by: `AI_CLI_ANTHROPIC_MODEL` environment variable
  - Set using: `./ai-cli set-model <model-name>` (when Anthropic is active)

#### Ollama Settings

- **`ollama_url`** (string, optional): Ollama server URL
  - Default: `"http://localhost:11434"`
  - Example: `"http://192.168.1.100:11434"`
  - Can be overridden by: `AI_CLI_OLLAMA_URL` environment variable
  - Set using: `./ai-cli set-ollama-url <url>`

- **`ollama_model`** (string, optional): Ollama model name
  - Default: `"gpt-oss:20b"`
  - Examples: `"gpt-oss:20b"`, `"llama3.1"`, `"llama3.2"`, `"mistral"`, `"mixtral"`
  - Can be overridden by: `AI_CLI_OLLAMA_MODEL` environment variable
  - Set using: `./ai-cli set-model <model-name>` (when Ollama is active)
  - **Note**: Requires models with reliable function calling support for agentic
    tool execution. qwen models (qwen3-coder, qwen2.5-coder) do not work
    reliably with Ollama's function calling implementation.

### Configuration Priority

Settings are resolved in the following priority order (highest to lowest):

1. **Command-line flags** (e.g., `--server http://myserver:8080`)
2. **AI_CLI_* environment variables** (e.g., `AI_CLI_SERVER_URL`)
3. **Configuration file** (`~/.ai-workbench-cli.json`)
4. **Legacy environment variables** (e.g., `ANTHROPIC_API_KEY`)
5. **Built-in defaults**

This allows you to:

- Store common settings in the config file
- Override with environment variables for specific sessions
- Override with flags for individual commands

### Viewing Current Configuration

Use the `show-config` command to see all configuration values and their
sources:

```bash
./ai-cli show-config
```

Example output:

```
Server Settings:
  Server URL: http://localhost:8080 (default)

LLM Provider:
  Active Provider: anthropic (config file)

Anthropic Settings:
  API Key: sk-ant-... (config file)
  Model: claude-opus-4 (config file)

Ollama Settings:
  Server URL: http://localhost:11434 (default)
  Model: llama3.1 (config file)

Configuration Priority Order:
  1. Command-line flags (highest)
  2. AI_CLI_* environment variables
  3. Configuration file settings
  4. Legacy environment variables
  5. Built-in defaults (lowest)
```

### Example Workflows

#### Setting Up for Development

Store common development settings in the config file:

```bash
# Set your development server
./ai-cli set-server http://localhost:8080

# Configure Ollama for local testing
./ai-cli set-llm ollama
./ai-cli set-model llama3.1

# Now all commands use these settings by default
./ai-cli ask-llm "List all users"
```

#### Using Different Environments

Use environment variables to override for production:

```bash
# Development uses config file settings
./ai-cli list-resources

# Production uses environment override
AI_CLI_SERVER_URL=https://prod.example.com:8080 ./ai-cli list-resources

# Or use command-line flag
./ai-cli --server https://prod.example.com:8080 list-resources
```

#### Switching Between LLM Providers

Store configuration for both providers, switch as needed:

```bash
# Set up Anthropic credentials once
./ai-cli set-anthropic-key sk-ant-...
./ai-cli set-llm anthropic
./ai-cli set-model claude-opus-4

# Switch to Ollama when rate-limited
./ai-cli set-llm ollama

# Switch back to Anthropic
./ai-cli set-llm anthropic
```

#### Secure API Key Management

For shared or less secure environments, use environment variables instead of
storing keys in the config file:

```bash
# Don't store API key in config file
./ai-cli set-llm anthropic
./ai-cli set-model claude-opus-4

# Set API key in environment (per-session)
export AI_CLI_ANTHROPIC_API_KEY="sk-ant-..."

# Now use the CLI
./ai-cli ask-llm "What databases are available?"
```

### Security Considerations

**API Keys in Configuration File:**

- The config file is created with `0600` permissions (owner read/write only)
- Only store API keys in the config file if your home directory is secure
- For shared systems or CI/CD environments, prefer environment variables
- Never commit the config file to version control

**Best Practices:**

- Use environment variables for CI/CD pipelines
- Use config file for personal development machines
- Regularly rotate API keys
- Use service tokens for automation (see MCP server documentation)

### Manual Configuration

While the CLI commands are the recommended way to manage configuration, you can
also manually edit `~/.ai-workbench-cli.json`:

```bash
# Edit configuration file
nano ~/.ai-workbench-cli.json

# Verify configuration
./ai-cli show-config
```

Ensure the file remains valid JSON and maintains `0600` permissions after
manual edits.

## Examples

### Using Interactive Shell Mode

The shell mode provides an interactive experience with command history and
interactive prompting:

```bash
# Start the interactive shell
./ai-cli

# The shell provides a prompt for entering commands
ai-workbench> help
# Shows all available commands

# List available tools
ai-workbench> list-tools

# Run a tool interactively (CLI will prompt for each parameter)
ai-workbench> run-tool create_user
# You'll be prompted:
# username (required):
#   Type: string
#   Value: alice
# email (required):
#   Type: string
#   Value: alice@example.com
# ...etc

# Enter interactive LLM conversation mode
ai-workbench> ask
# Now in LLM mode with separate history
You: What users are in the system?
# LLM responds...
You: Tell me more about the first user
# LLM responds with context...
# Press Ctrl+C to exit LLM mode and return to shell

# Check current configuration
ai-workbench> show-config

# Exit the shell
ai-workbench> quit
```

**Shell History**: Commands are automatically saved to
`~/.ai-workbench-cli-shell-history` and LLM conversations to
`~/.ai-workbench-cli-llm-history`. Use arrow keys to navigate history.

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
export AI_CLI_ANTHROPIC_API_KEY="your-api-key"
./ai-cli ask-llm "What users are in the system?"

# Or use Ollama (local)
# Make sure Ollama is running with a model installed
./ai-cli ask-llm "How do I create a new user?"

# The LLM has access to all tools and resources
./ai-cli ask-llm "What service tokens exist and what are they used for?"
```

### Configuring LLM Preferences

The CLI allows you to configure LLM provider and model preferences:

```bash
# Check current LLM configuration
./ai-cli show-llm

# Switch to Ollama with a function-calling capable model
./ai-cli set-llm ollama
./ai-cli set-model llama3.1

# Verify the configuration
./ai-cli show-llm
./ai-cli show-model

# Now use the LLM
./ai-cli ask-llm "List tables in the production database"

# Switch to Anthropic with a different model
export AI_CLI_ANTHROPIC_API_KEY="your-key"
./ai-cli set-llm anthropic
./ai-cli set-model claude-opus-4
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

Copyright (c) 2025 - 2026, pgEdge, Inc.
This software is released under The PostgreSQL License.
