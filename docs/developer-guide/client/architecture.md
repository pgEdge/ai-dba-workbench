# Client Architecture

The pgEdge AI DBA Workbench web client provides a
browser-based interface for monitoring and managing
PostgreSQL database estates. This page describes the
component structure, state management, and development
workflow for contributors.

## Overview

The web client includes the following features:

- Hierarchical monitoring dashboards display metrics at
  the estate, cluster, server, database, and object
  levels.
- The cluster navigator provides tree-based navigation
  across groups, clusters, and individual servers.
- An alert panel displays active alerts with grouped
  views and LLM-powered analysis.
- The event timeline shows configuration changes,
  restarts, and other notable events across selected
  servers.
- An AI overview panel presents LLM-generated summaries
  of database health and status.
- The admin panel manages users, groups, tokens, probes,
  alert rules, and notification channels.

## Technology Stack

The client uses the following core technologies:

- React provides the component framework.
- TypeScript adds static type checking to all source
  files.
- Material UI (MUI) supplies the component library and
  theming system.
- Vite handles bundling and development server duties.
- Vitest runs the unit test suite.

## Component Structure

The source code follows a feature-based directory layout
under `client/src/`.

```
client/src/
  components/
    AdminPanel/       Admin panel tabs and dialogs
    Dashboard/        Monitoring dashboard hierarchy
    ClusterNavigator/ Tree-based server navigation
    AlertPanel/       Alert display and analysis
    EventTimeline/    Event history display
    AIOverview/       LLM-generated health summaries
  hooks/              Custom React hooks
  services/           API client functions
  types/              TypeScript type definitions
  utils/              Shared utility functions
```

### Dashboard Hierarchy

The dashboard components form a five-level hierarchy:

- The estate dashboard shows aggregate metrics across
  all monitored servers.
- The cluster dashboard displays metrics for a group of
  related servers.
- The server dashboard presents metrics for a single
  PostgreSQL instance.
- The database dashboard shows metrics for one database
  on a server.
- The object dashboard provides table-level and
  index-level detail.

### Admin Panel

The admin panel uses expandable rows with a shared
`EffectivePermissionsPanel` component. Each tab (Users,
Groups, Tokens) follows the same interaction pattern for
consistency.

## Material UI Usage

The client uses MUI components with a custom theme.
Shared style constants live in
`client/src/components/Dashboard/styles.ts`.

The following conventions apply to MUI usage:

- Use `0.875rem` as the standard font size for labels
  and data values.
- Do not apply `KPI_LABEL_SX` to data values because
  the style includes uppercase and letter-spacing meant
  only for labels.
- Apply `mb: 5` (40px) bottom margin to sparkline
  containers in compact cards.

## State Management

The client manages state through React hooks and context
providers. Components fetch data from the server API and
store results in local state. The application does not
use a global state management library.

Custom hooks encapsulate common data-fetching patterns
and provide loading, error, and refresh states to
consuming components.

## API Integration

The services layer contains functions that communicate
with the MCP server REST API. Each service function
handles request construction, authentication headers,
and response parsing.

The client authenticates through session tokens obtained
from the `/api/v1/auth/login` endpoint. The token is
included in the `Authorization` header of subsequent
requests.

### LLM Integration

The AI overview and alert analysis features use the
server's LLM proxy at `/api/v1/llm`. The client sends
requests through the proxy and processes streamed
responses for display.

## Build and Development Workflow

### Prerequisites

Install the following tools before starting development:

- [Node.js 18](https://nodejs.org/) or later.
- npm (included with Node.js).

### Installation

Install the project dependencies from the client
directory.

In the following example, the `npm install` command
downloads all required packages:

```bash
cd client && npm install
```

### Development Server

Start the development server with hot module replacement.

In the following example, the `npm run dev` command
launches the Vite development server:

```bash
cd client && npm run dev
```

### Production Build

Create an optimized production build.

In the following example, the `npm run build` command
generates static assets in the `dist/` directory:

```bash
cd client && npm run build
```

### Running Tests

Run the unit test suite with Vitest.

In the following example, the `npx vitest run` command
executes all tests:

```bash
cd client && npx vitest run
```

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.

This software is released under The PostgreSQL License.
