# pgEdge AI Workbench MCP Server Documentation

## Overview

The pgEdge AI Workbench MCP Server is a Go-based implementation of the Model
Context Protocol (MCP), enabling AI assistants to interact with PostgreSQL
databases through a standardized, secure interface.

## Key Features

- **Model Context Protocol**: Full implementation of MCP specification
- **Session Management**: User-specific session contexts for database access
- **Multi-Connection Support**: Manage and query multiple PostgreSQL servers
- **Role-Based Access Control**: Fine-grained privilege system for users and
  groups
- **Historical Metrics**: Dedicated datastore for performance trend analysis
- **Secure by Default**: TLS/SSL support, read-only queries, encrypted
  credentials

## Architecture

The MCP server consists of several key components:

### Core Components

- **MCP Handler**: Processes JSON-RPC 2.0 requests over Server-Sent Events
- **Session Manager**: Maintains per-user database contexts
- **Connection Manager**: Manages encrypted PostgreSQL connection credentials
- **Privilege System**: Enforces RBAC for all operations
- **User Management**: User accounts, service tokens, and user tokens

### Data Flow

```
AI Assistant → HTTP/SSE → MCP Handler → Session Context
                                      ↓
                                Connection Manager → PostgreSQL Server(s)
                                      ↓
                                Datastore (Metrics)
```

## Documentation

### Getting Started

- [Server README](../../server/README.md) - Installation, configuration, and
  basic usage
- [Session Context System](session-context.md) - Working database contexts and
  datastore access

### Features

- **Session Context System**: See [session-context.md](session-context.md)
  - Setting and using database contexts
  - New MCP tools: `set_database_context`, `get_database_context`,
    `clear_database_context`
  - Datastore queries with `query_datastore`
  - Modified `execute_query` behavior

### MCP Tools

The server provides 30+ MCP tools organized by function:

#### User Management

- `authenticate_user` - Authenticate and obtain session token
- `create_user` - Create new user accounts
- `update_user` - Modify user account details
- `delete_user` - Remove user accounts

#### Token Management

- `create_service_token` - Create service tokens for automation
- `update_service_token` - Modify service token settings
- `delete_service_token` - Remove service tokens
- `create_user_token` - Create personal access tokens
- `list_user_tokens` - List user's tokens
- `delete_user_token` - Remove user tokens

#### Group Management

- `create_user_group` - Create user groups
- `update_user_group` - Modify group settings
- `delete_user_group` - Remove groups
- `list_user_groups` - List all groups
- `add_group_member` - Add users/groups to groups
- `remove_group_member` - Remove members from groups
- `list_group_members` - List group members
- `list_user_group_memberships` - List user's group memberships

#### Privilege Management

- `grant_connection_privilege` - Grant connection access to groups
- `revoke_connection_privilege` - Revoke connection access
- `list_connection_privileges` - List connection privileges
- `list_mcp_privilege_identifiers` - List available MCP privileges
- `grant_mcp_privilege` - Grant MCP tool/resource access
- `revoke_mcp_privilege` - Revoke MCP access
- `list_group_mcp_privileges` - List group's MCP privileges

#### Token Scoping

- `set_token_connection_scope` - Limit token to specific connections
- `set_token_mcp_scope` - Limit token to specific MCP items
- `get_token_scope` - View token scope restrictions
- `clear_token_scope` - Remove token scope restrictions

#### Database Operations

- `create_connection` - Add new PostgreSQL connection
- `update_connection` - Modify connection settings
- `delete_connection` - Remove connection
- `execute_query` - Run SQL queries (read-only, uses session context)

#### Session Context

- `set_database_context` - Set working database for session
- `get_database_context` - View current database context
- `clear_database_context` - Clear session context

#### Datastore

- `query_datastore` - Query historical metrics data

### MCP Resources

The server provides read-only resources accessible to AI assistants:

- `ai-workbench://connections/list` - List of available database connections
- `ai-workbench://connections/{id}` - Details of a specific connection
- `ai-workbench://tables/{connectionId}` - Tables in a database
- `ai-workbench://tables/{connectionId}/{tableName}/schema` - Table schema
- `ai-workbench://current-user/info` - Current user information
- `ai-workbench://current-user/groups` - User's group memberships
- `ai-workbench://current-user/privileges` - User's effective privileges
- `ai-workbench://groups/list` - All user groups
- `ai-workbench://groups/{id}/members` - Group members
- `ai-workbench://current-user/token-scope` - Token scope restrictions
- `ai-workbench://session/context` - Current session database context

## Security Model

### Authentication

- **User Accounts**: Username/password authentication
- **Service Tokens**: Long-lived tokens for automation (no expiration)
- **User Tokens**: Personal access tokens with optional expiration

### Authorization

The server implements a comprehensive RBAC system:

- **Superusers**: Full administrative access
- **User Groups**: Hierarchical group membership with inheritance
- **Connection Privileges**: Per-connection access levels (read/write/admin)
- **MCP Privileges**: Per-tool/resource access control
- **Token Scoping**: Optional limitation of token permissions

### Data Protection

- **Encrypted Credentials**: Database passwords encrypted with AES-256-GCM
- **Read-Only Queries**: All queries execute in `BEGIN READ ONLY` mode
- **TLS/SSL**: Optional HTTPS with certificate chain support
- **Session Isolation**: Complete isolation between user sessions

## Configuration

See [Server README Configuration
Section](../../server/README.md#configuration) for details on:

- Configuration file format
- Command-line flags
- TLS/SSL setup
- Database connection settings
- Server secret (encryption key)

## Development

### Building

```bash
cd server/src
go mod tidy
go build -o mcp-server
```

### Testing

```bash
# All tests
make test

# Skip database integration tests
SKIP_DB_TESTS=1 make test

# Specific package
go test -v ./src/mcp
go test -v ./src/session
go test -v ./src/privileges
```

### Code Coverage

```bash
SKIP_DB_TESTS=1 make coverage
```

### Linting

```bash
make lint
```

## API Reference

### Protocol

The server implements JSON-RPC 2.0 over Server-Sent Events (SSE).

**Endpoint**: `/sse`

**Request Format**:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "execute_query",
        "arguments": {
            "query": "SELECT version()"
        }
    }
}
```

**Response Format**:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Query results..."
            }
        ]
    }
}
```

**Error Format**:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "error": {
        "code": -32600,
        "message": "Invalid request",
        "data": "Additional error details"
    }
}
```

### Health Check

**Endpoint**: `/health`

**Response**:

```json
{
    "status": "ok",
    "initialized": true
}
```

## Troubleshooting

### Common Issues

#### Server Won't Start

- **Check configuration**: Verify `server.conf` is valid
- **Database connection**: Ensure PostgreSQL is accessible
- **Server secret**: Must be configured for encryption
- **Port conflicts**: Ensure port is not already in use

#### Authentication Failures

- **Password incorrect**: Verify credentials
- **User not found**: Check user exists with correct username
- **Token expired**: User tokens may have expiration dates

#### Permission Denied

- **Missing privileges**: Check user's group memberships and granted
  privileges
- **Token scope**: Verify token isn't scoped to exclude the resource
- **Connection access**: Ensure user has access to the connection

#### Session Context Issues

See [Session Context Troubleshooting](session-context.md#troubleshooting)

### Debug Mode

Run with verbose logging:

```bash
./mcp-server -v
```

This displays:

- Request/response details
- Connection operations
- Privilege checks
- Internal operations

## Contributing

See the main project [CONTRIBUTING.md](../../CONTRIBUTING.md) (if available)
for guidelines on:

- Code style
- Testing requirements
- Pull request process
- Security reporting

## License

This software is released under The PostgreSQL License. See
[LICENSE.md](../../LICENSE.md) for details.
