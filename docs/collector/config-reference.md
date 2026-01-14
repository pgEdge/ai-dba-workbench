# Configuration Reference

This document provides a complete reference for all Collector configuration
options.

## Configuration File Format

The Collector uses YAML format for its configuration file.

```yaml
# Comments start with #
datastore:
  host: localhost
  port: 5432
pool:
  datastore_max_connections: 25
```

## YAML Syntax Rules

The configuration file follows standard YAML syntax rules:

- Lines starting with `#` are comments.
- Nested values use indentation (two spaces recommended).
- String values can be quoted or unquoted.
- Empty lines are ignored.

## Datastore Connection Options

All datastore options are nested under the `datastore:` key in the YAML file.

### datastore.host

The `host` option specifies the PostgreSQL server hostname or IP address
for the datastore.

- **Type**: string
- **Default**: `localhost`
- **Required**: No
- **Command-line**: `-pg-host`
- **Example**: `host: db.example.com`

### datastore.hostaddr

The `hostaddr` option specifies the PostgreSQL server IP address and
bypasses DNS lookup.

- **Type**: string
- **Default**: none
- **Required**: No
- **Command-line**: `-pg-hostaddr`
- **Example**: `hostaddr: 192.168.1.100`
- **Note**: If set, the collector uses this address instead of `host`.

### datastore.database

The `database` option specifies the database name for the Collector's
datastore.

- **Type**: string
- **Default**: `ai_workbench`
- **Required**: Yes
- **Command-line**: `-pg-database`
- **Example**: `database: metrics`

### datastore.username

The `username` option specifies the username for the datastore connection.

- **Type**: string
- **Default**: `postgres`
- **Required**: Yes
- **Command-line**: `-pg-username`
- **Example**: `username: collector`

### datastore.password_file

The `password_file` option specifies the path to a file containing the
datastore password.

- **Type**: string (file path)
- **Default**: none
- **Required**: No (but strongly recommended)
- **Command-line**: `-pg-password-file`
- **Example**: `password_file: /etc/ai-workbench/password.txt`
- **File format**: Plain text, single line, no trailing newline

### datastore.port

The `port` option specifies the PostgreSQL server port number.

- **Type**: integer
- **Default**: `5432`
- **Required**: No
- **Range**: 1-65535
- **Command-line**: `-pg-port`
- **Example**: `port: 5433`

### datastore.sslmode

The `sslmode` option specifies the SSL/TLS mode for the datastore
connection.

- **Type**: string
- **Default**: `prefer`
- **Required**: No
- **Command-line**: `-pg-sslmode`
- **Options**: `disable`, `allow`, `prefer`, `require`, `verify-ca`,
  `verify-full`
- **Example**: `sslmode: require`

The following SSL modes are supported:

- `disable` - No SSL encryption.
- `allow` - Try non-SSL first, then SSL.
- `prefer` - Try SSL first, then non-SSL (default).
- `require` - Require SSL; do not verify certificate.
- `verify-ca` - Require SSL; verify certificate against CA.
- `verify-full` - Require SSL; verify certificate and hostname.

### datastore.sslcert

The `sslcert` option specifies the path to the client SSL certificate file.

- **Type**: string (file path)
- **Default**: none
- **Required**: No
- **Command-line**: `-pg-sslcert`
- **Example**: `sslcert: /etc/ai-workbench/client-cert.pem`
- **Note**: Use this option with `verify-ca` or `verify-full` modes.

### datastore.sslkey

The `sslkey` option specifies the path to the client SSL private key file.

- **Type**: string (file path)
- **Default**: none
- **Required**: No (required if `sslcert` is set)
- **Command-line**: `-pg-sslkey`
- **Example**: `sslkey: /etc/ai-workbench/client-key.pem`

### datastore.sslrootcert

The `sslrootcert` option specifies the path to the root CA certificate file.

- **Type**: string (file path)
- **Default**: none
- **Required**: No
- **Command-line**: `-pg-sslrootcert`
- **Example**: `sslrootcert: /etc/ai-workbench/ca-cert.pem`
- **Note**: The collector uses this certificate to verify the server.

## Connection Pool Options

All connection pool options are nested under the `pool:` key in the YAML file.

### pool.datastore_max_connections

The `datastore_max_connections` option specifies the maximum number of
concurrent connections to the datastore.

- **Type**: integer
- **Default**: `25`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `datastore_max_connections: 50`
- **Tuning**: Increase this value for more concurrent probe storage
  operations.

### pool.datastore_max_idle_seconds

The `datastore_max_idle_seconds` option specifies the maximum idle time
in seconds for datastore connections.

- **Type**: integer
- **Default**: `300` (5 minutes)
- **Required**: No
- **Min**: 0 (0 = no timeout)
- **Command-line**: Not available
- **Example**: `datastore_max_idle_seconds: 600`
- **Tuning**: Use longer values for connection warmth; use shorter values
  when resources are limited.

### pool.datastore_max_wait_seconds

The `datastore_max_wait_seconds` option specifies the maximum wait time
in seconds for an available datastore connection.

- **Type**: integer
- **Default**: `60`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `datastore_max_wait_seconds: 120`
- **Tuning**: Increase this value if you see storage timeout errors.

### pool.monitored_max_connections

The `monitored_max_connections` option specifies the maximum concurrent
connections per monitored database server.

