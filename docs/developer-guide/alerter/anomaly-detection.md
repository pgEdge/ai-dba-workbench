# Anomaly Detection

The alerter includes an AI-powered anomaly detection system that
identifies unusual patterns in metric data. The system uses a
tiered approach to balance detection accuracy with computational
efficiency.

## Overview

Anomaly detection complements threshold-based alerting by
identifying conditions that deviate from normal behavior without
requiring explicit thresholds. This approach is valuable when
normal values vary over time or when the expected range is not
well understood.

The anomaly detection system provides the following capabilities:

- Statistical analysis identifies values that deviate from
  baselines.
- Embedding similarity finds patterns matching known anomalies.
- LLM classification determines if anomalies require attention.
- Historical learning improves accuracy over time.

## Tiered Architecture

The anomaly detection system uses three tiers:

```
Tier 1: Statistical Analysis (z-score)
  - Fast, runs on every evaluation cycle
  - Creates candidates for values exceeding threshold
        |
        v
Pre-Tier Checks
  - Skips Tier 2 and Tier 3 when no alert could be raised
        |
        v
Tier 2: Embedding Similarity (pgvector)
  - Generates vector embeddings for anomaly context
  - Searches for similar past anomalies
  - May suppress based on similarity to false positives
        |
        v
Tier 3: LLM Classification
  - Analyzes anomaly context with reasoning model
  - Determines alert or suppress decision
  - Provides reasoning for the decision
```

Before Tier 2 runs on a candidate, the alerter checks whether an
alert could be raised for the candidate at all. The paid tiers are
skipped when any of the following conditions applies:

- an active blackout covers the connection.
- an active or acknowledged anomaly alert is already open for the
  same metric, connection and database.
- an anomaly alert on the same metric, connection and database
  cleared within the last five minutes.
- re-evaluation cleared a matching alert within the last 24 hours.
- a user acknowledged a matching alert as a false positive within
  the last 24 hours.

A skipped candidate is marked as processed without tier results or
an embedding; when an open alert exists, the candidate's `alert_id`
points at that alert. A persistent condition therefore costs no
embedding or LLM call on later evaluation cycles. The alerter repeats
the same checks just before creating an alert, because a blackout or
an acknowledgement can arrive during a slow Tier 3 call.

Tier 1 runs the same checks before it stores a candidate, and stores
none when any of them applies. A persistent condition therefore adds
no row to `anomaly_candidates` on each evaluation cycle whilst an
alert is open, a blackout is active or a suppression holds. The
pre-tier checks still run for a candidate that was stored before one
of those conditions began.

## Tier 1: Statistical Analysis

Tier 1 performs z-score analysis to identify statistical outliers.
The z-score measures how many standard deviations a value is from
the mean.

### Z-Score Calculation

The z-score formula is:

```
z-score = (current_value - baseline_mean) / baseline_stddev
```

A high absolute z-score indicates the value is unusual relative to
the baseline. The default sensitivity threshold is 3.0, meaning
values more than 3 standard deviations from the mean are flagged
as candidates.

### Configuration

Tier 1 settings are configured in the `anomaly.tier1` section:

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `true` | Enable Tier 1 detection |
| `default_sensitivity` | `3.0` | Z-score threshold |
| `evaluation_interval_seconds` | `60` | Evaluation interval |
| `clear_count` | `3` | In-band evaluations before an anomaly alert clears |

### Supported Metrics

Tier 1 scores a metric only when the metric's entry in the query
registry (`alerter/src/internal/database/metric_registry.go`)
carries a historical query, because the baseline calculator has
nothing else to build a baseline from. The `SupportsBaselines`
helper in the `database` package makes that check, and both the
baseline calculator and the detector skip any rule whose metric
fails the check. Of the 31 registry metrics, 16 carry a historical
query and 15 do not. The following metrics have no historical
query and are therefore never baselined or scored:

