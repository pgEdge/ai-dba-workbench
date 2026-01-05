# Session Context System

The AI Workbench MCP server provides a session context system that allows
users to set a "working database" for their session, similar to how a database
CLI maintains a current connection.

## Overview

The session context system eliminates the need to repeatedly specify connection
IDs and database names in query operations, enabling a more natural
conversational workflow with AI assistants.

## Benefits

- **Reduced Verbosity**: Set your working database once, query multiple times
- **Natural Workflow**: Similar to `\c database` in psql or `USE database` in
  MySQL
- **Error Prevention**: Reduces mistakes from copying/pasting connection IDs
- **Better UX**: AI assistants can focus on query logic rather than connection
  details

## How It Works

The session context is stored in-memory per user and consists of:

- **Connection ID**: Which PostgreSQL server to query (must be 1 or higher)
- **Database Name**: Optional specific database on that server

Once set, the context persists for the duration of your session and is
automatically used by the `execute_query` tool when no explicit connection ID
is provided.

## MCP Tools

### set_database_context

Sets the current database context for your session.

**Parameters:**

- `connectionId` (required, number): The connection ID to use. Cannot be 0.
- `databaseName` (optional, string): Specific database name on that server.

**Example:**

```json
{
    "name": "set_database_context",
    "arguments": {
        "connectionId": 3,
        "databaseName": "pgaweb"
    }
}
```

**Response:**

```
Database context set to connection ID 3, database: pgaweb

All subsequent execute_query calls will use this context.
```

**Access Control:**

- Service tokens: Always allowed
- Regular users: Requires `set_database_context` privilege
- User must have read access to the connection

### get_database_context

Retrieves the current database context for your session.

**Parameters:** None

**Example:**

```json
{
    "name": "get_database_context",
    "arguments": {}
}
```

**Response (when context exists):**

```
Current database context:
- Connection ID: 3
- Database: pgaweb

execute_query will use this context by default.
```

**Response (when no context):**

```
No database context set.

Use set_database_context to establish a working database.
```

**Access Control:**

- Service tokens: Always allowed
- Regular users: Requires `get_database_context` privilege

### clear_database_context

Clears the current database context for your session.

**Parameters:** None

**Example:**

```json
{
    "name": "clear_database_context",
    "arguments": {}
}
```

**Response:**

```
Database context cleared.

execute_query will now require explicit connectionId.
```

**Access Control:**

- Service tokens: Always allowed
- Regular users: Requires `clear_database_context` privilege

### execute_query (Modified)

The `execute_query` tool has been updated to use session context when no
explicit `connectionId` is provided.

**Parameters:**

- `query` (required, string): SQL query to execute
- `connectionId` (optional, number): Explicit connection ID. If omitted, uses
  session context. **Cannot be 0** (datastore access blocked).
- `databaseName` (optional, string): Specific database name (overrides
  connection default or session context)
- `maxRows` (optional, number): Maximum rows to return (default: 1000, max:
  10000)

**Behavior Changes:**

1. **Session Context Support**: If `connectionId` is not provided, the tool
   uses the session context set by `set_database_context`
2. **Datastore Blocked**: Connection ID 0 (datastore) is now blocked. Use
   `query_datastore` tool instead.

**Example (with context):**

```json
// First, set context
{
    "name": "set_database_context",
    "arguments": {
        "connectionId": 3,
        "databaseName": "pgaweb"
    }
}

// Then query without specifying connection
{
    "name": "execute_query",
    "arguments": {
        "query": "SELECT * FROM information_schema.tables"
    }
}
```

**Example (explicit connection):**

```json
{
    "name": "execute_query",
    "arguments": {
        "connectionId": 3,
        "databaseName": "mydb",
        "query": "SELECT * FROM users LIMIT 10"
    }
}
```

**Error Cases:**

- No context set and no connectionId provided: Returns error asking to set
  context or provide connectionId
- Connection ID 0 provided: Returns error directing user to use
  `query_datastore` instead

## Datastore Access

### query_datastore

The datastore (formerly "connection ID 0") contains historical metrics
collected from all monitored PostgreSQL servers. Access to this data is now
through a dedicated tool.

**Purpose:**

The datastore provides time-series views of database statistics for:

- Performance trend analysis
- Historical comparisons
- Capacity planning
- Troubleshooting past issues

**Key Tables in metrics Schema:**

- `pg_stat_database`: Historical database-level statistics
- `pg_stat_all_tables`: Historical table-level statistics
- `pg_stat_all_indexes`: Historical index usage statistics
- `pg_statio_all_tables`: Historical table I/O statistics
- `pg_stat_user_functions`: Historical function execution statistics
- `pg_stat_replication`: Historical replication lag and status
- `pg_stat_bgwriter`: Historical background writer statistics
- `pg_locks`: Historical lock contention data

**Parameters:**

