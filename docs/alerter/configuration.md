# Configuration Reference

The pgEdge AI DBA Workbench Alerter supports multiple configuration methods
with a clear precedence order. This reference documents all configuration
options, environment variables, and command-line flags.

## Configuration Precedence

The alerter applies configuration settings in the following order; later
sources override earlier ones:

1. Default values built into the application.
2. Configuration file settings (YAML format).
3. Environment variable overrides.
4. Command-line flag overrides.

## Configuration File

The alerter searches for its configuration file in these locations:

- `/etc/pgedge/ai-dba-alerter.yaml` (system-wide).
- `<binary-directory>/ai-dba-alerter.yaml` (alongside the executable).

You can specify a custom path using the `-config` flag.

## Command-Line Flags

The alerter accepts the following command-line flags:

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to the configuration file | Auto-detected |
| `-debug` | Enable debug logging | `false` |
| `-db-host` | Database host (overrides config) | None |
| `-db-port` | Database port (overrides config) | None |
| `-db-name` | Database name (overrides config) | None |
| `-db-user` | Database user (overrides config) | None |
| `-db-password` | Database password (overrides config) | None |
| `-db-sslmode` | Database SSL mode (overrides config) | None |

In the following example, the alerter starts with debug logging enabled and
a custom configuration file:

```bash
./ai-dba-alerter -debug -config /etc/ai-workbench/alerter.yaml
```

In the following example, the alerter connects to a specific database host
without using a configuration file:

```bash
./ai-dba-alerter -db-host db.example.com -db-name ai_workbench \
    -db-user alerter -db-password secret
```

## Environment Variables

The alerter recognizes these environment variables for datastore connection
settings; the variables override configuration file values but are overridden
by command-line flags:

| Variable | Description | Default |
|----------|-------------|---------|
| `AI_DBA_PG_HOST` | Database hostname | `localhost` |
| `AI_DBA_PG_HOSTADDR` | Database IP address (bypasses DNS) | None |
| `AI_DBA_PG_DATABASE` | Database name | `ai_workbench` |
| `AI_DBA_PG_USERNAME` | Database username | `postgres` |
| `AI_DBA_PG_PASSWORD` | Database password | None |
| `AI_DBA_PG_SSLMODE` | SSL connection mode | `prefer` |
| `AI_DBA_PG_SSLCERT` | Path to client SSL certificate | None |
| `AI_DBA_PG_SSLKEY` | Path to client SSL private key | None |
| `AI_DBA_PG_SSLROOTCERT` | Path to CA root certificate | None |

In the following example, environment variables configure the database
connection:

```bash
export AI_DBA_PG_HOST=db.example.com
export AI_DBA_PG_DATABASE=ai_workbench
export AI_DBA_PG_USERNAME=alerter
export AI_DBA_PG_PASSWORD=secret
./ai-dba-alerter
```

## Configuration File Reference

The configuration file uses YAML format. The following sections describe all
available options.

### Datastore Connection

The `datastore` section configures the connection to the AI DBA Workbench
PostgreSQL datastore:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `host` | string | `localhost` | Database server hostname |
| `hostaddr` | string | None | Database server IP address |
| `database` | string | `ai_workbench` | Database name |
| `username` | string | `postgres` | Database username |
| `password` | string | None | Database password |
| `password_file` | string | None | Path to password file |
| `port` | integer | `5432` | Database server port |
| `sslmode` | string | `prefer` | SSL connection mode |
| `sslcert` | string | None | Path to client certificate |
| `sslkey` | string | None | Path to client private key |
| `sslrootcert` | string | None | Path to CA certificate |

The `sslmode` option accepts these values:

- `disable` disables SSL encryption.
- `allow` attempts a non-SSL connection first and falls back to SSL.
- `prefer` attempts an SSL connection first and falls back to non-SSL.
- `require` requires SSL but does not verify the server certificate.
- `verify-ca` requires SSL and verifies the server certificate.
- `verify-full` requires SSL and verifies the certificate and hostname.

In the following example, the datastore section configures a secure connection
with certificate verification:

```yaml
datastore:
  host: db.example.com
  database: ai_workbench
  username: alerter
  password_file: /etc/ai-workbench/password.txt
  port: 5432
  sslmode: verify-full
  sslcert: /etc/ai-workbench/client-cert.pem
  sslkey: /etc/ai-workbench/client-key.pem
  sslrootcert: /etc/ai-workbench/ca-cert.pem
```

### Connection Pool

