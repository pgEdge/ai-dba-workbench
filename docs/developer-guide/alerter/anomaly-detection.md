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

### Baseline Selection

The alerter selects the most appropriate baseline for each
evaluation:

1. If an hourly baseline exists for the current hour, the alerter
   uses the hourly baseline.
2. If a daily baseline exists for the current day, the alerter
   uses the daily baseline.
3. Otherwise, the alerter uses the global baseline.

This approach accounts for time-based patterns in metric values.

### Variance Floor and Warmup Gate

Tier 1 applies two pre-checks before the z-score comparison.
The first protects the divisor from collapsing below a sensible
floor; the second suppresses detection on baselines that have
not yet observed enough data to be trustworthy. Both checks
live in [`alerter/src/internal/engine/anomalies.go`][anomalies-go];
`effectiveStdDev` at line 29 implements the floor, and
`isBaselineWarm` at line 53 implements the warmup gate.

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
`anomaly.tier1.warmup` in the alerter YAML.

The gate fails closed in two distinct ways. When
`SampleCount` falls below `MinSamples`, the gate reports the
baseline cold and detection is skipped. When `MinSpanHours` is
greater than zero and `EarliestSampleAt` is the zero value,
the gate also reports cold; this covers rows written before
the `metric_baselines.earliest_sample_at` column was added and
fallback baselines that lack raw sample timestamps. Setting
both `min_samples: 0` and `min_span_hours: 0` for a
`period_type` disables warmup suppression for that type; the
zero value on `MinSpanHours` also skips the
`EarliestSampleAt.IsZero()` check, so a stale row does not
spuriously fail closed when the operator has explicitly opted
out.

An unrecognised `period_type` falls back to the daily
thresholds, which are the strictest of the three defaults.
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

Configure the embedding provider in the `llm` section:

| Provider | Model | Dimensions |
|----------|-------|------------|
| Ollama | `nomic-embed-text` | 768 (resized to 1536) |
| OpenAI | `text-embedding-3-small` | 1536 |
| Voyage | `voyage-3-lite` | 1024 (resized to 1536) |

### Reasoning Providers

Configure the reasoning provider in the `llm` section:

| Provider | Model |
|----------|-------|
| Ollama | `qwen2.5:7b-instruct` |
| OpenAI | `gpt-4o-mini` |
| Anthropic | `claude-3-5-haiku-20241022` |
| Gemini | `gemini-2.0-flash` |

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
    reasoning_model: gpt-4o-mini
```

## Baseline Calculation

The anomaly detection system depends on accurate baselines. The
baseline calculator runs periodically to refresh baselines from
historical data.

### Baseline Types

The alerter calculates three baseline types:

- `all` baselines aggregate all historical values.
- `hourly` baselines group values by hour of day (0-23).
- `daily` baselines group values by day of week (0-6).

### Baseline Statistics

Each baseline stores the following values:

- `mean`: the average value.
- `stddev`: the standard deviation.
- `min`: the minimum observed value.
- `max`: the maximum observed value.
- `sample_count`: the number of samples.

### Lookback Period

The baseline calculator uses a configurable lookback period to
gather historical data. The default is 7 days. A longer lookback
period provides more stable baselines but may not reflect recent
changes in workload.

## Enabling and Disabling

Anomaly detection can be enabled or disabled at multiple levels:

- Globally through the `anomaly.enabled` configuration option.
- Per-tier through each tier's `enabled` option.
- Per-metric through the metric definition's `anomaly_enabled`
  flag.

Disabling Tier 2 causes candidates to pass directly to Tier 3.
Disabling Tier 3 causes all Tier 1 candidates that pass Tier 2
to generate alerts.

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
