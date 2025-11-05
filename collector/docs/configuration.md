# Configuration Guide

This guide explains how to configure the pgEdge AI Workbench Collector for
your environment.

## Configuration Sources

The Collector supports configuration through two sources:

1. **Configuration File**: A simple key-value file
2. **Command-Line Flags**: Override config file settings

Command-line flags take precedence over configuration file settings.

## Configuration File

### File Location

By default, the Collector looks for `ai-workbench.conf` in the same directory
as the executable. You can specify a different location:

```bash
./collector -config /path/to/custom-config.conf
```

If no config file is specified and the default file doesn't exist, the
Collector will use built-in defaults.

### File Format

The configuration file uses a simple key-value format:

```ini
# Comments start with #
key = value
another_key = "quoted value"

# Values can be unquoted
pg_host = localhost

# Or quoted (useful for values with spaces)
pg_host = "db.example.com"
```

**Rules:**

- Lines starting with `#` are comments
- Format: `key = value`
- Whitespace around `=` is trimmed
- Quoted values (`"value"`) have quotes removed
- Empty lines are ignored

### Sample Configuration

See the complete sample configuration file:
[ai-workbench.conf.sample](../../configs/ai-workbench.conf.sample)

## Configuration Options

### Datastore Connection Settings

These settings configure the connection to the Collector's datastore
(PostgreSQL database).

#### pg_host

PostgreSQL server hostname or IP address.

- **Type**: string
- **Default**: `localhost`
- **Example**: `pg_host = prod-db.example.com`
- **Command-line**: `-pg-host`

#### pg_hostaddr

PostgreSQL server IP address (optional, bypasses DNS lookup).

- **Type**: string
- **Default**: none
- **Example**: `pg_hostaddr = 192.168.1.100`
- **Command-line**: `-pg-hostaddr`
- **Note**: If set, used instead of `pg_host` for connection

#### pg_database

Database name for the Collector's datastore.

- **Type**: string
- **Default**: `ai_workbench`
- **Example**: `pg_database = metrics_db`
- **Command-line**: `-pg-database`

#### pg_username

Username for datastore connection.

- **Type**: string
- **Default**: `postgres`
- **Example**: `pg_username = collector`
- **Command-line**: `-pg-username`

#### pg_password_file

Path to file containing the datastore password.

- **Type**: string (file path)
- **Default**: none
- **Example**: `pg_password_file = /etc/ai-workbench/password.txt`
- **Command-line**: `-pg-password-file`
- **Note**: File should contain only the password, no extra whitespace

**Example password file:**

```bash
# Create password file
echo "my-secure-password" > /etc/ai-workbench/password.txt
chmod 600 /etc/ai-workbench/password.txt
```

#### pg_port

PostgreSQL server port number.

- **Type**: integer
- **Default**: `5432`
- **Range**: 1-65535
- **Example**: `pg_port = 5433`
- **Command-line**: `-pg-port`

#### pg_sslmode

SSL/TLS mode for datastore connection.

- **Type**: string
- **Default**: `prefer`
- **Options**: `disable`, `allow`, `prefer`, `require`, `verify-ca`,
  `verify-full`
- **Example**: `pg_sslmode = require`
- **Command-line**: `-pg-sslmode`

**SSL Modes:**

- `disable`: No SSL
- `allow`: Try non-SSL first, then SSL
- `prefer`: Try SSL first, then non-SSL (default)
- `require`: Require SSL, don't verify certificate
- `verify-ca`: Require SSL, verify certificate against CA
- `verify-full`: Require SSL, verify certificate and hostname

#### pg_sslcert

Path to client SSL certificate file.

- **Type**: string (file path)
- **Default**: none
- **Example**: `pg_sslcert = /etc/ai-workbench/client-cert.pem`
- **Command-line**: `-pg-sslcert`
- **Note**: Used with `pg_sslmode = verify-ca` or `verify-full`

#### pg_sslkey

Path to client SSL private key file.

- **Type**: string (file path)
- **Default**: none
- **Example**: `pg_sslkey = /etc/ai-workbench/client-key.pem`
- **Command-line**: `-pg-sslkey`
- **Note**: Used with client certificates

#### pg_sslrootcert

Path to root CA certificate file.

