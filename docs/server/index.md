# MCP Server Documentation

The pgEdge AI DBA Workbench MCP Server implements the Model Context Protocol
(MCP), providing AI assistants with standardized access to PostgreSQL systems
through HTTP/HTTPS endpoints.

## Overview

The MCP server acts as a bridge between AI language models and PostgreSQL
databases, enabling natural language interaction with your data. Key features
include:

- Full JSON-RPC 2.0 implementation over HTTP/HTTPS for MCP
  protocol.
- SQLite-based user management with session and API tokens
  for authentication.
- Role-based access control with groups, privileges, and
  token scopes for fine-grained authorization.
- Admin panel for managing users, groups, and tokens through
  the web client.
- Query execution, schema introspection, and analysis through
  database tools.
- Optional server-side LLM integration for web clients via
  LLM proxy.
- Persistent storage for chat sessions provides conversation
  history.
- Notification channel management for configuring alert delivery
  through Email, Slack, Mattermost, and Webhook channels.
- Blackout management for suppressing alerts during planned
  maintenance with support for hierarchical scopes and
  recurring schedules.

## Getting Started

### Prerequisites

- Go 1.23 or higher
- PostgreSQL 14 or higher (for database features)
- Optional: LLM API keys (Anthropic, OpenAI) or Ollama

### Installation

Build from source:

```bash
cd server
make build
```

The binary is created at `bin/ai-dba-server`.

### First Run

1. Create a user account:

   ```bash
   ./bin/ai-dba-server -add-user -username admin
   ```

   The system prompts you for a password.

2. Create a service token (optional, for API access):

   ```bash
   ./bin/ai-dba-server -add-token -token-note "My API Token" -token-expiry "90d"
   ```

   Save the displayed token because the system will not show it again.

3. Start the server:

   ```bash
   ./bin/ai-dba-server
   ```

   The server starts on port 8080 by default.

4. Test the connection:

   ```bash
   # Using a service token
   curl -X POST http://localhost:8080/mcp/v1 \
     -H "Authorization: Bearer YOUR_TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}'
   ```

## Documentation Sections

### API Reference

- [API Reference](api-reference.md) - Interactive OpenAPI documentation with
  endpoint details, request/response schemas, and examples

### Authentication

- [Authentication Guide](authentication.md) - Complete guide to user management,
  tokens, and security features

### Configuration

- [Configuration Guide](configuration.md) - Server configuration options,
  environment variables, and examples

### Connection Management

- [Connection Management](connections.md) - Managing database connections from
  the datastore

### Notification Channels

- [Notification Channels](notification-channels.md) - Configure
  alert delivery through Email, Slack, Mattermost, and Webhook
  channels

### Blackout Management

- [Blackout Management](blackouts.md) - Suppress alerts during
  maintenance with one-time blackouts and recurring schedules

## Architecture

The MCP server is built with the following components:

```
┌───────────────────────────────────────────────────────────────────────┐
│                            HTTP Server                                 │
│  ┌────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────────┐  │
│  │  /mcp/v1   │  │/api/v1/auth │  │/api/v1/llm  │  │   /api/v1/*   │  │
│  │MCP Protocol│  │    Login    │  │  LLM Proxy  │  │  Connections, │  │
│  │            │  │             │  │             │  │ Conversations │  │
│  └─────┬──────┘  └──────┬──────┘  └──────┬──────┘  └───────┬───────┘  │
└────────┼───────────────┼───────────────┼─────────────────┼────────────┘
         │               │               │                 │
         ▼               │               ▼                 │
┌─────────────────┐      │        ┌─────────────┐          │
│   MCP Server    │      │        │ LLM Clients │          │
│                 │      │        │             │          │
│ ┌─────────────┐ │      │        │ - Anthropic │          │
│ │   Tools     │ │      │        │ - OpenAI    │          │
│ ├─────────────┤ │      │        │ - Ollama    │          │
│ │  Resources  │ │      │        └─────────────┘          │
│ ├─────────────┤ │      │                                 │
│ │   Prompts   │ │      │                                 │
│ └─────────────┘ │      │                                 │
└────────┬────────┘      │                                 │
         │               │                                 │
         ▼               ▼                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Authentication Layer                        │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                   Auth Store (SQLite)                     │   │
│  │  ┌──────────┐  ┌─────────────────┐  ┌────────────────┐   │   │
│  │  │  Users   │  │ Session Tokens  │  │    Tokens      │   │   │
│  │  └──────────┘  └─────────────────┘  └────────────────┘   │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Database Connection Pool                      │
│                   (Per-Session Isolation)                        │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
                      ┌─────────────┐
                      │ PostgreSQL  │
                      └─────────────┘
```

### Key Components

- The HTTP server handles incoming requests and routes them to handlers.
- The MCP server implements Model Context Protocol with tools, resources,
  and prompts.
- The auth store is a SQLite database managing users, sessions,
  tokens, groups, and token scopes.
- The RBAC system enforces access control through group
  memberships, privileges, and token scope restrictions.
- The LLM proxy is an optional feature to proxy LLM requests
  for web clients.
- The conversation store is a SQLite database for persistent chat history.
- The database pool provides per-session connection management for isolation.

## Available Tools

The MCP server exposes these built-in tools:

### Monitored Database Tools

These tools operate on the currently selected monitored database connection:

| Tool | Description |
|------|-------------|
| `query_database` | Execute SQL queries (read-only by default) |
| `get_schema_info` | Get table and column information |
| `execute_explain` | Run EXPLAIN/EXPLAIN ANALYZE on queries |
| `similarity_search` | Vector similarity search (requires pgvector) |
| `count_rows` | Count rows in tables |

### Datastore Tools

These tools query the metrics datastore for historical data collected by the
collector:

| Tool | Description |
|------|-------------|
| `list_probes` | List available metrics probes in the datastore |
| `describe_probe` | Get column details for a specific metrics probe |
| `query_metrics` | Query historical metrics with time-based aggregation |
| `list_connections` | List available monitored database connections |

See [Metrics Tools](metrics.md) for detailed documentation on querying
collected metrics.

### Utility Tools

| Tool | Description |
|------|-------------|
| `generate_embedding` | Generate text embeddings |
| `search_knowledgebase` | Search documentation knowledgebase |

## Available Resources

| Resource URI | Description |
|--------------|-------------|
| `pg://system_info` | PostgreSQL server information (version, platform) |
| `pg://connection_info` | Current database connection details |

Resources can be disabled in configuration; see
[Configuration](configuration.md) for details.

## Available Prompts

| Prompt | Description |
|--------|-------------|
| `explore-database` | Guide for exploring database schema |
| `setup-semantic-search` | Configure vector similarity search |
| `diagnose-query-issue` | Analyze query performance problems |
| `design-schema` | Database schema design assistance |

## Security

The server implements multiple security layers:

- All MCP endpoints require valid tokens for authentication.
- Password hashing uses Bcrypt with cost factor 12.
- Token hashing uses SHA256 for secure storage.
- Per-IP rate limiting protects against brute force attacks.
- Automatic account lockout occurs after failed login attempts.
- Each token gets its own database connection pool for session isolation.

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.

This software is released under The PostgreSQL License.
