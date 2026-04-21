# Server Configuration

The MCP server supports configuration through a YAML
file, command-line flags, and environment variables.

## Configuration Priority

The server applies configuration values in the
following order from highest to lowest priority:

1. Command-line flags (highest priority).
2. Environment variables.
3. Configuration file.
4. Built-in defaults (lowest priority).

## Configuration File

### File Locations

The server searches for its configuration file in
the following order:

1. The path specified via the `-config` flag.
2. `/etc/pgedge/ai-dba-server.yaml` (system-wide).
3. `./ai-dba-server.yaml` (binary directory).

### Example Configuration

A complete example configuration file is available at
[examples/ai-dba-server.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-server.yaml)
in the project repository.

```yaml
#=====================================================
# HTTP SERVER CONFIGURATION
#=====================================================
http:
  # Address to listen on (host:port or :port)
  address: ":8080"

  # TLS/HTTPS Configuration
  tls:
    enabled: false
    # cert_file: "/etc/ai-workbench/certs/cert.pem"
    # key_file: "/etc/ai-workbench/certs/key.pem"
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
    max_failed_attempts_before_lockout: 0
    max_user_token_days: 0
    rate_limit_window_minutes: 15
    rate_limit_max_attempts: 10

#=====================================================
# CONNECTION SECURITY
#=====================================================
connection_security:
  allow_internal_networks: false
  # allowed_hosts:
  #   - "db.example.com"
  #   - "192.168.1.0/24"
  # blocked_hosts:
  #   - "169.254.169.254"

#=====================================================
# DATABASE CONNECTION
#=====================================================
database:
  host: "localhost"
  port: 5432
  database: "ai_workbench"
  user: "postgres"
  # password: ""
  sslmode: "prefer"
  pool_max_conns: 4
  pool_min_conns: 0
  pool_max_conn_idle_time: "30m"

#=====================================================
# EMBEDDING GENERATION
#=====================================================
embedding:
  enabled: false
  provider: "ollama"
  model: "nomic-embed-text"
  ollama_url: "http://localhost:11434"
  # voyage_api_key_file: "~/.voyage-api-key"
  # openai_api_key_file: "~/.openai-api-key"

#=====================================================
# LLM CONFIGURATION (Web Client Chat Proxy)
#=====================================================
llm:
  provider: "anthropic"
  model: "claude-sonnet-4-5"
  # anthropic_api_key_file: "~/.anthropic-api-key"
  # openai_api_key_file: "~/.openai-api-key"
  # gemini_api_key_file: "~/.gemini-api-key"
  ollama_url: "http://localhost:11434"
  max_tokens: 4096
  temperature: 0.7
  max_iterations: 50
  compact_tool_descriptions: "auto"

#=====================================================
# KNOWLEDGEBASE CONFIGURATION
#=====================================================
knowledgebase:
  enabled: false
  # database_path: "/usr/share/pgedge/postgres-mcp-kb/kb.db"
  embedding_provider: "ollama"
  embedding_model: "nomic-embed-text"
  embedding_ollama_url: "http://localhost:11434"

#=====================================================
# CHAT MEMORY CONFIGURATION
#=====================================================
memory:
  enabled: true

#=====================================================
# BUILT-IN TOOLS, RESOURCES, AND PROMPTS
#=====================================================
builtins:
  tools:
    query_database: true
    get_schema_info: true
    similarity_search: true
    execute_explain: true
    count_rows: true
    test_query: true
    list_probes: true
    describe_probe: true
    query_metrics: true
    list_connections: true
    get_alert_history: true
    get_alert_rules: true
    get_metric_baselines: true
    get_blackouts: true
    query_datastore: true
    generate_embedding: true
    search_knowledgebase: true
    store_memory: true
    recall_memories: true
    delete_memory: true
  resources:
    system_info: true
    connection_info: true

#=====================================================
# PATHS AND DATA DIRECTORIES
#=====================================================
# secret_file: "/etc/ai-workbench/server.secret"
# custom_definitions_path: "/etc/ai-workbench/defs.yaml"
# data_dir: "/var/lib/ai-workbench/data"
```