- **Type**: string (file path)
- **Default**: none
- **Example**: `pg_sslrootcert = /etc/ai-workbench/ca-cert.pem`
- **Command-line**: `-pg-sslrootcert`
- **Note**: Used to verify server certificate

### Connection Pool Settings

These settings control connection pool behavior for both the datastore and
monitored connections.

#### datastore_pool_max_connections

Maximum number of concurrent connections to the datastore.

- **Type**: integer
- **Default**: `25`
- **Example**: `datastore_pool_max_connections = 50`
- **Note**: Higher values allow more concurrent probe storage operations

#### datastore_pool_max_idle_seconds

Maximum idle time (seconds) before closing idle datastore connections.

- **Type**: integer
- **Default**: `300` (5 minutes)
- **Example**: `datastore_pool_max_idle_seconds = 600`
- **Note**: Set to 0 to disable idle connection cleanup

#### datastore_pool_max_wait_seconds

Maximum time (seconds) to wait for an available datastore connection.

- **Type**: integer
- **Default**: `60`
- **Example**: `datastore_pool_max_wait_seconds = 120`
- **Note**: Probe storage operations will fail if timeout is exceeded

#### monitored_pool_max_connections

Maximum concurrent connections PER monitored database server.

- **Type**: integer
- **Default**: `5`
- **Example**: `monitored_pool_max_connections = 10`
- **Note**: This is per-server, not total. 10 servers with limit 5 = 50 max
  connections

#### monitored_pool_max_idle_seconds

Maximum idle time (seconds) before closing idle monitored connections.

- **Type**: integer
- **Default**: `300` (5 minutes)
- **Example**: `monitored_pool_max_idle_seconds = 600`

#### monitored_pool_max_wait_seconds

Maximum time (seconds) to wait for an available monitored connection.

- **Type**: integer
- **Default**: `60`
- **Example**: `monitored_pool_max_wait_seconds = 120`
- **Note**: Probe execution will fail if timeout is exceeded

### Security Settings

#### server_secret

Per-installation secret for encryption (REQUIRED).

- **Type**: string
- **Default**: none
- **Example**: `server_secret = randomly-generated-secret-string`
- **Note**: Used to encrypt/decrypt passwords for monitored connections
- **Important**: Keep this secret secure. If lost, passwords must be re-entered

**Generate a secure secret:**

```bash
openssl rand -base64 32
```

## Command-Line Flags

All datastore connection options can be specified as command-line flags:

```bash
./collector \
    -config /path/to/config.conf \
    -pg-host localhost \
    -pg-database ai_workbench \
    -pg-username collector \
    -pg-password-file /path/to/password.txt \
    -pg-port 5432 \
    -pg-sslmode prefer
```

**Note**: Connection pool and security settings can only be configured in the
configuration file.

## Configuration Examples

### Minimal Configuration

```ini
# Minimal working configuration
pg_host = localhost
pg_database = ai_workbench
pg_username = collector
pg_password_file = /etc/ai-workbench/password.txt
server_secret = your-random-secret-here
```

### Production Configuration

```ini
# Production configuration with SSL and tuned pools

# Datastore connection
pg_host = datastore.internal.example.com
pg_database = ai_workbench_prod
pg_username = collector_prod
pg_password_file = /var/secrets/ai-workbench/db-password.txt
pg_port = 5432
pg_sslmode = verify-full
pg_sslcert = /etc/ai-workbench/certs/client-cert.pem
pg_sslkey = /etc/ai-workbench/certs/client-key.pem
pg_sslrootcert = /etc/ai-workbench/certs/ca-cert.pem

# Connection pools (tuned for 50 monitored servers)
datastore_pool_max_connections = 100
datastore_pool_max_idle_seconds = 300
datastore_pool_max_wait_seconds = 60
monitored_pool_max_connections = 10
monitored_pool_max_idle_seconds = 300
monitored_pool_max_wait_seconds = 120

# Security
server_secret = production-secret-from-secure-storage
```

### Development Configuration

```ini
# Development configuration with minimal security

pg_host = localhost
pg_database = ai_workbench_dev
pg_username = postgres
pg_password_file = ~/.pgpass
pg_port = 5432
pg_sslmode = disable

# Smaller pools for development
datastore_pool_max_connections = 10
monitored_pool_max_connections = 3

# Development secret (DO NOT USE IN PRODUCTION)
server_secret = dev-secret-not-for-production
```