- `query` (required, string): SQL query to execute on the datastore
- `maxRows` (optional, number): Maximum rows to return (default: 1000, max:
  10000)

**Example:**

```json
{
    "name": "query_datastore",
    "arguments": {
        "query": "SELECT collected_at, database_name, tup_inserted, tup_updated, tup_deleted FROM metrics.pg_stat_database WHERE database_name = 'mydb' ORDER BY collected_at DESC LIMIT 100"
    }
}
```

**When to Use:**

- ✓ "Show performance trends over time"
- ✓ "Historical query statistics"
- ✓ "Compare metrics from last week"
- ✓ "Analyze replication lag history"
- ✓ "Track index usage patterns"

**When NOT to Use:**

- ✗ "Show current tables in mydb" (use `execute_query` with
  `set_database_context`)
- ✗ "What data is in the users table" (use `execute_query`)
- ✗ "Run this UPDATE query" (neither tool supports writes)

**Access Control:**

- Service tokens: Always allowed
- Regular users: Requires `query_datastore` privilege
- All authenticated users typically have access for monitoring purposes

**Transaction Mode:**

All queries run in `BEGIN READ ONLY` transaction mode to prevent data
modification.

## MCP Resources

### ai-workbench://session/context

A read-only MCP resource that provides the current session context.

**Response (with context):**

```json
{
    "hasContext": true,
    "connectionId": 3,
    "databaseName": "pgaweb"
}
```

**Response (without context):**

```json
{
    "hasContext": false,
    "message": "No database context set."
}
```

## Typical Workflow

### Interactive Database Exploration

```
1. User: "I want to explore the pgaweb database on the kielbasa server"
   AI: Calls set_database_context(connectionId: 3, databaseName: "pgaweb")

2. User: "Show me all tables"
   AI: Calls execute_query("SELECT * FROM information_schema.tables")

3. User: "What's in the users table?"
   AI: Calls execute_query("SELECT * FROM users LIMIT 10")

4. User: "Done with this database"
   AI: Calls clear_database_context()
```

### Historical Analysis

```
1. User: "Show me database performance trends for the last week"
   AI: Calls query_datastore("SELECT collected_at, database_name, blks_read,
       blks_hit FROM metrics.pg_stat_database WHERE collected_at > NOW() -
       INTERVAL '7 days' ORDER BY collected_at")
```

### Mixed Workflow

```
1. User: "Compare current table sizes to last week"
   AI: First uses query_datastore() to get historical sizes
   AI: Then uses set_database_context() + execute_query() to get current sizes
   AI: Compares and presents results
```

## Implementation Details

### Session Storage

Session contexts are stored in memory using a thread-safe map:

```go
type DatabaseContext struct {
    ConnectionID int    `json:"connectionId"`
    DatabaseName string `json:"databaseName,omitempty"`
}
```

Key characteristics:

- Keyed by username
- Thread-safe with RWMutex
- In-memory only (lost on server restart)
- Isolated per user

### Security Considerations

1. **Connection Access**: Users can only set context to connections they have
   read access to
2. **Datastore Isolation**: Datastore (ID 0) blocked from `execute_query` to
   prevent confusion
3. **Privilege Checks**: All tools require appropriate MCP privileges (except
   for service tokens)
4. **User Isolation**: Each user's context is completely isolated

## Migration Notes

### Breaking Changes

**Connection ID 0 No Longer Accessible via execute_query**

Previous behavior:
```json
{
    "name": "execute_query",
    "arguments": {
        "connectionId": 0,
        "query": "SELECT * FROM metrics.pg_stat_database"
    }
}
```

New behavior:
```json
{
    "name": "query_datastore",
    "arguments": {
        "query": "SELECT * FROM metrics.pg_stat_database"
    }
}
```

### New Privileges Required

Regular users (non-service tokens) need the following privileges to use the
new tools:

- `set_database_context`
- `get_database_context`
- `clear_database_context`
- `query_datastore`

Service tokens automatically have access to all tools.

## Troubleshooting

### "No database context set" Error

**Problem**: Called `execute_query` without connectionId and without setting
context first.

**Solution**: Either:

1. Set context first: `set_database_context(connectionId: 3)`
2. Or provide explicit connectionId: `execute_query(connectionId: 3, ...)`

### "Cannot use connection ID 0" Error

**Problem**: Tried to access the datastore through `execute_query` or
`set_database_context`.

**Solution**: Use `query_datastore` tool instead for historical metrics.

### "Access denied" Errors

**Problem**: User lacks required MCP privilege.

**Solution**: Grant appropriate privilege through `grant_mcp_privilege` tool or
use a service token.

### Context Lost After Server Restart

**Problem**: Session context doesn't persist across server restarts.

**Solution**: This is by design. Session contexts are in-memory only. Simply
re-establish your context using `set_database_context` after reconnecting.
