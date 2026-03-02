# AI DBA Workbench Client

[![CI - Client](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/ci-client.yml)

The web client for the pgEdge AI DBA Workbench provides
a browser-based interface for cluster monitoring and
management.

For complete documentation, visit
[docs.pgedge.com](https://docs.pgedge.com).

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Building](#building)
- [Testing](#testing)
- [Configuration](#configuration)
- [Documentation](#documentation)

## Features

The client provides the following capabilities:

- The application uses React with TypeScript for type
  safety and maintainability.
- Hierarchical monitoring dashboards display estate,
  cluster, server, and database metrics.
- The cluster navigator shows servers with color-coded
  replication edges.
- The AI chat interface supports natural language
  queries through multiple LLM providers.
- The administration panel manages users, groups,
  tokens, probes, and alert rules.
- Light and dark themes persist across browser sessions.

## Prerequisites

Before starting development, install the following tools:

- [Node.js 18](https://nodejs.org/) or later.
- [npm 9](https://docs.npmjs.com/) or later.

## Getting Started

Install the project dependencies:

```bash
npm install
```

Start the development server:

```bash
npm run dev
```

The application starts at `http://localhost:5173` by
default.

## Building

Create a production build with the following command:

```bash
npm run build
```

The build process generates output in the `dist`
directory.

## Testing

Run the test suite with the following command:

```bash
npm test
```

Run tests in watch mode for development:

```bash
npm run test:watch
```

Generate a coverage report:

```bash
npm run test:coverage
```

## Configuration

The development server proxies API requests to the MCP
server running on port 8080. Configure the proxy in
`vite.config.js` if the server runs on a different port.

## Documentation

For detailed documentation, see the
[Developer's Guide](../docs/developer-guide/index.md).

---

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

We welcome your project contributions; for more
information, see
[docs/developer-guide/contributing.md](../docs/developer-guide/contributing.md).

For more information, visit
[docs.pgedge.com](https://docs.pgedge.com).

This project is licensed under the
[PostgreSQL License](../LICENSE.md).
