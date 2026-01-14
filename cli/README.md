# pgEdge AI DBA Workbench CLI

The AI CLI (Natural Language Agent) is a production-ready command-line
interface for interacting with PostgreSQL databases using natural language.

## Features

The CLI provides the following capabilities:

- The application supports multiple LLM providers including Anthropic, OpenAI,
  and Ollama.
- Users can connect to an MCP server via HTTP with token or user
  authentication.
- The CLI enables runtime configuration changes via slash commands.
- Anthropic prompt caching provides up to 90% cost savings on repeated queries.
- The agentic tool execution system automatically runs database tools based on
  LLM decisions.
- A PostgreSQL-themed UI displays colorful output with elephant animations.
- Configuration supports YAML files, environment variables, and command-line
  flags.
- Built-in slash commands provide access to tools, resources, and settings.
- The system maintains conversation context across multiple queries.
- Command history persists across sessions for convenient recall.

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

1. Build the CLI:

   ```bash
   make build
   ```

2. Ensure the MCP server is running and has LLM API keys configured. The
   server manages API keys for Anthropic, OpenAI, or Ollama connections.

3. Connect to the MCP server:

   ```bash
   # With service token
   ./bin/ai-cli -mcp-url http://localhost:8080 -mcp-token "your-token"

   # With username/password
   ./bin/ai-cli -mcp-url http://localhost:8080 -mcp-username admin
   ```

## Configuration

### Command Line Flags

The CLI accepts the following command-line flags:

| Flag | Description |
|------|-------------|
| `-config` | Path to configuration file |
| `-version` | Show version and exit |
| `-mcp-url` | MCP server URL (required) |
| `-mcp-token` | MCP server authentication token (for token mode) |
| `-mcp-username` | MCP server username (for user mode) |
| `-mcp-password` | MCP server password (for user mode) |
| `-llm-provider` | LLM provider: anthropic, openai, or ollama |
| `-llm-model` | LLM model to use |
| `-no-color` | Disable colored output |

Note that API keys are configured on the MCP server, not the CLI.

### Configuration File

The CLI searches for configuration files in the following locations:

- `./.ai-dba-cli.yaml` (current directory)
- `~/.ai-dba-cli.yaml` (home directory)
- `/etc/pgedge/ai-dba-cli.yaml` (system-wide)

The following example shows the available configuration options:

```yaml
mcp:
  url: http://localhost:8080
  auth_mode: user  # "token" or "user"
  # token: your-token-here
  # username: admin
  # password: will-prompt-if-not-set
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

For a complete configuration example with comments, see
[examples/ai-dba-cli.yaml](../examples/ai-dba-cli.yaml).

### Environment Variables

The following environment variables configure CLI behavior:

| Variable | Description |
|----------|-------------|
| `PGEDGE_MCP_URL` | MCP server URL |
| `PGEDGE_MCP_TOKEN` | Authentication token |
| `PGEDGE_MCP_USERNAME` | Username for authentication |
| `PGEDGE_MCP_PASSWORD` | Password for authentication |
| `PGEDGE_LLM_PROVIDER` | LLM provider (anthropic, openai, ollama) |
| `PGEDGE_LLM_MODEL` | LLM model name |
| `NO_COLOR` | Disable colored output |

Note that LLM API keys are configured on the MCP server.

## Interactive Commands

The CLI supports the following basic commands:

- The `help` command shows available commands.
- The `quit` or `exit` command exits the CLI.
- The `clear` command clears the screen.

### Slash Commands

Slash commands provide access to settings and configuration.

| Command | Description |
|---------|-------------|
| `/help` | Show help for slash commands |
| `/list providers` | List available LLM providers |
| `/list models` | List available models for current provider |
| `/list connections` | List available database connections |
| `/list tools` | List available MCP tools |
| `/list resources` | List available MCP resources |
| `/set provider <name>` | Switch LLM provider |
| `/set model <name>` | Change LLM model |
| `/set connection <id>` | Select a database connection |
| `/set status-messages <on\|off>` | Toggle status messages |
| `/set markdown <on\|off>` | Toggle markdown rendering |
| `/set color <on\|off>` | Toggle colored output |
| `/set debug <on\|off>` | Toggle debug messages |
| `/show provider` | Show current LLM provider |
| `/show model` | Show current LLM model |
| `/show connection` | Show current database connection |
| `/show settings` | Display all current settings |
| `/history list` | List saved conversations |
| `/history show <id>` | Show a saved conversation |
| `/history continue <id>` | Continue a saved conversation |
| `/history delete <id>` | Delete a saved conversation |
| `/save [name]` | Save current conversation |
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
reduce costs and improve response times.

The caching system provides the following benefits:

- Tool definitions are cached after the first request.
- Cached input tokens cost approximately 90% less than regular input tokens.
- Cache entries expire after 5 minutes of inactivity.

## Documentation

See the [CLI Documentation](../docs/cli/index.md) for detailed information.

---

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

We welcome your project contributions; for more information, see
[docs/developers.md](../docs/developers.md).

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](../LICENSE.md).
