# Server Architecture

The MCP server bridges AI language models and PostgreSQL
databases through the Model Context Protocol. This page
describes the internal architecture, transport layer, and
extension points that developers should understand when
contributing to the server codebase.

## MCP Protocol Implementation

The server implements the Model Context Protocol using
JSON-RPC 2.0 over HTTP/HTTPS. All MCP requests arrive at
the `/mcp/v1` endpoint and follow the standard JSON-RPC
request/response cycle.

The MCP layer registers two categories of capabilities:

- Tools expose actions such as query execution, schema
  introspection, and metrics retrieval.
- Resources provide read-only data like server information
  and connection details.

## HTTP Server and Routing

The HTTP server routes incoming requests to four main
handler groups.

```
/mcp/v1         MCP protocol (JSON-RPC 2.0)
/api/v1/auth    Authentication (login, token management)
/api/v1/llm     LLM proxy (optional)
/api/v1/*       REST endpoints (connections, conversations)
```

Each handler group applies its own middleware chain for
authentication, rate limiting, and request validation.

## System Architecture Diagram

The following diagram illustrates the major components and
their relationships.

```
+---------------------------------------------------------+
|                      HTTP Server                        |
|  +-----------+ +------------+ +----------+ +----------+ |
|  |  /mcp/v1  | |/api/v1/auth| |/api/v1/  | |/api/v1/  | |
|  |    MCP    | |   Login    | |llm Proxy | |REST APIs | |
|  +-----------+ +------------+ +----------+ +----------+ |
+---------------------------------------------------------+
        |               |             |            |
        v               |             v            |
+-----------------+     |      +-------------+     |
|   MCP Server    |     |      | LLM Clients |     |
|                 |     |      |             |     |
| +-------------+ |     |      | - Anthropic |     |
| |    Tools    | |     |      | - OpenAI    |     |
| +-------------+ |     |      | - Ollama    |     |
| |  Resources  | |     |      +-------------+     |
| +-------------+ |     |                          |
+-----------------+     |                          |
        |               |                          |
        v               v                          v
+---------------------------------------------------------+
|                 Authentication Layer                     |
|  +---------------------------------------------------+  |
|  |              Auth Store (SQLite)                   |  |
|  |  +--------+ +----------------+ +--------------+   |  |
|  |  | Users  | | Session Tokens | |    Tokens    |   |  |
|  |  +--------+ +----------------+ +--------------+   |  |
|  +---------------------------------------------------+  |
+---------------------------------------------------------+
        |
        v
+---------------------------------------------------------+
|           Database Connection Pool                      |
|              (Per-Session Isolation)                     |
+---------------------------------------------------------+
        |
        v
  +-------------+
  |  PostgreSQL |
  +-------------+
```

## Authentication and Authorization

The server stores all authentication data in a SQLite
database managed by the auth store. The auth store handles
users, session tokens, API tokens, groups, and token scopes.

### Authentication Flow

Session-based authentication proceeds as follows:

1. A client sends credentials to `/api/v1/auth/login`.
2. The auth handler validates the password against a Bcrypt
   hash with cost factor 12.
3. The server issues a session token on success.
4. Subsequent requests include the token in the
   `Authorization` header.

API tokens follow a similar flow but skip the login step.
The server hashes API tokens with SHA256 before storage.

### Role-Based Access Control

The RBAC system enforces authorization through three layers:

- Groups define base permission levels for users.
- Privileges control access to specific MCP tools and
  resources.
- Token scopes restrict individual tokens to a subset of
  the user's permissions.

The effective access level for any request equals the
minimum of the user's group level and the token scope
level.

### Security Measures

The server applies the following security protections:

- Per-IP rate limiting guards against brute force attacks.
- Automatic account lockout triggers after repeated failed
  login attempts.
- Each token receives its own database connection pool for
  session isolation.

## Tool and Resource Registration

Developers extend the server by registering new tools or
resources with the MCP server instance.

### Tool Registration

Each tool provides a name, description, input schema, and
a handler function. The MCP server validates incoming
parameters against the JSON Schema before invoking the
handler.

Tools fall into four categories:

- Monitored database tools operate on PostgreSQL
  connections and accept optional `connection_id` and
  `database_name` parameters.
- Datastore tools query the metrics datastore for
  historical data collected by the collector.
- Alert tools retrieve alert data from the monitoring
  system.
- Utility tools provide general-purpose functions like
  embedding generation.

### Resource Registration

Resources expose read-only data through URI-based
identifiers. The configuration file controls which
resources the server enables at startup.

## LLM Proxy Architecture

The LLM proxy provides an optional server-side gateway for
web clients that lack direct access to LLM APIs. The proxy
supports three providers:

- Anthropic Claude models through the Anthropic API.
- OpenAI models through the OpenAI API.
- Local models through the Ollama API.

The proxy handles API key management, request formatting,
and response streaming. Web clients send requests to
`/api/v1/llm` and receive streamed responses.

## Conversation Storage

The server persists chat sessions in a SQLite database for
conversation history. Each conversation belongs to a user
and stores the complete message sequence. The REST API
exposes endpoints for creating, listing, and retrieving
conversations.

## Database Connection Management

The server maintains a pool of database connections with
per-session isolation. Each authenticated token receives
its own connection pool to prevent cross-session data
leakage.

Connection metadata originates from the datastore, which
the collector populates. The server reads connection
definitions and establishes pools on demand.

## License

Copyright (c) 2025 - 2026, pgEdge, Inc.

This software is released under The PostgreSQL License.
