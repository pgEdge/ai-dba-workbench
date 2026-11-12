# Collector Configuration

The collector supports configuration through a YAML file and
command-line flags. The collector loads configuration in the following
order; later sources override earlier ones:

1. Built-in defaults.
2. Configuration file.
3. Command-line flags.

The collector overrides a configuration file setting only when you
actually pass the corresponding flag; a flag you omit never
overrides the file, even though the flag has a built-in default. A
flag you do pass always wins, including when the value you give it
happens to equal that flag's default, such as `-pg-port 5432`.

## File Location

The collector searches for its configuration file in the following
locations in order:

1. The path specified via the `-config` flag.
2. The per-user `config` directory at `~/.config/pgedge/ai-dba-collector.yaml`
   on Linux (honouring `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-collector.yaml`
   on macOS, and `%AppData%\pgedge\ai-dba-collector.yaml`
   on Windows.
3. `/etc/pgedge/ai-dba-collector.yaml` (system-wide).

If `-config` is set and the file is missing, the collector exits with
an error. If `-config` is not set and none of the default locations
contain a configuration file, the collector uses built-in defaults
silently. The collector no longer searches the binary directory or the
current working directory.

## File Format

The configuration file uses YAML format with nested sections.

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

## Datastore Connection Options

All datastore options are nested under the `datastore:` key in the
YAML file.

### datastore.host

The `host` option specifies the PostgreSQL server hostname or IP
address for the datastore.

- Type: string
- Default: `localhost`
- Required: No
- Command-line: `-pg-host`
- Example: `host: db.example.com`

### datastore.hostaddr

The `hostaddr` option specifies the PostgreSQL server IP address and
bypasses DNS lookup.

- Type: string
- Default: none
- Required: No
- Command-line: `-pg-hostaddr`
- Example: `hostaddr: 192.168.1.100`
- Note: The collector uses this address instead of `host` when both
  are set.

### datastore.database

The `database` option specifies the database name for the datastore.

- Type: string
- Default: `ai_workbench`
- Required: Yes
- Command-line: `-pg-database`
- Example: `database: metrics`

### datastore.username

The `username` option specifies the username for the datastore
connection.

- Type: string
- Default: `postgres`
- Required: Yes
- Command-line: `-pg-username`
- Example: `username: ai_workbench`

### datastore.password_file

The `password_file` option specifies the path to a file containing
the datastore password.

- Type: string (file path)
- Default: none
- Required: No (but strongly recommended)
- Command-line: `-pg-password-file`
- Example: `password_file: /etc/ai-workbench/pw.txt`
- File format: Plain text with the password only.

In the following example, the commands create a password file with
secure permissions:

```bash
echo "my-secure-password" \
    > /etc/ai-workbench/password.txt
chmod 600 /etc/ai-workbench/password.txt
```

### datastore.port

The `port` option specifies the PostgreSQL server port number.

- Type: integer
- Default: `5432`
- Required: No
- Range: 1-65535
- Command-line: `-pg-port`
- Example: `port: 5433`

### datastore.sslmode

The `sslmode` option specifies the SSL/TLS mode for the datastore
connection.

- Type: string
- Default: `prefer`
- Required: No
- Command-line: `-pg-sslmode`
- Example: `sslmode: require`

The following SSL modes are supported:

- `disable` disables SSL encryption.
- `allow` attempts a non-SSL connection first and falls back to SSL.
- `prefer` attempts an SSL connection first and falls back to
  non-SSL.
- `require` requires SSL but does not verify the server certificate.
- `verify-ca` requires SSL and verifies the server certificate
  against the CA.
- `verify-full` requires SSL and verifies the certificate and
  hostname.

### datastore.sslcert

The `sslcert` option specifies the path to the client SSL certificate
file.

- Type: string (file path)
- Default: none
- Required: No
- Command-line: `-pg-sslcert`
- Example: `sslcert: /etc/ai-workbench/client.pem`
- Note: Use with `verify-ca` or `verify-full` modes.

### datastore.sslkey

The `sslkey` option specifies the path to the client SSL private key
file.

- Type: string (file path)
- Default: none
- Required: No (required if `sslcert` is set)
- Command-line: `-pg-sslkey`
- Example: `sslkey: /etc/ai-workbench/client-key.pem`

### datastore.sslrootcert

The `sslrootcert` option specifies the path to the root CA certificate
file.

- Type: string (file path)
- Default: none
- Required: No
- Command-line: `-pg-sslrootcert`
- Example: `sslrootcert: /etc/ai-workbench/ca.pem`
- Note: The collector uses this certificate to verify the server.

## Connection Pool Options

All connection pool options are nested under the `pool:` key in the
YAML file. Pool settings support configuration only through the
configuration file; command-line flags are not available for pool
options.

### pool.datastore_max_connections

The `datastore_max_connections` option specifies the maximum number of
concurrent connections to the datastore.

- Type: integer
- Default: `25`
- Min: 1
- Example: `datastore_max_connections: 50`
- Tuning: Increase for more concurrent probe storage.

### pool.datastore_max_idle_seconds

The `datastore_max_idle_seconds` option specifies the maximum idle
time in seconds for datastore connections.

- Type: integer
- Default: `300` (5 minutes)
- Min: 0 (disables idle cleanup)
- Example: `datastore_max_idle_seconds: 600`

### pool.datastore_max_wait_seconds

The `datastore_max_wait_seconds` option specifies the maximum wait
time in seconds for an available datastore connection.

- Type: integer
- Default: `60`
- Min: 1
- Example: `datastore_max_wait_seconds: 120`
- Tuning: Probe storage fails if the timeout expires.

### pool.max_connections_per_server

The `max_connections_per_server` option specifies the maximum number
of connections the collector keeps open to each monitored server,
counting idle connections as well as those in use. The limit covers
every database on the server, so a probe that visits each database in
turn closes idle connections to other databases before it opens a new
one.

The collector keeps one pool for server-wide probes and one for each
database that a probe visits. A value lower than the number of those
databases plus one means that probes close and reopen a connection on
most visits, which raises the rate of new sessions on the server. A
value of at least the database count plus one lets each pool keep an
idle connection between visits.

- Type: integer
- Default: `3`
- Min: 1
- Example: `max_connections_per_server: 5`
- Note: This limit applies to each monitored connection, not to the
  collector as a whole. The server component opens its own
  connections to monitored servers, which this limit does not cover.

### pool.monitored_max_idle_seconds

The `monitored_max_idle_seconds` option specifies the maximum idle
time in seconds for monitored connections.

- Type: integer
- Default: `300` (5 minutes)
- Min: 0 (disables idle cleanup)
- Example: `monitored_max_idle_seconds: 600`

### pool.monitored_max_wait_seconds

The `monitored_max_wait_seconds` option specifies the maximum wait
time in seconds for an available monitored connection.

- Type: integer
- Default: `60`
- Min: 1
- Example: `monitored_max_wait_seconds: 120`
- Tuning: Probe execution fails if the timeout expires.

## Probe Scheduler Options

All probe scheduler options are nested under the `scheduler:` key in
the YAML file. Scheduler settings support configuration only through
the configuration file; command-line flags are not available for
scheduler options. These options govern how many probe executions run
at once and how the collector spreads the first execution of each
probe after it starts.

### scheduler.max_concurrent_probes

The `max_concurrent_probes` option specifies the maximum number of
probe executions that run at the same time across every monitored
connection.

- Type: integer
- Default: `8`
- Min: 1
- Example: `max_concurrent_probes: 16`
- Note: This limit applies to the collector as a whole, so the memory
  and connection demand of probes actually executing no longer scales
  with the number of monitored connections multiplied by the number of
  probes. The scheduler still keeps one goroutine per connection and
  probe pair, so that smaller baseline does still grow with the number
  of monitored connections.

### scheduler.max_concurrent_probes_per_connection

The `max_concurrent_probes_per_connection` option specifies the
maximum number of the `max_concurrent_probes` slots that any single
monitored connection may hold at the same time.

- Type: integer
- Default: `2`
- Min: 1
- Example: `max_concurrent_probes_per_connection: 3`
- Note: The collector clamps a value greater than
  `max_concurrent_probes` to `max_concurrent_probes`, because a
  ceiling above the global cap can never take effect.
- Note: A probe holds its slot for as long as the execution runs, up
  to `pool.monitored_max_wait_seconds`, so a monitored server that
  accepts connections but answers slowly drives every one of its
  probes to that timeout. This ceiling stops such a server occupying
  the whole global budget and queueing the probes of healthy
  connections behind it.

### scheduler.startup_jitter_seconds

The `startup_jitter_seconds` option specifies the upper bound, in
seconds, on the random delay the collector applies before the first
execution of a probe that is past due, has never run, or whose last
collection time could not be determined because the query against the
datastore failed.

- Type: integer
- Default: `60`
- Min: 0 (disables the stagger and runs such probes immediately)
- Example: `startup_jitter_seconds: 120`
- Note: The collector draws the delay uniformly from zero up to the
  smaller of the probe's collection interval and this value, so a
  past-due 30-second probe still starts within 30 seconds whilst an
  hourly probe starts within the configured window.
- Note: The collector realigns the probe's timer after that first
  execution, so the offset also spreads out steady-state collection.

## Metrics Storage Options

The metrics storage option selects where the collector stores the
tables in the `metrics` schema. The option is a top-level key in the
YAML file.

### use_coldfront_for_metrics

The `use_coldfront_for_metrics` option stores each metrics table in
pgEdge ColdFront, as a PostgreSQL view over an Apache Iceberg table on
object storage, instead of as a partitioned PostgreSQL table.

- Type: boolean
- Default: `false`
- Required: No
- Command-line: `-use-coldfront-for-metrics`
- Example: `use_coldfront_for_metrics: true`
- Note: Only the collector reads this option; the server and alerter
  work against either kind of datastore without a setting.
- Note: Choose the mode when you create the datastore; the collector
  refuses to start when the value does not match the existing metrics
  tables.
- Note: The Workbench performs no metrics retention in this mode, and
  the collector ignores every `retention_days` setting.
- Note: The operator must schedule the ColdFront compactor for every
  metrics table in this mode.

The [Storing Metrics in pgEdge ColdFront](coldfront-metrics.md)
document describes the prerequisites, the compactor requirement, and
the behavioural differences of this mode.

## Connection Security Options

The connection security options limit what a stored monitored
connection can make the collector do. The options are nested under the
`connection_security:` key in the YAML file. The collector reads them
at start-up only, so a change takes effect when the collector restarts.

### connection_security.ssl_cert_dir

The `ssl_cert_dir` option names the absolute directory that the SSL
certificate, key and root certificate files of monitored connections
must live in. The collector reads these files itself when the collector
connects to a monitored database, so the directory limits which files a
user who can create a connection can make the collector open.

- Type: string (directory path)
- Default: `""` (unset)
- Required: No
- Command-line: `-connection-ssl-cert-dir`
- Example: `ssl_cert_dir: /etc/pgedge/ai-dba-workbench/certs`
- Note: The option has no environment variable.
- Note: The collector refuses to start when the option holds a relative
  path.

The option has the same rules as the server option of the same name.
Before the collector builds a pool for a connection, the collector
checks each stored SSL file path against the following rules:

- the path is absolute and contains no `..` element or control
  character.
- the path resolves, following symbolic links, to a location inside the
  `ssl_cert_dir` directory.

When the option is unset, the collector refuses every connection that
names an SSL file; a root certificate value of `system`, which selects
the platform trust store, is always allowed. The collector does not
monitor a refused connection, logs the connection ID and the name of
the refused field without the path, and records an error ending in
`invalid connection parameters` against the connection.

Set the option on the collector as well as on the server, and keep the
certificate files at the same paths on the collector host, because both
components read the same stored paths. The directory, every file and
link in it, and every parent directory must be writable only by root
or the account that runs the collector. The collector resolves a path
before the database driver opens the file, so an account that can
replace a symbolic link in the directory, or replace the directory
itself through a writable parent, between the two steps could still
point the driver at a file outside the directory.

In the following example, the collector accepts SSL files in the
`/etc/pgedge/ai-dba-workbench/certs` directory:

```yaml
connection_security:
  ssl_cert_dir: /etc/pgedge/ai-dba-workbench/certs
```

### Connection Address Checks

The collector checks each address that a monitored connection's host
resolves to each time the collector connects, and connects only to the
addresses that pass. The check defeats DNS rebinding, in which a host
name that the server accepted when the connection was saved later
resolves to another address. The collector has none of the server's
`connection_security` host settings, so the collector refuses only the
following fixed set of addresses:

- the unspecified addresses `0.0.0.0/8` and `::`.
- the link-local unicast ranges `169.254.0.0/16` and `fe80::/10`,
  which include the `169.254.169.254` cloud metadata address.
- the IPv4 link-local multicast range `224.0.0.0/24` and every IPv6
  multicast address of link-local scope, such as `ff02::1`.
- the cloud metadata addresses `fd00:ec2::254` and `100.100.100.200`.
- an address in `::/96`, `64:ff9b::/96` or `2002::/16` that embeds one
  of these IPv4 addresses.

The collector allows loopback and private addresses, because a
collector routinely monitors servers on the same host or network. The
collector refuses a link-local target even when the server's
`allow_internal_networks` option is `true`, so the collector cannot
monitor a link-local target that the server accepts. When no resolved
address passes, the connection attempt fails with an error that says
the host resolves only to addresses that connections are not permitted
to.

## Security Options

The collector uses a secret file and AES-256-GCM encryption to protect
stored passwords. The `secret_file` option specifies the path to a file
containing the per-installation secret for password encryption.

- Type: string (file path)
- Default: Searches in order:
    1. The per-user config directory at
       `~/.config/pgedge/ai-dba-collector.secret` on Linux
       (honouring `$XDG_CONFIG_HOME`),
       `~/Library/Application Support/pgedge/ai-dba-collector.secret`
       on macOS, and
       `%AppData%\pgedge\ai-dba-collector.secret` on
       Windows.
    2. `/etc/pgedge/ai-dba-collector.secret` (system-wide).
- Required: Yes (a secret file must exist)
- Example: `secret_file: /etc/pgedge/collector.secret`
- Note: The collector no longer searches the binary directory or the
  current working directory for the secret file.

The collector uses AES-256-GCM encryption to protect stored passwords.
Each password is encrypted with a unique cryptographically random
salt. The collector derives the encryption key from the server secret
using PBKDF2 with SHA256 and 100,000 iterations.

In the following example, the `openssl` command generates a secure
secret:

```bash
openssl rand -base64 32 \
    > /etc/pgedge/ai-dba-collector.secret
chmod 600 /etc/pgedge/ai-dba-collector.secret
```

Keep this file secure with restricted permissions. Loss of the secret
file requires re-entering all monitored connection passwords. Do not
manually encrypt passwords; use the MCP server API to create and
manage connections with passwords.

The credentials stored against a monitored connection belong to a
PostgreSQL role on the monitored server, which is separate from the
datastore role configured above. The
[Monitored Database Privileges](monitored-database-privileges.md)
document describes the grants that role needs, including the
`pg_monitor` predefined role and the per-database `CONNECT` privilege.

## Command-Line Flags

The following table lists all available command-line flags.

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | Auto-detected |
| `-v` | Enable verbose logging | `false` |
| `-pg-host` | PostgreSQL hostname | `localhost` |
| `-pg-hostaddr` | PostgreSQL IP address | none |
| `-pg-database` | Database name | `ai_workbench` |
| `-pg-username` | Database username | `postgres` |
| `-pg-password-file` | Path to password file | none |
| `-pg-port` | Database port | `5432` |
| `-pg-sslmode` | SSL mode | `prefer` |
| `-pg-sslcert` | Client SSL certificate | none |
| `-pg-sslkey` | Client SSL key | none |
| `-pg-sslrootcert` | Root SSL certificate | none |
| `-use-coldfront-for-metrics` | Store metrics tables in pgEdge ColdFront | `false` |
| `-connection-ssl-cert-dir` | Directory that monitored connections' SSL files must be in | none |

In the following example, the command uses flags to override
configuration file settings:

```bash
./ai-dba-collector \
    -config /path/to/config.yaml \
    -pg-host localhost \
    -pg-database ai_workbench \
    -pg-username collector \
    -pg-password-file /path/to/password.txt \
    -pg-port 5432 \
    -pg-sslmode prefer
```

## Per-Server Probe Configuration

The collector supports customizing probe settings for individual
monitored servers through the `probe_configs` database table.

### Configuration Hierarchy

Probe settings use a three-level fallback hierarchy:

1. Connection-specific settings in `probe_configs` where
   `connection_id` matches the monitored connection.
2. Global default settings in `probe_configs` where
   `scope = 'global'`.
3. Hardcoded default values defined in the collector source code.

### Automatic Configuration

When the collector marks a new connection as monitored
(`is_monitored = TRUE`), it creates per-server probe configurations
by copying the global defaults.

### Modifying Probe Settings

Probe settings are managed through direct SQL updates to the
`probe_configs` table.

In the following example, the `UPDATE` statement changes the
collection interval for a specific server:

```sql
UPDATE probe_configs
SET collection_interval_seconds = 30
WHERE name = 'pg_stat_activity'
  AND connection_id = 1;
```

In the following example, the `UPDATE` statement disables a probe for
a specific server:

```sql
UPDATE probe_configs
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements'
  AND connection_id = 2;
```

In the following example, the `UPDATE` statement changes the global
retention period:

```sql
UPDATE probe_configs
SET retention_days = 60
WHERE name = 'pg_stat_database'
  AND scope = 'global';
```

### Automatic Reload

The collector reloads probe configurations from the database every
5 minutes; changes take effect without requiring a restart.

Collection interval and enabled status changes take effect within
5 minutes. Retention changes take effect on the next garbage
collection run (within 24 hours). A collector that stores metrics in
pgEdge ColdFront runs no garbage collection and ignores retention
changes.

### Viewing Current Configuration

In the following example, the query displays the probe configuration
for a specific connection:

```sql
SELECT pc.name,
       pc.collection_interval_seconds,
       pc.retention_days,
       pc.is_enabled
FROM probe_configs pc
WHERE pc.connection_id = 1
ORDER BY pc.name;
```

## Configuration Validation

The collector validates configuration at startup and requires the
following fields to be set:

- `datastore.host` must contain a hostname or IP.
- `datastore.database` must contain a database name.
- `datastore.username` must contain a username.
- A secret file must exist in one of the search paths.

The collector validates the following ranges:

- `datastore.port` must be between 1 and 65535.
- Pool `max_connections` values must be greater than 0.
- Pool `max_idle_seconds` values must be 0 or greater.
- Pool `max_wait_seconds` values must be greater than 0.
- `scheduler.max_concurrent_probes` must be greater than 0.
- `scheduler.max_concurrent_probes_per_connection` must be greater
  than 0.
- `scheduler.startup_jitter_seconds` must be 0 or greater.
- `connection_security.ssl_cert_dir` must be an absolute path when
  set.

## Tuning Guidelines

The following guidelines help select appropriate values for pool and
timeout settings.

### Datastore Pool Size

Choose `datastore_max_connections` based on the number of probes and
monitored servers. Use the following formula as a starting point:

`(number of probes * concurrent servers) / 2`

For example, 24 probes with 10 monitored servers suggests
approximately 120 connections.

### Monitored Pool Size

Start with a `max_connections_per_server` value of 3 and increase the
value if timeout errors occur. Higher network latency may require more
connections.

### Probe Concurrency and Startup Stagger

Lower `max_concurrent_probes` when the collector runs under a tight
memory limit, because each probe execution holds its result set in
memory for the duration of the run. The three scheduler settings work
together: the concurrency cap bounds the peak,
`max_concurrent_probes_per_connection` bounds what one monitored
connection may take of that peak, and `startup_jitter_seconds` spreads
the restart burst that would otherwise reach the cap immediately and
leave the remaining probes queued behind it. A deployment limited to a
few hundred megabytes of memory typically pairs a
`max_concurrent_probes` value of 4 with a `startup_jitter_seconds`
value of 60 or more.

A memory budget alone does not size `max_concurrent_probes`, because a
probe holds its slot until the execution finishes or reaches
`pool.monitored_max_wait_seconds`. A monitored server that accepts
connections but answers slowly therefore drives every one of its
probes to that timeout, and the slots it demands are the sum of
`monitored_max_wait_seconds / interval` over its enabled probes. With
the default 120-second timeout and the 34 seeded probes, whose
intervals run from 30 seconds to 3600 seconds, one such server demands
roughly 17.7 slots against a default cap of 8. Set
`max_concurrent_probes` above that total for the slowest servers you
expect to monitor, and keep `max_concurrent_probes_per_connection` at
a small fraction of the cap, so that a single slow server cannot
consume the whole budget and stretch the 30-second probes of healthy
connections into minutes.

The cap interacts with `max_connections_per_server` as well, since a
probe holds a monitored connection while it runs. Setting
`max_concurrent_probes` far above the total of
`max_connections_per_server` across monitored servers only moves the
queue from the scheduler to the connection pool.

### Idle Timeout

The default idle timeout of 300 seconds (5 minutes) works well for
most environments. Use longer values when connections are expensive to
create.

### Wait Timeout

The default wait timeout of 60 seconds works for most environments.
Use longer values for burst load patterns.

## Configuration Examples

The following examples show minimal, production, and development
configurations.

### Minimal Configuration

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: collector
  password_file: /etc/ai-workbench/password.txt
```

### Production Configuration

```yaml
datastore:
  host: db.internal.example.com
  database: ai_workbench_prod
  username: ai_workbench
  password_file: /var/secrets/password.txt
  port: 5432
  sslmode: verify-full
  sslcert: /etc/ai-workbench/certs/client.pem
  sslkey: /etc/ai-workbench/certs/client-key.pem
  sslrootcert: /etc/ai-workbench/certs/ca.pem

pool:
  datastore_max_connections: 100
  datastore_max_idle_seconds: 300
  datastore_max_wait_seconds: 60
  max_connections_per_server: 10
  monitored_max_idle_seconds: 300
  monitored_max_wait_seconds: 120

scheduler:
  max_concurrent_probes: 8
  max_concurrent_probes_per_connection: 2
  startup_jitter_seconds: 60

connection_security:
  ssl_cert_dir: /etc/pgedge/ai-dba-workbench/certs

secret_file: /var/secrets/collector.secret
```

### Development Configuration

```yaml
datastore:
  host: localhost
  database: ai_workbench_dev
  username: postgres
  port: 5432
  sslmode: disable

pool:
  datastore_max_connections: 10
  max_connections_per_server: 3

secret_file: ./ai-dba-collector.secret
```

## Troubleshooting

The following sections describe common error messages and corrective
steps.

### "Configuration file not found"

This error indicates a problem locating the configuration file.

- Check the file path for typos and correct any errors found.
- Use absolute paths instead of relative paths.
- Verify that file permissions allow the collector to read the file.

### "Failed to parse configuration"

This error indicates a YAML syntax problem in the configuration file.

- Check for YAML syntax errors in indentation and correct them.
- Ensure nested keys are properly indented.
- Validate the YAML syntax using an online validator.

### "Too many connections"

This error indicates that the pool size exceeds the database limit.

- Reduce `datastore_max_connections` to a lower value.
- Reduce `max_connections_per_server` to a lower value.
- Check the PostgreSQL `max_connections` setting on the target
  servers.

### "Connection timeout"

This error indicates that connections are not available within the
configured wait period.

- Increase the `*_max_wait_seconds` values.
- Increase the pool sizes for the affected component.
- Check network connectivity to the database server.
- Verify that the database server is responsive.

### ColdFront Metrics Storage Errors

The collector checks the metrics storage mode before running any
migration and refuses to start when the datastore does not match the
`use_coldfront_for_metrics` setting. The collector logs each startup
error after the prefix `Failed to initialize datastore: failed to
initialize schema: `, so the message text appears within a longer log
line. The following table describes each startup error and the
corrective step:

| Error Message Contains | Cause | Fix |
|------------------------|-------|-----|
| `use_coldfront_for_metrics is enabled but the coldfront extension is not installed in the datastore` | The option is enabled, but the datastore has no `coldfront.create_iceberg_table` function, so the `coldfront` extension is not installed. | Install ColdFront 1.0.0-beta3 or later and run `CREATE EXTENSION coldfront`, or disable the option. |
| `use_coldfront_for_metrics is enabled but the installed coldfront extension is too old` | The installed ColdFront build lacks `coldfront._partition_clause(jsonb,text[])` or `coldfront.ensure_attached()`; the message names the missing function. | Install ColdFront 1.0.0-beta3 or later, which provides both the `coldfront._partition_clause(jsonb,text[])` and `coldfront.ensure_attached()` functions; a development build must include pgEdge ColdFront pull request #92. |
| `use_coldfront_for_metrics is enabled but the datastore already stores metrics in PostgreSQL tables` | The datastore was created in standard mode. | Use a new datastore for ColdFront storage, or disable the option. |
| `use_coldfront_for_metrics is disabled but the datastore stores metrics in ColdFront views` | The datastore was created in ColdFront mode. | Enable the option, or use a new datastore for standard storage. |
| `failed to create the Iceberg namespace "metrics" for the ColdFront metrics tables` | The collector could not attach the Lakekeeper catalog or create the `metrics` namespace in the warehouse. | Check that the warehouse named in `coldfront.warehouse` is reachable from the datastore and writable, and that the ColdFront catalog settings are correct. |
| `the workbench_coldfront schema is owned by role` | Another role owns the helper schema that the collector writes metrics through. | Drop the `workbench_coldfront` schema, or make the collector's datastore role its owner. |
| `the workbench_coldfront schema holds functions owned by other roles` | Another role owns a function in the helper schema; the message lists each such function. | Drop the listed functions before starting the collector. |
| `failed to create the workbench_coldfront schema` | The collector's datastore role cannot create a schema in the datastore database. | Grant the role the `CREATE` privilege on the datastore database. |

The two mismatch errors name up to three of the affected metrics
relations. The
[Storing Metrics in pgEdge ColdFront](coldfront-metrics.md) document
describes the prerequisites of ColdFront mode.

ColdFront reports `Namespace with name 'metrics' does not exist` when
a metrics table is created before its Iceberg namespace exists on a
Lakekeeper catalog. The collector creates the namespace before the
first migration, so this error means that the namespace step did not
take effect. Check that the warehouse named in `coldfront.warehouse`
is reachable and writable, and then restart the collector.

### ColdFront Object Storage Errors

On a running ColdFront datastore, object-store input and output errors,
slow metrics reads, or PostgreSQL backends that pg_duckdb aborts,
followed by a crash restart of the datastore server, usually mean that
snapshots and small data files have accumulated in the metrics tables.
Take the following steps:

- Schedule the ColdFront compactor with the `--expire-snapshots`
  option and a lowered `--expire-older-than` value for every metrics
  table, as the
  [Scheduling the ColdFront Compactor](coldfront-metrics.md#scheduling-the-coldfront-compactor)
  section describes.
- Reduce the number of monitored connections or the probe frequency
  when the snapshot count grows faster than the compactor expires
  snapshots.
- Check that the object storage is healthy, reachable from the
  datastore, and not rejecting or throttling requests.
- Lengthen the collection interval of high-frequency probes when the
  compactor cannot keep up.

## Security Best Practices

Follow these practices to protect collector credentials and
connections.

### Protecting Secrets

Set restrictive file permissions on configuration files and password
files.

```bash
chmod 600 /etc/pgedge/ai-dba-collector.yaml
chmod 600 /etc/ai-workbench/password.txt
```

Use dedicated password files rather than inline passwords. Generate
strong random secrets for the server secret. Never commit
configuration files with real secrets to version control.

### SSL/TLS Configuration

For production deployments, always use SSL with certificate
verification:

```yaml
datastore:
  sslmode: verify-full
  sslcert: /path/to/client-cert.pem
  sslkey: /path/to/client-key.pem
  sslrootcert: /path/to/ca-cert.pem
```