## Command-Line Flags

### General Options

| Flag | Description |
|------|-------------|
| `-config string` | Path to configuration file |
| `-debug` | Enable debug logging |
| `-data-dir string` | Data directory for auth and conversations |
| `-trace-file string` | Path to MCP trace file |

### HTTP Server Options

| Flag | Description |
|------|-------------|
| `-addr string` | HTTP address (default: `:8080`) |
| `-tls` | Enable TLS/HTTPS |
| `-cert string` | Path to TLS certificate file |
| `-key string` | Path to TLS key file |
| `-chain string` | Path to TLS certificate chain |

### Database Connection Options

| Flag | Description |
|------|-------------|
| `-db-host string` | Database host |
| `-db-port int` | Database port |
| `-db-name string` | Database name |
| `-db-user string` | Database user |
| `-db-password string` | Database password |
| `-db-sslmode string` | SSL mode |

### Token Management Options

| Flag | Description |
|------|-------------|
| `-add-token` | Add a new service token |
| `-remove-token string` | Remove token by ID or hash |
| `-list-tokens` | List all service tokens |
| `-token-note string` | Annotation for new token |
| `-token-expiry string` | Token expiry duration |
| `-user string` | Owner username for the token |

### User Management Options

| Flag | Description |
|------|-------------|
| `-add-user` | Add a new user |
| `-update-user` | Update an existing user |
| `-delete-user` | Delete a user |
| `-list-users` | List all users |
| `-enable-user` | Enable a user account |
| `-disable-user` | Disable a user account |
| `-add-service-account` | Add a service account |
| `-username string` | Username for user commands |
| `-password string` | Password for user commands |
| `-full-name string` | Full name for the user |
| `-email string` | Email address for the user |
| `-user-note string` | Annotation for new user |

### Group Management Options

| Flag | Description |
|------|-------------|
| `-add-group` | Add a new RBAC group |
| `-delete-group` | Delete an RBAC group |
| `-list-groups` | List all RBAC groups |
| `-add-member` | Add a user or group to a group |
| `-remove-member` | Remove a member from a group |
| `-group string` | Group name for group commands |
| `-member-group string` | Member group for nesting |
| `-set-superuser` | Set superuser status |
| `-unset-superuser` | Remove superuser status |

### Privilege Management Options

| Flag | Description |
|------|-------------|
| `-grant-privilege` | Grant an MCP privilege |
| `-revoke-privilege` | Revoke an MCP privilege |
| `-grant-connection` | Grant connection access |
| `-revoke-connection` | Revoke connection access |
| `-list-privileges` | List MCP privileges |
| `-show-group-privileges` | Show group privileges |
| `-register-privilege` | Register a new privilege |
| `-privilege string` | MCP privilege identifier |
| `-privilege-type string` | Privilege type |
| `-privilege-description string` | Description |
| `-connection int` | Connection ID |
| `-access-level string` | Access level (default: read) |

### Token Scope Options

| Flag | Description |
|------|-------------|
| `-scope-token-connections` | Set connection scope |
| `-scope-token-tools` | Set MCP tool scope |
| `-clear-token-scope` | Clear all scope restrictions |
| `-show-token-scope` | Show current token scope |
| `-token-id int` | Token ID for scope commands |
| `-scope-connections string` | Connection ID list |
| `-scope-tools string` | Tool name list |

## Configuration Sections

### HTTP Server (`http`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `address` | string | `:8080` | Listen address |
| `tls.enabled` | bool | `false` | Enable TLS |
| `tls.cert_file` | string | | Certificate file |
| `tls.key_file` | string | | Private key file |
| `tls.chain_file` | string | | Certificate chain |
| `trusted_proxies` | list | `[]` | Trusted proxy CIDRs |
| `cors_origin` | string | `""` | Allowed CORS origin |
| `auth.max_failed_attempts_before_lockout` | int | `0` | Lock after N failures |
| `auth.max_user_token_days` | int | `0` | Max token lifetime |
| `auth.rate_limit_window_minutes` | int | `15` | Rate limit window |
| `auth.rate_limit_max_attempts` | int | `10` | Max attempts per window |

