# MCP Tools

The MCP server provides tools that enable AI assistants to perform operations
on the system. Tools are callable functions that can create, update, and delete
system resources.

## Overview

Tools in MCP allow AI assistants to make changes to the system. The pgEdge AI
Workbench MCP server provides tools for managing user accounts and service
tokens.

## Tool Discovery

To discover available tools, use the `tools/list` method:

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list"
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "tools": [
            {
                "name": "create_user",
                "description": "Create a new user account",
                "inputSchema": { ... }
            },
            ...
        ]
    }
}
```

## Authentication

### authenticate_user

Authenticates a user and returns a session token.

**Note:** This tool does NOT require superuser privileges, as users need to
authenticate to obtain a session token.

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username to authenticate"
        },
        "password": {
            "type": "string",
            "description": "Password for authentication"
        }
    },
    "required": ["username", "password"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "authenticate_user",
        "arguments": {
            "username": "admin",
            "password": "password123"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Authentication successful. Session token: abc123...\nExpires at: 2025-11-07T16:00:00Z"
            }
        ]
    }
}
```

## User Management Tools

**IMPORTANT:** All user management tools require superuser privileges. See the
[Authentication Guide](../authentication.md) for details on superuser access
control.

### create_user

Creates a new user account. **Requires superuser privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username for the new user"
        },
        "email": {
            "type": "string",
            "description": "Email address for the new user"
        },
        "fullName": {
            "type": "string",
            "description": "Full name of the user"
        },
        "password": {
            "type": "string",
            "description": "Password for the new user"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Whether the user should have superuser privileges",
            "default": false
        },
        "passwordExpiry": {
            "type": "string",
            "description": "Password expiry date (YYYY-MM-DD format, optional)"
        }
    },
    "required": ["username", "email", "fullName", "password"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "create_user",
        "arguments": {
            "username": "jdoe",
            "email": "jdoe@example.com",
            "fullName": "John Doe",
            "password": "SecurePass123!",
            "isSuperuser": false,
            "passwordExpiry": "2026-01-15"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "User 'jdoe' created successfully"
            }
        ]
    }
}
```

### update_user

Updates an existing user account. **Requires superuser privileges.**

Supports partial updates - only the fields provided will be changed.

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username of the user to update"
        },
        "email": {
            "type": "string",
            "description": "New email address (optional)"
        },
        "fullName": {
            "type": "string",
            "description": "New full name (optional)"
        },
        "password": {
            "type": "string",
            "description": "New password (optional)"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Update superuser status (optional)"
        },
        "passwordExpiry": {
            "type": "string",
            "description": "New password expiry date (YYYY-MM-DD format, optional)"
        },
        "clearPasswordExpiry": {
            "type": "boolean",
            "description": "Clear password expiry (optional)",
            "default": false
        }
    },
    "required": ["username"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
        "name": "update_user",
        "arguments": {
            "username": "jdoe",
            "email": "john.doe@example.com",
            "passwordExpiry": "2027-01-15"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "User 'jdoe' updated successfully"
            }
        ]
    }
}
```

### delete_user

Deletes a user account. **Requires superuser privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username of the user to delete"
        }
    },
    "required": ["username"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
        "name": "delete_user",
        "arguments": {
            "username": "jdoe"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 3,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "User 'jdoe' deleted successfully"
            }
        ]
    }
}
```

## Service Token Management Tools

**IMPORTANT:** All service token management tools require superuser privileges.
See the [Authentication Guide](../authentication.md) for details on superuser
access control.

### create_service_token

Creates a new service token for API authentication. **Requires superuser
privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "Name for the service token"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Whether the token should have superuser privileges",
            "default": false
        },
        "note": {
            "type": "string",
            "description": "Optional note about the token"
        },
        "expiresAt": {
            "type": "string",
            "description": "Expiry date (YYYY-MM-DD format, optional)"
        }
    },
    "required": ["name"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
        "name": "create_service_token",
        "arguments": {
            "name": "api-integration",
            "isSuperuser": false,
            "note": "Token for external API integration",
            "expiresAt": "2026-01-15"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 4,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Service token 'api-integration' created successfully\nToken: AbCdEf123456...\nIMPORTANT: Save this token now. You won't be able to see it again."
            }
        ]
    }
}
```

