# Installation Guide

This guide covers installing the pgEdge AI DBA
Workbench for production environments. The system
consists of four components: a collector, a server,
an alerter, and a web client.

## System Requirements

The following minimum requirements apply to all
deployment environments.

### Hardware

- 4 CPU cores.
- 16 GB RAM.
- 120 GB disk space for binaries and datastore.

### Software

- [PostgreSQL 14](https://www.postgresql.org/download/)
  or later for the datastore.
- Linux x86_64 operating system for the server-side
  components.

### Network

- The collector requires network access to each
  monitored PostgreSQL server.
- The alerter requires network access to the
  datastore.
- The server requires network access to the datastore
  and must be reachable by web client users.
- Database credentials for the datastore and each
  monitored PostgreSQL server.

## Downloading Binaries

Download pre-built binaries from the
[GitHub releases page](https://github.com/pgEdge/ai-dba-workbench/releases).
Each release includes the following components:

- The `ai-dba-collector` binary for the collector
  service.
- The `ai-dba-server` binary for the server service.
- The `ai-dba-alerter` binary for the alerter service.
- The `client` directory containing pre-built web
  client files.

### Install Server-Side Binaries

Create a deployment directory and copy the downloaded
binaries to that location.

In the following example, the commands install the
binaries to `/opt/ai-workbench`:

```bash
sudo mkdir -p /opt/ai-workbench
sudo cp ai-dba-collector /opt/ai-workbench/
sudo cp ai-dba-server /opt/ai-workbench/
sudo cp ai-dba-alerter /opt/ai-workbench/
sudo chmod +x /opt/ai-workbench/ai-dba-*
```

### Install the Web Client

Copy the pre-built web client files to the
deployment directory:

```bash
sudo cp -r client /opt/ai-workbench/client
```

Serve the web client files using a reverse proxy or
static file server such as Nginx. The web client
consists of static HTML, CSS, and JavaScript files
that require no server-side runtime.

!!! note
    To build the components from source instead,
    see the
    [Developer Guide](../developer-guide/index.md).

## Setting Up systemd Services

Create systemd service files to run each component as
a background service.

### Collector Service

Create the service file at
`/etc/systemd/system/ai-workbench-collector.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Collector
After=network.target postgresql.service

[Service]
Type=simple
User=ai-workbench
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-collector \
    -config /etc/pgedge/ai-dba-collector.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Server Service

Create the service file at
`/etc/systemd/system/ai-workbench-server.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Server
After=network.target postgresql.service

[Service]
Type=simple
User=ai-workbench
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-server \
    -config /etc/pgedge/ai-dba-server.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Alerter Service

Create the service file at
`/etc/systemd/system/ai-workbench-alerter.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Alerter
After=network.target postgresql.service

[Service]
Type=simple
User=ai-workbench
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-alerter \
    -config /etc/pgedge/ai-dba-alerter.yaml
Restart=always
RestartSec=10
EnvironmentFile=/etc/ai-workbench/alerter.env

[Install]
WantedBy=multi-user.target
```

### Enable and Start Services

Reload the systemd daemon and enable each service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable ai-workbench-collector
sudo systemctl enable ai-workbench-server
sudo systemctl enable ai-workbench-alerter
sudo systemctl start ai-workbench-collector
sudo systemctl start ai-workbench-server
sudo systemctl start ai-workbench-alerter
```

Check the status of each service:

```bash
sudo systemctl status ai-workbench-collector
sudo systemctl status ai-workbench-server
sudo systemctl status ai-workbench-alerter
```

## Verifying the Installation

After starting all components, verify the installation
by following these steps.

### Check the Collector

The collector logs probe executions to `stderr`. Use
the following command to verify the collector is
running:

```bash
sudo systemctl status ai-workbench-collector
```

### Check the Server

The server listens on the configured HTTP port. Use
the following command to test connectivity:

```bash
curl -s http://localhost:8080/api/v1/capabilities
```

A successful response confirms the server is running
and accepting requests.

### Check the Alerter

The alerter logs rule evaluations to `stderr`. Use
the following command to verify the alerter is
running:

```bash
sudo systemctl status ai-workbench-alerter
```

### Check Metrics Collection

Connect to the datastore and verify that metrics
tables contain recent data:

```sql
SELECT COUNT(*), MAX(collected_at)
FROM metrics.pg_stat_activity;
```

A non-zero count with a recent timestamp confirms
the collector is gathering metrics.

## Next Steps

After completing the installation, configure each
component for your environment:

- Review the
  [collector configuration](configuration/collector.md)
  for datastore and pool settings.
- Review the
  [server configuration](configuration/server.md)
  for authentication, TLS, and LLM settings.
- Review the
  [alerter configuration](configuration/alerter.md)
  for threshold and anomaly detection settings.
- Review the
  [client configuration](configuration/client.md)
  for proxy and build settings.
