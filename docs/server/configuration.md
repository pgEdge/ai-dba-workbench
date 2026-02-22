# Configuration Guide

The MCP server can be configured through a YAML configuration file, command-line
flags, or environment variables.

## Configuration Priority

Configuration values are applied in the following order (highest to lowest
priority):

1. Command-line flags (highest priority)
2. Environment variables
3. Configuration file
4. Built-in defaults (lowest priority)

## Configuration File

### File Locations

The server searches for configuration in the following order:

1. Path specified via `-config` flag
2. `/etc/pgedge/ai-dba-server.yaml` (system-wide)
3. `./ai-dba-server.yaml` (binary directory)

### Example Configuration

A complete example configuration file is available at
[examples/ai-dba-server.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-server.yaml)
in the project root directory.

```yaml
#=========================================================================
# HTTP SERVER CONFIGURATION
#=========================================================================
http:
  # Address to listen on (host:port or :port for all interfaces)
  address: ":8080"

  # TLS/HTTPS Configuration
  tls:
    enabled: false
    # cert_file: "/etc/ai-workbench/certs/server-cert.pem"
    # key_file: "/etc/ai-workbench/certs/server-key.pem"
    # chain_file: "/etc/ai-workbench/certs/chain.pem"

  # Trusted reverse proxies (CIDR ranges)
  # trusted_proxies:
  #   - "10.0.0.0/8"
  #   - "172.16.0.0/12"

  # CORS origin for cross-origin deployments
  # cors_origin: "https://workbench.example.com"

  # Authentication Configuration
  auth:
    enabled: true
    max_failed_attempts_before_lockout: 0  # 0 = disabled
    max_user_token_days: 0  # 0 = unlimited
    rate_limit_window_minutes: 15
    rate_limit_max_attempts: 10

#=========================================================================
# CONNECTION SECURITY
#=========================================================================
connection_security:
  allow_internal_networks: false
  # allowed_hosts:
  #   - "db.example.com"
  #   - "192.168.1.0/24"
  # blocked_hosts:
  #   - "169.254.169.254"

#=========================================================================
# DATABASE CONNECTION
#=========================================================================
database:
  host: "localhost"
  port: 5432
  database: "ai_workbench"
  user: "postgres"
  # password: ""  # If not set, uses .pgpass file
  sslmode: "prefer"
  pool_max_conns: 4
  pool_min_conns: 0
  pool_max_conn_idle_time: "30m"

#=========================================================================
# EMBEDDING GENERATION
#=========================================================================
embedding:
  enabled: false
  provider: "ollama"  # ollama, voyage, or openai
  model: "nomic-embed-text"
  ollama_url: "http://localhost:11434"
  # voyage_api_key_file: "~/.voyage-api-key"
  # voyage_base_url: "https://api.voyageai.com/v1/embeddings"
  # openai_api_key_file: "~/.openai-api-key"
  # openai_base_url: "https://api.openai.com/v1"

#=========================================================================
# LLM CONFIGURATION (Web Client Chat Proxy)
#=========================================================================
llm:
  provider: "anthropic"  # anthropic, openai, gemini, or ollama
  model: "claude-sonnet-4-5"
  # anthropic_api_key_file: "~/.anthropic-api-key"
  # anthropic_base_url: "https://api.anthropic.com/v1"
  # openai_api_key_file: "~/.openai-api-key"
  # openai_base_url: "https://api.openai.com/v1"
  # gemini_api_key_file: "~/.gemini-api-key"
  # gemini_base_url: "https://generativelanguage.googleapis.com"
  ollama_url: "http://localhost:11434"
  max_tokens: 4096
  temperature: 0.7
  max_iterations: 50
  compact_tool_descriptions: "auto"  # auto, true, or false

#=========================================================================
# KNOWLEDGEBASE CONFIGURATION
#=========================================================================
knowledgebase:
  enabled: false
  # database_path: "/var/lib/ai-workbench/knowledgebase.db"
  embedding_provider: "ollama"
  embedding_model: "nomic-embed-text"
  embedding_ollama_url: "http://localhost:11434"
  # embedding_voyage_base_url: "https://api.voyageai.com/v1/embeddings"
  # embedding_openai_base_url: "https://api.openai.com/v1"

#=========================================================================
# BUILT-IN TOOLS, RESOURCES, AND PROMPTS
#=========================================================================
builtins:
  tools:
    # Database tools
    query_database: true
    get_schema_info: true
    similarity_search: true
    execute_explain: true
    count_rows: true
    # Datastore/metrics tools
    list_probes: true
    describe_probe: true
    query_metrics: true
    list_connections: true
    # Alert tools
    get_alert_history: true
    get_alert_rules: true
    get_metric_baselines: true
    # Datastore tools
    query_datastore: true
    # Utility tools
    generate_embedding: true
    search_knowledgebase: true
  resources:
    system_info: true
    connection_info: true

#=========================================================================
# PATHS AND DATA DIRECTORIES
#=========================================================================
# secret_file: "/etc/ai-workbench/server.secret"
# custom_definitions_path: "/etc/ai-workbench/custom-definitions.yaml"
# data_dir: "/var/lib/ai-workbench/data"
```

