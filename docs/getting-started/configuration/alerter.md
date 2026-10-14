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

The alerter overrides a configuration file setting only when you
actually pass the corresponding flag; a flag you omit never
overrides the file, even though the flag has a built-in default. A
flag you do pass always wins, including when the value you give it
happens to equal that flag's default.

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
| `trigger_count` | integer | `2` | Consecutive breaching samples that raise an alert; must be at least 1 |
| `clear_count` | integer | `3` | Consecutive samples within the threshold that clear an alert; must be at least 1 |

The `trigger_count` and `clear_count` options stop an alert
from being raised and cleared repeatedly when a metric hovers
near its threshold. The alerter raises an alert only after
the threshold has been breached on `trigger_count`
consecutive metric samples, and clears an active alert only
after the metric has been back within the threshold on
`clear_count` consecutive samples. A sample is one collection
by the collector rather than one evaluation cycle; the
evaluator and the alert cleaner read the same sample several
times between collections, but each sample counts once. A
sample that breaches the threshold starts the clear count
again, and one that does not breach it starts the trigger
count again.

Setting both options to `1` restores the earlier behaviour of
raising and clearing on a single sample. The counts are held
in memory, so restarting the alerter starts them again from
zero; at worst a restart delays a raise or a clear by a few
samples. The counts do not apply to the `metric_staleness` and
`probe_unavailable` rules or to connection error alerts, which
do not judge metric samples.

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
| `clear_count` | integer | `3` | Consecutive in-band samples before an anomaly alert clears |

The `anomaly.tier1.clear_count` option sets how many
consecutive samples of the metric behind an active anomaly
alert must score back inside the sensitivity band before the
alerter clears the alert. Only a newly collected sample
counts; tier 1 usually evaluates more often than the probe
collects, and an evaluation that re-scores a sample already
counted does not add to the count. The value must be at least
1; the alerter refuses to start, or to apply a reload, with a
smaller value. For a probe that collects every 300 seconds,
the default of 3 clears an alert about ten minutes after the
first in-band sample. A `SIGHUP` reload applies a new value
from the next evaluation.

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
skips a cold baseline, and a fall-through from a cold hourly or
daily row to a warm, less specific one is logged at the same
level. On the long-term test host, enable debug
logging in the alerter and inspect recent suppressions with
`sudo journalctl -u ai-workbench-alerter.service --since 10m`.
The log line names the connection, metric, period type, and
sample count, which is enough to confirm whether a missing
alert reflects warmup suppression or a genuinely quiet metric.

#### Tier 1: Per-Metric Floors

The `anomaly.tier1.metric_floors` map sets two floors for each
metric, so that a value which is unusual for a quiet baseline
but harmless in absolute terms does not raise an alert. Each
key is a metric name, and each entry accepts two options:

| Option | Type | Description |
|--------|------|-------------|
| `min_value` | float | Value below which the metric is never anomalous; `0` disables |
| `min_stddev` | float | Minimum z-score divisor in the metric's units; `0` disables |

A value under `min_value` raises no anomaly candidate, and it
counts as in band when the alerter decides whether to clear an
open anomaly alert. The `min_stddev` option raises the divisor
of the z-score to at least the given value, so a small change
on a baseline that has never moved scores a small z-score; it
only takes effect whilst it exceeds 5% of the baseline mean,
the default relative variance floor. When the baseline has no
spread of its own, the alert severity is capped at `warning`;
threshold rules remain the way to raise a critical alert on an
absolute value.