**Important**: The token value is only shown once at creation time. Save it
securely as it cannot be retrieved later.

### update_service_token

Updates an existing service token. **Requires superuser privileges.**

Supports partial updates.

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "Name of the service token to update"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Update superuser status (optional)"
        },
        "note": {
            "type": "string",
            "description": "Update note (optional)"
        },
        "expiresAt": {
            "type": "string",
            "description": "New expiry date (YYYY-MM-DD format, optional)"
        },
        "clearNote": {
            "type": "boolean",
            "description": "Clear the note (optional)",
            "default": false
        },
        "clearExpiresAt": {
            "type": "boolean",
            "description": "Clear expiry date (optional)",
            "default": false
        }
    },
    "required": ["name"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 5,
    "method": "tools/call",
    "params": {
        "name": "update_service_token",
        "arguments": {
            "name": "api-integration",
            "note": "Updated: Token for external API v2 integration",
            "expiresAt": "2027-01-15"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 5,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Service token 'api-integration' updated successfully"
            }
        ]
    }
}
```

### delete_service_token

Deletes a service token. **Requires superuser privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "Name of the service token to delete"
        }
    },
    "required": ["name"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 6,
    "method": "tools/call",
    "params": {
        "name": "delete_service_token",
        "arguments": {
            "name": "api-integration"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 6,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Service token 'api-integration' deleted successfully"
            }
        ]
    }
}
```

## Connection Management Tools

The MCP server provides tools for managing database connections. Regular users
can manage their own connections if they have the appropriate MCP privileges
granted via group membership. Service tokens must have superuser privileges to
create connections.

### create_connection

Creates a new database connection with encrypted password storage. **Service
tokens require superuser privileges. Regular users need MCP privilege via group
membership.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "User-friendly name for the connection"
        },
        "host": {
            "type": "string",
            "description": "Hostname or IP address of the PostgreSQL server"
        },
        "port": {
            "type": "number",
            "description": "Port number for PostgreSQL connection",
            "default": 5432
        },
        "databaseName": {
            "type": "string",
            "description": "Name of the database to connect to"
        },
        "username": {
            "type": "string",
            "description": "Database username for authentication"
        },
        "password": {
            "type": "string",
            "description": "Database password (will be encrypted)"
        },
        "hostaddr": {
            "type": "string",
            "description": "Numeric IP address (optional)"
        },
        "sslmode": {
            "type": "string",
            "description": "SSL mode (disable, allow, prefer, require, verify-ca, verify-full)",
            "default": "prefer"
        },
        "sslcert": {
            "type": "string",
            "description": "Path to client SSL certificate (optional)"
        },
        "sslkey": {
            "type": "string",
            "description": "Path to client SSL key (optional)"
        },
        "sslrootcert": {
            "type": "string",
            "description": "Path to root certificate (optional)"
        },
        "isShared": {
            "type": "boolean",
            "description": "Whether this connection is shared",
            "default": false
        },
        "isMonitored": {
            "type": "boolean",
            "description": "Whether this connection should be monitored",
            "default": true
        }
    },
    "required": ["name", "host", "databaseName", "username", "password"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 7,
    "method": "tools/call",
    "params": {
        "name": "create_connection",
        "arguments": {
            "name": "Production Database",
            "host": "prod.example.com",
            "port": 5432,
            "databaseName": "myapp",
            "username": "dbuser",
            "password": "SecureDBPass123!",
            "sslmode": "require",
            "isMonitored": true
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 7,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Connection 'Production Database' created successfully with ID: 42"
            }
        ]
    }
}
```

**Security Note**: Passwords are encrypted using AES-256-GCM before storage,
with encryption keys derived from the server secret and connection username.

### update_connection

Updates an existing database connection. **Users can only update their own
connections unless they are superusers.**

Supports partial updates - only the fields provided will be changed.

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "id": {
            "type": "number",
            "description": "Connection ID to update"
        },
        "name": {
            "type": "string",
            "description": "New name (optional)"
        },
        "host": {
            "type": "string",
            "description": "New host (optional)"
        },
        "port": {
            "type": "number",
            "description": "New port (optional)"
        },
        "databaseName": {
            "type": "string",
            "description": "New database name (optional)"
        },
        "username": {
            "type": "string",
            "description": "New database username (optional)"
        },
        "password": {
            "type": "string",
            "description": "New password (optional, will be re-encrypted)"
        },
        "hostaddr": {
            "type": "string",
            "description": "New hostaddr (optional)"
        },
        "sslmode": {
            "type": "string",
            "description": "New SSL mode (optional)"
        },
        "sslcert": {
            "type": "string",
            "description": "New SSL certificate path (optional)"
        },
        "sslkey": {
            "type": "string",
            "description": "New SSL key path (optional)"
        },
        "sslrootcert": {
            "type": "string",
            "description": "New SSL root cert path (optional)"
        },
        "isShared": {
            "type": "boolean",
            "description": "Update shared status (optional)"
        },
        "isMonitored": {
            "type": "boolean",
            "description": "Update monitoring status (optional)"
        }
    },
    "required": ["id"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 8,
    "method": "tools/call",
    "params": {
        "name": "update_connection",
        "arguments": {
            "id": 42,
            "sslmode": "verify-full",
            "isMonitored": false
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 8,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Connection with ID 42 updated successfully"
            }
        ]
    }
}
```

