# Migration History

This document details all database schema migrations for the pgEdge AI
Workbench, including the purpose, changes, and important notes for each
migration.

## Migration System

All migrations are embedded in `/collector/src/database/schema.go` in the
`registerMigrations()` function. Migrations are applied automatically on
collector startup in sequential order.

### Migration Tracking

The `schema_version` table tracks applied migrations:

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

Each migration records its version, description, and application timestamp.

## Migration 1: Initial Schema (Consolidated)

**Version**: 1
**Description**: "Initial schema with all tables"
**Status**: Active (all new installations start here)

### Purpose

Migration 1 consolidates 43 original development migrations into a single,
clean schema definition. This simplification:

- Reduces installation time for new deployments
- Provides a clean column organization pattern
- Maintains all original functionality
- Serves as the baseline for future migrations

### Column Organization Pattern

Migration 1 introduces a consistent column ordering:

1. Primary key columns
2. Foreign key columns
3. Control/status indicators (is_enabled, is_superuser, is_shared, etc.)
4. Most important to least important data fields
5. Timestamps (created_at, updated_at, applied_at, inserted_at, etc.)

### Tables Created

#### Core Schema Tables

**schema_version**
- Tracks applied migrations
- Used by SchemaManager to determine which migrations to apply

**user_accounts**
- User authentication and basic authorization
- Columns: id, username, email, is_superuser, full_name, password_hash,
  password_expiry, created_at, updated_at
- Constraints: username unique, non-empty checks
- Indexes: username, email

**service_tokens**
- Long-lived authentication tokens for automated systems
- Columns: id, name, token_hash, is_superuser, note, expires_at, created_at,
  updated_at
- Constraints: name unique, token_hash unique, non-empty checks
- Indexes: name, token_hash, expires_at

**user_tokens**
- Personal access tokens for user API access
- Columns: id, user_id, token_hash, expires_at, created_at
- Constraints: token_hash unique, expires_at must be future
- Foreign key: user_id -> user_accounts(id) CASCADE DELETE
- Indexes: user_id, token_hash, expires_at

**connections**
- PostgreSQL server connection configurations
- Columns: id, owner_username, owner_token, is_shared, is_monitored, name,
  host, hostaddr, port, database_name, username, password_encrypted, sslmode,
  sslcert, sslkey, sslrootcert, created_at, updated_at
- Constraints: exactly one owner (username XOR token), port range (1-65535)
- Foreign keys:
  - owner_username -> user_accounts(username) UPDATE CASCADE, DELETE RESTRICT
  - owner_token -> service_tokens(name) UPDATE CASCADE, DELETE RESTRICT
- Indexes: name, owner_username, owner_token, is_monitored (partial)

**probe_configs**
- Configuration for monitoring probes (global and per-connection)
- Columns: id, connection_id, is_enabled, name, description,
  collection_interval_seconds, retention_days, created_at, updated_at
- Constraints: positive intervals/retention, unique (name) for global,
  unique (name, connection_id) for server-specific
- Foreign key: connection_id -> connections(id) CASCADE DELETE
- Indexes: is_enabled, unique constraints

#### Metrics Schema

**metrics schema**
- Dedicated schema for time-series metrics data
- Separates operational data from configuration data

**Partitioned Time-Series Tables** (all in metrics schema):

All metrics tables follow this pattern:
- Partitioned by RANGE (collected_at)
- Include connection_id for multi-server monitoring
- Include collected_at timestamp
- Have indexes on (collected_at) and (connection_id, collected_at)

