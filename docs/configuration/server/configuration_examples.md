# Configuration Examples

The examples that follow demonstrate how you might use server properties to
configure your system. A complete sample configuration file is available at
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
    max_failed_attempts_before_lockout: 10
    max_user_token_days: 0
    rate_limit_window_minutes: 15
    rate_limit_max_attempts: 10
    # Local username and password login
    local:
      enabled: true
    # Federated (OpenID Connect) login
    # oidc:
    #   enabled: true
    #   issuer: "https://idp.example.com"
    #   client_id: "pgedge-workbench"
    #   client_secret_file: "/etc/pgedge/oidc-client-secret.txt"
    #   redirect_url: "https://workbench.example.com/api/v1/auth/oidc/callback"
    audit_retention_days: 90

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
  # password_file: "/etc/pgedge/password.txt"
  sslmode: "prefer"
  pool_max_conns: 20
  pool_min_conns: 0
  pool_max_conn_idle_time: "30m"
  statement_timeout: "30s"

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
  # gemini_api_key_file: "~/.gemini-api-key"

#=====================================================
# LLM CONFIGURATION (Chat Proxy and AI Analysis)
#=====================================================
llm:
  provider: "anthropic"
  model: "claude-sonnet-5"
  # anthropic_api_key_file: "~/.anthropic-api-key"
  # openai_api_key_file: "~/.openai-api-key"
  # gemini_api_key_file: "~/.gemini-api-key"
  ollama_url: "http://localhost:11434"
  max_tokens: 4096
  temperature: 0.7
  max_iterations: 50
  compact_tool_descriptions: "auto"
  # reasoning_effort: "medium"
  # prompt_caching: "auto"
  # extra_body: {}

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
    list_probes: true
    describe_probe: true
    query_metrics: true
    list_connections: true
    get_alert_history: true
    get_alert_rules: true
    get_metric_baselines: true
    get_blackouts: true
    get_timeline_events: true
    query_datastore: true
    describe_datastore: true
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

## Sample: Development Configuration

```yaml
http:
  address: ":8080"
  tls:
    enabled: false
  auth:
    local:
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

## Sample: Production Configuration

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
    local:
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
  pool_max_conns: 20
embedding:
  enabled: true
  provider: "voyage"
  model: "voyage-3"
  voyage_api_key_file: "/etc/ai-workbench/voyage-key"
llm:
  provider: "anthropic"
  model: "claude-sonnet-5"
  anthropic_api_key_file: "/etc/ai-workbench/ant-key"
secret_file: "/etc/ai-workbench/server.secret"
data_dir: "/var/lib/ai-workbench/data"
```