## Command-Line Flags

### General Options

| Flag | Description |
|------|-------------|
| `-config string` | Path to configuration file |
| `-debug` | Enable debug logging (logs HTTP requests/responses) |
| `-data-dir string` | Data directory for auth database and conversations |
| `-trace-file string` | Path to trace file for logging MCP requests/responses |

### HTTP Server Options

| Flag | Description |
|------|-------------|
| `-addr string` | HTTP server address (default: `:8080`) |
| `-tls` | Enable TLS/HTTPS |
| `-cert string` | Path to TLS certificate file |
| `-key string` | Path to TLS key file |
| `-chain string` | Path to TLS certificate chain file |

### Database Connection Options

| Flag | Description |
|------|-------------|
| `-db-host string` | Database host |
| `-db-port int` | Database port |
| `-db-name string` | Database name |
| `-db-user string` | Database user |
| `-db-password string` | Database password |
| `-db-sslmode string` | SSL mode (disable, require, verify-ca, verify-full) |

### Token Management Options

| Flag | Description |
|------|-------------|
| `-add-token` | Add a new service token |
| `-remove-token string` | Remove token by ID or hash prefix |
| `-list-tokens` | List all service tokens |
| `-token-note string` | Annotation for new token |
| `-token-expiry string` | Token expiry (30d, 1y, 2w, 12h, never) |
| `-user string` | Owner username for the new token |

### User Management Options

| Flag | Description |
|------|-------------|
| `-add-user` | Add a new user |
| `-update-user` | Update an existing user |
| `-delete-user` | Delete a user |
| `-list-users` | List all users |
| `-enable-user` | Enable a user account |
| `-disable-user` | Disable a user account |
| `-add-service-account` | Add a new service account |
| `-username string` | Username for user commands |
| `-password string` | Password for user commands |
| `-full-name string` | Full name for user management commands |
| `-email string` | Email address for user management commands |
| `-user-note string` | Annotation for new user |

### Group Management Options

| Flag | Description |
|------|-------------|
| `-add-group` | Add a new RBAC group |
| `-delete-group` | Delete an RBAC group |
| `-list-groups` | List all RBAC groups |
| `-add-member` | Add a user or group to a group |
| `-remove-member` | Remove a user or group from a group |
| `-group string` | Group name for group commands |
| `-member-group string` | Member group name for nested membership |
| `-set-superuser` | Set superuser status for a user |
| `-unset-superuser` | Remove superuser status from a user |

### Privilege Management Options

| Flag | Description |
|------|-------------|
| `-grant-privilege` | Grant an MCP privilege to a group |
| `-revoke-privilege` | Revoke an MCP privilege from a group |
| `-grant-connection` | Grant connection access to a group |
| `-revoke-connection` | Revoke connection access from a group |
| `-list-privileges` | List all registered MCP privileges |
| `-show-group-privileges` | Show privileges for a specific group |
| `-register-privilege` | Register a new MCP privilege identifier |
| `-privilege string` | MCP privilege identifier |
| `-privilege-type string` | MCP privilege type (tool, resource, prompt) |
| `-privilege-description string` | Description for the privilege |
| `-connection int` | Connection ID for connection privileges |
| `-access-level string` | Access level (read, read_write); default: read |