The alerter has built-in floors for its standard metrics, for
example `min_value: 50` and `min_stddev: 5` for CPU usage
percent, and `min_value: 10` and `min_stddev: 5` for the
session count; the
[anomaly detection guide](../../developer-guide/alerter/anomaly-detection.md#per-metric-floors)
lists them all. An entry overrides a built-in floor field by
field, so an entry that sets only `min_value` keeps the
built-in `min_stddev`. The alerter refuses a negative or
non-finite value, or an empty metric name, and logs a warning
at startup and on each reload for an entry naming a metric
that anomaly detection does not score, such as a mistyped
name.

In the following example, the `metric_floors` map raises the
CPU value floor and disables both floors for the session
count:

```yaml
anomaly:
  tier1:
    metric_floors:
      pg_sys_cpu_usage_info.processor_time_percent:
        min_value: 70
      pg_stat_activity.count:
        min_value: 0
        min_stddev: 0
```

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

The following table describes the tier 3 options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 3 |
| `timeout_seconds` | integer | `30` | Timeout for each LLM API call |
| `decision_suppress_threshold` | number | `0.5` | Probability that a candidate is not a real issue at which the decision model suppresses it; must be greater than 0 and less than 1 whilst a decision provider is configured |

Tier 3 classifies each candidate with the reasoning model
unless `llm.decision_provider` selects a decision model.
With a decision provider, tier 3 asks the decision model
first and uses the reasoning model only as a fallback; the
[Decision Model](#decision-model-decision_provider) section
describes the behaviour. Tier 3 runs whilst it is enabled
and either a reasoning or a decision provider initialised at
startup.

The decision model returns the probability that an anomaly
is a real issue requiring attention. Tier 3 suppresses the
candidate when one minus that probability is at least
`decision_suppress_threshold`, and raises the alert
otherwise. The default of `0.5` suppresses a candidate that
the model judges more likely than not to be a false
positive; a higher value suppresses fewer candidates. The
alerter checks the value only when a decision provider is
configured, and ignores the option otherwise. Each provider
calibrates its probabilities differently, so tune the
threshold for the provider in use rather than carrying a
value across providers.

Each model call has its own `timeout_seconds` limit. A
candidate whose decision model call fails and then falls
back to the reasoning model can therefore take up to twice
the timeout.

#### Provider Health (`provider_health`)

The `anomaly.provider_health` section controls the system
alert that the alerter raises when calls to the embedding,
reasoning or decision provider keep failing. Whilst a
provider fails, anomaly detection runs in a degraded mode:
tier 2 compares no candidate with past anomalies, tier 3
raises each candidate that reaches the tier without LLM
classification, and the re-evaluation worker leaves
acknowledged anomaly alerts unchanged. Whilst the decision
provider fails, tier 3 classifies each candidate with the
reasoning model where one is configured, and otherwise
raises the candidate without classification.

The following table describes the provider health options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `failure_threshold` | integer | `3` | Consecutive failed calls that raise the alert; must be at least 1 |
| `failure_rate` | number | `0.3` | Share of the last `failure_rate_window` calls that raises the alert when they fail; must be greater than 0 and at most 1 |
| `failure_rate_window` | integer | `20` | Number of recent calls the failure rate covers; must be between 1 and 1000. A small window can set a limit of one failed call, as `failure_rate: 0.3` with a window of `3` does, so the first failure raises the alert before `failure_threshold` is reached |

The alerter counts consecutive failures separately for tier
2 embeddings, tier 3 classification, tier 3 decisions and
re-evaluation, and for each provider. When one count reaches the threshold, the
alerter raises a single warning alert for that tier and
provider, and updates the alert's failure count as further
calls fail. The next successful call to that provider resets
the count and clears the alert, unless the failure rate
described below keeps the alert open; tier 3 classification and
re-evaluation share the reasoning provider, so a successful
call from either clears the alerts of both. The decision
provider serves tier 3 decisions alone, so only a
successful decision call clears its alert. After clearing
an alert, the alerter does not raise it again for five
minutes, so that a provider that fails intermittently cannot
send a burst of notifications. A call cancelled before it
completes counts as neither a failure nor a success,
although a call that times out counts as a failure. A tier
3 or re-evaluation call also counts as a failure when the
provider answers with a response that holds no decision the
alerter can parse; a decision call counts as a failure when
the model refuses to answer, gives no answer, or gives a
probability outside the range 0 to 1. The failure is
recorded whatever the alerter then does with the candidate
or alert, which may be
to act on a keyword found in the response or to take the
tier's default decision.

When anomaly detection is enabled, the alerter also checks
the providers of the enabled tiers at startup. A failed
check raises the alert for its tier immediately, whatever
the threshold or the five-minute wait; a failed reasoning
check raises the tier 3 alert. A successful reasoning check
clears both the tier 3 and the re-evaluation alerts. When
tier 3 is enabled, the alerter also checks the decision
provider by listing its models, which costs no tokens. At
startup the alerter also clears any open provider alert for
a tier or provider that is no longer configured or enabled.
See
[System Alerts](../../user-guide/alerts/index.md#system-alerts)
for how the alert appears.

The failure rate catches a provider that fails a steady
share of its calls without failing consecutively, such as a
provider that rejects every other request. The alerter keeps
the outcomes of the last `failure_rate_window` calls for each
tier and provider, and sets a limit of `failure_rate` times
the window, rounded up; the defaults give a limit of 6 failed
calls out of 20. The alerter raises the alert when the
failed calls among those outcomes reach the limit, even
before the window is full, and the five-minute wait still
applies. A raised alert stays open whilst the failed calls
remain at or above the limit, and clears on the first
successful call that leaves fewer failed calls than the
limit. A run of consecutive failures that reached the
threshold counts as a single failed call once the provider
recovers, provided that single failed call leaves the provider
below the limit; an outage therefore clears on the first successful
call, as the consecutive count alone would. Setting
`failure_rate` to `1` leaves the consecutive count as
effectively the only rule that raises the alert, provided
`failure_rate_window` is at least `failure_threshold`.

In the following example, the alerter raises the alert after
five consecutive failures, or once 10 of the last 40 calls
have failed:

```yaml
anomaly:
  provider_health:
    failure_threshold: 5
    failure_rate: 0.25
    failure_rate_window: 40
```

### Baseline Calculation (`baselines`)

The `baselines` section configures baseline metric
calculation for anomaly detection.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `refresh_interval_seconds` | integer | `3600` | Refresh interval |
| `lookback_days` | integer | `15` | Historical lookback in days |

The lookback also caps how old a baseline's earliest sample can
be, because each refresh reads only samples inside the window
and rewrites `earliest_sample_at` from them. A warmup tier whose
`min_span_hours` exceeds `lookback_days` multiplied by 24 can
therefore never become warm, and the alerter logs a warning on
each baseline cycle naming the tier. The default of 15 days
covers the daily tier's 336 hour span with a day to spare and
gives every weekday at least two occurrences in the window.

### Correlation (`correlation`)

The `correlation` section configures alert correlation
across metrics.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `window_seconds` | integer | `120` | Correlation time window |

### LLM Providers (`llm`)

The `llm` section configures LLM providers for tier 3
anomaly detection and embedding generation, and the
optional decision model for tier 3.

Each provider section names its own embedding model, and the alerter
accepts any model name the chosen provider recognises rather than
checking the name against a fixed list. The defaults in the tables
below apply when the configuration leaves the model empty, and an
OpenAI-protocol-compatible local model server can supply embeddings
under whatever model name that server uses.

The configured embedding model must not produce vectors with more
than 4000 dimensions. The alerter stores anomaly embeddings as
`halfvec(4000)` and zero-pads each embedding to 4000 dimensions; 4000
is pgvector's HNSW index limit for the `halfvec` type. The alerter
does not check the width of a model at startup: when the model turns
out to be wider than 4000 dimensions, the alerter never truncates the
vector, but instead logs an error naming the vector width and the
limit for each anomaly candidate it processes, stores no embedding for
the candidate, and passes the candidate straight to tier 3. Watch the
alerter log after changing the embedding model, because the error
first appears when the first anomaly candidate reaches tier 2, not
when the alerter starts. Storing embeddings as `halfvec` requires
pgvector `0.7.0` or newer; upgrading an existing installation against
an older pgvector causes the embedding-column migration to fail.

The following table describes the provider selection
options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `embedding_provider` | string | `ollama` | Embedding provider |
| `reasoning_provider` | string | `ollama` | Classification provider |
| `decision_provider` | string | None | Optional tier 3 decision provider: `typesafe` or `openai`; empty, `none` or `disabled` turns the decision model off |
| `max_tokens` | integer | `4096` | Max output tokens per reasoning call |

#### Decision Model (`decision_provider`)

A decision model answers a yes or no question with the
probability of yes, rather than generating text. When
`decision_provider` names a provider, tier 3 asks the
decision model whether each candidate is a real issue that
requires attention, and applies
`anomaly.tier3.decision_suppress_threshold` to the
probability it returns. The decision model receives the
same context as the reasoning model, without the reasoning
model's instructions for a JSON response.

The following providers serve decision models:

- `typesafe` uses the TypeSafe Jev models, configured in the
  `llm.typesafe` section; the default model is `jev-latest`.
- `openai` uses the OpenAI Decisions API with the model in
  `llm.openai.decision_model`; the default model is
  `gpt-6-luna`.

The alerter sends the decision provider the tier 3
classification context for each candidate, which includes:

- the metric name, value and z-score, the connection ID,
  the database name and the detection time.
- the baseline mean, standard deviation and period.
- the similarity and decision of similar past anomalies.
- the notes of past acknowledgements of the same metric and
  connection, with the usernames of the users who
  acknowledged them.
- the names and roles of cluster peer connections, and the
  titles of active alerts on those peers.

The decision provider receives nothing beyond what the
reasoning provider already receives. A decision provider
such as TypeSafe may, however, be a third party that has not
received the data before, so review the provider's data
handling terms before enabling it.

The decision model applies to tier 3 classification only.
The re-evaluation of acknowledged anomaly alerts always uses
the reasoning provider, whatever `decision_provider` holds.

When the decision model fails for a candidate, tier 3 falls
back to the reasoning model for that candidate. A failure
includes an error, a timeout, a refusal to answer, a
missing answer and a probability outside the range 0 to 1.
When no reasoning provider is configured, tier 3 raises the
candidate as an alert without classification, the same
fail-safe that applies when the reasoning model fails. The
candidate's tier 3 error records the decision model failure
in either case.

A decision provider can serve tier 3 on its own, without a
reasoning provider; the alerter then has no fallback and
does not re-evaluate acknowledged alerts with an LLM. When
the decision provider cannot be created at startup, for
example because the TypeSafe API key is missing, the alerter
logs a warning and runs tier 3 on the reasoning provider
alone.

For a decision model verdict, the candidate's tier 3 result
is a one-line summary rather than a model response, as in
the following example:

```text
Decision model typesafe/jev-latest: alert (P(real issue)=0.87, suppress threshold 0.50)
```

The `tier3_source` column of the `anomaly_candidates` table
records whether a `decision` or a `reasoning` model produced
the tier 3 outcome, and the `tier3_model` column records the
provider and model in the form `provider/model`. When every
model fails, the columns name the last model tried.

In the following example, the `llm` section selects the
TypeSafe decision model and keeps the Anthropic reasoning
model as the fallback:

```yaml
llm:
  reasoning_provider: anthropic
  decision_provider: typesafe
  anthropic:
    api_key_file: /etc/ai-workbench/anthropic-api-key.txt
  typesafe:
    api_key_file: /etc/ai-workbench/typesafe-api-key.txt
    decision_model: jev-latest
anomaly:
  tier3:
    decision_suppress_threshold: 0.5
```

The alerter creates the decision provider at startup only,
so changing `decision_provider` or the `llm.typesafe`
settings takes a restart. A configuration reload does apply
a new `decision_suppress_threshold`.

#### Choosing a Decision Model

A spike in October 2026 compared the decision providers
with the stored tier 3 decisions on 150 real candidates
from the previous 30 days, sampled evenly between candidates
stored as alerts and candidates stored as suppressed. The
stored decisions came from the reasoning model, so the
figures measure agreement with that model rather than
accuracy against decisions that users labelled; no such
labels existed. The reasoning model, Claude Haiku 4.5,
agreed with the stored decisions on 91% of the candidates
when run again.

The following table compares the models at the default
threshold of 0.5:

| Model | Agreement with stored decisions | Median latency | Cost per 1,000 decisions |
|-------|---------------------------------|----------------|--------------------------|
| TypeSafe `jev-latest` | 89% | About 200 ms | About US$0.04 |
| OpenAI `gpt-6-luna` decisions | 79% | Not measured | Not confirmed |
| Claude Haiku 4.5 reasoning | 91% | About 3 s | About US$1.93 |

The Jev model agreed with Claude Haiku 4.5 on 88% of the
candidates, and never suppressed a candidate stored as an
alert: every disagreement was an alert where the stored
decision was to suppress. The OpenAI decision model
suppressed some candidates that were stored as alerts. The
costs use the published prices as of October 2026: Jev
charges US$0.042 per million input tokens with no charge
for output, and Claude Haiku 4.5 charges US$1 per million
input tokens and US$5 per million output tokens. The spike
could not confirm the OpenAI decision price from official
pricing.

On this evidence, `typesafe` is the recommended decision
provider; `openai` is supported, but agreed less often and
was more willing to suppress real alerts. Raising
`decision_suppress_threshold` above 0.5 reduced agreement
in the spike. Because each provider calibrates its
probabilities differently, test any change to the threshold
against your own candidates for the provider in use.

Re-evaluation stays on the reasoning model because of the
spike. In a synthetic test, the Jev model never cleared an
alert whose acknowledgement note explained the anomaly, and
no real acknowledgements were available to validate either
decision model.

#### Output Token Budget (`max_tokens`)

The `max_tokens` option caps the number of output
tokens a reasoning model may generate for a single
call. The budget applies to the following reasoning
work:

- The tier 3 anomaly classification applies the budget
  to each candidate that the alerter examines.
- The re-evaluation of acknowledged alerts applies the
  budget to each alert that the alerter revisits.

The default value is `4096` tokens; a value less than
or equal to zero falls back to the default. Earlier
releases capped both calls at 500 tokens and offered
no way to change the limit.

A reasoning model counts its internal thinking tokens
against this same budget. A budget that suits a
conventional model can therefore leave no room for the
verdict itself. The model then returns a response that
contains no text, which the alerter treats as a tier 3
failure. A failed tier 3 classification fails safe and
raises the alert; the alerter records the cause in the
candidate's tier 3 error field. Raise `max_tokens`
when these failures appear; alternatively, select a
model that emits less reasoning.

In the following example, the `llm` section raises the
budget for a reasoning model that a local llama.cpp
server hosts:

```yaml
llm:
  reasoning_provider: openai
  max_tokens: 8192
  openai:
    base_url: http://localhost:8080/v1
    reasoning_model: qwen3-8b
```

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
| `reasoning_model` | string | `gpt-6-luna` | Reasoning model |
| `decision_model` | string | `gpt-6-luna` | Decisions API model when `decision_provider` is `openai` |

The decision model shares the `api_key_file` and
`base_url` options with the OpenAI embedding and reasoning
models. A `base_url` that points at a local inference server
therefore also receives the decision requests, and the
server must implement the OpenAI Decisions API for them to
succeed.

The `openai` provider works with any server that
implements the OpenAI-compatible API. Set `base_url`
to point at a local inference server and set
`embedding_model` to the model that server provides.
The API key is optional when using a custom base URL.

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

A local server that hosts a reasoning model usually
needs a larger output budget than the default. For
details, see
[Output Token Budget](#output-token-budget-max_tokens).

#### Anthropic Configuration

The `llm.anthropic` section configures the Anthropic
provider.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.anthropic.com/v1` | Anthropic base URL |
| `reasoning_model` | string | `claude-haiku-4-5` | Reasoning model |

#### Gemini Configuration

The `llm.gemini` section configures the Google Gemini
provider. Model availability varies by Gemini API key
tier; run ListModels to verify which models a given
key can access.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |
| `reasoning_model` | string | `gemini-3.8-flash` | Reasoning model |
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

#### TypeSafe Configuration

The `llm.typesafe` section configures the TypeSafe
provider, which serves the Jev decision models. The alerter
uses TypeSafe only as a `decision_provider`; TypeSafe cannot
serve as an embedding or reasoning provider.

The following table describes the TypeSafe options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.typesafe.ai/v1` | TypeSafe base URL |
| `decision_model` | string | `jev-latest` | Decision model |

The TypeSafe provider always requires an API key, even
when `base_url` points elsewhere, such as at a proxy. The
alerter reads the key only from `api_key_file`, and does
not fall back to the `TYPESAFE_API_KEY` environment
variable.

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
options of the `openai`, `anthropic`, `gemini`, `voyage`
and `typesafe` sections. When a configured key file cannot
be read, the alerter logs a warning and continues; a
provider whose key was not loaded then fails to initialise.

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

A reload does not recreate the LLM providers, which the
alerter builds once at startup. Changing a provider, a
model, a base URL or an API key file, including
`llm.decision_provider`, therefore takes a restart; the
reloaded values of settings such as
`anomaly.tier3.decision_suppress_threshold` apply at once.

In the following example, the `kill` command reloads
the configuration:

```bash
kill -HUP $(pidof ai-dba-alerter)
```