Metrics tables created:
1. **pg_stat_activity** - Current backend activity
2. **pg_stat_all_tables** - Table statistics per database
3. **pg_stat_all_indexes** - Index statistics per database
4. **pg_stat_replication** - Replication status
5. **pg_stat_replication_slots** - Replication slot status
6. **pg_stat_wal_receiver** - WAL receiver status
7. **pg_stat_recovery_prefetch** - Recovery prefetch stats
8. **pg_stat_subscription** - Logical replication subscription status
9. **pg_stat_subscription_stats** - Logical replication statistics
10. **pg_stat_ssl** - SSL connection information
11. **pg_stat_gssapi** - GSSAPI authentication info
12. **pg_stat_archiver** - WAL archiver status
13. **pg_stat_io** - I/O statistics by backend type
14. **pg_stat_bgwriter** - Background writer statistics
15. **pg_stat_checkpointer** - Checkpoint statistics
16. **pg_stat_wal** - WAL generation statistics
17. **pg_stat_slru** - SLRU cache statistics
18. **pg_stat_database** - Database-wide statistics
19. **pg_stat_database_conflicts** - Recovery conflict statistics
20. **pg_statio_all_tables** - Table I/O statistics
21. **pg_statio_all_indexes** - Index I/O statistics
22. **pg_statio_all_sequences** - Sequence I/O statistics
23. **pg_stat_user_functions** - User function statistics
24. **pg_stat_statements** - SQL statement execution statistics (extension)
25. **pg_sys_os_info** - OS information (system_stats extension)
26. **pg_sys_cpu_info** - CPU information
27. **pg_sys_cpu_usage_info** - CPU usage statistics
28. **pg_sys_memory_info** - Memory usage
29. **pg_sys_io_analysis_info** - I/O analysis
30. **pg_sys_disk_info** - Disk information
31. **pg_sys_load_avg_info** - System load averages
32. **pg_sys_process_info** - Process information
33. **pg_sys_network_info** - Network statistics
34. **pg_sys_cpu_memory_by_process** - Per-process resource usage

### Default Probe Configurations

Migration 1 inserts default global probe configurations (connection_id = NULL)
for all supported probes with sensible defaults:

- Server-scoped probes: 30-900 second intervals, 7 day retention
- Database-scoped probes: 300 second intervals, 7 day retention
- System stats probes: 600 second intervals, 7 day retention

### Important Notes

1. **Partition Management**: Metrics tables are partitioned but no partitions
   are created in this migration. Partitions are created automatically when
   data is inserted via the probe collection system.

2. **Extensions Required**: Some metrics tables depend on PostgreSQL
   extensions being installed on the monitored server:
   - pg_stat_statements
   - system_stats (for pg_sys_* probes)

3. **Column Data Types**: Carefully chosen for each metric type:
   - OID for PostgreSQL object identifiers
   - BIGINT for counters (to avoid overflow)
   - INET for IP addresses
   - TEXT for unbounded strings
   - VARCHAR with size limits where appropriate

## Migration 2: User Sessions

**Version**: 2
**Description**: "Add user_sessions table for authentication tokens"

### Purpose

Adds session management for user authentication, enabling:
- Temporary session tokens after username/password login
- Token-based API access
- Session expiration and renewal

### Changes

**New Table: user_sessions**

```sql
CREATE TABLE user_sessions (
    session_token TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_username
        FOREIGN KEY (username)
        REFERENCES user_accounts(username)
        ON DELETE CASCADE
);
```

**Indexes:**
- Primary key on session_token for fast lookups
- Index on username for user session queries
- Index on expires_at for cleanup queries

**Foreign Key:**
- username -> user_accounts(username) with CASCADE DELETE
  (deleting a user invalidates all their sessions)

### Session Token Behavior

