# MCP Server Documentation

The pgEdge AI DBA Workbench MCP Server implements the Model Context Protocol
(MCP), providing AI assistants with standardized access to PostgreSQL systems
through HTTP/HTTPS endpoints.

## Overview

The MCP server acts as a bridge between AI language models and PostgreSQL
databases, enabling natural language interaction with your data. Key features
include:

- **MCP Protocol**: Full JSON-RPC 2.0 implementation over HTTP/HTTPS
- **Authentication**: SQLite-based user management with session and service
  tokens
- **Database Tools**: Query execution, schema introspection, and analysis
- **LLM Proxy**: Optional server-side LLM integration for web clients
- **Conversation History**: Persistent storage for chat sessions

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

1. **Create a user account**:

   ```bash
   ./bin/ai-dba-server -add-user -username admin
   ```

   You'll be prompted for a password.

2. **Create a service token** (optional, for API access):

   ```bash
   ./bin/ai-dba-server -add-token -token-note "My API Token" -token-expiry "90d"
   ```

   Save the displayed token - it won't be shown again.

3. **Start the server**:

   ```bash
   ./bin/ai-dba-server
   ```

   The server starts on port 8080 by default.

4. **Test the connection**:

   ```bash
   # Using a service token
   curl -X POST http://localhost:8080/mcp/v1 \
     -H "Authorization: Bearer YOUR_TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}'
   ```

## Documentation Sections

### Authentication

- [Authentication Guide](authentication.md) - Complete guide to user management,
  tokens, and security features

### Configuration

- [Configuration Guide](configuration.md) - Server configuration options,
  environment variables, and examples

### Connection Management

- [Connection Management](connections.md) - Managing database connections from
  the datastore

## Architecture

The MCP server is built with the following components:

```
┌─────────────────────────────────────────────────────────────────┐
│                         HTTP Server                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │   /mcp/v1    │  │  /api/llm/*  │  │  /api/conversations  │   │
│  │  MCP Endpoint │  │  LLM Proxy   │  │  History Storage     │   │
│  └──────┬───────┘  └──────┬───────┘  └──────────┬───────────┘   │
└─────────┼─────────────────┼─────────────────────┼───────────────┘
          │                 │                     │
          ▼                 ▼                     ▼
┌─────────────────┐  ┌─────────────┐  ┌───────────────────────┐
│   MCP Server    │  │ LLM Clients │  │  Conversation Store   │
│                 │  │             │  │     (SQLite)          │
│ ┌─────────────┐ │  │ - Anthropic │  └───────────────────────┘
│ │   Tools     │ │  │ - OpenAI   │
│ ├─────────────┤ │  │ - Ollama   │
│ │  Resources  │ │  └─────────────┘
│ ├─────────────┤ │
│ │   Prompts   │ │
│ └─────────────┘ │
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Authentication Layer                        │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                   Auth Store (SQLite)                     │   │
│  │  ┌──────────┐  ┌─────────────────┐  ┌────────────────┐   │   │
│  │  │  Users   │  │ Session Tokens  │  │ Service Tokens │   │   │
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

- **HTTP Server**: Handles incoming requests and routes to appropriate handlers
- **MCP Server**: Implements the Model Context Protocol with tools, resources,
  and prompts
- **Auth Store**: SQLite database managing users, sessions, and service tokens
- **LLM Proxy**: Optional feature to proxy LLM requests for web clients
- **Conversation Store**: SQLite database for persistent chat history
- **Database Pool**: Per-session connection management for security isolation

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

See [Metrics Tools](metrics.md) for detailed documentation on querying
collected metrics.

### Utility Tools

| Tool | Description |
|------|-------------|
| `generate_embedding` | Generate text embeddings |
| `search_knowledgebase` | Search documentation knowledgebase |

## Available Resources

| Resource | Description |
|----------|-------------|
| `pg://system_info` | PostgreSQL server information (version, platform) |
| `pg://connection_info` | Current database connection details |

## Available Prompts

| Prompt | Description |
|--------|-------------|
| `explore-database` | Guide for exploring database schema |
| `setup-semantic-search` | Configure vector similarity search |
| `diagnose-query-issue` | Analyze query performance problems |
| `design-schema` | Database schema design assistance |

## Security

The server implements multiple security layers:

- **Authentication required**: All MCP endpoints require valid tokens
- **Password hashing**: Bcrypt with cost factor 12
- **Token hashing**: SHA256 for secure storage
- **Rate limiting**: Per-IP protection against brute force attacks
- **Account lockout**: Automatic disabling after failed login attempts
- **Session isolation**: Each token gets its own database connection pool

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.

This software is released under The PostgreSQL License.