#### CORS Origin (`cors_origin`)

The `cors_origin` option sets the value for the
`Access-Control-Allow-Origin` response header. The empty
default suits same-origin deployments, where the web
client and the server share an origin. The server omits
CORS headers entirely in that case.

Set `cors_origin` to an explicit origin when the web
client runs on a different origin than the server. For
example, use `https://workbench.example.com` as the
value. The server returns
`Access-Control-Allow-Credentials: true` so browsers can
attach session cookies to cross-origin requests.

The server rejects a `cors_origin` value of `"*"` at
startup when authentication is enabled. Browsers disallow
combining `Access-Control-Allow-Origin: *` with
`Access-Control-Allow-Credentials: true` per the Fetch
spec. Use an explicit origin, or leave the option empty
for same-origin deployments.

In the following example, the `http` section allows
cross-origin requests from a dedicated workbench host:

```yaml
http:
  address: ":8443"
  cors_origin: "https://workbench.example.com"
  auth:
    enabled: true
```

### Connection Security (`connection_security`)

The connection security section controls SSRF
protection for user-created database connections.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `allow_internal_networks` | bool | `false` | Allow RFC 1918 addresses |
| `allowed_hosts` | list | `[]` | Always-permitted hosts |
| `blocked_hosts` | list | `[]` | Always-blocked hosts |

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
| `enabled` | bool | `false` | Enable embeddings |
| `provider` | string | `ollama` | Provider name |
| `model` | string | varies | Model name |
| `ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `voyage_api_key_file` | string | | Voyage API key path |
| `openai_api_key_file` | string | | OpenAI API key path |
| `voyage_base_url` | string | `https://api.voyageai.com/v1/embeddings` | Voyage base URL |
| `openai_base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |

### LLM Proxy (`llm`)

The LLM proxy is optional. When no valid API key is
configured for the chosen provider, the server
disables all AI features at startup. The web client
detects the server's capabilities through the
`/api/v1/capabilities` endpoint and hides all
AI-dependent UI elements. The hidden elements include
the AI Overview, Ask Ellie chat, alert analysis, chart
analysis, and server analysis. The system continues to
function as a monitoring tool without AI features.

For Ollama, a valid URL is sufficient because Ollama
does not require an API key. For Anthropic, OpenAI,
and Gemini, the corresponding API key file must
contain a valid key.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `provider` | string | `anthropic` | LLM provider |
| `model` | string | `claude-sonnet-4-5` | Model name |
| `anthropic_api_key_file` | string | | Anthropic key path |
| `openai_api_key_file` | string | | OpenAI key path |
| `gemini_api_key_file` | string | | Gemini key path |
| `ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `max_tokens` | int | `4096` | Max response tokens |
| `temperature` | float | `0.7` | Sampling temperature |
| `max_iterations` | int | `50` | Max tool-call iterations |
| `compact_tool_descriptions` | string | `auto` | Tool description mode |
| `timeout_seconds` | int | `120` | LLM HTTP request timeout |
| `anthropic_base_url` | string | `https://api.anthropic.com/v1` | Anthropic base URL |
| `openai_base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |
| `gemini_base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |

#### Request Timeout (`timeout_seconds`)

The `timeout_seconds` option sets the HTTP client
timeout, in seconds, for every request the server
makes to the configured LLM provider. The timeout
applies uniformly to Anthropic, OpenAI, Gemini, and
Ollama, including Ask Ellie chat requests. The
default value is `120` seconds; values less than or
equal to zero fall back to the default.

Increase this value when running local models on
low-resource hardware or when full-context prompts
regularly exceed the default; large prompts that
combine schema, metrics, and connection data can
take longer than 120 seconds on slow Ollama hosts.
Raising the timeout to `300` prevents `context
deadline exceeded` errors in these environments.

In the following example, the `llm` section extends
the timeout for a slow local Ollama server:

```yaml
llm:
  provider: "ollama"
  model: "llama3:latest"
  ollama_url: "http://localhost:11434"
  timeout_seconds: 300
```