### delete_connection

Deletes a database connection. **Users can only delete their own connections
unless they are superusers.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "id": {
            "type": "number",
            "description": "Connection ID to delete"
        }
    },
    "required": ["id"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 9,
    "method": "tools/call",
    "params": {
        "name": "delete_connection",
        "arguments": {
            "id": 42
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 9,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Connection with ID 42 deleted successfully"
            }
        ]
    }
}
```

## Query Execution Tool

### execute_query

Executes SQL queries on database connections in read-only mode. This powerful
tool enables AI assistants to query both real-time statistics from individual
databases and historical metrics from the datastore.

**Security:** Queries always run in read-only transaction mode (`BEGIN READ
ONLY`) to prevent data modification. Query timeout is set to 30 seconds.

**Access Control:**
- Users can query connections they own
- Users can query connections where they have connection-level read privileges
- Users with `execute_query` MCP privilege can query any connection they have
  access to
- **Special case:** All authenticated users can query connection ID 0 (the
  datastore)
- Superusers can query any connection

---

### ⚠️ CRITICAL: Connection Selection Rules

**Connection ID 0 = DATASTORE ONLY (historical metrics ONLY)**
**Connection ID 1+ = ACTUAL DATABASES on PostgreSQL servers**

**When querying actual database content:**

❌ **WRONG:** User asks "show tables in pgaweb database" → DO NOT USE
`connectionId: 0`
✅ **RIGHT:** User asks "show tables in pgaweb database" → USE `connectionId:
(find kielbasa server), databaseName: "pgaweb"`

❌ **WRONG:** User asks "what's in the users table" → DO NOT USE `connectionId:
0`
✅ **RIGHT:** User asks "what's in the users table" → USE `connectionId:
(actual server), databaseName: (actual database)`

❌ **WRONG:** User asks "describe schema in mydb" → DO NOT USE `connectionId:
0`
✅ **RIGHT:** User asks "describe schema in mydb" → USE `connectionId: (actual
server), databaseName: "mydb"`

✅ **ONLY use connectionId: 0 for:** "show historical metrics", "performance
trends", "analyze metrics over time"

**Mandatory Steps:**
1. Find the server connection (read connections resource)
2. Get the connection ID for that server (e.g., kielbasa = ID 3)
3. Set `connectionId` to that ID (NOT 0)
4. Set `databaseName` parameter to the database mentioned (e.g., "pgaweb")
5. Execute the query immediately

---

**Database Selection:**

By default, queries execute on the database configured for the connection.
However, you can optionally specify a different `databaseName` parameter to
connect to ANY database on the same PostgreSQL server (subject to PostgreSQL's
authentication rules in pg_hba.conf). This is extremely useful for:

- Querying system databases like `postgres` for server-wide statistics (e.g.,
  `SELECT * FROM pg_stat_database` to see all databases)
- Switching between multiple application databases on the same server
- Checking template databases (template0, template1) or other databases
- **MOST IMPORTANTLY:** Querying a specific database when the connection is
  configured for a different default database

**Important:** For connection ID 0 (datastore), the `databaseName` parameter is
ignored and always uses the datastore database.

#### Understanding the Datastore

Connection ID **0** is special - it represents the datastore that contains
historical performance metrics collected from all monitored PostgreSQL servers.
The datastore's `metrics` schema provides time-series data for trend analysis
and historical comparisons.

**Key metrics tables:**

- `metrics.pg_stat_database` - Database-level statistics over time
- `metrics.pg_stat_bgwriter` - Background writer metrics
- `metrics.pg_stat_all_tables` - Per-table statistics history
- `metrics.pg_stat_all_indexes` - Index usage and efficiency
- `metrics.pg_stat_statements` - Query performance history
- `metrics.pg_stat_replication` - Replication lag history
- `metrics.pg_settings` - Configuration changes over time

Each metrics table includes:
- `collected_at` - When the metric was collected
- `connection_name` - Which database/server it came from
- Original PostgreSQL statistic columns

#### Combining Real-Time and Historical Data

For comprehensive analysis, query both sources:

1. **Real-time queries** (on individual connections) - Get current state
2. **Historical queries** (on datastore, connection 0) - Analyze trends

**Example workflow:**

```sql
-- Step 1: Query datastore for historical trend
SELECT collected_at, numbackends, xact_commit
FROM metrics.pg_stat_database
WHERE connection_name = 'production' AND collected_at > NOW() - INTERVAL '7 days'
ORDER BY collected_at;

