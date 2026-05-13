# Installation Guide

The pgEdge AI DBA Workbench consists of four components: a collector,
a server, an alerter, and a web client.

## Installation Paths by Method

The Workbench supports three deployment methods: pre-built binary files
or source code from GitHub, Docker, and RPM/DEB packages from pgEdge.

Each method places files in different locations. The following table
summarizes the locations for each deployment method.

| Resource | GitHub Release | Docker | RPM/DEB Package |
|----------|---------------|--------|-----------------|
| Binaries | `/opt/ai-workbench/` | `/usr/local/bin/` | `/usr/bin/` |
| Config | `/etc/pgedge/` | `/etc/pgedge/` (mounted) | `/etc/pgedge/` |
| Data | user-chosen | `/data/` | `/var/lib/pgedge/<service>/` |
| Logs | `stderr` | `stderr` | `/var/log/pgedge/<service>/` |
| Client files | `/opt/ai-workbench/client/` | container-served | `/usr/share/pgedge/ai-dba-client/` |
| systemd units | `pgedge-ai-dba-*.service` | N/A | `pgedge-ai-dba-*.service` |
| Run-as user | user-chosen | container user | `pgedge` |

!!! note
    RPM and DEB packages are available from the
    [pgEdge Enterprise Repository](https://docs.pgedge.com/enterprise/).
    Contact pgEdge for access details.

The installation steps below demonstrate the GitHub release method with
sample paths; adjust the paths to match your deployment method.


## System Requirements

The following minimum requirements apply to all deployment environments.

The collector, server, and alerter components share the following
hardware requirements:

- A minimum of 4 CPU cores is required.
- The system requires at least 16 GB of RAM.
- The installation requires 120 GB of disk space for binaries and
  the datastore.

Before installing the Workbench with binary files or building the
project from source, install the following software:

- [Go 1.24](https://go.dev/doc/install) or later is required for
  building server-side components.
- [Node.js 18](https://nodejs.org/) or later is required for building
  the web client.
- [PostgreSQL 14](https://www.postgresql.org/download/) or later is
  required for the datastore.
- [Make](https://www.gnu.org/software/make/) is required for build
  automation.
- [nginx](https://nginx.org/en/docs/) is required to serve the client.


Each component requires specific network access to operate correctly:

- The collector requires network access to each monitored PostgreSQL
  server.
- The alerter requires network access to the datastore.
- The server requires network access to the datastore and must be
  reachable by web client users.
- Database credentials for the datastore and each monitored PostgreSQL
  server are required.


## Using Binary Files to Install Workbench

The [GitHub releases page](https://github.com/pgEdge/ai-dba-workbench/releases)
provides pre-built binaries for each release. Each release includes the
following components:

- The `ai-dba-collector` binary for the collector service.
- The `ai-dba-server` binary for the server service.
- The `ai-dba-alerter` binary for the alerter service.
- The `ai-dba-client.tar.gz` archive containing pre-built web client
  files.

The Quick Start Guide contains detailed instructions for using binary
files to install and configure the
[Workbench](docs/getting-started/quick-start.md).


## Building AI DBA Workbench from Source Code

The project uses Makefiles for building and testing; all components
can be built from the top-level directory.

In the following example, the `make` command builds all components:

```bash
make all
```

In the following example, the `make` command builds the `collector`
component individually:

```bash
cd collector && make build
```

After completing the installation, create configuration files and
configure each component for your environment. Copy sample configuration
files from the
[GitHub repository](https://github.com/pgEdge/ai-dba-workbench/tree/main/examples):

- The [Collector Configuration](configuration/collector.md) file
  describes datastore and connection pool settings. The `collector.yaml`
  file must include the location of:

    * [The secret_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#security-options)
    * [The password_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#datastorepassword_file)

- The [Server Configuration](configuration/server.md) file describes
  authentication, TLS, and LLM settings. The `server.yaml` file must
  include:

    * [The secret_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#security-options)
    * The password associated with the user that owns the
      `/opt/ai-workbench/data` directory (under the `database:` section).

- The [Alerter Configuration](configuration/alerter.md) file describes
  threshold and anomaly detection settings. The `alerter.yaml` file
  must include:

    * [The secret_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#security-options)
    * [The password_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#datastorepassword_file)

- The [Client Configuration](configuration/client.md) file describes
  proxy and build settings.


## Configuring systemd Services

The following sections provide details about creating systemd service
files to run each component as a background service.

### Collector Service

The collector service file configures the collector to start
automatically and restart on failure.

Create the service file at
`/etc/systemd/system/pgedge-ai-dba-collector.service`; replace the
`user_name` placeholder with the name of the operating system user
account that owns the `/opt/ai-workbench/data` directory:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Collector
After=network.target postgresql.service

[Service]
Type=simple
User=user_name
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-collector \
    -config /etc/pgedge/ai-dba-collector.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Server Service

The server service file configures the server to start automatically
and restart on failure.

Create the service file at
`/etc/systemd/system/pgedge-ai-dba-server.service`; replace the
`user_name` placeholder with the name of the operating system user
account that owns the `/opt/ai-workbench/data` directory:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Server
After=network.target postgresql.service

[Service]
Type=simple
User=user_name
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-server \
    -config /etc/pgedge/ai-dba-server.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Alerter Service

The alerter service file configures the alerter to start automatically
and restart if the process exits.

Create the service file at
`/etc/systemd/system/pgedge-ai-dba-alerter.service`; replace the
`user_name` placeholder with the name of the operating system user
account that owns the `/opt/ai-workbench/data` directory:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Alerter
After=network.target postgresql.service

[Service]
Type=simple
User=user_name
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-alerter \
    -config /etc/pgedge/ai-dba-alerter.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Enable and Start the Services

Use `systemctl` to reload the daemon and enable each service.

In the following example, the `systemctl` commands reload the daemon,
enable all services, and start each one:

```bash
sudo systemctl daemon-reload
sudo systemctl enable pgedge-ai-dba-collector
sudo systemctl enable pgedge-ai-dba-server
sudo systemctl enable pgedge-ai-dba-alerter
sudo systemctl start pgedge-ai-dba-collector
sudo systemctl start pgedge-ai-dba-server
sudo systemctl start pgedge-ai-dba-alerter
```

In the following example, the `systemctl status` command checks the
status of each service:

```bash
sudo systemctl status pgedge-ai-dba-collector
sudo systemctl status pgedge-ai-dba-server
sudo systemctl status pgedge-ai-dba-alerter
```

## Running the Workbench

Before running the Workbench, add a user to the `auth.db` file. The
`auth.db` file is the server's own database for user credentials, storing
authentication details only for AI Workbench. Use the command:

```bash
/opt/ai-workbench/ai-dba-server -add-user -username user_name
```

The command prompts you for a Workbench password, and optional user details.
In the following example, the command creates a login for the AI DBA
Workbench server:

```bash
/opt/ai-workbench/ai-dba-server -add-user -username susan
Enter password: 
Confirm password: 
Enter full name (optional): Susan
Enter email address (optional): susan@pgedge.com
Enter notes for this user (optional): 

======================================================================
User created successfully!
======================================================================

Username:  susan
Full Name: Susan
Email:    susan@pgedge.com
Status:   Enabled
======================================================================
```

Copy the client files to the appropriate directory:

```bash
sudo mkdir -p /opt/ai-workbench/client
sudo cp -r assets index.html favicon.ico /opt/ai-workbench/client/
```

Install and configure nginx to serve the client files and proxy API
requests to the server:

```bash
sudo apt install nginx
```

Create the nginx configuration file at
`/etc/nginx/sites-available/ai-dba-workbench`:

```nginx
server {
    listen 80;
    server_name your_server_hostname_or_ip;

    root /opt/ai-workbench/client;
    index index.html;

    location /api/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /mcp/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 300s;
    }

    location = /health {
        proxy_pass http://localhost:8080;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

Enable the configuration and restart nginx:

```bash
sudo ln -s /etc/nginx/sites-available/ai-dba-workbench /etc/nginx/sites-enabled/ai-dba-workbench
sudo rm /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl restart nginx
```

Open a browser and navigate to `http://<server-ip>`; provide
authentication details when the Workbench opens.

![Log in to the AI DBA Workbench](../images/workbench_login.png)

After logging into the Workbench, you are ready to create a connection to your
Postgres server; select the '+' next to the DATABASE SERVERS heading in the
left navigation panel to add a server definition.

![Adding a server definition](../images/add_server.png)

### Connecting to a Local PostgreSQL Server

By default, the server blocks connections to internal and private IP
addresses. To monitor a PostgreSQL instance on the same host or local network,
enable internal network connections in the server configuration file:

```bash
sudo vi /etc/pgedge/ai-dba-server.yaml
```

Locate the `connection_security` section and set `allow_internal_networks` to
`true`:

```yaml
connection_security:
  allow_internal_networks: true
```

Restart the server to apply the change:

```bash
sudo systemctl restart pgedge-ai-dba-server
```

Then, when you define a server, provide connection details and specify
`localhost` in the host name before selecting `Save` to connect.

![Connected to a Local Server](../images/connected_server.png)


## Verifying the State of Individual Components

After starting all components, verify the installation by completing
the following steps.

### Checking the Collector

The collector logs probe executions to `stderr`. Use the following
command to confirm the collector is running:

```bash
sudo systemctl status pgedge-ai-dba-collector
```

A successful response confirms the collector is running:

```bash
pgedge-ai-dba-collector.service - pgEdge AI DBA Workbench Collector
 Loaded: loaded (/etc/systemd/system/pgedge-ai-dba-collector.service; enabled; vendor preset: enabled)
 Active: active (running) since Tue 2026-05-12 14:45:17 UTC; 24h ago
Main PID: 59722 (ai-dba-collecto)
   Tasks: 12 (limit: 4527)
  Memory: 16.3M
     CPU: 4.233s
  CGroup: /system.slice/pgedge-ai-dba-collector.service
          └─59722 /opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml

May 12 14:45:17 n1 systemd[1]: Started pgEdge AI DBA Workbench Collector.
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 pgEdge AI DBA Workbench Collector v1.0.0-beta1 starting...
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Configuration loaded from: /etc/pgedge/ai-dba-collector.yaml
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Schema is up to date
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Datastore connection established
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Probe scheduler started
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Collector is running. Press Ctrl+C to stop.
```

### Checking the Server

The server listens on the configured HTTP port. Use the following
command to test connectivity:

```bash
curl -s http://localhost:8080/health
```

A successful response confirms the server is running and accepting
requests:

```bash
curl -s http://localhost:8080/health
{"status":"ok","server":"pgedge-postgres-mcp","version":"1.0.0-beta1"}
```

### Checking the Alerter

The alerter logs rule evaluations to `stderr`. Use the following
command to confirm the alerter is running:

```bash
sudo systemctl status pgedge-ai-dba-alerter
```

A successful response confirms the alerter is running:

```bash
pgedge-ai-dba-alerter.service - pgEdge AI DBA Workbench Alerter
 Loaded: loaded (/etc/systemd/system/pgedge-ai-dba-alerter.service; enabled; vendor preset: enabled)
 Active: active (running) since Tue 2026-05-12 15:32:55 UTC; 23h ago
Main PID: 63620 (ai-dba-alerter)
   Tasks: 12 (limit: 4527)
  Memory: 6.6M
     CPU: 6.382s
  CGroup: /system.slice/pgedge-ai-dba-alerter.service
          └─63620 /opt/ai-workbench/ai-dba-alerter -config /etc/pgedge/ai-dba-alerter.yaml

May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculator started (interval: 1h0m0s)
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Alert cleaner started
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 12 16:32:55 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 12 16:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 13 11:54:51 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 13 11:54:51 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 13 12:54:51 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 13 12:54:51 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
```

### Checking Metrics Collection

Connect to the datastore and run the following query to verify that
metrics tables contain recent data.

In the following example, the `psql` command connects to the datastore:

```bash
sudo -u postgres psql -d ai_workbench
psql (18.3 (Ubuntu 18.3-1.pgdg22.04+1))
Type "help" for help.
```

In the following example, the `SELECT` statement queries the
`metrics.pg_stat_activity` table for a row count and the most recent
collection timestamp:

```sql
SELECT COUNT(*), MAX(collected_at) FROM metrics.pg_stat_activity;
 count |              max
-------+-------------------------------
  1014 | 2026-05-13 14:56:37.453882+00
(1 row)
```

A non-zero count with a recent timestamp confirms the collector is
gathering metrics.