- `age_percent`
- `pg_node_role.subscription_worker_down`
- `pg_replication_slots.inactive`
- `pg_replication_slots.inactive_count`, deliberately: it is a
  presence count that is almost always zero, and the
  `replication_slot_inactive` threshold rule on
  `pg_replication_slots.inactive` already alerts on the condition it
  counts.
- `pg_replication_slots.retained_bytes`
- `pg_settings.max_connections`, deliberately: it is a configuration
  value that exists for the `high_max_connections` threshold rule.
- `pg_stat_activity.max_lock_wait_seconds`
- `pg_stat_all_tables.dead_tuple_percent`
- `pg_stat_archiver.failed_count_delta`
- `pg_stat_checkpointer.checkpoints_req_delta`
- `pg_stat_replication.lag_bytes`
- `pg_stat_replication.replay_lag_seconds`
- `pg_stat_replication.standby_disconnected`
- `pg_stat_statements.slow_query_count`
- `table_last_autovacuum_hours`

Threshold-based rules for these metrics are unaffected. Earlier
releases built a baseline for such metrics from the single most
recent sample; that baseline held one sample and no earliest sample
timestamp, so the warmup gate rejected the row on every cycle and
the metric was silently never scored. The fallback path has been
removed, and the baseline calculator deletes any leftover
`metric_baselines` rows for unsupported metrics at the start of
each cycle so that the `get_metric_baselines` MCP tool does not
report them.

### Database Scoping

Baselines are stored per connection and, for metrics whose
historical query returns a database name (for example
`cache_hit_ratio`, `deadlocks_delta` and `temp_files_delta`), per
database. Tier 1 reads back the baselines for the same database as
the value being scored, so a value from one database is never
compared with another database's baseline. A metric whose latest
value is scoped to a table or other object is skipped, because the
`metric_baselines` table has no object column to pair the value
with; no such metric currently has a historical query.

The anomaly candidate records the database name alongside the
connection, so the active-alert check in `GetActiveAnomalyAlert`
deduplicates per database rather than collapsing every database
on the connection into one alert.

### Stale Baseline Rows

The baseline calculator writes an `hourly` or `daily` row only
while that bucket holds at least three samples, and visits a
connection or database only while it has samples inside
`baselines.lookback_days`. Once it has rebuilt a metric, it deletes
every row of that metric whose `last_calculated` timestamp is older
than the start of the current cycle, because the cycle did not
rewrite those rows and their statistics no longer describe the
data. A metric whose historical query fails, or for which any
baseline row fails to upsert, keeps its rows for that cycle; a row
that failed to upsert still carries the previous cycle's timestamp,
so pruning would otherwise delete the very row the cycle could not
refresh.

### Baseline Selection

The `selectBaseline` helper in
[`alerter/src/internal/engine/anomalies.go`][anomalies-go] picks
the row to score a value against from the baselines for that
connection, database and metric. The helper considers the
candidates in this order of preference:

1. The `hourly` baseline whose `hour_of_day` matches the current
   hour.
2. The `daily` baseline whose `day_of_week` matches the current
   weekday.
3. The `all` baseline.

The first candidate that passes the warmup gate described below is
used; a cold hourly row does not block a warm daily or `all` row,
and a cold `all` row does not block a warm hourly one. When a
cold row is passed over in favour of a less specific warm one, a
debug log line names both rows. When no candidate is warm,
detection is suppressed for that value and a debug log line
reports the period type, sample count and earliest sample time of
the most preferred row that exists.

Whether a tier can ever be selected depends on the baseline
lookback as well as on its warmup thresholds. The historical
queries read only samples inside `baselines.lookback_days`, and
each refresh rewrites `earliest_sample_at` from those samples, so
no baseline's span can exceed the lookback. A tier whose
`min_span_hours` is longer than the lookback in hours is dead
configuration: the selector skips it every time and falls
through to the next tier. `calculateBaselines` calls
`Config.UnreachableWarmupPeriods` on each cycle and logs a
warning naming any such tier. The shipped defaults (a 15 day
lookback against 24, 120 and 336 hour spans) leave every tier
reachable, and `TestShippedWarmupTiersReachable` pins that.