-- Step 2: Query real-time data on actual connection
SELECT numbackends, xact_commit
FROM pg_stat_database
WHERE datname = current_database();

-- Step 3: Compare and identify anomalies
```

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "connectionId": {
            "type": "number",
            "description": "Connection ID (use 0 for datastore with historical metrics)"
        },
        "query": {
            "type": "string",
            "description": "SQL query to execute (runs in read-only mode)"
        },
        "databaseName": {
            "type": "string",
            "description": "Optional: specific database name to connect to on the server (overrides connection's default database). Ignored for connection ID 0."
        },
        "maxRows": {
            "type": "number",
            "description": "Maximum rows to return (default: 1000, max: 10000)",
            "default": 1000
        }
    },
    "required": ["connectionId", "query"]
}
```

#### Example Usage

**Query historical metrics from datastore:**

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 10,
    "method": "tools/call",
    "params": {
        "name": "execute_query",
        "arguments": {
            "connectionId": 0,
            "query": "SELECT connection_name, AVG(numbackends) as avg_connections, MAX(numbackends) as max_connections FROM metrics.pg_stat_database WHERE collected_at > NOW() - INTERVAL '24 hours' GROUP BY connection_name",
            "maxRows": 100
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 10,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Query executed successfully on connection 'datastore' (ID: 0)\n\nColumns: [connection_name, avg_connections, max_connections]\nRows returned: 3\n\nResults:\n{\n  \"columns\": [\"connection_name\", \"avg_connections\", \"max_connections\"],\n  \"rows\": [\n    [\"production\", 45.2, 78],\n    [\"staging\", 12.5, 23],\n    [\"development\", 5.1, 12]\n  ],\n  \"rowCount\": 3,\n  \"truncated\": false\n}"
            }
        ]
    }
}
```

**Query real-time statistics from a database:**

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 11,
    "method": "tools/call",
    "params": {
        "name": "execute_query",
        "arguments": {
            "connectionId": 42,
            "query": "SELECT datname, numbackends, xact_commit, xact_rollback, blks_read, blks_hit FROM pg_stat_database WHERE datname = current_database()"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 11,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Query executed successfully on connection 'Production Database' (ID: 42), database 'myapp'\n\nColumns: [datname, numbackends, xact_commit, xact_rollback, blks_read, blks_hit]\nRows returned: 1\n\nResults:\n{\n  \"columns\": [\"datname\", \"numbackends\", \"xact_commit\", \"xact_rollback\", \"blks_read\", \"blks_hit\"],\n  \"rows\": [\n    [\"myapp\", 52, 1234567, 234, 45678, 9876543]\n  ],\n  \"rowCount\": 1,\n  \"truncated\": false\n}"
            }
        ]
    }
}
```

