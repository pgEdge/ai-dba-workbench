# Configuring the Collector

The collector supports configuration through a YAML file and command-line
flags. The collector loads configuration in the following order; later
sources override earlier ones:

1. Built-in defaults.
2. Configuration file.
3. Command-line flags.

The collector searches for a Collector configuration file in the following
locations in the following order:

1. The path specified via the `-config` flag.
2. The per-user `config` directory at `~/.config/pgedge/ai-dba-collector.yaml`
   on Linux (honouring `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-collector.yaml` on macOS, and
   `%AppData%\pgedge\ai-dba-collector.yaml` on Windows.
3. `/etc/pgedge/ai-dba-collector.yaml` (system-wide).

If `-config` is set and the file is missing, the collector exits with an
error. If `-config` is not set and none of the default locations contain
a configuration file, the collector uses built-in defaults silently. The
collector no longer searches the binary directory or the current working
directory.

The collector validates configuration at startup; the following fields
must be set:

- `datastore.host` must contain a hostname or IP.
- `datastore.database` must contain a database name.
- `datastore.username` must contain a username.
- A secret file must exist in one of the search paths.

The collector also validates the following ranges:

- `datastore.port` must be between 1 and 65535.
- Pool `max_connections` values must be greater than 0.
- Pool `max_idle_seconds` values must be 0 or greater.
- Pool `max_wait_seconds` values must be greater than 0.


## Reloading or Restarting the Collector

The collector has no signal-based reload for its configuration file, unlike the server and alerter. Changing any property or command-line flag — requires a full restart to take effect. The collector's shutdown handler only responds to SIGINT and SIGTERM, and both signals stop the process rather than reapplying configuration.

If you have configured systemd, you can restart the service through systemctl to apply configuration changes:

```bash
sudo systemctl restart pgedge-ai-dba-collector
```

Without systemd, stop and relaunch the process manually with the same configuration flag used originally:

```bash
pkill -f ai-dba-collector opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml &
```

Per-server probe configuration is the one exception: the collector stores it in the datastore rather than the YAML file, polls for changes every 5 minutes, and applies them automatically without a restart. 


## Security Best Practices

The collector uses a secret file and AES-256-GCM encryption to protect stored
passwords; the secret file is required. The `secret_file` option specifies the
path to a file containing the per-installation secret for password encryption.

The Collector searches the following locations in order:
    1. The per-user config directory at
       `~/.config/pgedge/ai-dba-collector.secret` on Linux (honouring
       `$XDG_CONFIG_HOME`),
       `~/Library/Application Support/pgedge/ai-dba-collector.secret` on macOS,
       and `%AppData%\pgedge\ai-dba-collector.secret` on Windows.
    2. `/etc/pgedge/ai-dba-collector.secret` (system-wide).

!!! note

    The collector no longer searches the binary directory or the current
    working directory for the secret file.

The collector uses AES-256-GCM encryption to protect stored passwords and
encrypts each password with a unique cryptographically random salt. It
derives the encryption key from the server secret using PBKDF2 with SHA256
and 100,000 iterations.

In the following example, the `openssl` command generates a secure secret:

```bash
openssl rand -base64 32 \
    > /etc/pgedge/ai-dba-collector.secret
chmod 600 /etc/pgedge/ai-dba-collector.secret
```

Keep this file secure with restricted permissions. Loss of the secret file
requires re-entering all monitored connection passwords. Do not manually
encrypt passwords; use the MCP server API to create and manage connections with
passwords.

The following commands set restrictive file permissions on configuration files
and password files.

```bash
chmod 600 /etc/pgedge/ai-dba-collector.yaml
chmod 600 /etc/ai-workbench/password.txt
```

It's safer to use dedicated password files rather than inline passwords.
Generate strong random secrets for the server secret. Never commit
configuration files with real secrets to version control.

For additional security in a production deployment, always use SSL with
certificate verification:

```yaml
datastore:
  sslmode: verify-full
  sslcert: /path/to/client-cert.pem
  sslkey: /path/to/client-key.pem
  sslrootcert: /path/to/ca-cert.pem
```

## Specifying Collector Properties in a Configuration File

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
```

A sample configuration file is available at
[ai-dba-collector.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-collector.yaml)
in the project repository.

### Specifying datastore Connection Options in the Configuration File

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

### Specifying Connection Pool Options

All connection pool options are nested under the `pool:` key in the YAML file.
Pool settings support configuration only through the configuration file;
command-line flags are not available for pool options.

| Option | Type | Default | Min | Example | Description | Notes |
|--------|------|---------|-----|---------|-------------|-------|
| `datastore_max_connections` | integer | `25` | 1 | `datastore_max_connections: 50` | Maximum number of concurrent connections to the datastore. | Increase for more concurrent probe storage. |
| `datastore_max_idle_seconds` | integer | `300` (5 minutes) | 0 (disables idle cleanup) | `datastore_max_idle_seconds: 600` | Maximum idle time in seconds for datastore connections. | |
| `datastore_max_wait_seconds` | integer | `60` | 1 | `datastore_max_wait_seconds: 120` | Maximum wait time in seconds for an available datastore connection. | Probe storage fails if the timeout expires. |
| `max_connections_per_server` | integer | `3` | 1 | `max_connections_per_server: 5` | Maximum concurrent connections per monitored database server. | This limit applies per server, not total. |
| `monitored_max_idle_seconds` | integer | `300` (5 minutes) | 0 (disables idle cleanup) | `monitored_max_idle_seconds: 600` | Maximum idle time in seconds for monitored connections. | |
| `monitored_max_wait_seconds` | integer | `60` | 1 | `monitored_max_wait_seconds: 120` | Maximum wait time in seconds for an available monitored connection. | Probe execution fails if the timeout expires. |

### Guidelines for Tuning the Collector

The following guidelines help select appropriate values for pool and timeout
settings.

#### Datastore Pool Size

Choose `datastore_max_connections` based on the number of probes and monitored
servers. Use the following formula as a starting point:

`(number of probes * concurrent servers) / 2`

For example, 24 probes with 10 monitored servers suggests approximately 120
connections.

#### Monitored Pool Size

Start with a `max_connections_per_server` value of 3 and increase the value if
timeout errors occur. Higher network latency may require more connections.

#### Idle Timeout

The default idle timeout of 300 seconds (5 minutes) works well for most
environments. Use longer values when connections are expensive to create.

#### Wait Timeout

The default wait timeout of 60 seconds works for most environments. Use longer
values for burst load patterns.


## Using Command-Line Flags to Specify Configuration Properties

You can use a command-line flag to override built-in defaults and configuration
file settings. The following table lists the available command-line flags.

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | Auto-detected | Path to configuration file |
| `-v` | `false` | Enable verbose logging |
| `-pg-host` | `localhost` | PostgreSQL hostname |
| `-pg-hostaddr` | none | PostgreSQL IP address |
| `-pg-database` | `ai_workbench` | Database name |
| `-pg-username` | `postgres` | Database username |
| `-pg-password-file` | none | Path to password file |
| `-pg-port` | `5432` | Database port |
| `-pg-sslmode` | `prefer` | SSL mode |
| `-pg-sslcert` | none | Client SSL certificate |
| `-pg-sslkey` | none | Client SSL key |
| `-pg-sslrootcert` | none | Root SSL certificate |

In the following example, the command uses flags to override configuration file
settings:

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


## Per-Server Collector Probe Configuration

The collector supports customizing probe settings for individual monitored
servers through the `probe_configs` database table. When the collector marks
a new connection as monitored (`is_monitored = TRUE`), it creates per-server
probe configurations by copying the global defaults.

Probe settings use a three-level fallback hierarchy:

1. Connection-specific settings in `probe_configs` where `connection_id`
   matches the monitored connection.
2. Global default settings in `probe_configs` where `connection_id IS NULL`.
3. Hardcoded default values defined in the collector source code.

The following command displays the current probe configuration for a specific
connection:

```sql
SELECT pc.name,
       pc.collection_interval_seconds,
       pc.retention_days,
       pc.is_enabled
FROM probe_configs pc
WHERE pc.connection_id = 1
ORDER BY pc.name;
```


### Modifying Probe Settings

You manage probe settings through direct SQL updates to the `probe_configs`
table.

In the following example, the `UPDATE` statement changes the collection
interval for a specific server:

```sql
UPDATE probe_configs
SET collection_interval_seconds = 30
WHERE name = 'pg_stat_activity'
  AND connection_id = 1;
```

In the following example, the `UPDATE` statement disables a probe for a
specific server:

```sql
UPDATE probe_configs
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements'
  AND connection_id = 2;
```

In the following example, the `UPDATE` statement changes the global retention
period:

```sql
UPDATE probe_configs
SET retention_days = 60
WHERE name = 'pg_stat_database'
  AND connection_id IS NULL;
```


## Configuration Examples

The following examples show minimal, production, and development
configurations for the Collector.

### Minimal Configuration

This example shows the minimum settings required by the Collector.

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: collector
  password_file: /etc/ai-workbench/password.txt
```

### Production Configuration

This example shows a production configuration with SSL verification
and a connection pool.

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

This example shows a local development configuration with SSL
disabled and a small connection pool.

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

