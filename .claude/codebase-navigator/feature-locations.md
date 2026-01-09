# Feature Locations

This document maps features to their implementation locations in the codebase.

## Authentication & Authorization

### User Authentication

| Feature | Location | Description |
|---------|----------|-------------|
| Password hashing | `/server/src/auth/` | User password storage and verification |
| Token generation | `/server/src/auth/tokens.go` | Service and user token creation |
| Token validation | `/server/src/auth/tokens.go` | Token verification and lookup |
| Session management | `/server/src/auth/sessions.go` | User session lifecycle |

### Role-Based Access Control (RBAC)

| Feature | Location | Description |
|---------|----------|-------------|
| Group management | `/server/src/auth/rbac.go` | User groups and memberships |
| Privilege checking | `/server/src/auth/rbac.go` | Authorization verification |
| MCP privileges | `/server/src/auth/` | Tool and resource access control |
| Connection privileges | `/server/src/auth/` | Database connection access |

### Database Schema

| Table | Purpose |
|-------|---------|
| `user_accounts` | User credentials and metadata |
| `user_tokens` | User API tokens |
| `service_tokens` | Service API tokens |
| `user_sessions` | Active user sessions |
| `user_groups` | Authorization groups |
| `group_memberships` | User-to-group mappings |
| `group_mcp_privileges` | MCP tool/resource permissions |
| `group_connection_privileges` | Database connection permissions |

## Database Connection Management

### Server-Side

| Feature | Location | Description |
|---------|----------|-------------|
| Connection pool | `/server/src/database/pool.go` | pgx connection pooling |
| Connection CRUD | `/server/src/database/` | Create, read, update, delete connections |
| Credential encryption | `/server/src/database/` | Password encryption at rest |

### Collector-Side

| Feature | Location | Description |
|---------|----------|-------------|
| Connection pool | `/collector/src/database/pool.go` | pgx connection pooling |
| Monitored connections | `/collector/src/database/` | Connections being monitored |
| Semaphore control | `/collector/src/database/` | Concurrent query limiting |

### MCP Tools

| Tool | Location | Purpose |
|------|----------|---------|
| `connection_create` | `/server/src/mcp/tools/` | Create new connection |
| `connection_list` | `/server/src/mcp/tools/` | List available connections |
| `connection_update` | `/server/src/mcp/tools/` | Modify connection settings |
| `connection_delete` | `/server/src/mcp/tools/` | Remove connection |
| `connection_test` | `/server/src/mcp/tools/` | Test connection validity |

## MCP Protocol Implementation

### Core Protocol

| Feature | Location | Description |
|---------|----------|-------------|
| JSON-RPC handler | `/server/src/mcp/handler.go` | Request routing and response |
| Protocol methods | `/server/src/mcp/` | initialize, ping, etc. |
| Error handling | `/server/src/mcp/` | MCP error responses |

### Tools

| Category | Location | Examples |
|----------|----------|----------|
| Connection tools | `/server/src/mcp/tools/` | CRUD operations |
| Query tools | `/server/src/mcp/tools/` | SQL execution |
| User tools | `/server/src/mcp/tools/` | User management |
| Privilege tools | `/server/src/mcp/tools/` | RBAC management |

### Resources

| Resource | Location | Purpose |
|----------|----------|---------|
| Connection info | `/server/src/mcp/resources/` | Connection metadata |
| Schema info | `/server/src/mcp/resources/` | Database schema details |

## Data Collection & Metrics

### Collector Core

| Feature | Location | Description |
|---------|----------|-------------|
| Collector loop | `/collector/src/collector/` | Main collection cycle |
| Probe execution | `/collector/src/probes/` | Metric gathering |
| Scheduling | `/collector/src/collector/` | Collection timing |

### Probes

| Probe | Location | Metrics Collected |
|-------|----------|-------------------|
| pg_stat_activity | `/collector/src/probes/` | Active queries |
| pg_stat_database | `/collector/src/probes/` | Database statistics |
| pg_stat_replication | `/collector/src/probes/` | Replication status |
| pg_stat_user_tables | `/collector/src/probes/` | Table statistics |
| pg_stat_user_indexes | `/collector/src/probes/` | Index statistics |

### Metrics Storage

| Table | Schema | Purpose |
|-------|--------|---------|
| `pg_stat_*` | `metrics` | Partitioned time-series data |
| `probe_configs` | `public` | Probe enable/disable settings |

## Web Client Features

### Pages

| Page | Location | Purpose |
|------|----------|---------|
| Dashboard | `/client/src/pages/` | Main overview |
| Connections | `/client/src/pages/` | Connection management |
| Query | `/client/src/pages/` | SQL query interface |
| Settings | `/client/src/pages/` | User preferences |

### Components

| Component | Location | Purpose |
|-----------|----------|---------|
| Connection list | `/client/src/components/` | Display connections |
| Query editor | `/client/src/components/` | SQL input |
| Results table | `/client/src/components/` | Query results display |
| Navigation | `/client/src/components/` | App navigation |

### State Management

| Store | Location | Purpose |
|-------|----------|---------|
| Auth state | `/client/src/stores/` | User authentication |
| Connection state | `/client/src/stores/` | Active connections |
| Query state | `/client/src/stores/` | Query history and results |

## Configuration

### Server Configuration

| Setting | Location | Description |
|---------|----------|-------------|
| Database URL | Environment/config | Server database connection |
| Listen address | Environment/config | HTTP server bind address |
| Auth settings | Environment/config | Token expiration, etc. |

### Collector Configuration

| Setting | Location | Description |
|---------|----------|-------------|
| Database URL | Environment/config | Collector database connection |
| Collection interval | Environment/config | Metric gathering frequency |
| Probe settings | Database | Per-connection probe config |

### Client Configuration

| Setting | Location | Description |
|---------|----------|-------------|
| API URL | Environment/config | Server API endpoint |
| Theme | `/client/src/theme/` | MUI theme settings |