### Token Scope Options

| Flag | Description |
|------|-------------|
| `-scope-token-connections` | Set connection scope for a token |
| `-scope-token-tools` | Set MCP tool scope for a token |
| `-clear-token-scope` | Clear all scope restrictions from a token |
| `-show-token-scope` | Show current scope for a token |
| `-token-id int` | Token ID for token scope commands |
| `-scope-connections string` | Comma-separated list of connection IDs |
| `-scope-tools string` | Comma-separated list of tool names |

## Configuration Sections

### HTTP Server (`http`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `address` | string | `:8080` | Listen address (host:port) |
| `tls.enabled` | bool | `false` | Enable TLS/HTTPS |
| `tls.cert_file` | string | | Path to certificate file |
| `tls.key_file` | string | | Path to private key file |
| `tls.chain_file` | string | | Path to certificate chain |
| `trusted_proxies` | list | `[]` | CIDR ranges of trusted reverse proxies |
| `cors_origin` | string | `""` | Allowed CORS origin; empty disables CORS |
| `auth.max_failed_attempts_before_lockout` | int | `0` | Lock after N failures (0=disabled) |
| `auth.max_user_token_days` | int | `0` | Max user token lifetime (0=unlimited) |
| `auth.rate_limit_window_minutes` | int | `15` | Rate limit time window |
| `auth.rate_limit_max_attempts` | int | `10` | Max attempts per window |

### Connection Security (`connection_security`)

The connection security section controls SSRF protection for
user-created database connections.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `allow_internal_networks` | bool | `false` | Allow connections to RFC 1918 private addresses and other internal ranges |
| `allowed_hosts` | list | `[]` | Allowlist of hosts, IPs, or CIDRs that are always permitted |
| `blocked_hosts` | list | `[]` | Blocklist of hosts, IPs, or CIDRs that are never permitted (evaluated after allowed_hosts) |

### Database (`database`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `host` | string | `localhost` | Database host |
| `port` | int | `5432` | Database port |
| `database` | string | `postgres` | Database name |
| `user` | string | | Database user (required) |
| `password` | string | | Database password |
| `sslmode` | string | `prefer` | SSL mode |
| `pool_max_conns` | int | `4` | Max pool connections |
| `pool_min_conns` | int | `0` | Min pool connections |
| `pool_max_conn_idle_time` | string | `30m` | Max idle time |

### Embedding (`embedding`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable embedding generation |
| `provider` | string | `ollama` | Provider (ollama, voyage, openai) |
| `model` | string | varies | Model name |
| `ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `voyage_api_key_file` | string | | Path to Voyage API key |
| `openai_api_key_file` | string | | Path to OpenAI API key |
| `voyage_base_url` | string | `https://api.voyageai.com/v1/embeddings` | Override the Voyage AI API base URL |
| `openai_base_url` | string | `https://api.openai.com/v1` | Override the OpenAI API base URL |

### LLM Proxy (`llm`)

The LLM proxy is optional. When no valid API key is
configured for the chosen provider, the server
automatically disables all AI features at startup. The
web client detects the server's capabilities through
the `/api/v1/capabilities` endpoint and hides all
AI-dependent UI elements. The hidden elements include
the AI Overview, Ask Ellie chat, alert analysis, chart
analysis, and server analysis. The system continues to
function as a monitoring tool without AI features.