### High-Volume Configuration

```ini
# Configuration for monitoring many servers with high frequency

# Datastore on dedicated server
pg_host = metrics-db.internal.example.com
pg_database = ai_workbench
pg_username = collector
pg_password_file = /etc/ai-workbench/password.txt
pg_sslmode = require

# Large connection pools for high concurrency
datastore_pool_max_connections = 200
monitored_pool_max_connections = 15
datastore_pool_max_wait_seconds = 90
monitored_pool_max_wait_seconds = 90

# Longer idle timeout to keep connections warm
datastore_pool_max_idle_seconds = 600
monitored_pool_max_idle_seconds = 600

server_secret = high-volume-secret
```

## Tuning Guidelines

### Datastore Pool Size

Choose `datastore_pool_max_connections` based on:

- **Number of probes**: Each probe may need a connection to store metrics
- **Collection frequency**: More frequent collections need more connections
- **Datastore capacity**: Don't exceed the server's max connections

**Formula**: `(number of probes × concurrent monitored servers) / 2`

**Example**: 24 probes, 10 monitored servers = ~120 connections suggested

### Monitored Pool Size

Choose `monitored_pool_max_connections` based on:

- **Probe concurrency**: How many probes might run simultaneously
- **Monitored server capacity**: Don't overwhelm monitored servers
- **Network latency**: Higher latency may need more connections

**Recommendation**: Start with 5, increase if you see timeout errors

### Idle Timeout

Choose `*_pool_max_idle_seconds` based on:

- **Connection cost**: Longer timeout if connections are expensive to create
- **Resource constraints**: Shorter timeout if resources are limited
- **Activity patterns**: Longer timeout for constant activity

**Recommendation**: 300 seconds (5 minutes) is a good default

### Wait Timeout

Choose `*_pool_max_wait_seconds` based on:

- **Expected wait time**: How long is acceptable to wait
- **Failure strategy**: Shorter timeout fails faster
- **Load patterns**: Longer timeout for burst loads

**Recommendation**: 60 seconds for datastore, 120 seconds for monitored

## Troubleshooting

### "Configuration file not found"

- Check the file path is correct
- Use absolute paths, not relative paths
- Verify file permissions allow reading

### "Failed to parse configuration"

- Check for syntax errors in the config file
- Ensure key=value format is correct
- Remove any special characters from values
- Quote values containing spaces

### "Invalid configuration"

- Ensure required fields are set (server_secret)
- Verify port numbers are in range (1-65535)
- Check pool sizes are positive numbers

### "Too many connections"

- Reduce `datastore_pool_max_connections`
- Reduce `monitored_pool_max_connections`
- Check monitored servers' max_connections setting
- Verify other clients aren't consuming connections

### "Connection timeout"

- Increase `*_pool_max_wait_seconds`
- Increase pool sizes
- Check network connectivity
- Verify database servers are responsive

## Security Best Practices

### Protecting Secrets

1. **File Permissions**: Set restrictive permissions on config files

   ```bash
   chmod 600 /etc/ai-workbench/collector.conf
   chmod 600 /etc/ai-workbench/password.txt
   ```

2. **Password Files**: Use dedicated password files, not inline passwords

3. **Server Secret**: Generate strong random secrets

   ```bash
   openssl rand -base64 32
   ```

4. **Version Control**: Never commit configs with real secrets

### SSL/TLS Configuration

For production, always use SSL:

```ini
pg_sslmode = verify-full
pg_sslcert = /path/to/client-cert.pem
pg_sslkey = /path/to/client-key.pem
pg_sslrootcert = /path/to/ca-cert.pem
```

Generate certificates using your organization's PKI or:

```bash
# Self-signed example (not recommended for production)
openssl req -new -x509 -days 365 -nodes \
    -out client-cert.pem -keyout client-key.pem
```

## Next Steps

- [Architecture](architecture.md) - Understand how configuration affects
  architecture
- [Development](development.md) - Learn about development configuration
- [Config Reference](config-reference.md) - Complete configuration reference