#### Tool Descriptions (`compact_tool_descriptions`)

The `compact_tool_descriptions` option controls
whether the server sends compact or verbose tool
descriptions to the LLM. The option accepts three
values:

- `auto` (default) uses compact descriptions for
  localhost endpoints and verbose descriptions for
  remote APIs. Compact descriptions reduce prompt
  token count by approximately fifty-four percent.
- `true` always sends compact tool descriptions.
- `false` always sends verbose tool descriptions.

This setting affects the chat proxy (Ask Ellie) only.
MCP protocol clients always receive full tool
descriptions regardless of this setting.

In the following example, the `llm` section configures
verbose tool descriptions:

```yaml
llm:
  provider: "anthropic"
  model: "claude-sonnet-4-5"
  anthropic_api_key_file: "~/.anthropic-api-key"
  compact_tool_descriptions: "false"
```

#### OpenAI-Compatible Local Servers

The `openai` provider works with any server that
implements the OpenAI-compatible API. Set
`openai_base_url` to point at a local inference
server. The API key is optional when using a custom
base URL.

The following local inference servers are compatible:

- Docker Model Runner uses
  `http://localhost:12434/engines/llama.cpp/v1` as
  the default endpoint.
- llama.cpp uses `http://localhost:8080/v1` as the
  default endpoint.
- LM Studio uses `http://localhost:1234/v1` as the
  default endpoint.
- EXO uses `http://localhost:52415/v1` as the
  default endpoint.

In the following example, the `llm` section configures
a local llama.cpp server:

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
| `database_path` | string | `/usr/share/pgedge/postgres-mcp-kb/kb.db` | SQLite database path |
| `embedding_provider` | string | `ollama` | Embedding provider |
| `embedding_model` | string | `nomic-embed-text` | Embedding model |
| `embedding_ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `embedding_voyage_api_key_file` | string | | Voyage API key path |
| `embedding_openai_api_key_file` | string | | OpenAI key path |
| `embedding_voyage_base_url` | string | `https://api.voyageai.com/v1/embeddings` | Voyage base URL |
| `embedding_openai_base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |

### Memory (`memory`)

The memory section controls the chat memory feature
for Ask Ellie. Chat memory allows Ellie to store and
recall information across conversations.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable chat memory |

The memory feature requires a configured PostgreSQL
datastore connection. The server stores memories in
the datastore alongside other persistent data. When
memory is disabled, the three memory tools
(`store_memory`, `recall_memories`, `delete_memory`)
are unavailable even when enabled in the
`builtins.tools` section.

Creating system-scoped memories requires the
`store_system_memory` admin permission. Administrators
assign this permission to groups through the admin
panel. Users without the permission can only create
user-scoped memories.

In the following example, the `memory` section
disables chat memory:

```yaml
memory:
  enabled: false
```

The `PGEDGE_MEMORY_ENABLED` environment variable can
also control this setting; see
[Environment Variables](#environment-variables) for
details.

### Built-in Features (`builtins`)

The builtins section enables or disables individual
tools, resources, and prompts.

```yaml
builtins:
  tools:
    query_database: true
    get_schema_info: true
    similarity_search: true
    execute_explain: true
    count_rows: true
    test_query: true
    list_probes: true
    describe_probe: true
    query_metrics: true
    list_connections: true
    get_alert_history: true
    get_alert_rules: true
    get_metric_baselines: true
    get_blackouts: true
    query_datastore: true
    generate_embedding: true
    search_knowledgebase: true
    store_memory: true
    recall_memories: true
    delete_memory: true
  resources:
    system_info: true
    connection_info: true
```

#### Memory Tools

The memory tools (`store_memory`, `recall_memories`,
`delete_memory`) allow Ellie to store and recall
information across conversations. The server stores
memories in the PostgreSQL datastore. When embedding
generation is enabled, the `recall_memories` tool
uses semantic similarity for searching. The tool
falls back to text matching when embeddings are
unavailable.

Set any memory tool to `false` in the `builtins.tools`
section to disable the tool.

In the following example, the configuration disables
the `store_memory` and `delete_memory` tools while
keeping `recall_memories` enabled:

```yaml
builtins:
  tools:
    store_memory: false
    delete_memory: false
    recall_memories: true
