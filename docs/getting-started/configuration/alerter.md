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
following order:

1. The path specified via the `-config` flag.
2. The per-user config directory at
   `~/.config/pgedge/ai-dba-alerter.yaml` on Linux
   (honouring `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-alerter.yaml`
   on macOS, and `%AppData%\pgedge\ai-dba-alerter.yaml`
   on Windows.
3. `/etc/pgedge/ai-dba-alerter.yaml` (system-wide).

If `-config` is set and the file is missing, the
alerter exits with an error. If `-config` is not set
and none of the default locations contain a
configuration file, the alerter uses built-in defaults
silently. The alerter no longer searches the binary
directory or the current working directory. A `SIGHUP`
signal re-runs discovery on each reload, so a
configuration file installed at a default location
after startup is picked up on the next signal.

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

#### Tier 1: Variance Floor, Warmup, and Z-Score Cap

Three additional blocks under `anomaly.tier1` prevent the
detector from firing on baselines that have not yet stabilised.
These blocks are most relevant on young datastores; on a
datastore with one or two days of metric history, a baseline's
stored standard deviation can collapse far below the metric's
natural variation, producing z-scores in the thousands. The
variance floor and warmup gate suppress this failure mode, and
the z-score cap acts as defence in depth.

The `anomaly.tier1.max_z_score` option clamps the absolute
z-score symmetrically around zero before the sensitivity
comparison. The default is `100.0`; any genuine outlier sits
well below this value, and the cap simply prevents a runaway
divisor from generating multi-thousand-sigma scores. Setting
`max_z_score: 0` disables the cap.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_z_score` | float | `100.0` | Z-score clamp; `0` disables |

The `anomaly.tier1.variance_floor` block enforces a minimum
divisor on the z-score calculation. The effective standard
deviation is the larger of the raw stored value and a hybrid
floor; the floor itself is the larger of `relative_pct` times
the absolute baseline mean and `absolute_floor`. The relative
term dominates for non-zero metrics, while the absolute term
acts as a safety net when the mean approaches zero. Setting
both `relative_pct: 0` and `absolute_floor: 0` disables the
floor entirely; the detector then falls back to the existing
`stddev == 0` guard.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `relative_pct` | float | `0.05` | Floor as fraction of abs(mean) |
| `absolute_floor` | float | `0.001` | Absolute minimum stddev |

The `anomaly.tier1.warmup` block suppresses detection for
baselines that have not accumulated enough samples or enough
wall-clock observation time. Each `period_type` (`all`,
`hourly`, and `daily`) has its own pair of thresholds, and a
baseline is considered warm only when both are met. The `all`
baseline defaults require roughly one day of operation at the
60 second collection interval; the `hourly` and `daily`
defaults require enough span for each time bucket to have been
observed multiple times. Setting both `min_samples: 0` and
`min_span_hours: 0` for a given `period_type` disables warmup
suppression for that type.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `all.min_samples` | integer | `100` | Minimum sample count |
| `all.min_span_hours` | integer | `24` | Minimum span in hours |
| `hourly.min_samples` | integer | `5` | Minimum sample count |
| `hourly.min_span_hours` | integer | `120` | Minimum span in hours |
| `daily.min_samples` | integer | `3` | Minimum sample count |
| `daily.min_span_hours` | integer | `336` | Minimum span in hours |

In the following example, the `anomaly.tier1` section tightens
the variance floor and extends the `all` warmup window:

```yaml
anomaly:
  tier1:
    max_z_score: 100.0
    variance_floor:
      relative_pct: 0.10
      absolute_floor: 0.005
    warmup:
      all:
        min_samples: 200
        min_span_hours: 48
      hourly:
        min_samples: 5
        min_span_hours: 120
      daily:
        min_samples: 3
        min_span_hours: 336
```

Warmup suppressions are recorded at debug log level only; the
detector does not write a candidate row or an alert when it
skips a cold baseline. On the long-term test host, enable debug
logging in the alerter and inspect recent suppressions with
`sudo journalctl -u ai-workbench-alerter.service --since 10m`.
The log line names the connection, metric, period type, and
sample count, which is enough to confirm whether a missing
alert reflects warmup suppression or a genuinely quiet metric.

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

The configured embedding model must not produce vectors with more
than 4000 dimensions. The alerter stores anomaly embeddings as
`halfvec(4000)` and zero-pads each embedding to 4000 dimensions; 4000
is pgvector's HNSW index limit for the `halfvec` type. The alerter
rejects a model that exceeds 4000 dimensions with a clear error rather
than truncating the vector.

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
provider. Model availability varies by Gemini API key
tier; run ListModels to verify which models a given
key can access.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |
| `reasoning_model` | string | `gemini-2.5-flash` | Reasoning model |
| `embedding_model` | string | `gemini-embedding-001` | Embedding model |

When Gemini is the embedding provider, the
`embedding_model` must match the model that the KB
Builder used to produce the knowledgebase; the KB
Builder uses `gemini-embedding-001`.

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