- **Type**: integer
- **Default**: `5`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `monitored_max_connections: 10`
- **Note**: This limit applies per server, not total.

### pool.monitored_max_idle_seconds

The `monitored_max_idle_seconds` option specifies the maximum idle time
in seconds for monitored connections.

- **Type**: integer
- **Default**: `300` (5 minutes)
- **Required**: No
- **Min**: 0 (0 = no timeout)
- **Command-line**: Not available
- **Example**: `monitored_max_idle_seconds: 600`

### pool.monitored_max_wait_seconds

The `monitored_max_wait_seconds` option specifies the maximum wait time
in seconds for an available monitored connection.

- **Type**: integer
- **Default**: `60`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `monitored_max_wait_seconds: 120`
- **Tuning**: Increase this value if you see probe execution timeout
  errors.

## Security Options

### secret_file

The `secret_file` option specifies the path to a file containing the
per-installation secret for password encryption.

- **Type**: string (file path)
- **Default**: Searches in order:
    1. `/etc/pgedge/ai-dba-collector.secret`
    2. `<binary-directory>/ai-dba-collector.secret`
    3. `./ai-dba-collector.secret`
- **Required**: Yes (a secret file must exist in one of the search paths)
- **Command-line**: Not available
- **Example**: `secret_file: /etc/pgedge/ai-dba-collector.secret`

The collector uses this secret to encrypt and decrypt passwords for
monitored database connections. Keep this file secure with restricted
permissions (chmod 600). If you lose the secret file, you must re-enter
all monitored connection passwords.

In the following example, the `openssl` command generates a secure secret:

```bash
openssl rand -base64 32 > /etc/pgedge/ai-dba-collector.secret
chmod 600 /etc/pgedge/ai-dba-collector.secret
```

The collector uses PBKDF2 with SHA256 and 100,000 iterations to derive
encryption keys from the secret. This provides strong protection against
brute-force attacks.

## Command-Line Flags

The following table lists all available command-line flags.

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | See default search paths |
| `-v` | Enable verbose logging | `false` |
| `-pg-host` | PostgreSQL server hostname | `localhost` |
| `-pg-hostaddr` | PostgreSQL server IP address | none |
| `-pg-database` | PostgreSQL database name | `ai_workbench` |
| `-pg-username` | PostgreSQL username | `postgres` |
| `-pg-password-file` | Path to password file | none |
| `-pg-port` | PostgreSQL server port | `5432` |
| `-pg-sslmode` | SSL mode | `prefer` |
| `-pg-sslcert` | Path to client SSL certificate | none |
| `-pg-sslkey` | Path to client SSL key | none |
| `-pg-sslrootcert` | Path to root SSL certificate | none |

## Configuration Validation

The Collector validates configuration at startup and checks the following
requirements.

The collector requires these fields to be set:

- `datastore.host` - Must be set.
- `datastore.database` - Must be set.
- `datastore.username` - Must be set.
- Secret file - Must exist in one of the search paths.

The collector validates these ranges:

- `datastore.port` - Must be 1-65535.
- Pool `max_connections` values - Must be greater than 0.
- Pool `max_idle_seconds` values - Must be 0 or greater.
- Pool `max_wait_seconds` values - Must be greater than 0.

## Environment Variables

The Collector does not use environment variables for configuration. You
must specify all settings in the configuration file or as command-line
flags.

## Configuration Precedence

The collector loads configuration in this order (later sources override
earlier ones):

1. Built-in defaults.
2. Configuration file.
3. Command-line flags.

In the following example, the host value demonstrates precedence:

```bash
# datastore.host defaults to "localhost"
# Config file sets datastore.host: db1.example.com
# Command-line sets -pg-host db2.example.com
# Final value: db2.example.com
```

## Configuration Examples

### Minimal Configuration

The following example shows a minimal working configuration:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: collector
  password_file: /etc/ai-workbench/password.txt

# secret_file defaults to searching standard paths
```

### Production Configuration

The following example shows a production configuration with SSL:

```yaml
datastore:
  host: metrics-db.internal.example.com
  database: ai_workbench_prod
  username: collector_prod
  password_file: /var/secrets/ai-workbench/password.txt
  port: 5432
  sslmode: verify-full
  sslcert: /etc/ai-workbench/certs/client.pem
  sslkey: /etc/ai-workbench/certs/client-key.pem
  sslrootcert: /etc/ai-workbench/certs/ca.pem

pool:
  datastore_max_connections: 100
  datastore_max_idle_seconds: 300
  datastore_max_wait_seconds: 60
  monitored_max_connections: 10
  monitored_max_idle_seconds: 300
  monitored_max_wait_seconds: 120

secret_file: /var/secrets/ai-workbench/collector.secret
```

### Development Configuration

The following example shows a development configuration:

```yaml
datastore:
  host: localhost
  database: ai_workbench_dev
  username: postgres
  password_file: dev-password.txt
  sslmode: disable

pool:
  datastore_max_connections: 10
  monitored_max_connections: 3

secret_file: ./ai-dba-collector.secret
```

## See Also

- [Configuration Guide](configuration.md) - Detailed configuration guide.
- [Quick Start](quickstart.md) - Getting started.
- [Security Best Practices](quickstart.md#security-recommendations) -
  Security recommendations.
