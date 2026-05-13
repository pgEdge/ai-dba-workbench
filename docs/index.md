# pgEdge AI DBA Workbench

The pgEdge AI DBA Workbench is an AI-powered environment for monitoring,
managing, and troubleshooting PostgreSQL systems. The pgEdge AI DBA Workbench combines a Model Context Protocol (MCP) server
with a web-based user interface, a data collector, and an alert monitoring
service. The Workbench lets you query, analyze, and manage distributed
PostgreSQL clusters using natural language and intelligent automation. The
Workbench exposes pgEdge tools and data sources to both cloud-connected and
locally hosted language models; this design ensures full functionality in
air-gapped or secure environments.

## Supported Installation Methods

The Workbench supports three deployment methods: 

* [Installation with pre-built binary files](/getting-started/quick-start.md).
* [Installation with source code from GitHub](/getting-started/installation.md).
* [Installation via Docker using RPM/DEB packages from pgEdge](/getting-started/docker.md).

Each installation method places files in different locations. The following table
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


## System Requirements

The following minimum requirements apply to all deployment environments.

The collector, server, and alerter components share the following
hardware requirements:

- A minimum of 4 CPU cores is required.
- The system requires at least 16 GB of RAM.
- The installation requires 120 GB of disk space for binaries and
  the datastore.

Before installing the Workbench with binary files or building the
project from source, you'll need to install the following software:

- [Go 1.24](https://go.dev/doc/install) or later is required for
  building server-side components.
- [Node.js 18](https://nodejs.org/) or later is required for building
  the web client.
- [PostgreSQL 14](https://www.postgresql.org/download/) or later is
  required for the datastore.
- [Make](https://www.gnu.org/software/make/) is required for build
  automation.
- [nginx](https://nginx.org/en/docs/) is required to serve the client.
- All components require network connectivity to one another.
- Database credentials must carry appropriate permissions.

Each component requires specific network access to operate correctly:

- The collector requires network access to each monitored PostgreSQL
  server.
- The alerter requires network access to the datastore.
- The server requires network access to the datastore and must be
  reachable by web client users.
- Database credentials for the datastore and each monitored PostgreSQL
  server are required.


