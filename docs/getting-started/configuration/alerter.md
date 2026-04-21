# Alerter Configuration

The pgEdge AI DBA Workbench Alerter supports
configuration through YAML files and command-line
flags.

## Configuration Precedence

The alerter applies configuration settings in the
following order; later sources override earlier ones:

1. Default values built into the application.
2. Configuration file settings (YAML format).
3. Command-line flag overrides.

## Configuration File

The alerter searches for its configuration file in the
following locations:

- `/etc/pgedge/ai-dba-alerter.yaml` (system-wide).
- `<binary-directory>/ai-dba-alerter.yaml` (alongside
  the executable).

You can specify a custom path using the `-config`
flag.

A complete example configuration file is available at
[ai-dba-alerter.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-alerter.yaml)
in the project repository.

## Command-Line Flags

The alerter accepts the following command-line flags:

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | Auto-detected |
| `-debug` | Enable debug logging | `false` |
| `-db-host` | Database host | None |
| `-db-port` | Database port | None |
| `-db-name` | Database name | None |
| `-db-user` | Database user | None |
| `-db-password` | Database password | None |
| `-db-sslmode` | Database SSL mode | None |

In the following example, the alerter starts with
debug logging and a custom configuration file:

```bash
./ai-dba-alerter -debug \
    -config /etc/ai-workbench/alerter.yaml
```

In the following example, the alerter connects to a
specific database without a configuration file:

```bash
./ai-dba-alerter \
    -db-host db.example.com \
    -db-name ai_workbench \
    -db-user alerter \
    -db-password secret
```

## Configuration File Reference

The configuration file uses YAML format. The following
sections describe all available options.

### Datastore Connection (`datastore`)

The `datastore` section configures the connection to
the AI DBA Workbench PostgreSQL datastore.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `host` | string | `localhost` | Server hostname |
| `hostaddr` | string | None | Server IP address |
| `database` | string | `ai_workbench` | Database name |
| `username` | string | `postgres` | Database username |
| `password` | string | None | Database password |
| `password_file` | string | None | Path to password file |
| `port` | integer | `5432` | Server port |
| `sslmode` | string | `prefer` | SSL connection mode |
| `sslcert` | string | None | Client certificate path |
| `sslkey` | string | None | Client private key path |
| `sslrootcert` | string | None | CA certificate path |

The `sslmode` option accepts the following values:

- `disable` disables SSL encryption.
- `allow` attempts non-SSL first and falls back to
  SSL.
- `prefer` attempts SSL first and falls back to
  non-SSL.
- `require` requires SSL without certificate
  verification.
- `verify-ca` requires SSL and verifies the server
  certificate.
- `verify-full` requires SSL and verifies the
  certificate and hostname.

In the following example, the `datastore` section
configures a secure connection with certificate
verification:

```yaml
datastore:
  host: db.example.com
  database: ai_workbench
  username: ai_workbench
  password_file: /etc/ai-workbench/password.txt
  port: 5432
  sslmode: verify-full
  sslcert: /etc/ai-workbench/client-cert.pem
  sslkey: /etc/ai-workbench/client-key.pem
  sslrootcert: /etc/ai-workbench/ca-cert.pem
```

### Connection Pool (`pool`)

