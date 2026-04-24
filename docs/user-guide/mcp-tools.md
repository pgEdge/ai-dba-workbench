# MCP Tools and Resources

The MCP server implements the Model Context Protocol,
providing AI assistants with standardized access to
PostgreSQL systems. Compatible MCP clients can use these
tools and resources to interact with monitored databases.

## Connecting an MCP Client

The MCP server exposes a JSON-RPC 2.0 endpoint at
`/mcp/v1` over HTTP or HTTPS. Configure your MCP client
with the server address and a valid API token to begin
using the available tools.

## Monitored Database Tools

These tools operate on monitored database connections.
Each tool accepts optional `connection_id` and
`database_name` parameters to target a specific
database; the tool uses the currently selected connection
when these parameters are omitted.

| Tool | Description |
|------|-------------|
| `query_database` | Executes SQL queries against a monitored database. |
| `get_schema_info` | Retrieves table and column information from a database. |
| `execute_explain` | Runs EXPLAIN or EXPLAIN ANALYZE on a query. |
| `similarity_search` | Performs vector similarity search using pgvector. |
| `count_rows` | Counts rows in a specified table. |
| `test_query` | Validates SQL query correctness without executing the query. |

## Datastore Tools

These tools query the metrics datastore for historical
data collected by the collector service.

| Tool | Description |
|------|-------------|
| `list_probes` | Lists available metrics probes in the datastore. |
| `describe_probe` | Retrieves column details for a specific metrics probe. |
| `query_metrics` | Queries historical metrics with time-based aggregation. |
| `list_connections` | Lists available monitored database connections. |
| `query_datastore` | Executes read-only SQL queries against the datastore. |

## Alert Tools

These tools query alert data from the monitoring system.

| Tool | Description |
|------|-------------|
| `get_alert_history` | Queries alerts for monitored connections. |
| `get_alert_rules` | Queries alert rules and their effective thresholds. |
| `get_metric_baselines` | Queries statistical baselines for anomaly detection. |
| `get_blackouts` | Queries blackout periods and recurring schedules for monitored connections. |

## Memory Tools

These tools manage persistent memories that the AI
assistant can store and recall across conversations.

| Tool | Description |
|------|-------------|
| `store_memory` | Stores a persistent memory with a category, scope, and optional pinned flag. |
| `recall_memories` | Searches stored memories using semantic similarity; pinned memories are always included. |
| `delete_memory` | Deletes a stored memory by its ID; only the owning user can delete a memory. |

## Utility Tools

These tools provide general-purpose capabilities.

| Tool | Description |
|------|-------------|
| `generate_embedding` | Generates text embeddings from input text. |
| `search_knowledgebase` | Searches the pgEdge documentation knowledge base. |
| `read_resource` | Reads MCP resources via the tool interface for backward compatibility with older clients. |

## Available Resources

Resources expose read-only data that MCP clients can
retrieve without calling a tool. The following resources
are available:

| Resource URI | Description |
|--------------|-------------|
| `pg://system_info` | PostgreSQL server information including version and platform. |
| `pg://connection_info` | Current database connection details. |

Administrators can disable resources in the server
configuration.

## Authentication

All MCP endpoints require a valid API token for
authentication. Include the token in the `Authorization`
header of each request.

In the following example, a `curl` command lists the
available tools:

```bash
curl -X POST http://localhost:8080/mcp/v1 \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

## Related Documentation

- [Ask Ellie](ai/ask-ellie.md) describes the built-in
  AI assistant that uses these tools internally.
- [AI Overview](ai/overview.md) covers AI-powered
  summaries of database health.