The current hour and weekday are taken in UTC, and the baseline
calculator buckets samples by UTC hour and weekday when writing
the `hourly` and `daily` rows, so the two sides agree however the
alerter's process time zone is set. A metric's diurnal or weekly
cycle is still captured; the buckets are simply keyed in UTC.

Time-aware rows are preferred over the `all` row because a metric
with a daily or weekly cycle has a much tighter spread within one
period than across the whole lookback window. Scoring against the
grand mean inflates the divisor and hides genuine within-cycle
deviation.

### Variance Floor and Warmup Gate

Tier 1 applies two pre-checks before the z-score comparison.
The first protects the divisor from collapsing below a sensible
floor; the second suppresses detection on baselines that have
not yet observed enough data to be trustworthy. Both checks
live in [`alerter/src/internal/engine/anomalies.go`][anomalies-go];
the `effectiveStdDev` helper implements the floor, and the
`isBaselineWarm` helper implements the warmup gate.

[anomalies-go]:
    https://github.com/pgEdge/ai-dba-workbench/blob/main/alerter/src/internal/engine/anomalies.go

#### Motivating Failure Mode

On a young datastore with roughly 26 hours of data, the alerter
produced 5,765 anomaly candidates in 24 hours;
`pg_stat_activity.max_query_duration_seconds` averaged a |z| of
609 with a peak of 5,862. The peak value is not anomaly
detection; it is diagnostic of a baseline whose stored standard
deviation had collapsed several orders of magnitude below the
metric's natural variation. The existing `stddev == 0` guard
correctly skipped baselines with an exactly zero divisor, but
it could not catch the near-zero case that produced the runaway
scores.

#### Hybrid Variance Floor

The `effectiveStdDev` helper raises the divisor to a hybrid
floor before the z-score is computed. The floor combines a
relative term, scaled by the absolute baseline mean, and an
absolute term that acts as a safety net when the mean
approaches zero.

```text
relative_floor = |baseline.mean| * variance_floor.relative_pct
floor          = max(relative_floor, variance_floor.absolute_floor)
effective_stddev = max(baseline.stddev, floor)
```

The defaults are `relative_pct = 0.05` and `absolute_floor =
0.001`. The relative term dominates for most non-zero metrics,
while the absolute term keeps small-mean metrics from
collapsing the floor to zero. When both knobs are zero, the
floor collapses and the existing `stddev == 0` guard handles
the degenerate case.

#### Warmup Gate Semantics

The `isBaselineWarm` helper gates detection on two conditions
per `period_type`: a minimum sample count and a minimum
wall-clock span between the earliest recorded sample and now.
Both must hold for the baseline to be considered warm. Each of
the three `period_type` values (`all`, `hourly`, and `daily`)
carries its own threshold pair, configured under
`anomaly.tier1.warmup` in the alerter YAML. The gate is applied
to each candidate row in turn during baseline selection, so the
`hourly` thresholds decide whether the hourly row is used and the
`all` thresholds decide whether the `all` row is used.

The gate fails closed in two distinct ways. When
`SampleCount` falls below `MinSamples`, the gate reports the
baseline cold and detection is skipped. When `MinSpanHours` is
greater than zero and `EarliestSampleAt` is the zero value,
the gate also reports cold; this covers rows written before
the `metric_baselines.earliest_sample_at` column was added. Setting
both `min_samples: 0` and `min_span_hours: 0` for a
`period_type` disables warmup suppression for that type; the
zero value on `MinSpanHours` also skips the
`EarliestSampleAt.IsZero()` check, so a stale row does not
spuriously fail closed when the operator has explicitly opted
out.

