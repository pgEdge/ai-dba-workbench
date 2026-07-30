# pgEdge AI DBA Workbench

[![CI - Alerter](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml)
[![CI - Client](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml)
[![CI - Collector](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml)
[![CI - Docker](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docker.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docker.yml)
[![CI - Docs](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml)
[![CI - E2E](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-e2e.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-e2e.yml)
[![CI - Server](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml)


## Table of Contents

- [pgEdge AI DBA Workbench](#pgedge-ai-dba-workbench)
- [Using Binary Files to Install Workbench](#using-binary-files-to-install-workbench)
- [Building Workbench from Source](#building-workbench-from-source)
- Installing and Configuring pgEdge AI DBA Workbench:
  - [Installation Overview](docs/getting-started/installation_overview.md)
  - [Quick Start - Installing with Binaries](docs/getting-started/binary_install.md)
  - [Building from Source Code](docs/getting-started/build_from_source.md)
  - [Deploying with Docker](docs/getting-started/docker.md)
  - Configuring the Workbench:
    - [Server](docs/getting-started/configuration/server.md)
    - [Collector](docs/getting-started/configuration/collector.md)
    - [Alerter](docs/getting-started/configuration/alerter.md)
    - [Web Client](docs/getting-started/configuration/client.md)
    - [Configuring systemd Services](docs/getting-started/configuration/configure_systemd.md)
- User Guide:
  - [Using the Workbench](docs/user-guide/index.md)
  - Monitoring Dashboards:
    - [Reviewing the Monitoring Dashboards](docs/user-guide/dashboards/index.md)
    - [Estate Dashboard](docs/user-guide/dashboards/estate.md)
    - [Cluster Dashboard](docs/user-guide/dashboards/cluster.md)
    - [Server Dashboard](docs/user-guide/dashboards/server.md)
    - [Database Dashboard](docs/user-guide/dashboards/database.md)
    - [Object Dashboard](docs/user-guide/dashboards/object.md)
  - [Event Timeline](docs/user-guide/event-timeline.md)
  - Alerts:
    - [Monitoring Alerts](docs/user-guide/alerts/index.md)
    - [Alert Reference](docs/user-guide/alerts/rule-reference.md)
    - [AI Alert Analysis](docs/user-guide/alerts/ai-analysis.md)
    - [Managing Blackouts](docs/user-guide/alerts/blackouts.md)
  - AI Features:
    - [Using AI Features](docs/user-guide/ai/index.md)
    - [Ask Ellie](docs/user-guide/ai/ask-ellie.md)
    - [Connecting MCP Clients](docs/user-guide/ai/mcp-clients.md)
    - [Using Workbench with MCP Tools](docs/user-guide/ai/mcp-tools.md)
- Troubleshooting:
  - [Overview](docs/troubleshooting/index.md)
  - [Stale Server Status](docs/troubleshooting/stale-server-status.md)
  - [Troubleshooting](docs/troubleshooting/troubleshooting.md)
- Administrator's Guide:
  - [Overview](docs/admin-guide/index.md)
  - [TLS & Reverse Proxy](docs/admin-guide/tls-and-reverse-proxy.md)
  - [Verifying the Health of Components](docs/admin-guide/verify_health.md)
  - [Users & Authentication](docs/admin-guide/authentication.md)
  - [Connection Management](docs/admin-guide/connections.md)
  - [Alert Rules & Thresholds](docs/admin-guide/alert-rules.md)
  - [Notification Channels](docs/admin-guide/notification-channels.md)
  - [Probe Configuration](docs/admin-guide/probes.md)
  - REST API:
    - [API Reference](docs/admin-guide/api/reference.md)
    - [API Browser](docs/admin-guide/api/browser.md)
    - [Server Information](docs/admin-guide/api/server-info.md)
    - [Metrics API](docs/admin-guide/api/metrics.md)
- Developer's Guide:
  - [Overview](docs/developer-guide/index.md)
  - [Contributing](docs/developer-guide/contributing.md)
  - Collector:
    - [Architecture](docs/developer-guide/collector/architecture.md)
    - [Schema Design](docs/developer-guide/collector/schema.md)
    - [Schema Management](docs/developer-guide/collector/schema-management.md)
    - [Scheduler](docs/developer-guide/collector/scheduler.md)
    - [Probes](docs/developer-guide/collector/probes.md)
    - [Adding Probes](docs/developer-guide/collector/adding-probes.md)
    - [Probe Reference](docs/developer-guide/collector/probe-reference.md)
    - [pg_settings Usage](docs/developer-guide/collector/pg-settings-usage.md)
    - [Testing](docs/developer-guide/collector/testing.md)
  - Alerter:
    - [Architecture](docs/developer-guide/alerter/architecture.md)
    - [Anomaly Detection](docs/developer-guide/alerter/anomaly-detection.md)
    - [Adding Rules](docs/developer-guide/alerter/adding-rules.md)
    - [Cron Expressions](docs/developer-guide/alerter/cron-expressions.md)
    - [Testing](docs/developer-guide/alerter/testing.md)
  - Server:
    - [Architecture](docs/developer-guide/server/architecture.md)
  - Client:
    - [Architecture](docs/developer-guide/client/architecture.md)
  - End-to-End Testing:
    - [Overview](docs/developer-guide/e2e/index.md)
  - Design:
    - [Node Role Probe](docs/developer-guide/design/node-role-probe.md)
- [Changelog](docs/changelog.md)
- [Issues](#issues)
- [Contributing](#contributing)
- [License](#license)

## pgEdge AI DBA Workbench

The pgEdge AI DBA Workbench is a unified environment for monitoring and
management of any PostgreSQL v14+ instance, including Supabase and Amazon
RDS, with an optional AI agent. The Workbench watches every instance,
catches anomalies before they become outages, and walks through diagnosis
and resolution step by step.

The Workbench combines a Model Context Protocol (MCP) Server with a
web-based user interface and data collector. Users can query, analyze, and
manage distributed clusters using natural language and intelligent
automation. The Workbench exposes pgEdge tools and data sources such as
Spock replication status, cluster configuration, and operational metrics
to language models.

The architecture supports switching between cloud-connected LLMs like
Claude and locally hosted models from Ollama. This design ensures similar
levels of functionality in air-gapped or secure environments. The pgEdge
AI DBA Workbench bridges database administration and AI reasoning; it
offers an extensible foundation for observability, troubleshooting, and
intelligent workflow creation across the pgEdge ecosystem.

The pgEdge AI DBA Workbench consists of four main components:

- The [Collector](collector/README.md) monitors PostgreSQL servers and
  stores metrics in a centralized datastore.
- The [Server](server/README.md) provides MCP tools and resources for
  interacting with PostgreSQL systems.
- The [Alerter](alerter/README.md) evaluates collected metrics against
  thresholds and AI-powered anomaly detection to generate alerts.
- The [Client](client/README.md) provides a web-based user interface for
  the AI DBA Workbench.

The Workbench can be:

* installed with [binary files](#using-binary-files-to-install-workbench) from the Github repo.
* built from [source code](#building-workbench-from-source) from the Github repo.
* deployed in a [Docker container](#using-docker-to-install-workbench).
* installed with [packages from the pgEdge](https://docs.pgedge.com/enterprise/) repository.


### Using Binary Files to Install Workbench

Pre-built binary files for Workbench are available from the pgEdge repo at:
[https://github.com/pgEdge/ai-dba-workbench/releases](https://github.com/pgEdge/ai-dba-workbench/releases).

The `Quick Start - Installing with Binaries` guide contains detailed
instructions for using the binary files to install and configure
[the Workbench](docs/getting-started/binary_install.md).

### Building Workbench from Source

The Workbench can be built from source for local development or to
produce custom binaries.

The `Quick Start - Building from Source` guide contains detailed
instructions for cloning the repository, satisfying build dependencies,
and compiling the Workbench:
[Building from Source](docs/getting-started/build_from_source.md).

### Using Docker to Install Workbench

Pre-built container images for Workbench are published to the GitHub
Container Registry for each release.

The `Quick Start - Docker Deployment` guide contains detailed
instructions for deploying the Workbench using Docker Compose:
[Docker Deployment](docs/getting-started/docker.md).


## Issues

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

## Contributing

We welcome your project contributions; for more information, see
[docs/developer-guide/contributing.md](docs/developer-guide/contributing.md).

For more information, visit [docs.pgedge.com](https://docs.pgedge.com).

## License

This project is licensed under the
[PostgreSQL License](LICENSE.md).
