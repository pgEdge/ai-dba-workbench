# pgEdge AI DBA Workbench

[![CI - Alerter](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml)
[![CI - Client](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml)
[![CI - Collector](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml)
[![CI - Docker](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docker.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docker.yml)
[![CI - Docs](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml)
[![CI - Server](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml)


## Table of Contents

- [pgEdge AI DBA Workbench](#pgedge-ai-dba-workbench)
- [Installing pgEdge AI DBA Workbench](#installing-pgedge-ai-dba-workbench)
  - [Using Binary Files to Install Workbench](#using-binary-files-to-install-workbench)
  - [Building AI DBA Workbench from Source Code](#building-ai-dba-workbench-from-source-code)
- [Running the AI DBA Workbench Tests](#running-the-ai-dba-workbench-tests)
  - [Using Environment Variables for Testing](#using-environment-variables-for-testing)
- Installation and Configuration:
  - [Quick Start Guide](docs/getting-started/quick-start.md)
  - [Installation](docs/getting-started/installation.md)
  - [Docker Deployment](docs/getting-started/docker.md)
  - Configuring the Workbench:
    - [Configuring the Server](docs/getting-started/configuration/server.md)
    - [Configuring the Collector](docs/getting-started/configuration/collector.md)
    - [Configuring the Alerter](docs/getting-started/configuration/alerter.md)
    - [Configuring the Web Client](docs/getting-started/configuration/client.md)
- User Guide:
  - [Using the Workbench](docs/user-guide/index.md)
  - Monitoring Dashboards:
    - [Dashboard Overview](docs/user-guide/dashboards/index.md)
    - [Estate Dashboard](docs/user-guide/dashboards/estate.md)
    - [Cluster Dashboard](docs/user-guide/dashboards/cluster.md)
    - [Server Dashboard](docs/user-guide/dashboards/server.md)
    - [Database Dashboard](docs/user-guide/dashboards/database.md)
    - [Object Dashboard](docs/user-guide/dashboards/object.md)
  - Alerts:
    - [Understanding Alerts](docs/user-guide/alerts/index.md)
    - [Alert Reference](docs/user-guide/alerts/rule-reference.md)
    - [AI Alert Analysis](docs/user-guide/alerts/ai-analysis.md)
  - [Blackout Management](docs/user-guide/blackouts.md)
  - AI Features:
    - [AI Overview](docs/user-guide/ai/overview.md)
    - [Ask Ellie](docs/user-guide/ai/ask-ellie.md)
    - [Connecting MCP Clients](docs/user-guide/ai/mcp-clients.md)
  - [MCP Tools](docs/user-guide/mcp-tools.md)
- Administrator's Guide:
  - [Overview](docs/admin-guide/index.md)
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
  - Collector Architecture:
    - [Architecture](docs/developer-guide/collector/architecture.md)
    - [Schema Design](docs/developer-guide/collector/schema.md)
    - [Schema Management](docs/developer-guide/collector/schema-management.md)
    - [Scheduler](docs/developer-guide/collector/scheduler.md)
    - [Probes](docs/developer-guide/collector/probes.md)
    - [Adding Probes](docs/developer-guide/collector/adding-probes.md)
    - [Probe Reference](docs/developer-guide/collector/probe-reference.md)
    - [pg_settings Usage](docs/developer-guide/collector/pg-settings-usage.md)
    - [Testing](docs/developer-guide/collector/testing.md)
  - Alerter Architecture:
    - [Architecture](docs/developer-guide/alerter/architecture.md)
    - [Anomaly Detection](docs/developer-guide/alerter/anomaly-detection.md)
    - [Adding Rules](docs/developer-guide/alerter/adding-rules.md)
    - [Cron Expressions](docs/developer-guide/alerter/cron-expressions.md)
    - [Testing](docs/developer-guide/alerter/testing.md)
  - Server Architecture:
    - [Architecture](docs/developer-guide/server/architecture.md)
  - Client Architecture:
    - [Architecture](docs/developer-guide/client/architecture.md)
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


## Installing pgEdge AI DBA Workbench

Before installing the Workbench with binary files or building the project from
source, install the following software:

- [Go 1.24](https://go.dev/doc/install) or later for building server-side
  components.
- [Node.js 18](https://nodejs.org/) or later for building the web client.
- [PostgreSQL 14](https://www.postgresql.org/download/) or later for the
  datastore.
- [Make](https://www.gnu.org/software/make/) for build automation.


### Using Binary Files to Install Workbench

Pre-built binary files for Workbench are available from the pgEdge repo at:
[https://github.com/pgEdge/ai-dba-workbench/releases](https://github.com/pgEdge/ai-dba-workbench/releases).

The Quick Start Guide contains detailed instructions for using the binary
files to install and configure 
[the Workbench](docs/getting-started/quick-start.md). 


### Building AI DBA Workbench from Source Code

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

After building the project, you'll need to configure each component; for
detailed configuration instructions for each component, see the following
documentation:

- The [Server Configuration](docs/getting-started/configuration/server.md)
  reference covers all server options.
- The [Collector Configuration](docs/getting-started/configuration/collector.md)
  reference covers all collector options.
- The [Alerter Configuration](docs/getting-started/configuration/alerter.md)
  reference covers all alerter options.


## Running the AI DBA Workbench Tests

The project includes unit tests for each component. Use the following
commands to run the full test suite:

```bash
make test
make coverage
make lint
make test-all
```

To run tests for an individual component, use the following command:

```bash
cd collector && make test
```

Each sub-project and the top-level Makefile supports the following targets:

- `all` builds the project and is the default target.
- `test` runs the test suite.
- `coverage` runs tests with a coverage report.
- `lint` runs the linter.
- `test-all` runs tests, coverage, and the linter.
- `clean` removes build artifacts.
- `killall` kills any running processes.
- `help` shows the available targets.

### Using Environment Variables for Testing

The following environment variables control test behavior:

- `TEST_AI_WORKBENCH_SERVER` specifies the PostgreSQL connection string
  for the test database; the default is
  `postgres://postgres@localhost:5432/postgres`.
- `TEST_AI_WORKBENCH_KEEP_DB=1` preserves the test database after tests
  complete.


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
