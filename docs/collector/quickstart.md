# Quick Start Guide

This guide will help you get the pgEdge AI DBA Workbench Collector up and
running quickly.

## Prerequisites

Before you begin, ensure you have:

- Go 1.23 or later installed
- PostgreSQL 12 or later for the datastore
- Network access to the PostgreSQL servers you want to monitor
- Basic understanding of PostgreSQL administration

## Installation

### Option 1: Build from Source

1. Clone the repository:

   ```bash
   git clone https://github.com/pgedge/ai-workbench.git
   cd ai-workbench/collector
   ```

2. Build the Collector:

   ```bash
   cd src
   go mod tidy
   go build -o ai-dba-collector
   ```

3. The `ai-dba-collector` binary will be created in the `src` directory.

### Option 2: Download Pre-built Binary

(To be added when releases are available)

## Configuration

### Step 1: Prepare the Datastore Database

Create a PostgreSQL database for the Collector's datastore:

```sql
CREATE DATABASE ai_workbench;
CREATE USER collector WITH PASSWORD 'your-secure-password';
GRANT ALL PRIVILEGES ON DATABASE ai_workbench TO collector;
```

The Collector will automatically create the necessary schema when it starts.

### Step 2: Create a Configuration File

Copy the example configuration file:

```bash
cp ../examples/ai-dba-collector.yaml ai-dba-collector.yaml
```

Edit `ai-dba-collector.yaml` with your settings:

```yaml
# Datastore connection settings
datastore:
  host: localhost
  database: ai_workbench
  username: collector
  password_file: /path/to/password.txt
  port: 5432
  sslmode: prefer

# Path to server secret file (optional if using default paths)
# secret_file: /etc/pgedge/ai-dba-collector.secret
```

### Step 3: Create Password File

Create a file to store the datastore password:

```bash
echo "your-secure-password" > ~/.ai-workbench-password
chmod 600 ~/.ai-workbench-password
```

Update the `password_file` setting in your configuration to point to this file.

### Step 4: Create a Server Secret File

The server secret is used to encrypt passwords for monitored connections.
Generate a strong random secret and save it to a file:

```bash
# Generate a 32-byte random secret and save to file
openssl rand -base64 32 > ./ai-dba-collector.secret
chmod 600 ./ai-dba-collector.secret
```

The collector searches for the secret file in these locations (in order):

1. Path specified in `secret_file` config option
2. `/etc/pgedge/ai-dba-collector.secret`
3. `<binary-directory>/ai-dba-collector.secret`
4. `./ai-dba-collector.secret`

**Important**: Keep this secret file secure and never share it. If you lose
it, you will need to re-enter passwords for all monitored connections.

## Running the Collector

Start the Collector:

```bash
./ai-dba-collector -config ./ai-dba-collector.yaml
```

You should see output similar to:

```
2025/11/05 10:00:00 pgEdge AI DBA Workbench Collector v0.1.0 starting...
2025/11/05 10:00:00 Configuration loaded from: ./ai-dba-collector.yaml
2025/11/05 10:00:00 Initializing database schema...
2025/11/05 10:00:00 Database schema initialized
2025/11/05 10:00:00 Datastore connection established
2025/11/05 10:00:00 Creating monitored pool manager with max 5 connections per server, idle timeout 300s
2025/11/05 10:00:00 Probe scheduler started with 24 probe(s)
2025/11/05 10:00:00 Garbage collector started
2025/11/05 10:00:00 Collector is running. Press Ctrl+C to stop.
```

## Adding Monitored Connections

At this point, the Collector is running but not monitoring any servers. To add
servers to monitor, you need to insert records into the `connections` table in
the datastore.

### Method 1: Using SQL

Connect to the datastore and insert a connection:

```sql
-- Connect to the datastore
psql -h localhost -U collector -d ai_workbench

-- Insert a monitored connection
INSERT INTO connections (
    name,
    host,
    port,
    database_name,
    username,
    password_encrypted,
    is_shared,
    is_monitored,
    owner_username,
    owner_token
) VALUES (
    'Production Database',
    'prod-db.example.com',
    5432,
    'postgres',
    'monitoring_user',
    NULL,  -- Use the MCP server API to set the password
    true,  -- Shared connection
    true,  -- Enable monitoring
    'admin',
    'admin-token'
);
```

### Method 2: Using the MCP Server API (Recommended)

Use the MCP server's connection management API to add connections with
passwords. The API handles password encryption automatically using AES-256-GCM
with random salts.

## Verifying the Setup

### Check Collector Logs

