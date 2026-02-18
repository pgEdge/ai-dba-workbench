# CLI Documentation

The pgEdge AI CLI (Natural Language Agent) is a production-ready command-line
interface for interacting with PostgreSQL databases using natural language.

## Overview

The CLI connects to the MCP server and uses AI language models to translate
natural language queries into database operations.

The CLI includes the following features:

- Natural language queries allow users to ask questions in plain English.
- Multiple LLM support enables choice between Anthropic, OpenAI, or Ollama.
- The interactive interface provides real-time responses with colorful output.
- Conversation history persists across sessions for context continuity.
- Anthropic prompt caching reduces costs on repeated queries.

## Getting Started

### Prerequisites

The following requirements must be met before using the CLI:

- Go 1.23 or higher is required for building from source.
- A running MCP server instance must be available.
- The MCP server must have LLM API keys configured.

### Installation

Build from source:

```bash
cd cli
make build
```

The binary is created at `bin/ai-cli`.

### First Run

1. Start the MCP server in another terminal:

   ```bash
   cd server
   ./bin/ai-dba-server
   ```

2. Ensure the MCP server has LLM API keys configured. The server manages API
   keys for Anthropic, OpenAI, or Ollama connections.

3. Connect to the server:

   ```bash
   ./bin/ai-cli -mcp-url http://localhost:8080 -mcp-username admin
   ```

   The CLI prompts for your password securely.

## Configuration

### Configuration File

Create `~/.ai-dba-cli.yaml` with the following content:

```yaml
mcp:
  url: http://localhost:8080
  auth_mode: user
  username: admin
  # Or use token authentication:
  # auth_mode: token
  # token: your-service-token
  tls: false

llm:
  provider: anthropic
  model: claude-sonnet-4-5-20250929
  max_tokens: 4096
  temperature: 0.7

ui:
  no_color: false
  display_status_messages: true
  render_markdown: true
  debug: false
```

Note that API keys are configured on the MCP server rather than the CLI.

### Environment Variables

The following environment variables configure CLI behavior:

| Variable | Description |
|----------|-------------|
| `PGEDGE_MCP_URL` | MCP server URL |
| `PGEDGE_MCP_TOKEN` | Service token for authentication |
| `PGEDGE_MCP_USERNAME` | Username for user authentication |
| `PGEDGE_MCP_PASSWORD` | Password for user authentication |
| `PGEDGE_LLM_PROVIDER` | LLM provider (anthropic, openai, ollama) |
| `PGEDGE_LLM_MODEL` | LLM model name |
| `NO_COLOR` | Disable colored output |

API keys are configured on the MCP server.

## Authentication

The CLI supports two authentication methods:

### Username/Password

```bash
./bin/ai-cli -mcp-url http://localhost:8080 -mcp-username admin
```

The CLI prompts for your password securely. After successful authentication,
you receive a 24-hour session token.

### Service Token

```bash
./bin/ai-cli -mcp-url http://localhost:8080 -mcp-token "your-token"
```

Service tokens are pre-generated via the server CLI and don't expire (unless
configured with an expiry).

## Interactive Commands

### Basic Commands

| Command | Description |
|---------|-------------|
| `help` | Show available commands |
| `quit` / `exit` | Exit the CLI |
| `clear` | Clear the screen |

### Slash Commands

**Navigation:**

| Command | Description |
|---------|-------------|
| `/help` | Show slash command help |
| `/clear` | Clear the screen |
| `/quit`, `/exit` | Exit the CLI |

**LLM Settings:**

| Command | Description |
|---------|-------------|
| `/list providers` | List available LLM providers |
| `/list models` | List available models for current provider |
| `/set provider <name>` | Set LLM provider (anthropic, openai, ollama) |
| `/set model <name>` | Set LLM model |
| `/show provider` | Show current LLM provider |
| `/show model` | Show current LLM model |

**Database Connections:**

| Command | Description |
|---------|-------------|
| `/list connections` | List available database connections |
| `/list databases` | List databases on selected connection |
| `/set connection <id>` | Select a database connection |
| `/set connection none` | Clear the current connection |
| `/show connection` | Show current connection |

**MCP Resources:**

| Command | Description |
|---------|-------------|
| `/list tools` | List available MCP tools |
| `/list resources` | List available MCP resources |
| `/list prompts` | List available MCP prompts |
| `/prompt <name> [args]` | Execute an MCP prompt |

**Display Settings:**

| Command | Description |
|---------|-------------|
| `/set color on\|off` | Enable/disable colored output |
| `/set markdown on\|off` | Enable/disable markdown rendering |
| `/set status-messages on\|off` | Enable/disable status messages |
| `/set debug on\|off` | Enable/disable debug messages |
| `/show settings` | Show all current settings |

**Conversation History:**

| Command | Description |
|---------|-------------|
| `/history list` | List saved conversations |
| `/history load <id>` | Load a saved conversation |
| `/history rename <id> "title"` | Rename a saved conversation |
| `/history delete <id>` | Delete a saved conversation |
| `/history delete-all` | Delete all saved conversations |
| `/new` | Start a new conversation |
| `/save` | Save current conversation |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| Escape | Cancel current request |
| Up/Down | Navigate history |
| Ctrl+R | Search history |
| Ctrl+C/D | Exit |

## LLM Providers

### Anthropic Claude

Anthropic Claude is recommended for best results and supports prompt caching.

```yaml
llm:
  provider: anthropic
  model: claude-sonnet-4-5-20250929
```

The following Anthropic models are available:

- The `claude-sonnet-4-5-20250929` model is recommended for most use cases.
- The `claude-3-5-sonnet-20241022` model provides an alternative option.
- The `claude-3-opus-20240229` model offers higher capability at higher cost.
- The `claude-3-haiku-20240307` model provides faster responses at lower cost.

### OpenAI

OpenAI models provide an alternative to Anthropic.

```yaml
llm:
  provider: openai
  model: gpt-4o
```

The following OpenAI models are available:

- The `gpt-4o` model is the default for OpenAI provider.
- The `gpt-4-turbo` model offers high performance with large context.
- The `gpt-3.5-turbo` model provides faster responses at lower cost.

### Ollama (Local)

Ollama enables offline or private use with locally hosted models.

In the following example, the user starts Ollama and pulls a model:

```bash
# Start Ollama
ollama serve

# Pull a model
ollama pull qwen3-coder:latest
```

Configure the CLI to use Ollama:

```yaml
llm:
  provider: ollama
  model: qwen3-coder:latest
```

The default Ollama URL is `http://localhost:11434`.

## Conversation History

The CLI stores conversation history on the server when using user
authentication. This feature is not available with service tokens.

### Saving Conversations

```
You: What tables are in my database?
...
You: /save
System: Conversation saved (ID: conv_123456789)
```

### Loading Conversations

```
You: /history
System: Saved conversations (2):
  conv_123456789 - What tables exist?
  conv_987654321 - Query performance

You: /history load conv_123456789
System: Loaded conversation: What tables exist?
```

## Example Session

```
          _
   ______/ \-.   _           pgEdge AI DBA Workbench
.-/     (    o\_//           CLI: v1.0.0-alpha1  Server: v1.0.0-alpha1
 |  ___  \_/\---'            Type /quit to leave, /help for commands
 |_||  |_||

System: Connected to MCP server at http://localhost:8080
System: Using LLM: anthropic (claude-sonnet-4-5-20250929)
────────────────────────────────────────────────────────────────────

You: What tables are in my database?

  → Executing tool: get_schema_info