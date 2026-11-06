# Anomaly Detection Properties

Use the properties in the `anomaly` section of the alerter configuration file
to turn anomaly detection on or off, set the statistical limits that mark a
metric value as unusual, and control how the alerter compares and classifies
the resulting candidates before raising an alert. The alerter evaluates each
candidate in three tiers; each tier passes on only the candidates that it does
not resolve:

- tier 1 compares each metric with its baseline and flags values whose z-score
  exceeds the configured sensitivity.
- tier 2 compares each candidate with the embeddings of past anomalies to
  recognize patterns that the alerter has already seen.
- tier 3 asks an LLM to classify each remaining candidate and decides whether
  to raise an alert.

Tiers 2 and 3 use the providers that the `llm` section selects;
[LLM Provider Properties](llm_providers.md) describes those settings. The
[Configuration File Properties](configuration_file.md) page describes the
other sections of the file and the locations the alerter searches for it.

The following table describes the top-level `anomaly` option:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable anomaly detection |

## Tier 1: Statistical Analysis

The `anomaly.tier1` section configures z-score-based statistical detection. The
following table describes the tier 1 options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 1 |
| `default_sensitivity` | float | `3.0` | Z-score threshold |
| `evaluation_interval_seconds` | integer | `60` | Evaluation interval |
| `clear_count` | integer | `3` | Consecutive in-band samples before an anomaly alert clears |

The `anomaly.tier1.clear_count` option sets how many consecutive samples of the
metric behind an active anomaly alert must score back inside the sensitivity
band before the alerter clears the alert. Only a newly collected sample counts
toward the total; tier 1 usually evaluates more often than the probe collects,
and an evaluation that re-scores a sample already counted does not add to the
count. The value must be at least 1; the alerter refuses to start, or to apply
a reload, with a smaller value. For a probe that collects every 300 seconds,
the default of 3 clears an alert about ten minutes after the first in-band
sample. A `SIGHUP` reload applies a new value from the next evaluation.

## Tier 1: Variance Floor, Warmup, and Z-Score Cap

Three additional blocks under `anomaly.tier1` prevent the detector from firing
on baselines that have not yet stabilised. These blocks are most relevant on
young datastores; on a datastore with one or two days of metric history, a
baseline's stored standard deviation can collapse far below the metric's
natural variation, producing z-scores in the thousands. The variance floor and
warmup gate suppress this failure mode, and the z-score cap acts as defence in
depth.

