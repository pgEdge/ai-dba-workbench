# Data Flow

This document describes how data moves through the AI DBA Workbench system.

## System Overview

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client    │────▶│   Server    │────▶│  Database   │
│   (React)   │◀────│   (MCP)     │◀────│ (PostgreSQL)│
└─────────────┘     └─────────────┘     └─────────────┘
                           ▲
                           │
                    ┌──────┴──────┐
                    │  Collector  │────▶ Monitored DBs
                    │    (Go)     │◀────
                    └─────────────┘
```

## Client to Server Communication

### MCP Protocol Flow

1. **Client initiates connection**
   - Client: `POST /mcp` with JSON-RPC request
   - Server: Validates authentication token
   - Server: Routes to appropriate handler

2. **Tool execution**
   ```
   Client                    Server                    Database
     │                         │                          │
     │── tools/call ──────────▶│                          │
     │   {tool: "query_execute"│                          │
     │    params: {sql: "..."}}│                          │
     │                         │── Execute query ────────▶│
     │                         │◀── Results ──────────────│
     │◀── Result ─────────────│                          │
   ```

3. **Resource access**
   ```
   Client                    Server                    Database
     │                         │                          │
     │── resources/read ──────▶│                          │
     │   {uri: "connection://1"}                          │
     │                         │── Fetch metadata ───────▶│
     │                         │◀── Connection info ──────│
     │◀── Resource content ───│                          │
   ```

### Authentication Flow

```
Client                    Server                    Database
  │                         │                          │
  │── Initialize ──────────▶│                          │
  │   (with auth token)     │                          │
  │                         │── Validate token ───────▶│
  │                         │◀── User info ────────────│
  │                         │── Check privileges ─────▶│
  │                         │◀── Permissions ──────────│
  │◀── Session info ───────│                          │
```

## Collector Data Flow

### Metric Collection Cycle

```
Collector                 Database                  Monitored DB
    │                        │                          │
    │── Get enabled probes ─▶│                          │
    │◀── Probe configs ─────│                          │
    │                        │                          │
    │── For each connection: │                          │
    │   │                    │                          │
    │   │── Connect ─────────────────────────────────▶│
    │   │◀── Connection ─────────────────────────────│
    │   │                    │                          │
    │   │── Run probe queries ───────────────────────▶│
    │   │◀── Metric data ────────────────────────────│
    │   │                    │                          │
    │   │── Store metrics ──▶│                          │
    │                        │                          │
```

### Probe Execution

1. **Probe selection**
   - Collector queries `probe_configs` for enabled probes
   - Filters by connection and probe type

2. **Data gathering**
   - Each probe executes specific `pg_stat_*` queries
   - Results collected with timestamp

3. **Storage**
   - Metrics inserted into partitioned `metrics.*` tables
   - Partitioned by time (daily/weekly)

## Server Internal Data Flow

### Request Processing

```
HTTP Request
     │
     ▼
┌─────────────┐
│ HTTP Server │
└──────┬──────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│    Auth     │────▶│   Session   │
│  Middleware │     │   Store     │
└──────┬──────┘     └─────────────┘
       │
       ▼
┌─────────────┐
│ MCP Handler │
└──────┬──────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│Tool/Resource│────▶│  Database   │
│   Handler   │◀────│    Pool     │
└─────────────┘     └─────────────┘
```

### Authorization Check Flow

```
Request arrives
      │
      ▼
┌──────────────────┐
│ Extract token    │
│ from header      │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Validate token   │
│ (hash lookup)    │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Get user groups  │
│ (recursive)      │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Check privilege  │
│ for operation    │
└────────┬─────────┘
         │
         ▼
    Authorized?
    /        \
   Yes        No
    │          │
    ▼          ▼
 Execute    Error
```

## Database Schema Relationships

### Core Tables Flow

```
user_accounts
      │
      ├──▶ user_tokens
      │
      ├──▶ user_sessions
      │
      └──▶ group_memberships ──▶ user_groups
                                      │
                                      ├──▶ group_mcp_privileges
                                      │
                                      └──▶ group_connection_privileges
                                                     │
                                                     ▼
                                               connections
                                                     │
                                                     ▼
                                              probe_configs
```

### Metrics Tables Flow

```
connections
      │
      └──▶ (application-level reference, no FK)
                │
                ▼
         metrics.pg_stat_*
         (partitioned tables)
```

## Client State Flow

### React Component Hierarchy

```
App
 │
 ├── AuthProvider (context)
 │       │
 │       └── User state, tokens
 │
 ├── ConnectionProvider (context)
 │       │
 │       └── Active connections, selection
 │
 └── Pages
         │
         ├── Dashboard
         │       └── Fetches: metrics, status
         │
         ├── Connections
         │       └── Fetches: connection list, CRUD
         │
         └── Query
                 └── Fetches: query execution, results
```

### API Call Pattern

```typescript
// Typical data flow in React component
Component
    │
    ├── useEffect (mount)
    │       │
    │       └── Call API service
    │               │
    │               └── Fetch from server
    │                       │
    │                       └── Update state
    │
    └── Render with state
```

## Error Flow

### Server Error Handling

```
Error occurs
      │
      ▼
┌──────────────────┐
│ Wrap with context│
│ (fmt.Errorf)     │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Log with details │
│ (internal)       │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Return generic   │
│ error to client  │
└──────────────────┘
```

### Client Error Handling

```
API error received
        │
        ▼
┌──────────────────┐
│ Parse error      │
│ response         │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Display user-    │
│ friendly message │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Log details      │
│ (if debug mode)  │
└──────────────────┘
```
