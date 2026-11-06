# Configuration File Properties

The collector reads its settings from a YAML configuration file; command-line
flags override the matching settings in the file.

The collector searches for its configuration file in the following order:

1. The path that the `-config` flag specifies.
2. The per-user `config` directory at `~/.config/pgedge/ai-dba-collector.yaml`
   on Linux (honouring `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-collector.yaml` on macOS, and
   `%AppData%\pgedge\ai-dba-collector.yaml` on Windows.
3. The system-wide file at `/etc/pgedge/ai-dba-collector.yaml`.

If `-config` is set and the file is missing, the collector exits with an error.
If `-config` is not set and none of the default locations contain a
configuration file, the collector uses built-in defaults silently. The
collector does not search the binary directory or the current working
directory.

The following sections describe the settings that the collector validates at
startup and the properties in each section of the file.

## Validating the Configuration

The collector validates configuration at startup; the following fields must be
set:

- `datastore.host` must contain a hostname or IP.
- `datastore.database` must contain a database name.
- `datastore.username` must contain a username.
- A secret file must exist in one of the search paths.

The collector also validates the following ranges:

- `datastore.port` must be between 1 and 65535.
- Pool `max_connections` values must be greater than 0.
- Pool `max_idle_seconds` values must be 0 or greater.
- Pool `max_wait_seconds` values must be greater than 0.
- `scheduler.max_concurrent_probes` must be greater than 0.
- `scheduler.max_concurrent_probes_per_connection` must be greater than 0.
- `scheduler.startup_jitter_seconds` must be 0 or greater.

## Understanding the File Format

The configuration file uses YAML format with nested sections; the following
example shows the structure of the file:

```yaml
# Comments start with #

# Top-level settings
# secret_file: /etc/pgedge/ai-dba-collector.secret

# Nested sections
datastore:
  host: localhost
  port: 5432
  database: ai_workbench

pool:
  datastore_max_connections: 25
  max_connections_per_server: 3

scheduler:
  max_concurrent_probes: 8
  max_concurrent_probes_per_connection: 2
  startup_jitter_seconds: 60
```

A sample configuration file is available at
[ai-dba-collector.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-collector.yaml)
in the project repository.

## Specifying Datastore Connection Options

All datastore options are nested under the `datastore:` key in the YAML file.

| Option | Type | Default | Required | Command-line Flag | Example | Description | Notes |
|--------|------|---------|----------|--------------------|---------|-------------|-------|
| `host` | string | `localhost` | No | `-pg-host` | `host: db.example.com` | PostgreSQL server hostname or IP address | |
| `hostaddr` | string | none | No | `-pg-hostaddr` | `hostaddr: 192.168.1.100` | PostgreSQL server IP address; bypasses DNS lookup | Used instead of `host` when both are set. |
| `database` | string | `ai_workbench` | Yes | `-pg-database` | `database: metrics` | Database name for the datastore | |
| `username` | string | `postgres` | Yes | `-pg-username` | `username: ai_workbench` | Username for the datastore connection | |
| `password_file` | string (file path) | none | No | `-pg-password-file` | `password_file: /etc/ai-workbench/pw.txt` | Path to a file containing the datastore password | Plain text, password only. |
| `port` | integer | `5432` | No | `-pg-port` | `port: 5433` | PostgreSQL server port number | Range: 1-65535 |
| `sslmode` | string | `prefer` | No | `-pg-sslmode` | `sslmode: require` | SSL/TLS mode for the datastore connection | |
| `sslcert` | string (file path) | none | No | `-pg-sslcert` | `sslcert: /etc/ai-workbench/client.pem` | Path to the client SSL certificate file | Use with `verify-ca` or `verify-full` modes. |
| `sslkey` | string (file path) | none | If `sslcert` is set | `-pg-sslkey` | `sslkey: /etc/ai-workbench/client-key.pem` | Path to the client SSL private key file | Required if `sslcert` is set. |
| `sslrootcert` | string (file path) | none | No | `-pg-sslrootcert` | `sslrootcert: /etc/ai-workbench/ca.pem` | Path to the root CA certificate file | Used to verify the server. |

In the following example, the commands create a password file with secure
permissions:

```bash
echo "my-secure-password" \
    > /etc/ai-workbench/password.txt
chmod 600 /etc/ai-workbench/password.txt
```

The following SSL modes are supported:

- `disable` disables SSL encryption.
- `allow` attempts a non-SSL connection first and falls back to SSL.
- `prefer` attempts an SSL connection first and falls back to non-SSL.
- `require` requires SSL but does not verify the server certificate.
- `verify-ca` requires SSL and verifies the server certificate against the CA.
- `verify-full` requires SSL and verifies the certificate and hostname.

## Specifying Connection Pool Options

All connection pool options are nested under the `pool:` key in the YAML file.
Pool settings support configuration only through the configuration file;
command-line flags are not available for pool options.

| Option | Type | Default | Min | Example | Description | Notes |
|--------|------|---------|-----|---------|-------------|-------|
| `datastore_max_connections` | integer | `25` | 1 | `datastore_max_connections: 50` | Maximum number of concurrent connections to the datastore. | Increase for more concurrent probe storage. |
| `datastore_max_idle_seconds` | integer | `300` (5 minutes) | 0 (disables idle cleanup) | `datastore_max_idle_seconds: 600` | Maximum idle time in seconds for datastore connections. | |
| `datastore_max_wait_seconds` | integer | `60` | 1 | `datastore_max_wait_seconds: 120` | Maximum wait time in seconds for an available datastore connection. | Probe storage fails if the timeout expires. |
| `max_connections_per_server` | integer | `3` | 1 | `max_connections_per_server: 5` | Maximum number of connections the collector keeps open to each monitored server, counting idle connections as well as those in use. | The limit applies to each monitored connection, not to the collector as a whole; the server component's own connections to monitored servers are not covered. |
| `monitored_max_idle_seconds` | integer | `300` (5 minutes) | 0 (disables idle cleanup) | `monitored_max_idle_seconds: 600` | Maximum idle time in seconds for monitored connections. | |
| `monitored_max_wait_seconds` | integer | `60` | 1 | `monitored_max_wait_seconds: 120` | Maximum wait time in seconds for an available monitored connection. | Probe execution fails if the timeout expires. |

The `max_connections_per_server` limit covers every database on the server, so
a probe that visits each database in turn closes idle connections to other
databases before it opens a new one. The collector keeps one pool for
server-wide probes and one for each database that a probe visits. A value lower
than the number of those databases plus one means that probes close and reopen
a connection on most visits, which raises the rate of new sessions on the
server. A value of at least the database count plus one lets each pool keep an
idle connection between visits.

## Specifying Probe Scheduler Options

All probe scheduler options are nested under the `scheduler:` key in the YAML
file. Scheduler settings support configuration only through the configuration
file; command-line flags are not available for scheduler options. These options
govern how many probe executions run at once and how the collector spreads the
first execution of each probe after it starts.

| Option | Type | Default | Min | Example | Description | Notes |
|--------|------|---------|-----|---------|-------------|-------|
| `max_concurrent_probes` | integer | `8` | 1 | `max_concurrent_probes: 16` | Maximum number of probe executions that run at the same time across every monitored connection. | The limit applies to the collector as a whole. |
| `max_concurrent_probes_per_connection` | integer | `2` | 1 | `max_concurrent_probes_per_connection: 3` | Maximum number of the `max_concurrent_probes` slots that a single monitored connection may hold at the same time. | The collector clamps a value greater than `max_concurrent_probes` to `max_concurrent_probes`. |
| `startup_jitter_seconds` | integer | `60` | 0 (disables the stagger) | `startup_jitter_seconds: 120` | Upper bound, in seconds, on the random delay before the first execution of a probe that is past due, has never run, or whose last collection time could not be read from the datastore. | The delay is drawn uniformly from zero up to the smaller of the probe's interval and this value. |

Because `max_concurrent_probes` applies to the collector as a whole, the memory
and connection demand of executing probes does not scale with the number of
monitored connections multiplied by the number of probes. The scheduler still
keeps one goroutine per connection and probe pair, so that smaller baseline
still grows with the number of monitored connections.

A probe holds its slot for as long as the execution runs, up to
`pool.monitored_max_wait_seconds`, so a monitored server that accepts
connections but answers slowly drives every one of its probes to that timeout.
The `max_concurrent_probes_per_connection` ceiling stops such a server from
occupying the whole global budget and queueing the probes of healthy
connections behind it.

With `startup_jitter_seconds`, a past-due 30-second probe still starts within
30 seconds, while an hourly probe starts within the configured window. The
collector realigns the probe's timer after that first execution, so the offset
also spreads out steady-state collection.

## Guidelines for Tuning the Collector

To choose values for the pool and timeout settings:

- set `datastore_max_connections` from the number of probes and monitored
  servers, using `(number of probes * concurrent servers) / 2` as a starting
  point; for example, 24 probes and 10 monitored servers suggest about 120
  connections.
- start `max_connections_per_server` at the default of 3, and raise the value
  if timeout errors occur or if the network latency to the monitored servers is
  high.
- keep the default idle timeout of 300 seconds (5 minutes) in
  `datastore_max_idle_seconds` and `monitored_max_idle_seconds` for most
  environments, and use a longer value when new connections are expensive to
  create.
- keep the default wait timeout of 60 seconds in `datastore_max_wait_seconds`
  and `monitored_max_wait_seconds` for most environments, and use a longer
  value for workloads that arrive in bursts.
