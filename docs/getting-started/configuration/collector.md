# Collector Configuration

The collector supports configuration through a YAML
file and command-line flags. Command-line flags take
precedence over configuration file settings.

## Configuration Precedence

The collector loads configuration in the following
order; later sources override earlier ones:

1. Built-in defaults.
2. Configuration file.
3. Command-line flags.

## Configuration File

### File Location

The collector searches for its configuration file in
the following order:

1. The path specified via the `-config` flag.
2. The per-user config directory at
   `~/.config/pgedge/ai-dba-collector.yaml` on Linux
   (honouring `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-collector.yaml`
   on macOS, and `%AppData%\pgedge\ai-dba-collector.yaml`
   on Windows.
3. `/etc/pgedge/ai-dba-collector.yaml` (system-wide).

If `-config` is set and the file is missing, the
collector exits with an error. If `-config` is not set
and none of the default locations contain a
configuration file, the collector uses built-in
defaults silently. The collector no longer searches the
binary directory or the current working directory.

### File Format

The configuration file uses YAML format with nested
sections:

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
```

### Sample Configuration

A complete example configuration file is available at
[ai-dba-collector.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-collector.yaml)
in the project repository.

## Datastore Connection Options

All datastore options are nested under the
`datastore:` key in the YAML file.

### datastore.host

The `host` option specifies the PostgreSQL server
hostname or IP address for the datastore.

- Type: string
- Default: `localhost`
- Required: No
- Command-line: `-pg-host`
- Example: `host: db.example.com`

### datastore.hostaddr

The `hostaddr` option specifies the PostgreSQL server
IP address and bypasses DNS lookup.

- Type: string
- Default: none
- Required: No
- Command-line: `-pg-hostaddr`
- Example: `hostaddr: 192.168.1.100`
- Note: The collector uses this address instead of
  `host` when both are set.

### datastore.database

The `database` option specifies the database name for
the datastore.

- Type: string
- Default: `ai_workbench`
- Required: Yes
- Command-line: `-pg-database`
- Example: `database: metrics`

### datastore.username

The `username` option specifies the username for the
datastore connection.

- Type: string
- Default: `postgres`
- Required: Yes
- Command-line: `-pg-username`
- Example: `username: ai_workbench`

### datastore.password_file

The `password_file` option specifies the path to a
file containing the datastore password.

- Type: string (file path)
- Default: none
- Required: No (but strongly recommended)
- Command-line: `-pg-password-file`
- Example: `password_file: /etc/ai-workbench/pw.txt`
- File format: Plain text with the password only.

In the following example, the commands create a
password file with secure permissions:

```bash
echo "my-secure-password" \
    > /etc/ai-workbench/password.txt
chmod 600 /etc/ai-workbench/password.txt
```

### datastore.port

The `port` option specifies the PostgreSQL server port
number.

- Type: integer
- Default: `5432`
- Required: No
- Range: 1-65535
- Command-line: `-pg-port`
- Example: `port: 5433`

### datastore.sslmode

The `sslmode` option specifies the SSL/TLS mode for
the datastore connection.

- Type: string
- Default: `prefer`
- Required: No
- Command-line: `-pg-sslmode`
- Example: `sslmode: require`

The following SSL modes are supported:

- `disable` disables SSL encryption.
- `allow` attempts a non-SSL connection first and
  falls back to SSL.
- `prefer` attempts an SSL connection first and
  falls back to non-SSL.
- `require` requires SSL but does not verify the
  server certificate.
- `verify-ca` requires SSL and verifies the server
  certificate against the CA.
- `verify-full` requires SSL and verifies the
  certificate and hostname.

### datastore.sslcert

The `sslcert` option specifies the path to the client
SSL certificate file.

- Type: string (file path)
- Default: none
- Required: No
- Command-line: `-pg-sslcert`
- Example: `sslcert: /etc/ai-workbench/client.pem`
- Note: Use with `verify-ca` or `verify-full` modes.

### datastore.sslkey

The `sslkey` option specifies the path to the client
SSL private key file.

- Type: string (file path)
- Default: none
- Required: No (required if `sslcert` is set)
- Command-line: `-pg-sslkey`
- Example: `sslkey: /etc/ai-workbench/client-key.pem`

### datastore.sslrootcert

The `sslrootcert` option specifies the path to the
root CA certificate file.

- Type: string (file path)
- Default: none
- Required: No
- Command-line: `-pg-sslrootcert`
- Example: `sslrootcert: /etc/ai-workbench/ca.pem`
- Note: The collector uses this certificate to
  verify the server.

## Connection Pool Options

All connection pool options are nested under the
`pool:` key in the YAML file. Pool settings can only
be configured in the configuration file; command-line
flags are not available for pool options.

### pool.datastore_max_connections

The `datastore_max_connections` option specifies the
maximum number of concurrent connections to the
datastore.

- Type: integer
- Default: `25`
- Min: 1
- Example: `datastore_max_connections: 50`
- Tuning: Increase for more concurrent probe storage.

### pool.datastore_max_idle_seconds

The `datastore_max_idle_seconds` option specifies the
maximum idle time in seconds for datastore
connections.

- Type: integer
- Default: `300` (5 minutes)
- Min: 0 (disables idle cleanup)
- Example: `datastore_max_idle_seconds: 600`

### pool.datastore_max_wait_seconds

The `datastore_max_wait_seconds` option specifies the
maximum wait time in seconds for an available
datastore connection.

- Type: integer
- Default: `60`
- Min: 1
- Example: `datastore_max_wait_seconds: 120`
- Tuning: Probe storage fails if the timeout expires.

### pool.max_connections_per_server

The `max_connections_per_server` option specifies the
maximum concurrent connections per monitored database
server.

- Type: integer
- Default: `3`
- Min: 1
- Example: `max_connections_per_server: 5`
- Note: This limit applies per server, not total.

### pool.monitored_max_idle_seconds

The `monitored_max_idle_seconds` option specifies the
maximum idle time in seconds for monitored
connections.

- Type: integer
- Default: `300` (5 minutes)
- Min: 0 (disables idle cleanup)
- Example: `monitored_max_idle_seconds: 600`

### pool.monitored_max_wait_seconds

The `monitored_max_wait_seconds` option specifies the
maximum wait time in seconds for an available
monitored connection.

- Type: integer
- Default: `60`
- Min: 1
- Example: `monitored_max_wait_seconds: 120`
- Tuning: Probe execution fails if the timeout
  expires.

## Security Options

### secret_file

The `secret_file` option specifies the path to a file
containing the per-installation secret for password
encryption.

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
- Note: The collector no longer searches the binary
  directory or the current working directory for the
  secret file.

The collector uses AES-256-GCM encryption to protect
stored passwords. Each password is encrypted with a
unique cryptographically random salt. The encryption
key is derived from the server secret using PBKDF2
with SHA256 and 100,000 iterations.

In the following example, the `openssl` command
generates a secure secret:

```bash
openssl rand -base64 32 \
    > /etc/pgedge/ai-dba-collector.secret
chmod 600 /etc/pgedge/ai-dba-collector.secret
```

Keep this file secure with restricted permissions. If
you lose the secret file, you must re-enter all
monitored connection passwords.

Users should not manually encrypt passwords. Use the
MCP server API to create and manage connections with
passwords.

## Command-Line Flags

The following table lists all available command-line
flags.

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

In the following example, the command uses flags to
override configuration file settings:

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

The collector supports customizing probe settings for
individual monitored servers through the
`probe_configs` database table.

### Configuration Hierarchy

Probe settings use a three-level fallback hierarchy:

1. Connection-specific settings in `probe_configs`
   where `connection_id` matches the monitored
   connection.
2. Global default settings in `probe_configs` where
   `connection_id IS NULL`.
3. Hardcoded default values defined in the collector
   source code.

### Automatic Configuration

When a new connection is marked as monitored
(`is_monitored = TRUE`), the collector creates
per-server probe configurations by copying the global
defaults.

### Modifying Probe Settings

Probe settings are managed through direct SQL updates
to the `probe_configs` table.

In the following example, the `UPDATE` statement
changes the collection interval for a specific server:

```sql
UPDATE probe_configs
SET collection_interval_seconds = 30
WHERE name = 'pg_stat_activity'
  AND connection_id = 1;
```

In the following example, the `UPDATE` statement
disables a probe for a specific server:

```sql
UPDATE probe_configs
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements'
  AND connection_id = 2;
```

In the following example, the `UPDATE` statement
changes the global retention period:

```sql
UPDATE probe_configs
SET retention_days = 60
WHERE name = 'pg_stat_database'
  AND connection_id IS NULL;
```

### Automatic Reload

The collector reloads probe configurations from the
database every 5 minutes. Changes take effect without
requiring a restart.

Collection interval and enabled status changes take
effect within 5 minutes. Retention changes take effect
on the next garbage collection run (within 24 hours).

### Viewing Current Configuration

In the following example, the query displays the
probe configuration for a specific connection:

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

The collector validates configuration at startup. The
following fields must be set:

- `datastore.host` must contain a hostname or IP.
- `datastore.database` must contain a database name.
- `datastore.username` must contain a username.
- A secret file must exist in one of the search paths.

The collector validates the following ranges:

- `datastore.port` must be between 1 and 65535.
- Pool `max_connections` values must be greater
  than 0.
- Pool `max_idle_seconds` values must be 0 or
  greater.
- Pool `max_wait_seconds` values must be greater
  than 0.

## Tuning Guidelines

### Datastore Pool Size

Choose `datastore_max_connections` based on the number
of probes and monitored servers. Use the following
formula as a starting point:

`(number of probes * concurrent servers) / 2`

For example, 24 probes with 10 monitored servers
suggests approximately 120 connections.

### Monitored Pool Size

Start with a `max_connections_per_server` value of 3
and increase if you see timeout errors. Higher network
latency may require more connections.

### Idle Timeout

The default idle timeout of 300 seconds (5 minutes)
works well for most environments. Use longer values
when connections are expensive to create.

### Wait Timeout

The default wait timeout of 60 seconds works for most
environments. Use longer values for burst load
patterns.

## Configuration Examples

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

### "Configuration file not found"

- Check the file path for typos.
- Use absolute paths instead of relative paths.
- Verify that file permissions allow reading.

### "Failed to parse configuration"

- Check for YAML syntax errors in indentation.
- Ensure nested keys are properly indented.
- Validate the YAML syntax using an online validator.

### "Too many connections"

- Reduce `datastore_max_connections` to a lower value.
- Reduce `max_connections_per_server` to a lower
  value.
- Check the PostgreSQL `max_connections` setting on
  the target servers.

### "Connection timeout"

- Increase the `*_max_wait_seconds` values.
- Increase the pool sizes for the affected component.
- Check network connectivity to the database server.
- Verify that the database server is responsive.

## Security Best Practices

### Protecting Secrets

Set restrictive file permissions on configuration
files and password files:

```bash
chmod 600 /etc/pgedge/ai-dba-collector.yaml
chmod 600 /etc/ai-workbench/password.txt
```

Use dedicated password files rather than inline
passwords. Generate strong random secrets for the
server secret. Never commit configuration files with
real secrets to version control.

### SSL/TLS Configuration

For production deployments, always use SSL with
certificate verification:

```yaml
datastore:
  sslmode: verify-full
  sslcert: /path/to/client-cert.pem
  sslkey: /path/to/client-key.pem
  sslrootcert: /path/to/ca-cert.pem
```
