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
- A secret file must exist. When `secret_file` is not set, the collector
  searches for `ai-dba-collector.secret` in the per-user config directory
  (`~/.config/pgedge/` on Linux, `~/Library/Application Support/pgedge/` on
  macOS, and `%AppData%\pgedge\` on Windows) and then in `/etc/pgedge/`; when
  `secret_file` is set, the collector reads that file instead.
  [Security Best Practices](index.md#security-best-practices) describes how to
  create the secret.

The collector also validates the following ranges:

- `datastore.port` must be between 1 and 65535.
- Pool `max_connections` values must be greater than 0.
- Pool `max_idle_seconds` values must be 0 or greater.
- Pool `max_wait_seconds` values must be greater than 0.
- `scheduler.max_concurrent_probes` must be greater than 0.
- `scheduler.max_concurrent_probes_per_connection` must be greater than 0.
- `scheduler.startup_jitter_seconds` must be 0 or greater.
- `connection_security.ssl_cert_dir` must be an absolute path when set.

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
| `hostaddr` | string | none | No | `-pg-hostaddr` | `hostaddr: 192.168.1.100` | PostgreSQL server IP address | Not currently usable: the collector's PostgreSQL driver does not recognize `hostaddr` and passes it to the server as a setting, so the connection fails. Set `host` to the IP address instead. |
| `database` | string | `ai_workbench` | Yes | `-pg-database` | `database: metrics` | Database name for the datastore | |
| `username` | string | `postgres` | Yes | `-pg-username` | `username: ai_workbench` | Username for the datastore connection | |
| `password_file` | string (file path) | none | No | `-pg-password-file` | `password_file: /etc/ai-workbench/collector/pw.txt` | Path to a file containing the datastore password | Plain text, password only. An inline `password` setting takes precedence, and the collector then does not read this file. |
| `port` | integer | `5432` | No | `-pg-port` | `port: 5433` | PostgreSQL server port number | Range: 1-65535 |
| `sslmode` | string | `prefer` | No | `-pg-sslmode` | `sslmode: require` | SSL/TLS mode for the datastore connection | |
| `sslcert` | string (file path) | none | No | `-pg-sslcert` | `sslcert: /etc/ai-workbench/client.pem` | Path to the client SSL certificate file | Use with `verify-ca` or `verify-full` modes. |
| `sslkey` | string (file path) | none | If `sslcert` is set | `-pg-sslkey` | `sslkey: /etc/ai-workbench/client-key.pem` | Path to the client SSL private key file | Required if `sslcert` is set. |
| `sslrootcert` | string (file path) | none | No | `-pg-sslrootcert` | `sslrootcert: /etc/ai-workbench/ca.pem` | Path to the root CA certificate file | Used to verify the server. |

In the following example, the commands create a new directory that only the
collector's account can enter, prompt for the password without echoing it or
saving it in the shell history, and write the password to a file of mode `600`
in that directory, replacing an existing password file only after the write
succeeds; an empty password leaves an existing password file unchanged. Set
`PWFILE` to the path in your `datastore.password_file` setting, and replace
`pgedge` with the account that runs the collector. Use a directory that holds
nothing else, because `install -d` sets the mode and owner of an existing
directory too:

```bash
PWFILE=/etc/ai-workbench/collector/pw.txt
sudo install -d -m 700 -o pgedge "$(dirname "$PWFILE")" &&
IFS= read -rs -p "Datastore password: " PGPASS && echo &&
[ -n "$PGPASS" ] &&
printf '%s\n' "$PGPASS" |
    sudo -u pgedge sh -c 'umask 077
        cat > "$1.new" && mv "$1.new" "$1" || { rm -f "$1.new"; exit 1; }' \
        sh "$PWFILE"
unset PGPASS PWFILE
```

The following SSL modes are supported:

- `disable` disables SSL encryption.
- `allow` attempts a non-SSL connection first and falls back to SSL.
- `prefer` attempts an SSL connection first and falls back to non-SSL.
- `require` requires SSL; when `sslrootcert` is set, it also verifies the
  server certificate against that CA, without checking the hostname.
- `verify-ca` requires SSL and verifies the server certificate against the CA.
- `verify-full` requires SSL and verifies the certificate and hostname.

## Specifying Connection Pool Options

All connection pool options are nested under the `pool:` key in the YAML file.
Pool settings support configuration only through the configuration file;
command-line flags are not available for pool options.

| Option | Type | Default | Min | Example | Description | Notes |
|--------|------|---------|-----|---------|-------------|-------|
| `datastore_max_connections` | integer | `25` | 1 | `datastore_max_connections: 50` | Maximum number of concurrent connections to the datastore. | Increase for more concurrent probe storage. |
| `datastore_max_idle_seconds` | integer | `300` (5 minutes) | 0 | `datastore_max_idle_seconds: 600` | Maximum idle time in seconds for datastore connections. | A value of `0` closes each idle datastore connection at the next pool health check, which runs every minute. |
| `datastore_max_wait_seconds` | integer | `60` | 1 | `datastore_max_wait_seconds: 120` | Maximum wait time in seconds for an available datastore connection. | Probe storage fails if the timeout expires. |
| `max_connections_per_server` | integer | `3` | 1 | `max_connections_per_server: 5` | Maximum number of connections the collector keeps open to each monitored server, counting idle connections as well as those in use. | The limit applies to each monitored connection, not to the collector as a whole; the server component's own connections to monitored servers are not covered. |
| `monitored_max_idle_seconds` | integer | `300` (5 minutes) | 0 | `monitored_max_idle_seconds: 600` | Maximum idle time in seconds for monitored connections. | A value of `0` leaves the connection library's default of 30 minutes in effect. |
| `monitored_max_wait_seconds` | integer | `120` | 1 | `monitored_max_wait_seconds: 60` | Maximum time in seconds for each monitored probe execution, including waiting for an available monitored connection and running the probe. | Probe execution fails if the timeout expires. |

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

A probe holds its slot while it runs against the monitored server and while it
stores the results in the datastore. The `pool.monitored_max_wait_seconds`
timeout bounds the monitored execution, so a monitored server that accepts
connections but answers slowly drives every one of its probes to that timeout.
Storing the results has its own timeout for acquiring a datastore connection,
so a slow datastore can hold the slot for longer than
`pool.monitored_max_wait_seconds`.
When `max_concurrent_probes_per_connection` is lower than
`max_concurrent_probes`, the ceiling stops one such server from occupying the
whole global budget and queueing the probes of healthy connections behind it.
Several slow servers together can still use up the global budget.

With `startup_jitter_seconds`, the random delay before a past-due 30-second
probe is at most 30 seconds, while the delay before an hourly probe is at most
the configured window. The probe then waits for a free slot, so under
contention its first execution can start later than the delay alone
suggests. The
collector realigns the probe's timer after that first execution, so the offset
also spreads out steady-state collection.

## Specifying Metrics Storage Options

The metrics storage option selects where the collector stores the tables in the
`metrics` schema. The option is a top-level key in the YAML file.

| Option | Type | Default | Example | Description | Notes |
|--------|------|---------|---------|-------------|-------|
| `use_coldfront_for_metrics` | boolean | `false` | `use_coldfront_for_metrics: true` | Stores each metrics table in pgEdge ColdFront, as a PostgreSQL view over an Apache Iceberg table on object storage, instead of as a partitioned PostgreSQL table. | Command-line: `-use-coldfront-for-metrics`. |

Only the collector reads this option; the server and alerter work against
either kind of datastore without a setting. Choose the mode when you create the
datastore; the collector refuses to start when the value does not match the
existing metrics tables. The Workbench performs no metrics retention in this
mode, and the collector ignores every `retention_days` setting. The operator
must schedule the ColdFront compactor for every metrics table in this mode.

The [Storing Metrics in pgEdge ColdFront](coldfront-metrics.md) page describes
the prerequisites, the compactor requirement, and the behavioural differences
of this mode.

## Specifying Connection Security Options

The connection security options limit what a stored monitored connection can
make the collector do. The options are nested under the `connection_security:`
key in the YAML file. The collector reads them at startup only, so a change
takes effect when the collector restarts.

| Option | Type | Default | Example | Description | Notes |
|--------|------|---------|---------|-------------|-------|
| `ssl_cert_dir` | string (directory path) | `""` (unset) | `ssl_cert_dir: /etc/pgedge/ai-dba-workbench/certs` | Absolute directory that the SSL certificate, key and root certificate files of monitored connections must live in. | Command-line: `-connection-ssl-cert-dir`. The option has no environment variable. The collector refuses to start when the option holds a relative path. |

The collector reads the SSL files itself when the collector connects to a
monitored database, so the directory limits which files a user who can create
a connection can make the collector open. The option has the same rules as the
[server option of the same name](../server/configuration_file.md#using-the-ssl_cert_dir-property).
Before the collector builds a pool for a connection, the collector checks each
stored SSL file path against the following rules:

- The path is absolute and contains no `..` element or control character.
- The path resolves, following symbolic links, to a location inside the
  `ssl_cert_dir` directory.

When the option is unset, the collector refuses every connection that names an
SSL file; a root certificate value of `system`, which selects the platform
trust store, is always allowed. The collector does not monitor a refused
connection, logs the connection ID and the name of the refused field without
the path, and records an error ending in `invalid connection parameters`
against the connection.

Set the option on the collector as well as on the server, and keep the
certificate files at the same paths on the collector host, because both
components read the same stored paths. The directory, every file and link in
it, and every parent directory must be writable only by root or the account
that runs the collector. The collector resolves a path before the database
driver opens the file, so an account that can replace a symbolic link in the
directory, or replace the directory itself through a writable parent, between
the two steps could still point the driver at a file outside the directory.

In the following example, the collector accepts SSL files in the
`/etc/pgedge/ai-dba-workbench/certs` directory:

```yaml
connection_security:
  ssl_cert_dir: /etc/pgedge/ai-dba-workbench/certs
```

### Checking Addresses at Connect Time

The collector checks each address that a monitored connection's host resolves
to each time the collector connects, and connects only to the addresses that
pass. The check defeats DNS rebinding, in which a host name that the server
accepted when the connection was saved later resolves to another address. The
collector has none of the server's `connection_security` host settings, so the
collector refuses only the following fixed set of addresses:

- The unspecified addresses `0.0.0.0/8` and `::`.
- The link-local unicast ranges `169.254.0.0/16` and `fe80::/10`, which
  include the `169.254.169.254` cloud metadata address.
- The IPv4 link-local multicast range `224.0.0.0/24` and every IPv6 multicast
  address of link-local scope, such as `ff02::1`.
- The cloud metadata addresses `fd00:ec2::254`, `fd00:ec2::23`,
  `fd20:ce::254` and `100.100.100.200`.
- An address in `::/96`, `::ffff:0:0:0/96`, `64:ff9b::/96` or `2002::/16`
  that embeds one of these IPv4 addresses.
- An address in the local-use NAT64 prefix `64:ff9b:1::/48` whose IPv4
  address, read at any RFC 6052 offset within the prefix, is in
  `169.254.0.0/16` or is `100.100.100.200`.

The collector allows loopback and private addresses, because a collector
routinely monitors servers on the same host or network. The server refuses
the same set of addresses when the server connects, whatever the server's
`connection_security` settings say, so neither component connects to a
link-local target. When no resolved address passes, the connection attempt
fails with an error that says the host resolves only to addresses that
connections are not permitted to.

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
- keep the default wait timeouts of 60 seconds in `datastore_max_wait_seconds`
  and 120 seconds in `monitored_max_wait_seconds` for most environments, and
  use a longer value for workloads that arrive in bursts. The example
  configuration file sets `monitored_max_wait_seconds` to 60 explicitly, which
  is shorter than the built-in default.
