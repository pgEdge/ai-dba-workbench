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
    [pgEdge Enterprise Repository](https://docs.pgedge.com/enterprise/), and
    are used in the Docker deployment method documented 
    [here](docker.md). If you're using pgEdge packages for deployment, note
    that the packages create and use the pgedge user automatically, and you do
    not need to manually adjust your systemd files to manage the service.

    Contact pgEdge for information about accessing the 
    [enterprise packages](https://docs.pgedge.com/enterprise/).


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


## Customizing Configuration Files

The installation guides linked above share the details required to get a minimal
deployment of the Workbench installed and serving content.  Additional configuration options
are extensive; for details about options available in each configuration file, see:

- The [collector](configuration/collector.md) guide covers tuned
  connection pools and SSL.
- The [server](configuration/server.md) guide covers TLS, authentication,
  and LLM integration.
- The [alerter](configuration/alerter.md) guide covers anomaly detection
  and notification channels.
- The [web client](configuration/client.md) guide covers proxy settings
  and build options.

