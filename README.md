# pgEdge AI DBA Workbench

[![CI - Alerter](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-alerter.yml)
[![CI - Client](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml)
[![CI - Collector](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-collector.yml)
[![CI - Docker](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docker.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docker.yml)
[![CI - Docs](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-docs.yml)
[![CI - E2E](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-e2e.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-e2e.yml)
[![CI - Server](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-server.yml)


## Table of Contents

- [pgEdge AI DBA Workbench Architecture Overview](docs/index.md)
- [Using Binary Files to Install Workbench](#using-binary-files-to-install-workbench)
- [Building Workbench from Source](#building-workbench-from-source)
- Installing pgEdge AI DBA Workbench:
  - [Supported Installation Methods](docs/getting-started/installation_overview.md)
  - [Installing the Workbench from Binary Files](docs/getting-started/binary_install.md)
  - [Building the Workbench from Source Code](docs/getting-started/build_from_source.md)
  - [Deploying the Workbench with Docker](docs/getting-started/docker.md)
- Configuring the Workbench:
  - [Configuring the Server](docs/configuration/server/index.md)
    - [Configuration File Properties](docs/configuration/server/configuration_file.md)
    - [Command-Line Options](docs/configuration/server/command_line_options.md)
    - [Environment Variables](docs/configuration/server/environment_variables.md)
    - [Configuring Single Sign-On](docs/configuration/server/single_sign_on.md)
    - [Configuration Examples](docs/configuration/server/configuration_examples.md)
  - [Configuring the Collector](docs/configuration/collector/index.md)
    - [Configuration File Properties](docs/configuration/collector/configuration_file.md)
    - [Command-Line Flags](docs/configuration/collector/command_line_flags.md)
    - [Configuration Examples](docs/configuration/collector/configuration_examples.md)
    - [Storing Metrics in pgEdge ColdFront](docs/configuration/collector/coldfront-metrics.md)
  - [Configuring the Alerter](docs/configuration/alerter/index.md)
    - [Configuration File Properties](docs/configuration/alerter/configuration_file.md)
    - [Anomaly Detection Properties](docs/configuration/alerter/anomaly_detection.md)
    - [LLM Provider Properties](docs/configuration/alerter/llm_providers.md)
    - [Command-Line Flags](docs/configuration/alerter/command_line_flags.md)
  - [Configuring the Web Client](docs/configuration/client.md)
  - [Configuring systemd Services](docs/configuration/configure_systemd.md)
- Using the Workbench:
  - [Using the Workbench](docs/user-guide/index.md)
  - [Reviewing Dashboards](docs/user-guide/dashboards/index.md)
    - [Estate Dashboard](docs/user-guide/dashboards/estate.md)
    - [Server Dashboard](docs/user-guide/dashboards/server.md)
    - [Cluster Dashboard](docs/user-guide/dashboards/cluster.md)
    - [Reviewing Database Details](docs/user-guide/dashboards/database.md)
    - [Reviewing Object Details](docs/user-guide/dashboards/object.md)
  - [Using AI Features](docs/user-guide/ai/index.md)
    - [Enabling AI Mode](docs/user-guide/ai/enabling_ai.md)
    - [Ask Ellie](docs/user-guide/ai/ask-ellie.md)
    - [Using the AI Chart Analysis Feature](docs/user-guide/ai/chart-analysis.md)
    - [AI Alert Analysis](docs/user-guide/ai/ai-analysis.md)
    - [Connecting MCP Clients](docs/user-guide/ai/mcp-clients.md)
    - [Using Workbench with MCP Tools](docs/user-guide/ai/mcp-tools.md)
  - [Managing Alerts](docs/user-guide/alerts/index.md)
    - [Alert Reference](docs/user-guide/alerts/rule-reference.md)
    - [Managing Blackouts](docs/user-guide/alerts/blackouts.md)
- Administrator's Guide:
  - [Administrator's Guide](docs/admin-guide/index.md)
  - Managing Users and Permissions:
    - [Understanding the Permission Model](docs/admin-guide/managing-users-and-permissions/permission_model.md)
    - [Managing Permissions](docs/admin-guide/managing-users-and-permissions/permission_mgmt.md)
    - [Managing Accounts](docs/admin-guide/managing-users-and-permissions/accounts.md)
    - [Managing Groups](docs/admin-guide/managing-users-and-permissions/groups.md)
    - [Managing Tokens](docs/admin-guide/managing-users-and-permissions/tokens.md)
    - [Managing Single Sign-On](docs/admin-guide/managing-users-and-permissions/sso.md)
  - [Preparing Monitored Servers](docs/admin-guide/monitored-database-privileges.md)
  - [Managing Alert Rules](docs/admin-guide/alert-rules/index.md)
    - [Understanding Alert Behavior](docs/admin-guide/alert-rules/alert_behavior.md)
  - [Managing Probes](docs/admin-guide/probes/index.md)
    - [Understanding Probe Behavior](docs/admin-guide/probes/probe_behavior.md)
  - [Managing the Audit Log](docs/admin-guide/audit-log.md)
  - [Managing Notification Channels](docs/admin-guide/notification-channels/index.md)
    - [Configuring Email Channels](docs/admin-guide/notification-channels/email.md)
    - [Configuring Slack Channels](docs/admin-guide/notification-channels/slack.md)
    - [Configuring Mattermost Channels](docs/admin-guide/notification-channels/mattermost.md)
    - [Configuring Telegram Channels](docs/admin-guide/notification-channels/telegram.md)
    - [Configuring Webhook Channels](docs/admin-guide/notification-channels/webhook.md)
  - [Enforcing the Password Policy](docs/admin-guide/password.md)
  - [Connection Management](docs/admin-guide/connections.md)
  - [TLS and Reverse Proxy Requirements](docs/admin-guide/tls-and-reverse-proxy.md)
  - [Verifying the Health of a Component](docs/admin-guide/verify_health.md)
  - [Upgrading the Workbench](docs/admin-guide/upgrading.md)
  - [Upgrading to 2.0.0](docs/admin-guide/upgrading-to-2.0.md)
  - REST API:
    - [API Reference](docs/admin-guide/api/reference.md)
    - [API Browser](docs/admin-guide/api/browser.md)
    - [Server Information](docs/admin-guide/api/server-info.md)
    - [Metrics Tools](docs/admin-guide/api/metrics.md)
- Troubleshooting:
  - [Overview](docs/troubleshooting/index.md)
  - [Stale Server Status](docs/troubleshooting/stale-server-status.md)
  - [Configuration File Not Found](docs/troubleshooting/config-file-not-found.md)
  - [Configuration Parse Errors](docs/troubleshooting/config-parse-error.md)
  - [Too Many Connections](docs/troubleshooting/too-many-connections.md)
  - [Connection Timeouts](docs/troubleshooting/connection-timeout.md)
  - [ColdFront Metrics Storage Errors](docs/troubleshooting/coldfront-errors.md)
- Developer's Guide:
  - [Developer's Guide](docs/developer-guide/index.md)
  - [Contributing](docs/developer-guide/contributing.md)
  - Collector:
    - [Collector Architecture](docs/developer-guide/collector/architecture.md)
    - [Database Schema](docs/developer-guide/collector/schema.md)
    - [Schema Management](docs/developer-guide/collector/schema-management.md)
    - [Scheduler Implementation](docs/developer-guide/collector/scheduler.md)
    - [Probes System](docs/developer-guide/collector/probes.md)
    - [Adding New Probes](docs/developer-guide/collector/adding-probes.md)
    - [Probe Reference](docs/developer-guide/collector/probe-reference.md)
    - [pg_settings Probe Usage Guide](docs/developer-guide/collector/pg-settings-usage.md)
    - [Testing and Development](docs/developer-guide/collector/testing.md)
  - Alerter:
    - [Alerter Architecture](docs/developer-guide/alerter/architecture.md)
    - [Anomaly Detection](docs/developer-guide/alerter/anomaly-detection.md)
    - [Adding Alert Rules](docs/developer-guide/alerter/adding-rules.md)
    - [Cron Expression Format](docs/developer-guide/alerter/cron-expressions.md)
    - [Testing and Development](docs/developer-guide/alerter/testing.md)
  - Server:
    - [Server Architecture](docs/developer-guide/server/architecture.md)
  - Client:
    - [Client Architecture](docs/developer-guide/client/architecture.md)
  - End-to-End Testing:
    - [End-to-End Smoke Tests](docs/developer-guide/e2e/index.md)
  - Design:
    - [Node Role Probe Design](docs/developer-guide/design/node-role-probe.md)
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

The
[Installing the Workbench from Binary Files](docs/getting-started/binary_install.md)
guide contains detailed instructions for using the binary files to install
and configure the Workbench.

### Building Workbench from Source

The Workbench can be built from source for local development or to
produce custom binaries.

The
[Building the Workbench from Source Code](docs/getting-started/build_from_source.md)
guide contains detailed instructions for cloning the repository, satisfying
build dependencies, and compiling the Workbench.

### Using Docker to Install Workbench

Pre-built container images for Workbench are published to the GitHub
Container Registry for each release.

The [Deploying the Workbench with Docker](docs/getting-started/docker.md) guide
contains detailed instructions for deploying the Workbench using Docker
Compose.


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