- Session tokens are random UUIDs (not hashed, as they're randomly generated)
- Expires after 24 hours by default
- last_used_at updated on each request to track activity
- Inherits is_superuser status from user account

### Important Notes

1. **No Automatic Cleanup**: Expired sessions are not automatically deleted.
   Production systems should implement cleanup jobs.

2. **Security Model**: Session tokens are stored in plain text because they
   are randomly generated (unlike service tokens which are user-provided).

3. **Cascade Deletion**: Deleting a user account removes all their sessions
   immediately.

## Migration 3: PostgreSQL Settings Monitoring

**Version**: 3
**Description**: "Add pg_settings probe and metrics table"

### Purpose

Enables monitoring of PostgreSQL configuration settings over time, allowing:
- Configuration change tracking
- Historical configuration analysis
- Configuration drift detection

### Changes

**New Table: metrics.pg_settings**

```sql
CREATE TABLE metrics.pg_settings (
    connection_id INTEGER NOT NULL,
    database_name VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    setting TEXT,
    unit TEXT,
    category TEXT,
    short_desc TEXT,
    extra_desc TEXT,
    context TEXT,
    vartype TEXT,
    source TEXT,
    min_val TEXT,
    max_val TEXT,
    enumvals TEXT[],
    boot_val TEXT,
    reset_val TEXT,
    sourcefile TEXT,
    sourceline INTEGER,
    pending_restart BOOLEAN,
    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, database_name, collected_at, name)
) PARTITION BY RANGE (collected_at);
```

**Indexes:**
- idx_pg_settings_collected_at on (collected_at DESC)
- idx_pg_settings_connection_db_time on (connection_id, database_name,
  collected_at DESC)

**Default Probe Configuration:**
- Name: 'pg_settings'
- Description: 'Monitors PostgreSQL configuration settings'
- Interval: 600 seconds (10 minutes)
- Retention: 7 days

### Important Notes

1. **Per-Database Collection**: Settings are collected per database as some
   settings can have database-specific overrides (ALTER DATABASE ... SET).

2. **Array Column**: enumvals is TEXT[] for ENUM-type settings.

3. **Partitioning**: Uses same time-range partitioning as other metrics.

4. **Setting Changes**: Compare setting values over time to detect
   configuration changes. Pay special attention to pending_restart=true.

## Migration 4: Access Control Monitoring

**Version**: 4
**Description**: "Add pg_hba_file_rules and pg_ident_file_mappings probes"

### Purpose

Enables monitoring of PostgreSQL access control configuration:
- pg_hba.conf rules (client authentication)
- pg_ident.conf mappings (username mapping)

Useful for security auditing and access control management.

### Changes

**New Table: metrics.pg_hba_file_rules**

```sql
CREATE TABLE metrics.pg_hba_file_rules (
    connection_id INTEGER NOT NULL,
    rule_number INTEGER NOT NULL,
    file_name TEXT,
    line_number INTEGER,
    type TEXT,
    database TEXT[],
    user_name TEXT[],
    address TEXT,
    netmask TEXT,
    auth_method TEXT,
    options TEXT[],
    error TEXT,
    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, collected_at, rule_number)
) PARTITION BY RANGE (collected_at);
```

**New Table: metrics.pg_ident_file_mappings**

```sql
CREATE TABLE metrics.pg_ident_file_mappings (
    connection_id INTEGER NOT NULL,
    mapping_number INTEGER NOT NULL,
    file_name TEXT,
    line_number INTEGER,
    map_name TEXT,
    sys_name TEXT,
    pg_username TEXT,
    error TEXT,
    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, collected_at, mapping_number)
) PARTITION BY RANGE (collected_at);
```

**Indexes:**
- Time-based indexes on both tables for efficient queries

**Default Probe Configurations:**
- pg_hba_file_rules: 600 seconds (10 minutes), 7 days
- pg_ident_file_mappings: 600 seconds (10 minutes), 7 days

### Important Notes

1. **Array Columns**: database, user_name, and options are arrays to match
   PostgreSQL's multi-value support in pg_hba.conf.

2. **Error Column**: Captures parsing errors from invalid configuration lines.

3. **Security**: These tables contain sensitive security configuration.
   Ensure proper access controls.

4. **Change Detection**: Monitor for unexpected changes in authentication
   rules as they may indicate security issues.

## Migration 5: Enhanced User Tokens

**Version**: 5
**Description**: "Enhance user_tokens table for user-owned API tokens"

### Purpose

Enhanced the user_tokens table to properly support personal access tokens for
users, fixing the relationship between user tokens and user accounts.

### Changes

**Modified: user_tokens table**

Added proper foreign key constraint:

```sql
ALTER TABLE user_tokens
    ADD CONSTRAINT fk_user_tokens_user_id
    FOREIGN KEY (user_id)
    REFERENCES user_accounts(id)
    ON DELETE CASCADE;
```

**Note**: This constraint was missing in Migration 1's initial user_tokens
table definition but is now present in Migration 1 (the consolidation updated
Migration 1 to include it).

### Important Notes

1. **Idempotency**: The migration checks if the constraint already exists
   before attempting to add it.

2. **Cascade Behavior**: Deleting a user account now properly cascades to
   delete all their personal access tokens.

3. **Backward Compatibility**: This migration updates the schema without data
   loss for existing installations.

## Migration 6: User Groups and Privilege Management

**Version**: 6
**Description**: "User Groups and Privilege Management"

### Purpose

Implements a comprehensive Role-Based Access Control (RBAC) system:
- Hierarchical user groups
- Group-based privilege management
- MCP tool/resource access control
- Connection-level access control
- Token scoping (restricting tokens to specific connections/tools)

This migration transforms the simple is_superuser model into a full-featured
enterprise RBAC system.

### Changes

#### 1. user_groups Table

```sql
CREATE TABLE user_groups (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Purpose**: Define groups that can contain users and other groups.

**Indexes**: Unique index on name for fast lookups.

#### 2. group_memberships Table

```sql
CREATE TABLE group_memberships (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_group_id INTEGER NOT NULL,
    member_user_id INTEGER,
    member_group_id INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_exactly_one_member CHECK (
        (member_user_id IS NOT NULL AND member_group_id IS NULL) OR
        (member_user_id IS NULL AND member_group_id IS NOT NULL)
    ),
    CONSTRAINT fk_parent_group
        FOREIGN KEY (parent_group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    CONSTRAINT fk_member_user
        FOREIGN KEY (member_user_id)
        REFERENCES user_accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_member_group
        FOREIGN KEY (member_group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    UNIQUE (parent_group_id, member_user_id),
    UNIQUE (parent_group_id, member_group_id)
);
```

**Purpose**: Many-to-many relationship supporting both users and groups as
members of groups (hierarchical groups).

**Constraints**:
- Exactly one member type (user XOR group)
- Prevent duplicate memberships
- Cascade delete when parent group, member user, or member group is deleted

**Indexes**: Unique constraints and foreign key indexes.

**Circular Reference Prevention**: The application code uses recursive CTEs to
prevent circular group memberships when adding group-to-group relationships.

#### 3. mcp_privilege_identifiers Table

```sql
CREATE TABLE mcp_privilege_identifiers (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    identifier TEXT NOT NULL UNIQUE,
    item_type TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_item_type CHECK (item_type IN ('tool', 'resource', 'prompt'))
);
```

**Purpose**: Registry of all MCP items (tools, resources, prompts) that can
have privileges assigned.

**Item Types**:
- 'tool' - MCP tools (e.g., create_user, execute_query)
- 'resource' - MCP resources (e.g., connection metadata)
- 'prompt' - MCP prompts

**Indexes**: Unique index on identifier.

#### 4. group_mcp_privileges Table

```sql
CREATE TABLE group_mcp_privileges (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id INTEGER NOT NULL,
    privilege_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_group
        FOREIGN KEY (group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    CONSTRAINT fk_privilege
        FOREIGN KEY (privilege_id)
        REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
    UNIQUE (group_id, privilege_id)
);
```

**Purpose**: Grant specific MCP tool/resource/prompt access to groups.

**Inheritance**: Users in a group (including through nested group membership)
inherit all MCP privileges of that group.

**Indexes**: Unique constraint on (group_id, privilege_id), foreign key
indexes.

#### 5. group_connection_privileges Table

```sql
CREATE TABLE group_connection_privileges (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id INTEGER NOT NULL,
    connection_id INTEGER NOT NULL,
    access_level TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_group
        FOREIGN KEY (group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    CONSTRAINT fk_connection
        FOREIGN KEY (connection_id)
        REFERENCES connections(id) ON DELETE CASCADE,
    CONSTRAINT chk_access_level CHECK (access_level IN ('read', 'read_write')),
    UNIQUE (group_id, connection_id)
);
```

**Purpose**: Grant connection access to groups with specific access levels.

**Access Levels**:
- 'read' - Read-only access (query execution)
- 'read_write' - Full access (including DDL/DML)

**Indexes**: Unique constraint on (group_id, connection_id), foreign key
indexes.

#### 6. user_token_connection_scope Table

```sql
CREATE TABLE user_token_connection_scope (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_token_id INTEGER NOT NULL,
    connection_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_user_token
        FOREIGN KEY (user_token_id)
        REFERENCES user_tokens(id) ON DELETE CASCADE,
    CONSTRAINT fk_connection
        FOREIGN KEY (connection_id)
        REFERENCES connections(id) ON DELETE CASCADE,
    UNIQUE (user_token_id, connection_id)
);
```

**Purpose**: Restrict a user token to specific database connections.

**Behavior**: If no rows exist for a token, it has access to all connections
the user has access to. If rows exist, the token is restricted to only those
connections.

#### 7. user_token_mcp_scope Table

```sql
CREATE TABLE user_token_mcp_scope (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_token_id INTEGER NOT NULL,
    privilege_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_user_token
        FOREIGN KEY (user_token_id)
        REFERENCES user_tokens(id) ON DELETE CASCADE,
    CONSTRAINT fk_privilege
        FOREIGN KEY (privilege_id)
        REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
    UNIQUE (user_token_id, privilege_id)
);
```

**Purpose**: Restrict a user token to specific MCP tools/resources/prompts.

**Behavior**: If no rows exist, token has access to all MCP items the user has
access to. If rows exist, the token is restricted to only those items.

#### 8. service_token_connection_scope Table

```sql
CREATE TABLE service_token_connection_scope (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    service_token_id INTEGER NOT NULL,
    connection_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_service_token
        FOREIGN KEY (service_token_id)
        REFERENCES service_tokens(id) ON DELETE CASCADE,
    CONSTRAINT fk_connection
        FOREIGN KEY (connection_id)
        REFERENCES connections(id) ON DELETE CASCADE,
    UNIQUE (service_token_id, connection_id)
);
```

**Purpose**: Restrict service tokens to specific database connections.

**Behavior**: Same scoping logic as user tokens.

#### 9. service_token_mcp_scope Table

```sql
CREATE TABLE service_token_mcp_scope (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    service_token_id INTEGER NOT NULL,
    privilege_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_service_token
        FOREIGN KEY (service_token_id)
        REFERENCES service_tokens(id) ON DELETE CASCADE,
    CONSTRAINT fk_privilege
        FOREIGN KEY (privilege_id)
        REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
    UNIQUE (service_token_id, privilege_id)
);
```

**Purpose**: Restrict service tokens to specific MCP tools/resources/prompts.

**Behavior**: Same scoping logic as user tokens.

### Authorization Flow

The privilege system implements a multi-tier authorization check:

1. **Superuser bypass**: If user/token has is_superuser=true, grant access
2. **Group privileges**: Check if user is in any group with the required
   privilege
3. **Token scoping**: If using a token, verify the token isn't further
   restricted
4. **Connection access**: For connection operations, verify group has access
   and token isn't restricted

### Important Notes

1. **Hierarchical Inheritance**: A user in group B, where B is a member of
   group A, inherits all privileges from both groups.

2. **Token Scoping is Restrictive**: Scoping can only reduce access, never
   expand it beyond the user's base privileges.

3. **Cascade Deletes**: Deleting a group cascades to all memberships and
   privileges. Deleting a token cascades to all scopes.

4. **Privilege Identifiers**: New MCP tools/resources/prompts must be
   registered in mcp_privilege_identifiers before privileges can be granted.

5. **Performance**: Privilege checks use recursive CTEs to resolve group
   hierarchies. Monitor query performance on deep group hierarchies.

6. **Migration Safety**: All tables use IF NOT EXISTS for idempotent
   re-application.

## Migration Best Practices

### Adding New Migrations

When creating a new migration (e.g., Migration 7):

1. **Increment Version**: Use next sequential number
2. **Descriptive Description**: Clearly state what the migration does
3. **Idempotent SQL**: Always use IF NOT EXISTS, IF EXISTS
4. **Test Thoroughly**: Test on empty DB and DB with existing data
5. **Document Here**: Add section to this file explaining the changes
6. **Consider Rollback**: Document how to manually undo if needed

### Schema Modification Guidelines

1. **Never Modify Past Migrations**: Once applied in production, migrations
   are immutable
2. **Add, Don't Alter**: Prefer adding new columns/tables over altering
   existing ones
3. **Default Values**: Provide sensible defaults for new NOT NULL columns
4. **Index Carefully**: Add indexes for foreign keys and common query patterns
5. **Comment Everything**: Use COMMENT ON for all objects

### Testing Migrations

1. **Test Clean Install**: Verify all migrations apply to empty database
2. **Test Upgrade Path**: Apply each migration starting from each previous
   version
3. **Test Idempotency**: Re-run migrations to verify IF NOT EXISTS clauses work
4. **Verify Constraints**: Ensure foreign keys, checks, and unique constraints
   work correctly
5. **Check Performance**: Test query performance on large datasets

## Rollback Procedures

### Important Warning

PostgreSQL migrations are forward-only in this system. There is no automated
rollback mechanism. To roll back a migration:

1. **Backup First**: Always backup before attempting rollback
2. **Manual Reversal**: Write and execute reverse SQL statements
3. **Update schema_version**: Delete the row for the rolled-back version
4. **Restart Collector**: Ensure the collector doesn't re-apply the migration

### Example Rollback (Migration 2)

```sql
-- Backup first!
-- pg_dump -Fc -f backup_before_rollback.dump ai_workbench

-- Drop the table
DROP TABLE IF EXISTS user_sessions;

-- Remove from version tracking
DELETE FROM schema_version WHERE version = 2;

-- Verify
SELECT * FROM schema_version ORDER BY version;
```

## Version Compatibility

| Migration | Min PostgreSQL Version | Reason |
|-----------|------------------------|--------|
| 1 | 13 | GENERATED ALWAYS AS IDENTITY, modern partitioning, pg_stat views |
| 2 | 13 | (same as Migration 1) |
| 3 | 13 | pg_settings system view |
| 4 | 13 | pg_hba_file_rules, pg_ident_file_mappings views (PG 10+) |
| 5 | 13 | (no new requirements) |
| 6 | 13 | Recursive CTEs for hierarchy (PG 8.4+), but aligns with Migration 1 |

**Recommended Minimum**: PostgreSQL 14 for best performance and stability.

## Future Migration Considerations

Potential future migrations might include:

1. **Audit Logging**: Track all privilege changes and administrative actions
2. **Query History**: Store executed query history for compliance
3. **Alert Definitions**: User-defined monitoring alerts and thresholds
4. **Dashboard Definitions**: Saved dashboard layouts and configurations
5. **Data Export Jobs**: Scheduled exports of metrics data
6. **Retention Policies**: Automated partition management and cleanup
7. **Multi-Tenancy**: Organization/tenant isolation
8. **API Rate Limiting**: Track and limit API usage per user/token
