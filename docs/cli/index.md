# CLI Documentation

The pgEdge AI CLI (Natural Language Agent) is a production-ready command-line
interface for interacting with PostgreSQL databases using natural language.

## Overview

The CLI connects to the MCP server and uses AI language models (Anthropic
Claude, OpenAI, or Ollama) to translate your natural language queries into
database operations. Key features include:

- **Natural Language Queries**: Ask questions in plain English
- **Multiple LLM Support**: Choose between Anthropic, OpenAI, or local Ollama
- **Interactive Interface**: Real-time responses with colorful output
- **Conversation History**: Persistent conversations across sessions
- **Prompt Caching**: Cost savings with Anthropic prompt caching

## Getting Started

### Prerequisites

- Go 1.23 or higher (for building from source)
- Running MCP server instance
- LLM API key (Anthropic, OpenAI) or local Ollama installation

### Installation

Build from source:

```bash
cd cli
make build
```

The binary is created at `bin/ai-cli`.

### First Run

1. **Start the MCP server** (in another terminal):

   ```bash
   cd server
   ./bin/ai-dba-server
   ```

2. **Set up your LLM provider**:

   For Anthropic:

   ```bash
   export ANTHROPIC_API_KEY="your-api-key"
   ```

   For OpenAI:

   ```bash
   export OPENAI_API_KEY="your-api-key"
   ```

3. **Connect to the server**:

   ```bash
   ./bin/ai-cli -mcp-url http://localhost:8080 -mcp-username admin
   ```

   You'll be prompted for your password.

## Configuration

### Configuration File

Create `~/.ai-cli.yaml`:

```yaml
mcp:
  url: http://localhost:8080
  username: admin
  # Or use token authentication:
  # token: your-service-token

llm:
  provider: anthropic
  model: claude-sonnet-4-20250514
  anthropic_api_key_file: ~/.anthropic-api-key
  max_tokens: 4096
  temperature: 0.7

ui:
  no_color: false
  display_status_messages: true
  render_markdown: true
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `PGEDGE_MCP_URL` | MCP server URL |
| `PGEDGE_MCP_TOKEN` | Service token |
| `PGEDGE_MCP_USERNAME` | Username |
| `PGEDGE_LLM_PROVIDER` | LLM provider |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `OPENAI_API_KEY` | OpenAI API key |

## Authentication

The CLI supports two authentication methods:

### Username/Password

```bash
./bin/ai-cli -mcp-url http://localhost:8080 -mcp-username admin
```

Password will be prompted securely. After successful authentication, you receive
a 24-hour session token.

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
| `tools` | List MCP tools |
| `resources` | List MCP resources |

### Slash Commands

| Command | Description |
|---------|-------------|
| `/help` | Slash command help |
| `/set llm-provider <name>` | Switch LLM |
| `/set llm-model <name>` | Change model |
| `/show settings` | Show current settings |
| `/list models` | List available models |
| `/history` | List conversations |
| `/history load <id>` | Load conversation |
| `/save` | Save conversation |
| `/new` | New conversation |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| Escape | Cancel current request |
| Up/Down | Navigate history |
| Ctrl+R | Search history |
| Ctrl+C/D | Exit |

## LLM Providers

### Anthropic Claude

Recommended for best results. Supports prompt caching for cost savings.

```yaml
llm:
  provider: anthropic
  model: claude-sonnet-4-20250514
```

Available models:

- `claude-sonnet-4-20250514` (recommended)
- `claude-3-5-sonnet-20241022`
- `claude-3-opus-20240229`
- `claude-3-haiku-20240307`

### OpenAI

```yaml
llm:
  provider: openai
  model: gpt-4o
```

Available models:

- `gpt-4o`
- `gpt-4-turbo`
- `gpt-3.5-turbo`

### Ollama (Local)

For offline or private use:

```bash
# Start Ollama
ollama serve

# Pull a model
ollama pull llama3
```

```yaml
llm:
  provider: ollama
  model: llama3
  ollama_url: http://localhost:11434
```

## Conversation History

The CLI stores conversation history on the server when using user authentication
(not available with service tokens).

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
System: Loaded conversation
```

## Example Session

```
          _
   ______/ \-.   _           pgEdge AI CLI
.-/     (    o\_//           Type 'quit' to exit, 'help' for commands
 |  ___  \_/\---'
 |_||  |_||

System: Connected to MCP server at http://localhost:8080
System: Using LLM: anthropic (claude-sonnet-4-20250514)
────────────────────────────────────────────────────────────────────

You: What tables are in my database?

  → Executing tool: get_schema_info