For Ollama, a valid URL is sufficient; Ollama does not
require an API key. For Anthropic, OpenAI, and Gemini,
the corresponding API key file must be configured and
contain a valid key.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `provider` | string | `anthropic` | Provider (anthropic, openai, gemini, ollama) |
| `model` | string | `claude-sonnet-4-5` | Model name |
| `anthropic_api_key_file` | string | | Path to Anthropic API key |
| `openai_api_key_file` | string | | Path to OpenAI API key |
| `gemini_api_key_file` | string | | Path to Google Gemini API key |
| `ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `max_tokens` | int | `4096` | Max response tokens |
| `temperature` | float | `0.7` | Sampling temperature |
| `max_iterations` | int | `50` | Max tool-calling iterations per request |
| `compact_tool_descriptions` | string | `auto` | Tool description mode (auto, true, false) |
| `anthropic_base_url` | string | `https://api.anthropic.com/v1` | Override the Anthropic API base URL |
| `openai_base_url` | string | `https://api.openai.com/v1` | Override the OpenAI API base URL |
| `gemini_base_url` | string | `https://generativelanguage.googleapis.com` | Override the Gemini API base URL |

#### Tool Descriptions (`compact_tool_descriptions`)

The `compact_tool_descriptions` option controls whether the server sends
compact or verbose tool descriptions to the LLM. This setting optimizes
prompt token usage when using local models or rate-limited APIs.

The option accepts three values:

- `auto` (default) - The server uses compact descriptions for localhost
  endpoints (localhost, 127.x.x.x, ::1, 0.0.0.0) and verbose descriptions
  for remote APIs. Compact descriptions reduce prompt token count by
  approximately fifty-four percent, significantly improving response times
  for local models.

- `true` - Always send compact tool descriptions to the LLM.

- `false` - Always send verbose tool descriptions to the LLM.

This setting affects the chat proxy (Ask Ellie) only. MCP protocol
clients always receive full tool descriptions regardless of this setting.

In the following example, the `llm` section configures the server to
always use verbose tool descriptions:

```yaml
llm:
  provider: "anthropic"
  model: "claude-sonnet-4-5"
  anthropic_api_key_file: "~/.anthropic-api-key"
  compact_tool_descriptions: "false"
```

#### OpenAI-Compatible Local Servers

The `openai` provider works with any server that implements
the OpenAI-compatible API. Set `openai_base_url` to point
at a local inference server. The API key is optional when
using a custom base URL; omit `openai_api_key_file` if the
server does not require authentication.

The following local inference servers are compatible:

- Docker Model Runner uses
  `http://localhost:12434/engines/llama.cpp/v1` as the
  default endpoint.
- llama.cpp uses `http://localhost:8080/v1` as the default
  endpoint.
- LM Studio uses `http://localhost:1234/v1` as the default
  endpoint.
- EXO uses `http://localhost:52415/v1` as the default
  endpoint.

In the following example, the `llm` section configures a
local llama.cpp server:

```yaml
llm:
  provider: "openai"
  model: "my-local-model"
  openai_base_url: "http://localhost:8080/v1"
```

### Knowledgebase (`knowledgebase`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable knowledgebase |
| `database_path` | string | | Path to SQLite database |
| `embedding_provider` | string | `ollama` | Embedding provider |
| `embedding_model` | string | `nomic-embed-text` | Embedding model |
| `embedding_ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `embedding_voyage_base_url` | string | `https://api.voyageai.com/v1/embeddings` | Override the Voyage AI API base URL |
| `embedding_openai_base_url` | string | `https://api.openai.com/v1` | Override the OpenAI API base URL |

### Built-in Features (`builtins`)

Enable or disable individual tools, resources, and prompts:

```yaml
builtins:
  tools:
    # Database tools
    query_database: true
    get_schema_info: true
    similarity_search: true
    execute_explain: true
    count_rows: true
    # Datastore/metrics tools
    list_probes: true
    describe_probe: true
    query_metrics: true
    list_connections: true
    # Alert tools
    get_alert_history: true
    get_alert_rules: true
    get_metric_baselines: true
    # Datastore tools
    query_datastore: true
    # Utility tools
    generate_embedding: true
    search_knowledgebase: true
  resources:
    system_info: true
    connection_info: true
```

### Paths and Directories

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `secret_file` | string | `/etc/pgedge/ai-dba-server.secret` | Path to encryption secret file |
| `custom_definitions_path` | string | | Path to custom prompts/resources file |
| `data_dir` | string | `./data/` | Directory for auth and conversation databases |
| `trace_file` | string | | Path to MCP request/response trace file |

## Data Directory

The data directory stores persistent data:

