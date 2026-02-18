# Ask Ellie

The Ask Ellie feature provides an AI-powered database
assistant within the workbench. Ellie answers questions
about PostgreSQL databases, analyzes performance, and
searches the pgEdge knowledge base.

## Overview

Ellie uses a large language model with access to
monitoring tools. The assistant can query databases,
analyze metrics, inspect schemas, and search
documentation on behalf of the user.

The Ask Ellie feature provides the following
capabilities:

- The assistant executes read-only SQL queries against
  monitored database connections.
- The assistant queries historical metrics with
  time-based aggregation.
- The assistant inspects database schemas across
  monitored connections.
- The assistant searches the pgEdge knowledge base for
  documentation.
- The assistant analyzes query execution plans.
- The assistant reviews alert history and alert rule
  configuration.

## Opening the Chat

Click the chat button in the bottom-right corner of
the workbench to open the Ask Ellie panel. The panel
appears alongside the current view without replacing
existing content.

Type a question in the input field and press Enter to
send. The assistant processes the question and may
execute one or more tools before responding. A status
indicator shows active tool execution.

Use Shift+Enter to add a new line without sending the
message.

## Conversation History

The workbench stores conversation history in the
PostgreSQL datastore. Each conversation is associated
with the authenticated user.

Click the history button in the chat panel header to
view previous conversations. The history overlay
displays all saved conversations sorted by most recent
update.

The following conversation management actions are
available:

- Click a conversation to load its full message
  history.
- Use the context menu to rename or delete a
  conversation.
- Click the plus button in the header to start a new
  conversation.
- Use the Clear All button to remove all conversations.

## Downloading Conversations

The chat panel header includes a download button next
to the History, New Chat, and Close buttons. Click the
download button to save the current conversation as a
markdown file.

The exported file includes the following content:

- A title containing the conversation name.
- The date of the export.
- All user and assistant messages in order.

The workbench saves the file with the name format
`ellie-chat-{YYYY-MM-DD}.md`, where the date reflects
the day of the download.

The download button is disabled when the conversation
contains no messages. The button is also disabled while
the assistant is generating a response.

## Available Tools

Ellie has access to monitoring tools that execute
automatically during a conversation. The following
table describes the available tools:

| Tool | Description |
|------|-------------|
| `list_connections` | Lists all monitored database connections with IDs, names, and status. |
| `query_database` | Executes a read-only SQL query on a monitored database. |
| `query_metrics` | Queries historical metrics with time-based aggregation. |
| `query_datastore` | Executes read-only SQL against the monitoring datastore. |
| `search_knowledgebase` | Searches the pgEdge documentation knowledge base. |
| `get_schema_info` | Retrieves schema information from a monitored database. |
| `execute_explain` | Runs EXPLAIN ANALYZE on a query for performance analysis. |
| `list_probes` | Lists available monitoring probes. |
| `describe_probe` | Provides details about a specific monitoring probe. |
| `get_alert_history` | Retrieves historical alerts for a connection. |
| `get_alert_rules` | Retrieves current alert rules and thresholds. |

## API Reference

The REST API exposes endpoints for managing chat
conversations.

### Chat Endpoint

The following endpoint sends a message to the LLM:

```
POST /api/v1/llm/chat
```

This endpoint requires authentication.

### Conversation Endpoints

The following endpoints manage conversation
persistence:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/conversations` | Lists all conversations for the authenticated user. |
| `POST` | `/api/v1/conversations` | Creates a new conversation. |
| `GET` | `/api/v1/conversations/{id}` | Retrieves a conversation by ID. |
| `PUT` | `/api/v1/conversations/{id}` | Updates a conversation with new messages. |
| `PATCH` | `/api/v1/conversations/{id}` | Renames a conversation. |
| `DELETE` | `/api/v1/conversations/{id}` | Deletes a conversation. |
| `DELETE` | `/api/v1/conversations?all=true` | Deletes all conversations. |

### Tool Endpoints

The following endpoints provide tool access:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/mcp/tools` | Lists available MCP tools. |
| `POST` | `/api/v1/mcp/tools/call` | Executes an MCP tool by name. |

## Configuration

The Ask Ellie feature requires an LLM provider
configured in the server settings. The server cannot
process chat messages without a valid LLM configuration.

For LLM provider setup instructions, see
[Configuration](configuration.md).