The `pool` section configures the database connection pool:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_connections` | integer | `10` | Maximum concurrent connections |
| `max_idle_seconds` | integer | `300` | Idle connection timeout |

### Threshold Evaluation

The `threshold` section configures threshold-based alert evaluation:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `evaluation_interval_seconds` | integer | `60` | Evaluation interval |

### Anomaly Detection

The `anomaly` section configures the tiered anomaly detection system:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable anomaly detection |

#### Tier 1: Statistical Analysis

The `anomaly.tier1` section configures z-score-based detection:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 1 detection |
| `default_sensitivity` | float | `3.0` | Z-score threshold |
| `evaluation_interval_seconds` | integer | `60` | Evaluation interval |

#### Tier 2: Embedding Similarity

The `anomaly.tier2` section configures pgvector-based similarity search:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 2 detection |
| `suppression_threshold` | float | `0.85` | Suppression threshold |
| `similarity_threshold` | float | `0.3` | Similarity threshold |

#### Tier 3: LLM Classification

The `anomaly.tier3` section configures LLM-based classification:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 3 detection |
| `timeout_seconds` | integer | `30` | LLM API timeout |

### Baseline Calculation

The `baselines` section configures baseline metric calculation:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `refresh_interval_seconds` | integer | `3600` | Baseline refresh interval |
| `lookback_days` | integer | `7` | Historical data lookback period in days |

### Correlation

The `correlation` section configures alert correlation:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `window_seconds` | integer | `120` | Correlation time window |

### LLM Providers

The `llm` section configures LLM providers for tier 3 anomaly detection:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `embedding_provider` | string | `ollama` | Provider for embeddings |
| `reasoning_provider` | string | `ollama` | Provider for classification |

#### Ollama Configuration

The `llm.ollama` section configures the local Ollama provider:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | `http://localhost:11434` | Ollama server URL |
| `embedding_model` | string | `nomic-embed-text` | Embedding model name |
| `reasoning_model` | string | `qwen2.5:7b-instruct` | Reasoning model name |

#### OpenAI Configuration

The `llm.openai` section configures the OpenAI provider:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `embedding_model` | string | `text-embedding-3-small` | Embedding model |
| `reasoning_model` | string | `gpt-4o-mini` | Reasoning model |

#### Anthropic Configuration

The `llm.anthropic` section configures the Anthropic provider:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `reasoning_model` | string | `claude-3-5-haiku-20241022` | Reasoning model |

#### Voyage Configuration

The `llm.voyage` section configures the Voyage provider for embeddings:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `embedding_model` | string | `voyage-3-lite` | Embedding model |

### Notifications

The `notifications` section configures the notification delivery
system for sending alerts via external channels:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable notification delivery |
| `secret_file` | string | None | Path to encryption key file |
| `process_interval_seconds` | integer | `30` | Processing interval for pending notifications |
| `reminder_check_interval_minutes` | integer | `60` | Interval for checking due reminders |
| `max_retry_attempts` | integer | `3` | Maximum retries for failed notifications |
| `retry_backoff_minutes` | list | `[5, 15, 60]` | Backoff schedule for retries in minutes |
| `http_timeout_seconds` | integer | `30` | HTTP request timeout for webhooks |
| `http_max_idle_conns` | integer | `10` | Maximum idle HTTP connections |

The `secret_file` option specifies a file containing a
hex-encoded 32-byte key for encrypting sensitive notification
channel credentials.

In the following example, the `notifications` section enables
delivery with custom retry settings:

```yaml
notifications:
  enabled: true
  secret_file: /etc/ai-workbench/notifications.key
  process_interval_seconds: 30
  max_retry_attempts: 5
  retry_backoff_minutes: [5, 15, 30]
  http_timeout_seconds: 60
```

## API Key Management

API keys for LLM providers should be stored in files with restricted
permissions. The alerter reads API keys from the paths specified in the
`api_key_file` options.

In the following example, an API key file is created with secure permissions:

```bash
echo "sk-your-api-key-here" > /etc/ai-workbench/openai-api-key.txt
chmod 600 /etc/ai-workbench/openai-api-key.txt
```

The corresponding configuration references the key file:

```yaml
llm:
  embedding_provider: openai
  reasoning_provider: openai
  openai:
    api_key_file: /etc/ai-workbench/openai-api-key.txt
    embedding_model: text-embedding-3-small
    reasoning_model: gpt-4o
```

## Signal Handling

The alerter responds to Unix signals for operational control:

- `SIGINT` and `SIGTERM` trigger a graceful shutdown.
- `SIGHUP` reloads the configuration file without restarting.

In the following example, the configuration is reloaded:

```bash
kill -HUP $(pidof ai-dba-alerter)
```

## Example Configuration

A complete example configuration file is available in the examples directory:

- `examples/ai-dba-alerter.yaml`

The example file includes detailed comments explaining each option and provides
templates for common deployment scenarios.
