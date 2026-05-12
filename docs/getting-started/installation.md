# Installation Guide

This guide covers installing the pgEdge AI DBA Workbench for production
environments. The system consists of four components: a collector, a
server, an alerter, and a web client.

## Installation Paths by Method

You can deploy the Workbench in three ways:

* With pre-built binary files or source code from Github
* Using Docker
* With RPM/DEB packages from pgEdge

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

The installation steps below demonstrate using the GitHub release method with
sample paths; adjust the paths to match your deployment method.


## System Requirements

The following minimum requirements apply to all deployment environments.

The collector, server, and alerter components share the following hardware
requirements:

- 4 CPU cores.
- 16 GB RAM.
- 120 GB disk space for binaries and datastore.

Before installing the Workbench with binary files or building the project from
source, install the following software:

- [Go 1.24](https://go.dev/doc/install) or later for building server-side
  components.
- [Node.js 18](https://nodejs.org/) or later for building the web client.
- [PostgreSQL 14](https://www.postgresql.org/download/) or later for the
  datastore.
- [Make](https://www.gnu.org/software/make/) for build automation.

Each component requires specific network access to operate correctly:

- The collector requires network access to each monitored PostgreSQL server.
- The alerter requires network access to the datastore.
- The server requires network access to the datastore and must be
  reachable by web client users.
- Database credentials for the datastore and each monitored PostgreSQL
  server are required.


## Using Binary Files to Install Workbench

You can download pre-built binaries from the 
[GitHub releases page](https://github.com/pgEdge/ai-dba-workbench/releases).
Each release includes the following components:

- The `ai-dba-collector` binary for the collector service.
- The `ai-dba-server` binary for the server service.
- The `ai-dba-alerter` binary for the alerter service.
- The `ai-dba-client.tar.gz` archive containing pre-built web client files.

The Quick Start Guide contains detailed instructions for using the binary
files to install and configure 
[the Workbench](docs/getting-started/quick-start.md). 


## Building AI DBA Workbench from Source Code

This project uses Makefiles for building and testing. All components can be
built from the top-level directory with the command:

```bash
make all
```

To build an individual component (for example the `collector`), use the
following command:

```bash
cd collector && make build
```

After completing the installation, create configuration files and configure
each component for your environment.  You can copy sample configuration files
from the
[Github repository](https://github.com/pgEdge/ai-dba-workbench/tree/main/examples):

- The [Collector Configuration](configuration/collector.md) configuration file
  describes datastore and connection pool settings. The `collector.yaml` file
  must include the location of:

    * [The secret_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#security-options)
    * [The password_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#datastorepassword_file)

- The [Server Configuration](configuration/server.md) configuration file
  describes authentication, TLS, and LLM settings. The server.yaml file must
  include:

    * [The secret_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#security-options)
    * The password associated with the user that owns owns the `/opt/ai-workbench/data` directory (under the `database:` section)
  
- The [Alerter Configuration](configuration/alerter.md) configuration file
  describes threshold and anomaly detection settings.

    * [The secret_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#security-options)
    * [The password_file](https://docs.pgedge.com/ai-dba-workbench/v1-0-0-beta1/getting-started/configuration/collector/#datastorepassword_file)

- The [Client Configuration](configuration/client.md) configuration file
  describes proxy and build settings.


## Configuring systemd Services

The following sections provide details about creating systemd service files
to run each component as a background service.

### Collector Service

The collector service file configures the collector to start
automatically and restart on failure. 

Create the service file at 
`/etc/systemd/system/pgedge-ai-dba-collector.service`; within the file,
replace the `user_name` placeholder with the name of the operating system
user account that owns the `/opt/ai-workbench/data` directory:

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
`/etc/systemd/system/pgedge-ai-dba-server.service`; within the file,
replace the `user_name` placeholder with the name of the operating system
user account that owns the `/opt/ai-workbench/data` directory:

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
and always restart if the process exits. Create the service file at
`/etc/systemd/system/pgedge-ai-dba-alerter.service`; within the file,
replace the `user_name` placeholder with the name of the operating system
user account that owns the `/opt/ai-workbench/data` directory:

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

Reload the systemd daemon and enable each service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable pgedge-ai-dba-collector
sudo systemctl enable pgedge-ai-dba-server
sudo systemctl enable pgedge-ai-dba-alerter
sudo systemctl start pgedge-ai-dba-collector
sudo systemctl start pgedge-ai-dba-server
sudo systemctl start pgedge-ai-dba-alerter
```

Check the status of each service:

```bash
sudo systemctl status pgedge-ai-dba-collector
sudo systemctl status pgedge-ai-dba-server
sudo systemctl status pgedge-ai-dba-alerter
```

## Verifying the Installation

After starting all components, verify the installation by completing the
following steps.

1. Check the collector. The collector logs probe executions to `stderr`.
   Use the following command to confirm the collector is running:

    ```bash
    sudo systemctl status pgedge-ai-dba-collector
    ```

2. Check the server. The server listens on the configured HTTP port.
   Use the following command to test connectivity:

    ```bash
    curl -s http://localhost:8080/health
    ```

   A successful response confirms the server is running and accepting
   requests:

    ```bash
    curl -s http://localhost:8080/health
    {"status":"ok","server":"pgedge-postgres-mcp","version":"1.0.0-beta1"}
    ```

3. Check the alerter. The alerter logs rule evaluations to `stderr`.
   Use the following command to confirm the alerter is running:

    ```bash
    sudo systemctl status pgedge-ai-dba-alerter
    ```

4. Check metrics collection. Connect to the datastore and run the
   following query to verify that metrics tables contain recent data:

    ```sql
    sudo -u postgres psql -d ai_workbench
    psql (18.3 (Ubuntu 18.3-1.pgdg22.04+1))
    Type "help" for help.
    ```

    ```sql
    SELECT COUNT(*), MAX(collected_at) FROM metrics.pg_stat_activity;
    ```

   A non-zero count with a recent timestamp confirms the collector is
   gathering metrics.