The `anomaly.tier1.max_z_score` option clamps the absolute z-score
symmetrically around zero before the sensitivity comparison. The default is
`100.0`; any genuine outlier sits well below this value, and the cap simply
prevents a runaway divisor from generating multi-thousand-sigma scores. Setting
`max_z_score: 0` disables the cap.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_z_score` | float | `100.0` | Z-score clamp; `0` disables |

The `anomaly.tier1.variance_floor` block enforces a minimum divisor on the
z-score calculation. The effective standard deviation is the larger of the raw
stored value and a hybrid floor; the floor itself is the larger of
`relative_pct` times the absolute baseline mean and `absolute_floor`. The
relative term dominates for non-zero metrics, while the absolute term acts as a
safety net when the mean approaches zero. Setting both `relative_pct: 0` and
`absolute_floor: 0` disables the floor entirely; the detector then falls back
to the existing `stddev == 0` guard.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `relative_pct` | float | `0.05` | Floor as fraction of abs(mean) |
| `absolute_floor` | float | `0.001` | Absolute minimum stddev |

The `anomaly.tier1.warmup` block suppresses detection for baselines that have
not accumulated enough samples or enough wall-clock observation time. Each
`period_type` (`all`, `hourly`, and `daily`) has its own pair of thresholds,
and a baseline is considered warm only when both are met. The `all` baseline
defaults require roughly one day of operation at the 60 second collection
interval; the `hourly` and `daily` defaults require enough span for each time
bucket to have been observed multiple times. Setting both `min_samples: 0` and
`min_span_hours: 0` for a given `period_type` disables warmup suppression for
that type.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `all.min_samples` | integer | `100` | Minimum sample count |
| `all.min_span_hours` | integer | `24` | Minimum span in hours |
| `hourly.min_samples` | integer | `5` | Minimum sample count |
| `hourly.min_span_hours` | integer | `120` | Minimum span in hours |
| `daily.min_samples` | integer | `3` | Minimum sample count |
| `daily.min_span_hours` | integer | `336` | Minimum span in hours |

In the following example, the `anomaly.tier1` section tightens the variance
floor and extends the `all` warmup window:

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

Warmup suppressions are recorded at debug log level only; the detector does not
write a candidate row or an alert when it skips a cold baseline. The detector
logs a fall-through from a cold hourly or daily baseline row to a warm, less
specific row at the same level. To inspect recent suppressions, enable debug
logging in the alerter and run
`sudo journalctl -u pgedge-ai-dba-alerter --since 10m`. The log line
names the connection, metric, period type, and sample count, which is enough to
confirm whether a missing alert reflects warmup suppression or a genuinely
quiet metric.

## Tier 1: Per-Metric Floors

The `anomaly.tier1.metric_floors` map sets two floors for each metric, so that
a value that is unusual for a quiet baseline but harmless in absolute terms
does not raise an alert. Each key is a metric name, and each entry accepts two
options:

| Option | Type | Description |
|--------|------|-------------|
| `min_value` | float | Value below which the metric is never anomalous; `0` disables |
| `min_stddev` | float | Minimum z-score divisor in the metric's units; `0` disables |

A value under `min_value` raises no anomaly candidate, and the value counts as
in band when the alerter decides whether to clear an open anomaly alert. The
`min_stddev` option sets a lower bound on the divisor of the z-score, so a
small change on a baseline that has never moved scores a small z-score. The
alerter divides by the largest of `min_stddev`, the baseline's own standard
deviation, `relative_pct` times the absolute baseline mean (5% by default), and
`absolute_floor`, so `min_stddev` takes effect only when it exceeds the other
three values. When the baseline has no spread of its own, the alerter caps the
alert severity at `warning`; threshold rules remain the way to raise a critical
alert on an absolute value.

The alerter has built-in floors for its standard metrics, for example
`min_value: 50` and `min_stddev: 5` for CPU usage percent, and `min_value: 10`
and `min_stddev: 5` for the session count; the
[anomaly detection guide](../../developer-guide/alerter/anomaly-detection.md#per-metric-floors)
lists them all. An entry overrides a built-in floor field by field, so an entry
that sets only `min_value` keeps the built-in `min_stddev`. The alerter refuses
a negative or non-finite value, or an empty metric name, and logs a warning at
startup and on each reload for an entry naming a metric that anomaly detection
does not score, such as a mistyped name.

In the following example, the `metric_floors` map raises the CPU value floor
and disables both floors for the session count:

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

## Tier 2: Embedding Similarity

The `anomaly.tier2` section configures pgvector-based similarity search for
pattern matching.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 2 |
| `suppression_threshold` | float | `0.85` | Suppression threshold |
| `similarity_threshold` | float | `0.3` | Similarity threshold |

## Tier 3: LLM Classification

The `anomaly.tier3` section configures LLM-based classification for complex
anomalies.

The following table describes the tier 3 options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable tier 3 |
| `timeout_seconds` | integer | `30` | Timeout for each LLM API call |
| `decision_suppress_threshold` | number | `0.5` | Probability that a candidate is not a real issue at which the decision model suppresses it; must be greater than 0 and less than 1 while a decision provider is configured |

Tier 3 classifies each candidate with the reasoning model unless
`llm.decision_provider` selects a decision model. With a decision provider,
tier 3 asks the decision model first and uses the reasoning model only as a
fallback; the [Decision Model](llm_providers.md#decision-model-decision_provider) section
describes the behavior. Tier 3 runs while tier 3 is enabled and either a
reasoning or a decision provider initialized at startup.

The decision model returns the probability that an anomaly is a real issue
requiring attention. Tier 3 suppresses the candidate when one minus that
probability is at least `decision_suppress_threshold`, and raises the alert
otherwise. The default of `0.5` suppresses a candidate that the model judges
more likely than not to be a false positive; a higher value suppresses fewer
candidates. The alerter checks the value only when a decision provider is
configured, and ignores the option otherwise. Each provider calibrates its
probabilities differently, so tune the threshold for the provider in use rather
than carrying a value across providers.

Each model call has its own `timeout_seconds` limit. A candidate whose decision
model call fails and then falls back to the reasoning model can therefore take
up to twice the timeout.

## Provider Health (`provider_health`)

The `anomaly.provider_health` section controls the system alert that the
alerter raises when calls to the embedding, reasoning, or decision provider
keep failing. While a provider fails, anomaly detection runs in a degraded mode
whose effect depends on the provider that fails:

- while the embedding provider fails, tier 2 compares no candidate with past
  anomalies and passes each candidate to tier 3, where a working reasoning or
  decision model still classifies the candidate.
- while the reasoning provider fails, tier 3 raises each candidate that the
  decision model does not classify without LLM classification.
- while the decision provider fails, tier 3 classifies each candidate with the
  reasoning model where one is configured, and otherwise raises the candidate
  without classification.
- while any provider fails, the re-evaluation worker leaves acknowledged
  anomaly alerts unchanged.

The following table describes the provider health options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `failure_threshold` | integer | `3` | Consecutive failed calls that raise the alert; must be at least 1 |
| `failure_rate` | number | `0.3` | Share of the last `failure_rate_window` calls that raises the alert when they fail; must be greater than 0 and at most 1 |
| `failure_rate_window` | integer | `20` | Number of recent calls the failure rate covers; must be between 1 and 1000. A small window can set a limit of one failed call, as `failure_rate: 0.3` with a window of `3` does, so the first failure raises the alert before `failure_threshold` is reached |

The alerter counts consecutive failures separately for tier 2 embeddings, tier
3 classification, tier 3 decisions, and re-evaluation, and for each provider.
When one count reaches the threshold, the alerter raises a single warning alert
for that tier and provider, and updates the alert's failure count as further
calls fail. The next successful call to that provider resets the count and
clears the alert, unless the failure rate described below keeps the alert open;
tier 3 classification and re-evaluation share the reasoning provider, so a
successful call from either clears the alerts of both. The decision provider
serves tier 3 decisions alone, so no reasoning call clears its alert; a
successful decision call clears it, as does a successful startup check of the
decision provider, described below. After clearing an alert, the alerter does
not raise the alert again for five minutes, so that a provider that fails
intermittently cannot send a burst of notifications. A call canceled before it
completes counts as neither a failure nor a success, although a call that times
out counts as a failure. A tier 3 or re-evaluation call also counts as a
failure when the provider answers with a response that holds no decision the
alerter can parse; a decision call counts as a failure when the model refuses
to answer, gives no answer, or gives a probability outside the range 0 to 1.
The alerter records the failure whatever the alerter then does with the
candidate or alert, which may be to act on a keyword found in the response or
to take the tier's default decision.

When anomaly detection is enabled, the alerter also checks the providers of the
enabled tiers at startup. A failed check raises the alert for its tier
immediately, whatever the threshold or the five-minute wait; a failed reasoning
check raises the tier 3 alert. A successful reasoning check clears both the
tier 3 and the re-evaluation alerts. When tier 3 is enabled, the alerter also
checks the decision provider by listing its models, which costs no tokens; a
successful listing counts as a successful decision call, so the listing can
clear the decision alert left open by a previous run. At startup, the alerter
also clears any open provider alert for a tier or provider that is no longer
configured or enabled. See
[System Alerts](../../user-guide/alerts/index.md#system-alerts) for how the alert
appears.

The failure rate catches a provider that fails a steady share of its calls
without failing consecutively, such as a provider that rejects every other
request. The alerter keeps the outcomes of the last `failure_rate_window` calls
for each tier and provider, and sets a limit of `failure_rate` times the
window, rounded up; the defaults give a limit of 6 failed calls out of 20. The
alerter raises the alert when the failed calls among those outcomes reach the
limit, even before the window is full, and the five-minute wait still applies.
A raised alert stays open while the failed calls remain at or above the limit,
and clears on the first successful call that leaves fewer failed calls than the
limit. A run of consecutive failures that reached the threshold counts as a
single failed call when the provider recovers, provided that single failed call
leaves the provider below the limit; an outage therefore clears on the first
successful call, as the consecutive count alone would. Setting `failure_rate`
to `1` leaves the consecutive count as effectively the only rule that raises
the alert, provided `failure_rate_window` is at least `failure_threshold`.

In the following example, the alerter raises the alert after five consecutive
failures, or when 10 of the last 40 calls have failed:

```yaml
anomaly:
  provider_health:
    failure_threshold: 5
    failure_rate: 0.25
    failure_rate_window: 40
```