An unrecognised `period_type` falls back to the daily
thresholds, which require the longest span of the three defaults.
This is defensive only; the column is enum-constrained at
write time.

#### Z-Score Cap

After the floored divisor produces a z-score, Tier 1 clamps
the signed value to `±max_z_score` when the cap is positive. A
cap of zero disables the clamp. The cap applies symmetrically
to both extreme positive and extreme negative scores, so a
runaway divisor cannot drive either tail beyond the configured
bound.

## Tier 2: Embedding Similarity

Tier 2 uses vector embeddings to find similar past anomalies.
This tier helps the system learn from historical decisions.

### Embedding Generation

The alerter builds a text representation of the anomaly context
including:

- The metric name and current value.
- The z-score and deviation direction.
- The connection and database identifiers.
- The baseline statistics.

An embedding provider converts this text into a high-dimensional
vector. The alerter stores these embeddings in the
`anomaly_embeddings` table.

### Similarity Search

The alerter searches for similar past anomalies using vector
similarity. The search uses cosine distance through the pgvector
extension. Results include past anomalies above the similarity
threshold along with their final decisions.

### Suppression Logic

Tier 2 may suppress an anomaly based on similar past anomalies:

- If the most similar anomaly was suppressed and similarity
  exceeds the suppression threshold, the current anomaly is
  also suppressed.
- If the most similar anomaly was alerted and similarity exceeds
  the threshold, the current anomaly is passed to Tier 3.
- If similarity is below the threshold, the anomaly proceeds to
  Tier 3.

### Configuration

Tier 2 settings are configured in the `anomaly.tier2` section:

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `true` | Enable Tier 2 detection |
| `suppression_threshold` | `0.85` | Similarity threshold for suppression |
| `similarity_threshold` | `0.3` | Minimum similarity to consider a match |

## Tier 3: LLM Classification

Tier 3 uses LLM reasoning to classify uncertain anomalies. The
LLM receives context about the anomaly and similar past anomalies,
then determines whether to alert or suppress.

### Classification Prompt

The alerter builds a classification prompt containing:

- The current anomaly details including metric, value, and
  z-score.
- Baseline statistics for context.
- Similar past anomalies with their decisions.
- Instructions for the classification response.

### Response Parsing

The alerter expects a JSON response with:

- `decision`: either `alert` or `suppress`.
- `confidence`: a value from 0 to 1.
- `reasoning`: an explanation of the decision.

If the response cannot be parsed as JSON, the alerter falls back
to keyword matching in the response text.

### Fail-Safe Behavior

When the LLM call fails, the alerter defaults to alerting. This
fail-safe ensures that potential issues are not missed due to LLM
unavailability.

### Configuration

Tier 3 settings are configured in the `anomaly.tier3` section:

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `true` | Enable Tier 3 detection |
| `timeout_seconds` | `30` | Timeout for LLM API calls |

## LLM Provider Configuration

The alerter supports multiple LLM providers for embeddings and
reasoning.

### Embedding Providers

Configure the embedding provider in the `llm` section. The following
table shows the default embedding model for each provider and the
dimensions that model produces:

| Provider | Default model | Dimensions |
|----------|-------|------------|
| Ollama | `nomic-embed-text` | 768 |
| OpenAI | `text-embedding-3-small` | 1536 |
| Voyage | `voyage-3-lite` | 512 |
| Gemini | `gemini-embedding-001` | 3072 |

Any model name the chosen provider recognises can be configured in
place of the default. The alerter keeps each embedding at the width the
model produces and zero-pads it to the `halfvec(4000)` column when it
is stored or used as a query vector. A model that produces more than
4000 dimensions is not truncated: the width check runs when the
embedding is stored or searched, so the alerter logs an error for each
candidate it processes with such a model, stores no embedding and
passes the candidate through to tier 3.

### Reasoning Providers

Configure the reasoning provider in the `llm` section:

