# Configuration Reference

Complete reference for all Collector configuration options.

## Configuration File Format

```ini
# Comment lines start with #
key = value
key = "value with spaces"
```

## Datastore Connection Options

### pg_host

PostgreSQL server hostname or IP address for the datastore.

- **Type**: string
- **Default**: `localhost`
- **Required**: No
- **Command-line**: `-pg-host`
- **Example**: `pg_host = db.example.com`

### pg_hostaddr

PostgreSQL server IP address (bypasses DNS lookup).

- **Type**: string
- **Default**: none
- **Required**: No
- **Command-line**: `-pg-hostaddr`
- **Example**: `pg_hostaddr = 192.168.1.100`
- **Note**: If set, used instead of pg_host

### pg_database

Database name for the Collector's datastore.

- **Type**: string
- **Default**: `ai_workbench`
- **Required**: Yes
- **Command-line**: `-pg-database`
- **Example**: `pg_database = metrics`

### pg_username

Username for datastore connection.

- **Type**: string
- **Default**: `postgres`
- **Required**: Yes
- **Command-line**: `-pg-username`
- **Example**: `pg_username = collector`

### pg_password_file

Path to file containing the datastore password.

- **Type**: string (file path)
- **Default**: none
- **Required**: No (but strongly recommended)
- **Command-line**: `-pg-password-file`
- **Example**: `pg_password_file = /etc/ai-workbench/password.txt`
- **File format**: Plain text, single line, no trailing newline

### pg_port

PostgreSQL server port number.

- **Type**: integer
- **Default**: `5432`
- **Required**: No
- **Range**: 1-65535
- **Command-line**: `-pg-port`
- **Example**: `pg_port = 5433`

### pg_sslmode

SSL/TLS mode for datastore connection.

- **Type**: string
- **Default**: `prefer`
- **Required**: No
- **Command-line**: `-pg-sslmode`
- **Options**: `disable`, `allow`, `prefer`, `require`, `verify-ca`,
  `verify-full`
- **Example**: `pg_sslmode = require`

**SSL Mode Descriptions**:

- `disable` - No SSL encryption
- `allow` - Try non-SSL first, then SSL
- `prefer` - Try SSL first, then non-SSL (default)
- `require` - Require SSL, don't verify certificate
- `verify-ca` - Require SSL, verify certificate against CA
- `verify-full` - Require SSL, verify certificate and hostname

### pg_sslcert

Path to client SSL certificate file.

- **Type**: string (file path)
- **Default**: none
- **Required**: No
- **Command-line**: `-pg-sslcert`
- **Example**: `pg_sslcert = /etc/ai-workbench/client-cert.pem`
- **Note**: Used with verify-ca or verify-full

### pg_sslkey

Path to client SSL private key file.

- **Type**: string (file path)
- **Default**: none
- **Required**: No (required if pg_sslcert set)
- **Command-line**: `-pg-sslkey`
- **Example**: `pg_sslkey = /etc/ai-workbench/client-key.pem`

### pg_sslrootcert

Path to root CA certificate file.

- **Type**: string (file path)
- **Default**: none
- **Required**: No
- **Command-line**: `-pg-sslrootcert`
- **Example**: `pg_sslrootcert = /etc/ai-workbench/ca-cert.pem`
- **Note**: Used to verify server certificate

## Connection Pool Options

### datastore_pool_max_connections

Maximum number of concurrent connections to the datastore.

- **Type**: integer
- **Default**: `25`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `datastore_pool_max_connections = 50`
- **Tuning**: Increase for more concurrent probe storage operations

### datastore_pool_max_idle_seconds

Maximum idle time (seconds) for datastore connections.

- **Type**: integer
- **Default**: `300` (5 minutes)
- **Required**: No
- **Min**: 0 (0 = no timeout)
- **Command-line**: Not available
- **Example**: `datastore_pool_max_idle_seconds = 600`
- **Tuning**: Longer for connection warmth, shorter for resource limits

