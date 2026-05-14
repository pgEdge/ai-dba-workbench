# Installing pgEdge AI DBA Workbench

The pgEdge AI DBA Workbench is an AI-powered environment for monitoring,
managing, and troubleshooting PostgreSQL systems. The Workbench combines a
Model Context Protocol (MCP) server with a web-based user interface, a data
collector, and an alert monitoring service. The Workbench enables users to
query, analyze, and manage distributed PostgreSQL clusters using natural
language and intelligent automation. The Workbench exposes pgEdge tools and
data sources to both cloud-connected and locally hosted language models; this
design ensures full functionality in air-gapped or secure environments.

## Supported Installation Methods

The Workbench supports three deployment methods.

- [Building from binary files](binary_install.md) is the easiest method to use
  to deploy the Workbench.
- [Building from source code](build_from_source.md) ensures you have the 
  latest Workbench features available.
- The [Docker guide](docker.md) walks you through a Workbench deployment
  via Docker using RPM/DEB packages from pgEdge.

Each installation method places files in different locations. The following
table summarizes the locations for each deployment method.

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


## System Requirements

The following minimum requirements apply to all deployment environments. The
collector, server, and alerter components share the following hardware
requirements:

- A minimum of 4 CPU cores is required.
- The system requires at least 16 GB of RAM.
- The installation requires 120 GB of disk space for binaries and the
  datastore.

Before installing the Workbench with binary files or building the project
from source, install the following software:

- [Go 1.24](https://go.dev/doc/install) or later is required for building
  server-side components.
- [Node.js 18](https://nodejs.org/) or later is required for building the
  web client.
- [PostgreSQL 14](https://www.postgresql.org/download/) or later is required
  for the datastore.
- [Make](https://www.gnu.org/software/make/) is required for build automation.
- [nginx](https://nginx.org/en/docs/) is required to serve the client.

Each component requires specific network access to operate correctly:

- The collector requires network access to each monitored PostgreSQL server.
- The alerter requires network access to the datastore.
- The server requires network access to the datastore and must be reachable
  by web client users.
- Database credentials for the datastore and each monitored PostgreSQL server
  are required.


## Verifying the Health of an Individual Component

After installing the Workbench and starting all components, verify the health
of each component with the following commands.

### Checking the Collector

The collector logs probe executions to `stderr`. Use the following command to
confirm the collector is running:

```bash
sudo systemctl status pgedge-ai-dba-collector
```

A healthy collector shows an active status, a confirmed datastore connection,
and active probe scheduling. The following example shows the expected output:

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

The server listens on the configured HTTP port. Use the following command to
test connectivity:

```bash
curl -s http://localhost:8080/health
```

A successful response confirms the server is running and accepting requests.
The following example shows the expected output:

```bash
curl -s http://localhost:8080/health
{"status":"ok","server":"pgedge-postgres-mcp","version":"1.0.0-beta1"}
```

### Checking the Alerter

The alerter logs rule evaluations to `stderr`. Use the following command to
confirm the alerter is running:

```bash
sudo systemctl status pgedge-ai-dba-alerter
```

A healthy alerter shows an active status and regular baseline recalculation
on its hourly schedule. The following example shows the expected output:

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
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 1 connections, 28 rules (lookback: 7 days)
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 12 16:32:55 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 1 connections, 28 rules (lookback: 7 days)
May 12 16:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 13 11:54:51 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 1 connections, 28 rules (lookback: 7 days)
May 13 11:54:51 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 13 12:54:51 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 1 connections, 28 rules (lookback: 7 days)
May 13 12:54:51 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
```

### Checking Metrics Collection

Connect to the datastore and run the following query to verify that metrics
tables contain recent data.

In the following example, the `psql` command connects to the datastore:

```bash
sudo -u postgres psql -d ai_workbench
psql (18.3 (Ubuntu 18.3-1.pgdg22.04+1))
Type "help" for help.
```

In the following example, the `SELECT` statement queries the
`metrics.pg_stat_activity` table and returns the activity count and the most
recent collection timestamp:

```sql
SELECT COUNT(*), MAX(collected_at) FROM metrics.pg_stat_activity;
 count |              max
-------+-------------------------------
  8046 | 2026-05-14 12:50:48.167367+00
(1 row)
```

A non-zero count with a recent timestamp confirms the collector is gathering
metrics.