```

### Paths and Directories

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `secret_file` | string | `/etc/pgedge/ai-dba-server.secret` | Encryption secret path |
| `custom_definitions_path` | string | | Custom prompts file path |
| `data_dir` | string | `./data/` | Auth and conversation data |
| `trace_file` | string | | MCP trace file path |

## Data Directory

The data directory stores persistent data files:

- `auth.db` stores authentication data in SQLite.
- `conversations.db` stores chat history in SQLite.

The default location is `./data/` relative to the
server binary.

In the following example, the `-data-dir` flag
specifies a custom location:

```bash
./ai-dba-server -data-dir /var/lib/ai-workbench/data
```

The data directory can also be set in the
configuration file:

```yaml
data_dir: "/var/lib/ai-workbench/data"
```

## MCP Tracing

The server can log all MCP requests and responses to a
trace file for debugging purposes.

In the following example, the `-trace-file` flag
enables tracing:

```bash
./ai-dba-server \
    -trace-file /var/log/ai-workbench/mcp-trace.log
```

The trace file can also be set in the configuration
file:

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
    cert_file: "/etc/ai-workbench/certs/cert.pem"
    key_file: "/etc/ai-workbench/certs/key.pem"
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
  user: "ai_workbench"
  sslmode: "verify-full"
  pool_max_conns: 10
embedding:
  enabled: true
  provider: "voyage"
  model: "voyage-3"
  voyage_api_key_file: "/etc/ai-workbench/voyage-key"
llm:
  provider: "anthropic"
  model: "claude-sonnet-4-5"
  anthropic_api_key_file: "/etc/ai-workbench/ant-key"
secret_file: "/etc/ai-workbench/server.secret"
data_dir: "/var/lib/ai-workbench/data"
```

## Environment Variables

The server reads the following environment variables
at startup.

### `PGEDGE_POSTGRES_CONNECTION_STRING`

The `PGEDGE_POSTGRES_CONNECTION_STRING` variable
provides a PostgreSQL connection string for the
datastore. The server uses this variable as a fallback
when the configuration file does not specify database
settings.

The server applies connection settings in the
following priority order:

1. Database configuration from the YAML file or
   command-line flags.
2. The `PGEDGE_POSTGRES_CONNECTION_STRING` variable.
3. The default value
   `postgres://localhost/postgres?sslmode=disable`.

In the following example, the variable specifies a
remote database:

```bash
export PGEDGE_POSTGRES_CONNECTION_STRING=\
"postgres://user:pass@db.example.com:5432/\
ai_workbench?sslmode=require"
```

### `PGEDGE_DB_LOG_LEVEL`

The `PGEDGE_DB_LOG_LEVEL` variable controls the
verbosity of database operation logging. The server
writes database log messages to `stderr` with a
`[DATABASE]` prefix.

The following values are supported:

- `none` disables all database logging (default).
- `info` logs connections, queries, and errors.
- `debug` logs metadata, pool details, and queries.
- `trace` logs full queries, row counts, and timings.

An empty or unrecognized value defaults to `none`.

In the following example, the variable enables debug
logging:

```bash
export PGEDGE_DB_LOG_LEVEL=debug
```

### `PGEDGE_MEMORY_ENABLED`

The `PGEDGE_MEMORY_ENABLED` variable controls the
chat memory feature for Ask Ellie. The server reads
this variable at startup and applies the value as an
override to the `memory.enabled` configuration
option.

The following values are supported:

- `true` enables chat memory (default).
- `false` disables chat memory.

In the following example, the variable disables chat
memory:

```bash
export PGEDGE_MEMORY_ENABLED=false
```

## Configuration Reload

The server supports runtime configuration reload via
`SIGHUP`:

```bash
kill -SIGHUP $(pgrep ai-dba-server)
```

A `SIGHUP` signal reloads the following settings:

- Database connection settings.
- LLM proxy settings.
- Knowledgebase settings.

Authentication settings and HTTP server settings
require a full restart.
