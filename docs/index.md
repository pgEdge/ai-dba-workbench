# pgEdge AI DBA Workbench

An AI-powered environment for monitoring, managing, and
troubleshooting PostgreSQL systems.

## Overview

The pgEdge AI DBA Workbench combines a Model Context
Protocol (MCP) server with a web-based user interface,
data collector, and alert monitoring service. The Workbench
enables users to query, analyze, and manage distributed
PostgreSQL clusters using natural language and intelligent
automation. The system exposes pgEdge tools and data
sources to both cloud-connected and locally hosted language
models, ensuring full functionality in air-gapped or
secure environments.

## Architecture

The AI DBA Workbench consists of four components that
work together to provide monitoring, alerting, and
AI-powered database management.

### Data Collection Layer

The Collector continuously monitors PostgreSQL servers
and collects metrics into a centralized datastore. The
Collector provides the following features:

- The Collector supports multi-server monitoring with
  independent connection pools.
- 34 built-in probes cover PostgreSQL system views.
- Automated data management handles partitioning and
  retention policies.
- Secure connections use encryption and SSL/TLS support.

### Intelligence Layer

The MCP Server implements the Model Context Protocol and
provides AI assistants with standardized access to
PostgreSQL systems. The MCP Server provides the following
features:

- The server uses HTTP/HTTPS transport with JSON-RPC 2.0.
- SQLite-based authentication supports users and tokens.
- Role-based access control manages groups and privileges.
- Database tools enable queries, schema introspection,
  and analysis.
- The LLM proxy supports Anthropic, OpenAI, Gemini, and
  Ollama providers.
- Conversation history management preserves chat context.

### Alert Monitoring Layer

The Alerter evaluates collected metrics against thresholds
and uses AI-powered anomaly detection to generate alerts.
The Alerter provides the following features:

- Threshold-based alerting supports configurable rules.
- Tiered anomaly detection uses statistical analysis,
  embeddings, and LLM classification.
- Blackout scheduling accommodates maintenance windows.
- Notification delivery integrates with email, Slack,
  Mattermost, and webhooks.

### Presentation Layer

The Client provides a web-based user interface for cluster
monitoring and management. The Client provides the
following features:

- Hierarchical dashboards display estate, cluster, and
  server metrics.
- Cluster topology visualization shows replication edges.
- The AI-powered chat interface supports natural language
  queries.
- The administration panel manages users, groups, and
  tokens.

## Where to Start

Choose a section based on your role and goals.

- The [Quick Start](getting-started/quick-start.md) guide
  helps new users set up the Workbench for the first time.
- The [User Guide](user-guide/index.md) covers dashboards,
  alerts, and AI features for day-to-day usage.
- The [Administrator's Guide](admin-guide/index.md)
  explains authentication, connections, and system
  configuration.
- The [Developer's Guide](developer-guide/index.md)
  provides architecture details and contribution
  guidelines for each component.

## System Requirements

The Workbench requires the following software and
resources.

- [PostgreSQL 14](https://www.postgresql.org/download/)
  or higher is required for the datastore.
- [Go 1.24](https://go.dev/doc/install) or higher is
  required to build server-side components from source.
- [Node.js 18](https://nodejs.org/) or higher is required
  to build the web client from source.
- Network connectivity must exist between all components.
- Database credentials must have appropriate permissions.

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.

This software is released under the
[PostgreSQL License](LICENSE.md).
