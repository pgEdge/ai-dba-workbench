# Configuration File Properties

The alerter reads its settings from a YAML configuration file; command-line
flags override the matching settings in the file.

The alerter searches for its configuration file in the following order:

1. The path that the `-config` flag specifies.
2. The per-user config directory at `~/.config/pgedge/ai-dba-alerter.yaml` on
   Linux (honouring `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-alerter.yaml` on macOS, and
   `%AppData%\pgedge\ai-dba-alerter.yaml` on Windows.
3. The system-wide file at `/etc/pgedge/ai-dba-alerter.yaml`.

If `-config` is set and the file is missing, the alerter exits with an error.
If `-config` is not set and none of the default locations contain a
configuration file, the alerter uses built-in defaults silently. The alerter
does not search the binary directory or the current working directory. A
`SIGHUP` signal re-runs discovery on each reload, so a configuration file
installed at a default location after startup is picked up on the next signal.
A reload does not apply every setting;
[Reloading or Restarting the Alerter](index.md#reloading-or-restarting-the-alerter)
lists the settings that require a restart.

The following sections describe the properties in each section of the
configuration file:

- [Datastore Connection (`datastore`)](#datastore-connection-datastore)
- [Connection Pool (`pool`)](#connection-pool-pool)
- [Threshold Evaluation (`threshold`)](#threshold-evaluation-threshold)
- [Baseline Calculation (`baselines`)](#baseline-calculation-baselines)
- [Correlation (`correlation`)](#correlation-correlation)
- [Notifications (`notifications`)](#notifications-notifications)

The `anomaly` and `llm` sections of the file have pages of their own;
[Anomaly Detection Properties](anomaly_detection.md) and
[LLM Provider Properties](llm_providers.md) describe those properties. A
complete sample configuration file is available at
[ai-dba-alerter.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-alerter.yaml)
in the project repository.

## Datastore Connection (`datastore`)

The `datastore` section configures the connection to the AI DBA Workbench
PostgreSQL datastore.

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
- `allow` attempts non-SSL first and falls back to SSL.
- `prefer` attempts SSL first and falls back to non-SSL.
- `require` requires SSL without certificate verification.
- `verify-ca` requires SSL and verifies the server certificate.
- `verify-full` requires SSL and verifies the certificate and hostname.

## Connection Pool (`pool`)

The `pool` section configures the database connection pool.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_connections` | integer | `10` | Max concurrent connections |
| `max_idle_seconds` | integer | `300` | Idle connection timeout |

## Threshold Evaluation (`threshold`)

The `threshold` section configures how the alerter evaluates threshold-based
alerts. The thresholds themselves are not in this file; set the estate-wide
values on the `Alert Defaults` tab of the Administration console, and set
overrides on the `ALERT OVERRIDES` tab of the server, cluster, or group edit
dialog; see [Managing Alert Rules](../../admin-guide/alert-rules/index.md). The
following table describes the threshold options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `evaluation_interval_seconds` | integer | `60` | Evaluation interval |
| `trigger_count` | integer | `2` | Consecutive breaching samples that raise an alert; must be at least 1 |
| `clear_count` | integer | `3` | Consecutive samples within the threshold that clear an alert; must be at least 1 |

The `trigger_count` and `clear_count` options stop an alert from being raised
and cleared repeatedly when a metric hovers near its threshold. The alerter
raises an alert only after the metric breaches the threshold on `trigger_count`
consecutive metric samples, and clears an active alert only after the metric
has been back within the threshold on `clear_count` consecutive samples. A
sample is one collection by the collector rather than one evaluation cycle; the
evaluator and the alert cleaner read the same sample several times between
collections, but each sample counts once. A sample that breaches the threshold
restarts the clear count, and a sample that does not breach the threshold
restarts the trigger count.

Setting both options to `1` restores the earlier behavior of raising and
clearing on a single sample. The alerter holds the counts in memory, so
restarting the alerter starts the counts again from zero; at worst, a restart
delays a raise or a clear by a few samples. The counts do not apply to the
`metric_staleness` and `probe_unavailable` rules or to connection error alerts,
which do not judge metric samples.

## Baseline Calculation (`baselines`)

The `baselines` section configures baseline metric calculation for anomaly
detection. The following table describes the baseline options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `refresh_interval_seconds` | integer | `3600` | Refresh interval |
| `lookback_days` | integer | `15` | Historical lookback in days |

The lookback also caps the age of a baseline's earliest sample, because each
refresh reads only samples inside the window and rewrites `earliest_sample_at`
from them. A warmup tier whose `min_span_hours` exceeds `lookback_days`
multiplied by 24 can therefore never become warm, and the alerter logs a
warning on each baseline cycle naming the tier. The default of 15 days covers
the daily tier's 336 hour span with a day to spare and gives every weekday at
least two occurrences in the window.

## Correlation (`correlation`)

The `correlation` section configures alert correlation across metrics.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `window_seconds` | integer | `120` | Correlation time window |

## Notifications (`notifications`)

The `notifications` section configures the notification delivery system for
sending alerts through external channels. The channels themselves are not in
this file; create and manage them on the channel tabs of the Administration
console, such as `Email Channels` and `Slack Channels`; see
[Managing Notification Channels](../../admin-guide/notification-channels/index.md).

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

The `secret_file` option specifies a file containing the same plain text secret
used by the server component. The alerter uses this secret to decrypt
notification channel credentials that the server encrypted. The alerter and the
server must reference the same secret file.

In the following example, the `notifications` section enables delivery with
custom retry settings:

```yaml
notifications:
  enabled: true
  secret_file: /etc/pgedge/server.secret
  process_interval_seconds: 30
  max_retry_attempts: 5
  retry_backoff_minutes: [5, 15, 30]
  http_timeout_seconds: 60
```
