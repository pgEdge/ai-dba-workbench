# pgEdge AI Workbench

The pgEdge AI Workbench is a unified environment for interacting with pgEdge's
distributed and non-distributed PostgreSQL systems through artificial
intelligence and traditional methods. It combines a Model Context Protocol
(MCP) Server with a web-based user interface and data collector, enabling users
 to query, analyze, and manage distributed clusters using natural language and
 intelligent automation. The Workbench exposes pgEdge tools and data sources
 — such as Spock replication status, cluster configuration, and operational
 metrics — to either hosted or both hosted and locally running language models.
 Its architecture supports seamless switching between cloud-connected LLMs like
 Claude and locally hosted models from Ollama, ensuring the similar levels of
 functionality in air-gapped or secure environments. In essence, the pgEdge AI
 Workbench bridges the gap between database administration and AI reasoning,
 offering an extensible foundation for observability, troubleshooting, and
 intelligent workflow creation across the pgEdge ecosystem.

## Components

The pgEdge AI Workbench consists of three main components:

- **[Collector](collector/README.md)** - A monitoring service that collects
  metrics from PostgreSQL servers and stores them in a centralized datastore
  for analysis
- **[Server](server/README.md)** - An MCP server that provides tools and
  resources for interacting with PostgreSQL systems
- **[CLI](cli/README.md)** - A command-line interface for interacting with
  the MCP server
- **Client** - A web-based user interface for interacting with the AI
  Workbench (coming soon)

## Documentation

Comprehensive documentation is available in the [docs](docs/index.md)
directory:

- **[Documentation Index](docs/index.md)** - Main documentation entry point
- **[Collector Documentation](docs/collector/index.md)** - Data collection
  and monitoring
- **[Server Documentation](docs/server/index.md)** - MCP server and protocol
- **[CLI Documentation](docs/cli/index.md)** - Command-line interface

## Getting Started

For information on getting started with each component, please refer to:

- [Collector Quick Start](docs/collector/quickstart.md) - Set up monitoring
- [Server README](server/README.md) - Deploy the MCP server
- [CLI README](cli/README.md) - Use the command-line interface