### datastore_pool_max_wait_seconds

Maximum wait time (seconds) for an available datastore connection.

- **Type**: integer
- **Default**: `60`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `datastore_pool_max_wait_seconds = 120`
- **Tuning**: Increase if seeing storage timeout errors

### monitored_pool_max_connections

Maximum concurrent connections PER monitored database server.

- **Type**: integer
- **Default**: `5`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `monitored_pool_max_connections = 10`
- **Note**: This is per-server, not total

### monitored_pool_max_idle_seconds

Maximum idle time (seconds) for monitored connections.

- **Type**: integer
- **Default**: `300` (5 minutes)
- **Required**: No
- **Min**: 0 (0 = no timeout)
- **Command-line**: Not available
- **Example**: `monitored_pool_max_idle_seconds = 600`

### monitored_pool_max_wait_seconds

Maximum wait time (seconds) for an available monitored connection.

- **Type**: integer
- **Default**: `60`
- **Required**: No
- **Min**: 1
- **Command-line**: Not available
- **Example**: `monitored_pool_max_wait_seconds = 120`
- **Tuning**: Increase if seeing probe execution timeout errors

## Security Options

### server_secret

Per-installation secret for password encryption.

- **Type**: string
- **Default**: none
- **Required**: Yes
- **Command-line**: Not available
- **Example**: `server_secret = randomly-generated-secret-string`
- **Security**: Keep this secret secure. If lost, passwords must be re-entered
- **Generation**: `openssl rand -base64 32`

## Configuration Validation

The Collector validates configuration at startup:

**Required Fields:**

- `pg_host` - Must be set
- `pg_database` - Must be set
- `pg_username` - Must be set
- `server_secret` - Must be set

**Range Validation:**

- `pg_port` - Must be 1-65535
- Pool max_connections - Must be > 0
- Pool max_idle_seconds - Must be >= 0
- Pool max_wait_seconds - Must be > 0

## Environment Variables

The Collector does not currently use environment variables for configuration.
All configuration must be in the configuration file or command-line flags.

## Configuration Precedence

Configuration is loaded in this order (later overrides earlier):

1. Built-in defaults
2. Configuration file
3. Command-line flags

**Example:**

```bash
# pg_host defaults to "localhost"
# Config file sets pg_host = "db1.example.com"
# Command-line sets -pg-host db2.example.com
# Final value: db2.example.com
```

## Configuration Examples

### Minimal

```ini
pg_host = localhost
pg_database = ai_workbench
pg_username = collector
pg_password_file = /etc/ai-workbench/password.txt
server_secret = your-secret-here
```

### Production

```ini
# Datastore
pg_host = metrics-db.internal.example.com
pg_database = ai_workbench_prod
pg_username = collector_prod
pg_password_file = /var/secrets/ai-workbench/password.txt
pg_port = 5432
pg_sslmode = verify-full
pg_sslcert = /etc/ai-workbench/certs/client.pem
pg_sslkey = /etc/ai-workbench/certs/client-key.pem
pg_sslrootcert = /etc/ai-workbench/certs/ca.pem

# Connection pools
datastore_pool_max_connections = 100
datastore_pool_max_idle_seconds = 300
datastore_pool_max_wait_seconds = 60
monitored_pool_max_connections = 10
monitored_pool_max_idle_seconds = 300
monitored_pool_max_wait_seconds = 120

# Security
server_secret = production-secret-from-secure-storage
```

### Development

```ini
pg_host = localhost
pg_database = ai_workbench_dev
pg_username = postgres
pg_password_file = dev-password.txt
pg_sslmode = disable
datastore_pool_max_connections = 10
monitored_pool_max_connections = 3
server_secret = dev-secret-not-for-production
```

## See Also

- [Configuration Guide](configuration.md) - Detailed configuration guide
- [Quick Start](quickstart.md) - Getting started
- [Security Best Practices](quickstart.md#security-recommendations)