| Provider | Model |
|----------|-------|
| Ollama | `qwen2.5:7b-instruct` |
| OpenAI | `gpt-6-luna` |
| Anthropic | `claude-haiku-4-5` |
| Gemini | `gemini-3.8-flash` |

### Example Configuration

In the following example, the configuration uses Ollama for local
LLM processing:

```yaml
llm:
  embedding_provider: ollama
  reasoning_provider: ollama
  ollama:
    base_url: http://localhost:11434
    embedding_model: nomic-embed-text
    reasoning_model: qwen2.5:7b-instruct
```

In the following example, the configuration uses OpenAI for
cloud-based processing:

```yaml
llm:
  embedding_provider: openai
  reasoning_provider: openai
  openai:
    api_key_file: /etc/ai-workbench/openai-api-key.txt
    embedding_model: text-embedding-3-small
    reasoning_model: gpt-6-luna
```

## Baseline Calculation

The anomaly detection system depends on accurate baselines. The
baseline calculator runs periodically to refresh baselines from
historical data.

### Baseline Types

The alerter calculates three baseline types:

- `all` baselines aggregate all historical values.
- `hourly` baselines group values by UTC hour of day (0-23).
- `daily` baselines group values by UTC day of week (0-6, with 0
  being Sunday).

Each type is written per connection and, when the metric's
historical query returns a database name, per database. The
calculator only processes metrics that carry a historical query;
see the Supported Metrics section above for the list of metrics
that are skipped and for the clean-up of their leftover rows.

### Baseline Statistics

Each baseline stores the following values:

- `mean`: the average value.
- `stddev`: the standard deviation.
- `min`: the minimum observed value.
- `max`: the maximum observed value.
- `sample_count`: the number of samples.

### Lookback Period

The baseline calculator uses a configurable lookback period to
gather historical data. The default is 15 days, which gives every
weekday at least two occurrences in the window and lets the daily
tier's default 336 hour warmup span be reached. A longer lookback
period provides more stable baselines but may not reflect recent
changes in workload; a shorter one must be paired with shorter
warmup spans, as described under Baseline Selection.

## Anomaly Alert Recovery

Tier 1 also clears anomaly alerts whose metric has recovered. The
threshold alert cleaner re-evaluates a rule's condition, but an
anomaly alert has no rule or threshold to re-evaluate, so the
recovery check runs inside each Tier 1 pass instead. The code lives
in `alerter/src/internal/engine/anomaly_recovery.go`.

At the start of each pass, Tier 1 reads every anomaly alert whose
status is `active` and re-scores the metric, connection and database
behind each one against the same baseline Tier 1 selects for new
candidates. A metric that no enabled rule still covers is scored for
recovery only and never produces a new candidate. Each scored value
has one of the following effects on the alerts for its series:

- a value outside the sensitivity band refreshes the alert's
  `metric_value`, `anomaly_score` and `last_updated` columns, and
  raises the severity when the new z-score warrants a higher one.
- a value inside the band adds one to the alert's consecutive
  in-band count.
- a count that reaches `anomaly.tier1.clear_count` clears the alert
  and queues the usual `alert_clear` notification.

The refresh never lowers the severity, so a smaller deviation does
not quietly downgrade an alert raised as critical. The refresh
discards a cached AI analysis when the metric value changes, as the
threshold path does, but leaves the title and description alone, so
the description still reports the values at which the alert fired.
A refresh sends no notification.

The alerter holds an alert open, and drops its count back to zero,
on a completed pass that does not score its series inside the band.
The following conditions hold an alert open:

- the metric or the connection did not report a value.
- no warm baseline exists, or the floored standard deviation is zero.
- a blackout covers the connection or the database.
- the value lies outside the band.

The alerter therefore never clears an anomaly alert on the absence
of evidence. A failed blackout check is treated as an active
blackout, because holding an alert open for one more pass is cheaper
than clearing it during a maintenance window. A pass that fails
before it completes, for example because the active alerts, the
connections or the rules cannot be read, keeps the previous counts,
as does a clear that fails to write, so the next in-band pass
retries the clear.