- `auth.db` - SQLite database for authentication
- `conversations.db` - SQLite database for chat history

Default location: `./data/` relative to the server binary.

Specify a custom location:

```bash
./bin/ai-dba-server -data-dir /var/lib/ai-workbench/data
```

Or in configuration:

```yaml
data_dir: "/var/lib/ai-workbench/data"
```

## MCP Tracing

The server can log all MCP requests and responses to a trace file for
debugging purposes. Enable tracing via command line or configuration:

```bash
./bin/ai-dba-server -trace-file /var/log/ai-workbench/mcp-trace.log
```

Or in configuration:

```yaml
trace_file: "/var/log/ai-workbench/mcp-trace.log"
```

## Configuration Examples

### Development Configuration

```yaml
http:
  address: ":8080"
  tls:
    enabled: false
  auth:
    enabled: true
connection_security:
  allow_internal_networks: true
database:
  host: "localhost"
  port: 5432
  database: "ai_workbench"
  user: "postgres"
  sslmode: "disable"
embedding:
  enabled: true
  provider: "ollama"
  model: "nomic-embed-text"
llm:
  provider: "ollama"
  model: "llama3:latest"
```

### Production Configuration

```yaml
http:
  address: ":8443"
  tls:
    enabled: true
    cert_file: "/etc/ai-workbench/certs/server-cert.pem"
    key_file: "/etc/ai-workbench/certs/server-key.pem"
  trusted_proxies:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
  cors_origin: "https://workbench.example.com"
  auth:
    enabled: true
    max_failed_attempts_before_lockout: 5
    max_user_token_days: 90
connection_security:
  allow_internal_networks: false
  allowed_hosts:
    - "db.internal.example.com"
  blocked_hosts:
    - "169.254.169.254"
database:
  host: "db.internal.example.com"
  port: 5432
  database: "ai_workbench_prod"
  user: "mcp_server"
  sslmode: "verify-full"
  pool_max_conns: 10
embedding:
  enabled: true
  provider: "voyage"
  model: "voyage-3"
  voyage_api_key_file: "/etc/ai-workbench/voyage-api-key"
llm:
  provider: "anthropic"
  model: "claude-sonnet-4-5"
  anthropic_api_key_file: "/etc/ai-workbench/anthropic-api-key"
secret_file: "/etc/ai-workbench/server.secret"
data_dir: "/var/lib/ai-workbench/data"
```

## Environment Variables

The server reads the following environment variables at startup.

### `PGEDGE_POSTGRES_CONNECTION_STRING`

The `PGEDGE_POSTGRES_CONNECTION_STRING` variable provides a PostgreSQL
connection string for the datastore. The server uses this variable as
a fallback when the configuration file does not specify database
settings.

The server applies connection settings in the following priority
order:

1. Database configuration from the YAML file or command-line flags.
2. The `PGEDGE_POSTGRES_CONNECTION_STRING` environment variable.
3. The default value `postgres://localhost/postgres?sslmode=disable`.

In the following example, the variable specifies a remote database:

```bash
export PGEDGE_POSTGRES_CONNECTION_STRING=\
"postgres://user:pass@db.example.com:5432/ai_workbench?sslmode=require"
```

### `PGEDGE_DB_LOG_LEVEL`

The `PGEDGE_DB_LOG_LEVEL` variable controls the verbosity of
database operation logging. The server writes database log messages
to `stderr` with a `[DATABASE]` prefix.

The following values are supported:

- `none` disables all database logging (default).
- `info` logs connections, queries, and errors.
- `debug` logs metadata loading, pool details, and query details.
- `trace` logs full queries, row counts, and detailed timings.

An empty or unrecognized value defaults to `none`.

In the following example, the variable enables debug logging:

```bash
export PGEDGE_DB_LOG_LEVEL=debug
```

## Configuration Reload

The server supports runtime configuration reload via SIGHUP:

```bash
kill -SIGHUP $(pgrep ai-dba-server)
```

This reloads:

- Database connection settings
- LLM proxy settings
- Knowledgebase settings

Authentication settings and HTTP server settings require a restart.
