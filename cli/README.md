# pgEdge AI DBA Workbench CLI

The AI CLI (Natural Language Agent) is a production-ready, full-featured native
Go implementation that provides an interactive command-line interface for
chatting with your PostgreSQL database using natural language.

## Features

- **Multiple LLM Providers**: Support for Anthropic Claude, OpenAI, and Ollama
- **HTTP Mode**: Connect to an MCP server via HTTP with authentication
- **Multiple Auth Methods**: Service token or username/password authentication
- **Runtime Configuration**: Switch LLM providers and models via slash commands
- **Prompt Caching**: Automatic Anthropic prompt caching (up to 90% savings)
- **Agentic Tool Execution**: Automatically executes database tools based on LLM
  decisions
- **PostgreSQL-Themed UI**: Colorful output with elephant-themed animations
- **Flexible Configuration**: Configure via YAML file, environment variables, or
  command-line flags
- **Built-in Commands**: Help, list tools, list resources, clear screen
- **Slash Commands**: Claude Code-style commands for settings and configuration
- **Conversation History**: Maintains context across multiple queries
- **Persistent History**: Command history saved across sessions

## Building

```bash
# Build the CLI
make build

# Run tests
make test

# Run linting
make lint
```

The binary is created at `bin/ai-cli`.

## Quick Start

1. **Build the CLI**:

   ```bash
   make build
   ```

2. **Set up LLM provider** (at least one):

   For Anthropic:

   ```bash
   export ANTHROPIC_API_KEY="your-api-key"
   # Or create file: echo "your-api-key" > ~/.anthropic-api-key
   ```

   For OpenAI:

   ```bash
   export OPENAI_API_KEY="your-api-key"
   ```

   For Ollama:

   ```bash
   # Make sure Ollama is running: ollama serve
   # Pull a model: ollama pull llama3
   ```

3. **Connect to MCP server**:

   ```bash
   # With service token
   ./bin/ai-cli -mcp-url http://localhost:8080 -mcp-token "your-token"

   # With username/password
   ./bin/ai-cli -mcp-url http://localhost:8080 -mcp-username admin
   ```

## Configuration

### Command Line Flags

| Flag | Description |
|------|-------------|
| `-config string` | Path to configuration file |
| `-version` | Show version and exit |
| `-mcp-url string` | MCP server URL |
| `-mcp-token string` | MCP server authentication token |
| `-mcp-username string` | MCP server username |
| `-mcp-password string` | MCP server password |
| `-llm-provider string` | LLM provider: anthropic, openai, or ollama |
| `-llm-model string` | LLM model to use |
| `-anthropic-api-key string` | API key for Anthropic |
| `-openai-api-key string` | API key for OpenAI |
| `-ollama-url string` | Ollama server URL |
| `-no-color` | Disable colored output |

### Configuration File

Create a configuration file at one of these locations:

- `./.ai-cli.yaml` (current directory)
- `~/.ai-cli.yaml` (home directory)
- `/etc/pgedge/ai-cli.yaml` (system-wide)

Example configuration:

```yaml
mcp:
  url: http://localhost:8080
  # token: your-token-here
  # Or use username/password:
  # username: admin
  # password: will-prompt-if-not-set

llm:
  provider: anthropic
  model: claude-sonnet-4-20250514
  # API keys (priority: env vars > key files > config values)
  anthropic_api_key_file: ~/.anthropic-api-key
  openai_api_key_file: ~/.openai-api-key
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
| `PGEDGE_MCP_TOKEN` | Authentication token |
| `PGEDGE_MCP_USERNAME` | Username for authentication |
| `PGEDGE_MCP_PASSWORD` | Password for authentication |
| `PGEDGE_LLM_PROVIDER` | LLM provider |
| `PGEDGE_LLM_MODEL` | LLM model name |
| `PGEDGE_ANTHROPIC_API_KEY` | Anthropic API key |
| `ANTHROPIC_API_KEY` | Anthropic API key (fallback) |
| `PGEDGE_OPENAI_API_KEY` | OpenAI API key |
| `OPENAI_API_KEY` | OpenAI API key (fallback) |
| `PGEDGE_OLLAMA_URL` | Ollama server URL |
| `NO_COLOR` | Disable colored output |

## Interactive Commands

Once the CLI is running, you can use these commands:

- `help` - Show available commands
- `quit` or `exit` - Exit the CLI (also Ctrl+C or Ctrl+D)
- `clear` - Clear the screen
- `tools` - List available MCP tools
- `resources` - List available MCP resources

### Slash Commands

| Command | Description |
|---------|-------------|
| `/help` | Show help for slash commands |
| `/set llm-provider <provider>` | Switch LLM provider |
| `/set llm-model <model>` | Change LLM model |
| `/set status-messages <on\|off>` | Toggle status messages |
| `/set markdown <on\|off>` | Toggle markdown rendering |
| `/set color <on\|off>` | Toggle colored output |
| `/show settings` | Display current settings |
| `/list models` | List available models from provider |
| `/history` | List saved conversations |
| `/history load <id>` | Load a saved conversation |
| `/save` | Save current conversation |
| `/new` | Start a new conversation |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| Escape | Cancel the current LLM request |
| Up/Down | Navigate through command history |
| Ctrl+R | Reverse search through history |
| Ctrl+C | Exit the CLI |
| Ctrl+D | Exit the CLI (EOF) |

## Authentication Modes

### Service Token

Use a pre-generated service token:

```bash
./bin/ai-cli -mcp-url http://localhost:8080 -mcp-token "your-service-token"
```

### Username/Password

Authenticate with username and password:

```bash
# Password will be prompted
./bin/ai-cli -mcp-url http://localhost:8080 -mcp-username admin

# Or provide password (less secure)
./bin/ai-cli -mcp-url http://localhost:8080 \
  -mcp-username admin \
  -mcp-password "yourpassword"
```

## Prompt Caching (Anthropic)

When using Anthropic Claude, the CLI automatically uses prompt caching to
reduce costs and improve response times:

- Tool definitions are cached after the first request
- Cached input tokens cost ~90% less than regular input tokens
- Cache entries expire after 5 minutes of inactivity

## Documentation

See the [CLI Documentation](../docs/cli/index.md) for detailed information.