The Collector will log probe executions and any errors. Watch the logs to
ensure probes are running:

```bash
./ai-dba-collector -config ./ai-dba-collector.yaml 2>&1 | tee collector.log
```

### Check Metrics Tables

Connect to the datastore and verify metrics are being collected:

```sql
-- List all metrics tables
\dt metrics.*

-- Check recent data from pg_stat_activity
SELECT COUNT(*), MAX(collected_at)
FROM metrics.pg_stat_activity;

-- View a sample of collected data
SELECT connection_id, collected_at, datname, state, COUNT(*)
FROM metrics.pg_stat_activity
WHERE collected_at > NOW() - INTERVAL '1 hour'
GROUP BY connection_id, collected_at, datname, state
ORDER BY collected_at DESC
LIMIT 10;
```

### Check Probe Configuration

View the configured probes:

```sql
SELECT name, collection_interval_seconds, retention_days, is_enabled
FROM probes
ORDER BY name;
```

## Common Issues

### "Failed to connect to datastore"

- Verify PostgreSQL is running
- Check connection parameters in config file
- Ensure the database exists
- Verify the user has permissions
- Check SSL/TLS settings if using encrypted connections

### "No monitored connections found"

This is normal if you haven't added any connections yet. See "Adding
Monitored Connections" above.

### "Failed to execute probe"

- Verify the monitored server is accessible
- Check that the monitoring user has appropriate permissions
- Check SSL/TLS settings for the monitored connection
- Verify the database exists on the monitored server

### Schema Migration Errors

- Ensure the datastore user has CREATE privileges
- Check PostgreSQL version (must be 12 or later)
- Review the error message for specific issues

## Upgrading

When upgrading the Collector, schema migrations run automatically on startup.
There are typically no manual steps required for upgrades.

## Next Steps

Now that you have the Collector running, follow these steps:

1. [Configure additional settings](configuration.md) to optimize performance.
2. Learn about [the probe system](probes.md) to understand data collection.
3. Review [available probes](probe-reference.md) to see what the Collector
   gathers.
4. Explore [the architecture](architecture.md) to understand the system
   design.
5. Set up the MCP server to enable API access and user management.

## Stopping the Collector

To stop the Collector gracefully:

1. Press `Ctrl+C` in the terminal where it's running
2. Wait for the shutdown message
3. The Collector will:
   - Stop scheduling new probes
   - Wait for in-progress probes to complete
   - Close all connection pools
   - Exit cleanly

Output will show:

```
^C2025/11/05 10:05:00 Shutdown signal received, stopping...
2025/11/05 10:05:00 Stopping probe scheduler for pg_stat_activity
...
2025/11/05 10:05:01 Closing monitored connection pools...
2025/11/05 10:05:01 Monitored connection pools closed
2025/11/05 10:05:01 Closing datastore connection pool...
2025/11/05 10:05:01 Datastore connection pool closed
2025/11/05 10:05:01 Collector stopped
```

## Running as a Service

### systemd (Linux)

Create `/etc/systemd/system/ai-workbench-collector.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Collector
After=network.target postgresql.service

[Service]
Type=simple
User=collector
WorkingDirectory=/opt/ai-workbench/collector
ExecStart=/opt/ai-workbench/collector/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable ai-workbench-collector
sudo systemctl start ai-workbench-collector
sudo systemctl status ai-workbench-collector
```

### launchd (macOS)

Create `~/Library/LaunchAgents/com.pgedge.ai-workbench-collector.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.pgedge.ai-workbench-collector</string>
    <key>ProgramArguments</key>
    <array>
        <string>/opt/ai-workbench/collector/ai-dba-collector</string>
        <string>-config</string>
        <string>/etc/pgedge/ai-dba-collector.yaml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>/opt/ai-workbench/collector</string>
</dict>
</plist>
```

Load the service:

```bash
launchctl load ~/Library/LaunchAgents/com.pgedge.ai-workbench-collector.plist
```

## Security Recommendations

For production deployments:

1. Use strong credentials by generating strong passwords for database users and
   using a cryptographically random server secret.

2. Enable SSL/TLS by configuring SSL for both the datastore connection and
   monitored connections.

3. Limit permissions by granting only necessary database privileges and using
   a dedicated monitoring user with read-only access.

4. Secure configuration files by setting appropriate file permissions (600 or
   640), storing password files securely, and never committing secrets to
   version control.

5. Implement network security by using firewalls to restrict database access
   and considering SSH tunnels or VPNs for remote connections.

6. Keep the system updated by maintaining the Collector, monitoring for
   security advisories, and updating dependencies regularly.