The `pool` section configures the database connection
pool.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_connections` | integer | `10` | Max concurrent connections |
| `max_idle_seconds` | integer | `300` | Idle connection timeout |

### Threshold Evaluation (`threshold`)

The `threshold` section configures threshold-based
alert evaluation.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `evaluation_interval_seconds` | integer | `60` | Evaluation interval |

### Anomaly Detection (`anomaly`)

The `anomaly` section configures the tiered anomaly
detection system.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable anomaly detection |

#### Tier 1: Statistical Analysis

The `anomaly.tier1` section configures z-score-based
statistical detection.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 1 |
| `default_sensitivity` | float | `3.0` | Z-score threshold |
| `evaluation_interval_seconds` | integer | `60` | Evaluation interval |

#### Tier 2: Embedding Similarity

The `anomaly.tier2` section configures pgvector-based
similarity search for pattern matching.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 2 |
| `suppression_threshold` | float | `0.85` | Suppression threshold |
| `similarity_threshold` | float | `0.3` | Similarity threshold |

#### Tier 3: LLM Classification

The `anomaly.tier3` section configures LLM-based
classification for complex anomalies.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 3 |
| `timeout_seconds` | integer | `30` | LLM API timeout |

### Baseline Calculation (`baselines`)

The `baselines` section configures baseline metric
calculation for anomaly detection.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `refresh_interval_seconds` | integer | `3600` | Refresh interval |
| `lookback_days` | integer | `7` | Historical lookback in days |

### Correlation (`correlation`)

The `correlation` section configures alert correlation
across metrics.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `window_seconds` | integer | `120` | Correlation time window |

### LLM Providers (`llm`)

The `llm` section configures LLM providers for tier 3
anomaly detection and embedding generation.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `embedding_provider` | string | `ollama` | Embedding provider |
| `reasoning_provider` | string | `ollama` | Classification provider |

#### Ollama Configuration

The `llm.ollama` section configures the local Ollama
provider.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | `http://localhost:11434` | Ollama server URL |
| `embedding_model` | string | `nomic-embed-text` | Embedding model |
| `reasoning_model` | string | `qwen2.5:7b-instruct` | Reasoning model |

#### OpenAI Configuration

The `llm.openai` section configures the OpenAI
provider.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |
| `embedding_model` | string | `text-embedding-3-small` | Embedding model |
| `reasoning_model` | string | `gpt-4o-mini` | Reasoning model |

The `openai` provider works with any server that
implements the OpenAI-compatible API. Set `base_url`
to point at a local inference server. The API key is
optional when using a custom base URL.

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

In the following example, the `llm.openai` section
configures a local llama.cpp server:

```yaml
llm:
  reasoning_provider: openai
  openai:
    base_url: http://localhost:8080/v1
    reasoning_model: my-local-model
```

#### Anthropic Configuration

The `llm.anthropic` section configures the Anthropic
provider.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.anthropic.com/v1` | Anthropic base URL |
| `reasoning_model` | string | `claude-3-5-haiku-20241022` | Reasoning model |

#### Gemini Configuration

The `llm.gemini` section configures the Google Gemini
provider.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |
| `reasoning_model` | string | `gemini-2.0-flash` | Reasoning model |

#### Voyage Configuration

The `llm.voyage` section configures the Voyage
provider for embeddings.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.voyageai.com/v1/embeddings` | Voyage base URL |
| `embedding_model` | string | `voyage-3-lite` | Embedding model |

### Notifications (`notifications`)

The `notifications` section configures the
notification delivery system for sending alerts
through external channels.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable notifications |
| `secret_file` | string | None | Path to server secret |
| `process_interval_seconds` | integer | `30` | Processing interval |
| `reminder_check_interval_minutes` | integer | `60` | Reminder check interval |
| `max_retry_attempts` | integer | `3` | Max retry attempts |
| `retry_backoff_minutes` | list | `[5, 15, 60]` | Retry backoff schedule |
| `http_timeout_seconds` | integer | `30` | HTTP request timeout |
| `http_max_idle_conns` | integer | `10` | Max idle HTTP connections |

The `secret_file` option specifies a file containing
the same plain text secret used by the server
component. The alerter uses this secret to decrypt
notification channel credentials that the server
encrypted. The alerter and the server must reference
the same secret file.

In the following example, the `notifications` section
enables delivery with custom retry settings:

```yaml
notifications:
  enabled: true
  secret_file: /etc/ai-workbench/ai-dba-server.secret
  process_interval_seconds: 30
  max_retry_attempts: 5
  retry_backoff_minutes: [5, 15, 30]
  http_timeout_seconds: 60
```

## API Key Management

Store API keys for LLM providers in files with
restricted permissions. The alerter reads API keys
from the paths specified in the `api_key_file`
options.

In the following example, the commands create an API
key file with secure permissions:

```bash
echo "sk-your-api-key-here" \
    > /etc/ai-workbench/openai-api-key.txt
chmod 600 /etc/ai-workbench/openai-api-key.txt
```

The corresponding configuration references the key
file:

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

The alerter responds to Unix signals for operational
control:

- `SIGINT` and `SIGTERM` trigger a graceful shutdown.
- `SIGHUP` reloads the configuration file without
  restarting the process.

In the following example, the `kill` command reloads
the configuration:

```bash
kill -HUP $(pidof ai-dba-alerter)
```