**Query server-wide statistics using a different database:**

This example shows querying the `postgres` system database to get statistics
for ALL databases on the server:

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 12,
    "method": "tools/call",
    "params": {
        "name": "execute_query",
        "arguments": {
            "connectionId": 42,
            "databaseName": "postgres",
            "query": "SELECT datname, numbackends, xact_commit, blks_hit::float / NULLIF(blks_hit + blks_read, 0) * 100 as cache_hit_pct FROM pg_stat_database WHERE datname NOT IN ('template0', 'template1') ORDER BY xact_commit DESC"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 12,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Query executed successfully on connection 'Production Database' (ID: 42), database 'postgres'\n\nColumns: [datname, numbackends, xact_commit, cache_hit_pct]\nRows returned: 3\n\nResults:\n{\n  \"columns\": [\"datname\", \"numbackends\", \"xact_commit\", \"cache_hit_pct\"],\n  \"rows\": [\n    [\"myapp\", 52, 1234567, 98.5],\n    [\"analytics\", 12, 456789, 97.2],\n    [\"postgres\", 3, 12345, 99.1]\n  ],\n  \"rowCount\": 3,\n  \"truncated\": false\n}"
            }
        ]
    }
}
```

**Use Case:** This is particularly useful when you need cluster-wide information
that's only available from the `postgres` database, even if your connection is
configured for a specific application database.

#### Common Analysis Patterns

**1. Performance Degradation Detection:**

```sql
-- Datastore: Historical cache hit ratio trend
SELECT DATE(collected_at),
       AVG(blks_hit::float / NULLIF(blks_hit + blks_read, 0)) * 100 as cache_hit_pct
FROM metrics.pg_stat_database
WHERE connection_name = 'production'
GROUP BY DATE(collected_at)
ORDER BY DATE(collected_at);

-- Real-time: Current cache hit ratio
SELECT blks_hit::float / NULLIF(blks_hit + blks_read, 0) * 100 as cache_hit_pct
FROM pg_stat_database WHERE datname = current_database();
```

**2. Table Growth Analysis:**

```sql
-- Datastore: Insertion rate over time
SELECT collected_at, n_tup_ins, n_tup_upd
FROM metrics.pg_stat_all_tables
WHERE connection_name = 'production' AND relname = 'orders'
ORDER BY collected_at;

-- Real-time: Current table statistics
SELECT n_live_tup, n_dead_tup, last_vacuum, last_autovacuum
FROM pg_stat_user_tables WHERE relname = 'orders';
```

**3. Query Performance Monitoring:**

```sql
-- Datastore: Historical query performance
SELECT query, AVG(mean_exec_time) as avg_time, MAX(mean_exec_time) as max_time
FROM metrics.pg_stat_statements
WHERE connection_name = 'production'
GROUP BY query
ORDER BY avg_time DESC LIMIT 10;

-- Real-time: Current slow queries
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
ORDER BY mean_exec_time DESC LIMIT 10;
```

**4. Replication Health:**

```sql
-- Datastore: Replication lag history
SELECT collected_at, application_name,
       EXTRACT(epoch FROM replay_lag) as replay_lag_seconds
FROM metrics.pg_stat_replication
WHERE connection_name = 'production'
ORDER BY collected_at;

-- Real-time: Current replication status
SELECT application_name, state,
       pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes,
       replay_lag
