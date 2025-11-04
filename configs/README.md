# Configuration Files

This directory contains configuration files for the pgEdge AI Workbench Collector.

## Available Configurations

### dev.conf

Development configuration file for local testing.

**Settings:**
- Database: `ai_workbench` on localhost
- SSL Mode: `disable` (for local development)
- Pool Max Connections: 50 (increased for monitoring multiple databases)

**Usage:**

```bash
# From the repository root
./start_dev_server.sh

# Or manually
cd collector/src
go run . --config=../../configs/dev.conf
```

## Configuration Format

Configuration files use a simple key=value format:

```conf
# Database connection
pg_host = localhost
pg_database = ai_workbench
pg_port = 5432
pg_sslmode = disable

# Connection pool settings
pool_max_connections = 50
pool_max_idle_seconds = 300

# Optional: Server secret for encryption
# server_secret = your-secret-key-here
```

## Available Configuration Keys

### Database Connection

- `pg_host` - PostgreSQL server hostname
- `pg_hostaddr` - PostgreSQL server IP address (alternative to pg_host)
- `pg_database` - Database name for the collector's datastore
- `pg_username` - PostgreSQL username
- `pg_password_file` - Path to file containing PostgreSQL password
- `pg_port` - PostgreSQL server port (default: 5432)
- `pg_sslmode` - SSL mode (disable, require, verify-ca, verify-full)
- `pg_sslcert` - Path to SSL certificate file
- `pg_sslkey` - Path to SSL key file
- `pg_sslrootcert` - Path to SSL root certificate file

### Connection Pool

- `pool_max_connections` - Maximum number of connections in the pool
- `pool_max_idle_seconds` - Maximum time a connection can remain idle

### Security

- `server_secret` - Secret key for encryption (TODO: implement)

## Command Line Overrides

All configuration file settings can be overridden with command line flags:

```bash
go run . --config=dev.conf --pg-username=myuser --pg-port=5433
```