Acknowledged anomaly alerts take no part in recovery. The
re-evaluation worker owns those alerts, and its fingerprint covers
the alert's value, z-score and severity, so refreshing them would
invalidate a stored "keep" verdict and buy a fresh Tier 3 call. Every
recovery write is conditional on the alert still being `active`, so
an alert that a user acknowledges during a pass is left as the user
left it.

The consecutive counts live in memory. The counts restart from zero
when the alerter restarts or when a pass runs with anomaly detection
or Tier 1 disabled, so an alert then needs a further `clear_count`
in-band passes before it clears. Recovery uses Tier 1 alone and
makes no embedding or LLM call. When the alerter auto-disables
anomaly detection at startup or on a reload, as described in the
next section, recovery stops with it.

After an anomaly alert clears, the pre-tier checks skip new
candidates on the same metric, connection and database for the
five-minute alert cooldown that also applies to threshold alerts.
The cooldown applies however the alert was cleared, and stops a
value hovering at the edge of the band from raising, clearing and
raising again, with a paid Tier 2 and Tier 3
classification each time.

## Enabling and Disabling

Anomaly detection can be enabled or disabled at multiple levels:

- Globally through the `anomaly.enabled` configuration option.
- Per-tier through each tier's `enabled` option.
- Per-metric through the metric definition's `anomaly_enabled`
  flag.

Disabling Tier 2 causes candidates to pass directly to Tier 3.
Disabling Tier 3 causes all Tier 1 candidates that pass Tier 2
to generate alerts.

Tier 1 never raises an alert on its own, because raw statistical
detection is too noisy to alert on. Anomaly detection runs only
whilst Tier 2 is enabled with an embedding provider or Tier 3 is
enabled with a reasoning provider; disabling both tiers therefore
disables anomaly detection, and Tier 1 stores no candidates. The
alerter creates the providers at startup for the tiers enabled at
that point, so the rule applies in two places:

- At startup, the alerter sets `anomaly.enabled` to `false` and logs
  `Anomaly detection auto-disabled: no LLM providers available` when
  no provider initialises.
- On a configuration reload, the alerter sets `anomaly.enabled` to
  `false` and logs `Anomaly detection auto-disabled: no LLM provider
  available for the enabled tiers` when no enabled tier has a
  provider. A reload cannot add a provider, so enabling a tier that
  was disabled at startup takes a restart.

A processing run that is under way when a reload disables Tier 2 and
Tier 3 stops before its next candidate, rather than letting the rest
of the batch through with no tier result; the candidates it has not
reached stay queued until a tier is enabled again or they expire.

A candidate that stays unprocessed for too long is expired: the
alerter marks it as processed with no final decision, so it raises
no alert and the retention cleanup deletes it with the other
processed candidates. The alerter logs the number of candidates it
expires whenever that number is not zero. Expiry exists to clear
candidates that nothing will ever process, so the cut-off is set
well beyond the time a healthy queue needs to drain, and a genuine
candidate that is merely waiting behind a backlog is still assessed.
Each processing run handles up to 100 candidates in turn, and each
Tier 3 call can take up to `anomaly.tier3.timeout_seconds`, so the
cut-off is three times 100 Tier 3 timeouts, with a minimum of one
hour; at the default timeout of 30 seconds the cut-off is two and a
half hours. The alerter expires candidates before each processing
run and during the retention cleanup, which also runs whilst anomaly
detection is disabled.

## Monitoring Anomaly Detection

The alerter logs anomaly detection activity at debug level. Enable
debug logging to see:

- Tier 1 candidates created for z-score violations.
- Tier 2 similarity search results.
- Tier 3 LLM classification decisions.
- Final decisions and alert creation.

The `anomaly_candidates` table stores all candidates with their
tier results and final decisions. You can query this table to
analyze anomaly detection effectiveness.