FROM pg_stat_replication;
```

## Resource Access Tools

### read_resource

Reads data from MCP resources. This tool enables AI assistants to fetch actual
data from resources like the connections list, user accounts, service tokens,
and other system information.

**Common Use Case:** Before executing queries on a database, use this tool to
read the `ai-workbench://connections` resource to discover available connections
and their IDs. This prevents trial-and-error attempts with connection IDs.

**Access Control:**
- All authenticated users can read resources they have access to
- Resource access follows the same authorization rules as the underlying data
- Superusers can read all resources

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "uri": {
            "type": "string",
            "description": "The resource URI to read (e.g., 'ai-workbench://connections' to list all available database connections)"
        }
    },
    "required": ["uri"]
}
```

#### Available Resources

Common resources that can be read with this tool:

- `ai-workbench://connections` - List of all database connections with their IDs,
  names, hosts, and other metadata
- `ai-workbench://users` - List of user accounts
- `ai-workbench://service-tokens` - List of service tokens
- `ai-workbench://groups` - List of user groups
- `ai-workbench://mcp-privileges` - List of MCP privilege identifiers
- `ai-workbench://session/context` - Current session database context

For a complete list, use the `resources/list` MCP method.

#### Example Usage

**Discovering available database connections:**

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 13,
    "method": "tools/call",
    "params": {
        "name": "read_resource",
        "arguments": {
            "uri": "ai-workbench://connections"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 13,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "{\"id\":2,\"isShared\":false,\"isMonitored\":true,\"name\":\"holly.conx.page\",\"host\":\"holly.conx.page\",\"hostaddr\":null,\"port\":5432,\"databaseName\":\"postgres\",\"username\":\"postgres\",\"sslmode\":\"prefer\",\"createdAt\":\"2025-01-10T10:30:00Z\",\"updatedAt\":\"2025-01-10T10:30:00Z\"}\n\n{\"id\":3,\"isShared\":false,\"isMonitored\":true,\"name\":\"kielbasa\",\"host\":\"kielbasa.local\",\"hostaddr\":null,\"port\":5432,\"databaseName\":\"postgres\",\"username\":\"dbuser\",\"sslmode\":\"require\",\"createdAt\":\"2025-01-05T14:20:00Z\",\"updatedAt\":\"2025-01-05T14:20:00Z\"}"
            }
        ]
    }
}
```

**Typical Workflow:**

1. User asks: "List tables in the pgaweb database on holly"
2. AI calls `read_resource` with `uri: "ai-workbench://connections"` to discover
   connection named "holly.conx.page" has ID 2
3. AI calls `set_database_context` with `connectionId: 2` and
   `databaseName: "pgaweb"`
4. AI calls `execute_query` to list the tables

This approach eliminates guessing connection IDs and prevents unnecessary errors.

## Error Handling

Tools return standard JSON-RPC error responses when operations fail:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "error": {
        "code": -32603,
        "message": "Tool execution failed",
        "data": "user 'jdoe' already exists"
    }
}
```

Common error scenarios:

- User or token already exists (during creation)
- User or token not found (during update/delete)
- Invalid input parameters (invalid date format, missing required fields)
- Database connection errors
- Permission denied (user or token lacks superuser privileges)
- Authentication failed (invalid or expired token)

## Security Considerations

- All tool operations (except `authenticate_user`) require authentication
- User management and service token management operations require superuser
  privileges
- Password hashing is performed server-side using SHA-256
- Service token values are generated cryptographically
- Session tokens inherit superuser status from the user account
- Service tokens have independent superuser status
- All operations are logged for audit purposes
- Rate limiting may apply to prevent abuse

For detailed information on authentication and authorization, see the
[Authentication Guide](../authentication.md).

## Best Practices

1. **Password Security**: Always use strong passwords when creating users
2. **Token Expiry**: Set expiration dates for service tokens
3. **Notes**: Add descriptive notes to service tokens for easier management
4. **Least Privilege**: Only grant superuser privileges when necessary
5. **Token Storage**: Save service token values immediately after creation
6. **Regular Audits**: Periodically review users and tokens using resources
