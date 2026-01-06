# Configuration Guide

The MCP server can be configured through a YAML configuration file, command-line
flags, or environment variables.

## Configuration Priority

Configuration values are applied in the following order (highest to lowest
priority):

1. **Command-line flags** (highest priority)
2. **Environment variables**
3. **Configuration file**
4. **Built-in defaults** (lowest priority)

## Configuration File

### File Locations

The server searches for configuration in the following order:

1. Path specified via `-config` flag
2. `/etc/pgedge/ai-dba-server.yaml` (system-wide)
3. `./ai-dba-server.yaml` (binary directory)

### Example Configuration

A complete example configuration file is available at
[`examples/ai-dba-server.yaml`](../../examples/ai-dba-server.yaml).

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

  # Authentication Configuration
  auth:
    enabled: true
    max_failed_attempts_before_lockout: 0  # 0 = disabled
    max_user_token_days: 0  # 0 = unlimited
    rate_limit_window_minutes: 15
    rate_limit_max_attempts: 10

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
  # openai_api_key_file: "~/.openai-api-key"

#=========================================================================
# LLM CONFIGURATION (Web Client Chat Proxy)
#=========================================================================
llm:
  enabled: false
  provider: "anthropic"  # anthropic, openai, or ollama
  model: "claude-sonnet-4-5"
  # anthropic_api_key_file: "~/.anthropic-api-key"
  # openai_api_key_file: "~/.openai-api-key"
  ollama_url: "http://localhost:11434"
  max_tokens: 4096
  temperature: 0.7

#=========================================================================
# KNOWLEDGEBASE CONFIGURATION
#=========================================================================
knowledgebase:
  enabled: false
  # database_path: "/var/lib/ai-workbench/knowledgebase.db"
  embedding_provider: "ollama"
  embedding_model: "nomic-embed-text"
  embedding_ollama_url: "http://localhost:11434"

#=========================================================================
# BUILT-IN TOOLS, RESOURCES, AND PROMPTS
#=========================================================================
builtins:
  tools:
    query_database: true
    get_schema_info: true
    similarity_search: true
    execute_explain: true
    generate_embedding: true
    search_knowledgebase: true
    count_rows: true
  resources:
    system_info: true
  prompts:
    explore_database: true
    setup_semantic_search: true
    diagnose_query_issue: true
    design_schema: true

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

### User Management Options

| Flag | Description |
|------|-------------|
| `-add-user` | Add a new user |
| `-update-user` | Update an existing user |
| `-delete-user` | Delete a user |
| `-list-users` | List all users |
| `-enable-user` | Enable a user account |
| `-disable-user` | Disable a user account |
| `-username string` | Username for user commands |
| `-password string` | Password for user commands |
| `-user-note string` | Annotation for new user |

## Configuration Sections

### HTTP Server (`http`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `address` | string | `:8080` | Listen address (host:port) |
| `tls.enabled` | bool | `false` | Enable TLS/HTTPS |
| `tls.cert_file` | string | | Path to certificate file |
| `tls.key_file` | string | | Path to private key file |
| `tls.chain_file` | string | | Path to certificate chain |
| `auth.enabled` | bool | `true` | Enable authentication |
| `auth.max_failed_attempts_before_lockout` | int | `0` | Lock after N failures (0=disabled) |
| `auth.max_user_token_days` | int | `0` | Max user token lifetime (0=unlimited) |
| `auth.rate_limit_window_minutes` | int | `15` | Rate limit time window |
| `auth.rate_limit_max_attempts` | int | `10` | Max attempts per window |

### Database (`database`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `host` | string | `localhost` | Database host |
| `port` | int | `5432` | Database port |
| `database` | string | `postgres` | Database name |
| `user` | string | | Database user (required) |
| `password` | string | | Database password |
| `password_file` | string | | Path to password file |
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

### LLM Proxy (`llm`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable LLM proxy |
| `provider` | string | `anthropic` | Provider (anthropic, openai, ollama) |
| `model` | string | `claude-sonnet-4-5` | Model name |
| `anthropic_api_key_file` | string | | Path to Anthropic API key |
| `openai_api_key_file` | string | | Path to OpenAI API key |
| `ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `max_tokens` | int | `4096` | Max response tokens |
| `temperature` | float | `0.7` | Sampling temperature |

### Knowledgebase (`knowledgebase`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable knowledgebase |
| `database_path` | string | | Path to SQLite database |
| `embedding_provider` | string | `ollama` | Embedding provider |
| `embedding_model` | string | `nomic-embed-text` | Embedding model |
| `embedding_ollama_url` | string | `http://localhost:11434` | Ollama URL |

### Built-in Features (`builtins`)

Enable or disable individual tools, resources, and prompts:

```yaml
builtins:
  tools:
    query_database: true
    get_schema_info: true
    similarity_search: true
    execute_explain: true
    generate_embedding: true
    search_knowledgebase: true
    count_rows: true
  resources:
    system_info: true
  prompts:
    explore_database: true
    setup_semantic_search: true
    diagnose_query_issue: true
    design_schema: true
```

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

## Configuration Examples

### Development Configuration

```yaml
http:
  address: ":8080"
  tls:
    enabled: false
  auth:
    enabled: true
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
  enabled: false
```

### Production Configuration

```yaml
http:
  address: ":8443"
  tls:
    enabled: true
    cert_file: "/etc/ai-workbench/certs/server-cert.pem"
    key_file: "/etc/ai-workbench/certs/server-key.pem"
  auth:
    enabled: true
    max_failed_attempts_before_lockout: 5
    max_user_token_days: 90
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
  enabled: true
  provider: "anthropic"
  model: "claude-sonnet-4-5"
  anthropic_api_key_file: "/etc/ai-workbench/anthropic-api-key"
secret_file: "/etc/ai-workbench/server.secret"
data_dir: "/var/lib/ai-workbench/data"
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
