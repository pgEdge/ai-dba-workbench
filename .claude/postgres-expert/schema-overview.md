# pgEdge AI Workbench Database Schema Overview

## Architecture Summary

The pgEdge AI Workbench uses a PostgreSQL database for its metadata storage,
following a migration-based schema management approach. All migrations are
embedded in the compiled binary (`collector/src/database/schema.go`), making
deployment simple and ensuring schema consistency.

## Database Purpose

This database serves as the **metadata store** for the AI Workbench system. It
tracks:

- User authentication and authorization (RBAC)
- Database connection configurations
- Monitoring probe configurations
- Collected metrics from monitored PostgreSQL servers
- Service and user token management
- Group-based privilege management

**Important**: This is NOT the monitored PostgreSQL database. This is the
Workbench's own control/metadata database.

## Schema Organization

### Core Tables (Migration 1)

1. **schema_version** - Migration tracking
2. **user_accounts** - User authentication
3. **service_tokens** - Long-lived service authentication tokens
4. **user_tokens** - Personal access tokens for users
5. **connections** - PostgreSQL server connection definitions
6. **probe_configs** - Monitoring probe configuration

### Metrics Schema

All collected metrics are stored in the `metrics` schema with partitioned
tables for time-series data:

- **metrics.pg_stat_activity** - Current server activity
- **metrics.pg_stat_all_tables** - Table statistics
- **metrics.pg_stat_all_indexes** - Index statistics
- **metrics.pg_stat_replication** - Replication status
- **metrics.pg_stat_database** - Database-wide statistics
- And 20+ more metrics tables (see Migration 1 for complete list)

### Session and Authentication (Migration 2)

- **user_sessions** - Active user sessions with tokens

### Settings Monitoring (Migration 3)

- **metrics.pg_settings** - PostgreSQL configuration settings

### Access Control Monitoring (Migration 4)

- **metrics.pg_hba_file_rules** - pg_hba.conf rules
- **metrics.pg_ident_file_mappings** - pg_ident.conf mappings

### Enhanced User Tokens (Migration 5)

Modified **user_tokens** table to support user-owned API tokens with
proper foreign key relationships.

### RBAC System (Migration 6)

Complete role-based access control implementation:

- **user_groups** - Group definitions
- **group_memberships** - User and group membership (hierarchical)
- **mcp_privilege_identifiers** - Registry of available privileges
- **group_mcp_privileges** - MCP tool/resource access grants
- **group_connection_privileges** - Database connection access grants
- **user_token_connection_scope** - Token-level connection restrictions
- **user_token_mcp_scope** - Token-level MCP privilege restrictions
- **service_token_connection_scope** - Service token connection restrictions
- **service_token_mcp_scope** - Service token MCP privilege restrictions

## Key Design Patterns

### 1. Time-Series Partitioning

All metrics tables use **PARTITION BY RANGE (collected_at)** to efficiently
manage large volumes of time-series data. This enables:

- Fast queries on recent data
- Efficient data retention/cleanup
- Optimal index performance

### 2. Ownership Model

Connections can be owned by either:
- A user account (`owner_username`)
- A service token (`owner_token`)

Enforced via CHECK constraint ensuring exactly one owner type is set.

### 3. Hierarchical Groups

Groups can contain both users AND other groups, enabling nested privilege
inheritance. Circular references are prevented via recursive CTE checks.

### 4. Token Scoping

Tokens (both user and service) can be restricted to:
- Specific database connections
- Specific MCP tools/resources/prompts

This provides fine-grained access control beyond simple superuser flags.

### 5. Comprehensive Commenting

Every table, column, constraint, and index has a COMMENT describing its
purpose. This makes the schema self-documenting.

### 6. Defensive Constraints

The schema uses extensive CHECK constraints to enforce data integrity:
- Non-empty string validation
- Positive number ranges
- Valid port numbers (1-65535)
- Future timestamp validation
- Mutually exclusive column pairs

## Performance Considerations

### Indexes

The schema includes strategic indexes for:

1. **Authentication lookups** - Token hash indexes for fast auth
2. **Foreign key relationships** - All FK columns indexed
3. **Time-range queries** - Composite indexes on (connection_id, collected_at)
4. **Partial indexes** - E.g., only on enabled probes
5. **Unique constraints** - Enforced via unique indexes

### Partitioning Strategy

Metrics tables are partitioned by time range, typically by day or week. New
partitions must be created as data ages. Consider automated partition
management for production deployments.

### Connection Pooling

The system uses pgx connection pooling. Monitor pool utilization via:
- Active connections
- Idle connections
- Wait times

## Migration Strategy

### Version Control

Migrations are numbered sequentially (1, 2, 3...) and tracked in the
`schema_version` table. Each migration records:
- Version number
- Description
- Applied timestamp

### Idempotency

All migrations use `IF NOT EXISTS` clauses, making them safe to re-run.
However, the migration system tracks applied versions and skips already-applied
migrations.

### Migration 1 Consolidation

Migration 1 is special - it consolidated 43 original migrations into a single
comprehensive migration. This simplifies new installations while maintaining
upgrade compatibility.

## PostgreSQL Version Requirements

The schema is designed for **PostgreSQL 13+** due to use of:

- `GENERATED ALWAYS AS IDENTITY` for auto-incrementing columns
- Partitioning syntax introduced in PG 10, enhanced in PG 11+
- pg_stat_* views structure (some views added in PG 13+)

**Recommended**: PostgreSQL 14 or later for optimal performance and feature
support.

## Security Model

### Authentication

1. **User accounts** - Username/password (SHA-256 hashed)
2. **Session tokens** - Temporary tokens from successful login
3. **Service tokens** - Long-lived tokens for automation
4. **User tokens** - Personal access tokens

### Authorization

Three-tier privilege model:

1. **Superuser flag** - Bypass all checks (users and tokens)
2. **Group-based privileges** - RBAC for normal users
3. **Token scoping** - Further restriction of token capabilities

### Isolation

- Connection ownership ensures users only access their own connections
- Token scoping restricts access to specific connections/tools
- Foreign key CASCADE rules prevent orphaned data

## Data Retention

### Default Retention Periods

From `probe_configs` defaults:
- Most metrics: 7 days
- Long-term trends: 28 days

### Cleanup Strategy

No automatic cleanup is implemented. Production deployments should:

1. **Partition management** - Create new partitions, drop old ones
2. **Scheduled cleanup** - Cron jobs or pg_cron for token expiration
3. **Monitoring** - Alert on disk usage, partition sizes

## Common Queries

### Check Schema Version
```sql
SELECT * FROM schema_version ORDER BY version DESC;
```

### List All Tables with Row Counts
```sql
SELECT schemaname, tablename,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables
WHERE schemaname IN ('public', 'metrics')
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

### Find Active Monitoring
```sql
SELECT c.name, c.host, c.port, c.is_monitored,
       COUNT(pc.id) as probe_count
FROM connections c
LEFT JOIN probe_configs pc ON pc.connection_id = c.id AND pc.is_enabled
GROUP BY c.id, c.name, c.host, c.port, c.is_monitored;
```

### Check User Privileges
```sql
SELECT u.username, u.is_superuser,
       COUNT(DISTINCT gm.parent_group_id) as group_count
FROM user_accounts u
LEFT JOIN group_memberships gm ON gm.member_user_id = u.id
GROUP BY u.id, u.username, u.is_superuser;
```

## See Also

- `migration-history.md` - Detailed migration changelog
- `privilege-system.md` - RBAC implementation details
- `performance-notes.md` - Tuning and optimization
- `relationships.md` - Entity relationship diagrams
