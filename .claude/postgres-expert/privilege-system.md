# RBAC and Privilege System

This document provides detailed documentation of the Role-Based Access Control
(RBAC) system implemented in Migration 6.

## System Overview

The pgEdge AI Workbench implements a sophisticated multi-tier privilege system:

1. **Superuser Flag** - Bypass mechanism (backward compatible)
2. **Group-Based RBAC** - Fine-grained privilege management
3. **Hierarchical Groups** - Groups can contain other groups
4. **Token Scoping** - Further restriction of token capabilities
5. **Connection-Level Access** - Per-database access control
6. **MCP Item Privileges** - Per-tool/resource/prompt access

## Architecture

### Entity Hierarchy

```
user_accounts
    ├─> user_tokens (personal access tokens)
    │   ├─> user_token_connection_scope (limit to specific connections)
    │   └─> user_token_mcp_scope (limit to specific tools/resources)
    └─> group_memberships (direct membership)
        └─> user_groups
            ├─> group_mcp_privileges (tool/resource access)
            ├─> group_connection_privileges (database access)
            └─> group_memberships (nested groups)

service_tokens
    ├─> service_token_connection_scope (limit to specific connections)
    └─> service_token_mcp_scope (limit to specific tools/resources)
```

## Core Tables

### user_groups

**Purpose**: Define groups that can contain users and other groups.

**Schema**:
```sql
CREATE TABLE user_groups (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Usage**:
- Create logical groupings (departments, teams, roles)
- Organize users hierarchically
- Apply privileges to groups instead of individual users

**Examples**:
- "developers" - All development team members
- "dba-team" - Database administrators
- "read-only-analysts" - Users who can only read data
- "production-access" - Users who can access production databases

### group_memberships

**Purpose**: Many-to-many relationship supporting hierarchical group structure.

**Schema**:
```sql
CREATE TABLE group_memberships (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_group_id INTEGER NOT NULL,
    member_user_id INTEGER,
    member_group_id INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Constraints ensure exactly one member type
    CONSTRAINT chk_exactly_one_member CHECK (...),
    CONSTRAINT fk_parent_group FOREIGN KEY (parent_group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    CONSTRAINT fk_member_user FOREIGN KEY (member_user_id)
        REFERENCES user_accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_member_group FOREIGN KEY (member_group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    UNIQUE (parent_group_id, member_user_id),
    UNIQUE (parent_group_id, member_group_id)
);
```

**Key Features**:

1. **Dual Membership**: Can add users OR groups as members
2. **Hierarchical**: Groups can contain other groups
3. **Inheritance**: Members inherit all privileges from parent groups
4. **Circular Prevention**: Application code prevents circular references

**Membership Types**:
- **Direct User Membership**: member_user_id is set, member_group_id is NULL
- **Group Nesting**: member_group_id is set, member_user_id is NULL

**Example Hierarchy**:
```
all-staff (group)
    ├─> engineering (group)
    │   ├─> backend-team (group)
    │   │   ├─> alice (user)
    │   │   └─> bob (user)
    │   └─> frontend-team (group)
    │       └─> charlie (user)
    └─> operations (group)
        ├─> dba-team (group)
        │   └─> david (user)
        └─> sre-team (group)
            └─> eve (user)
```

In this example:
- alice inherits privileges from: backend-team, engineering, all-staff
- charlie inherits from: frontend-team, engineering, all-staff
- david inherits from: dba-team, operations, all-staff

### mcp_privilege_identifiers

**Purpose**: Registry of all MCP items that can have privileges.

**Schema**:
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

**Item Types**:

1. **tool** - MCP tools (functions that perform actions)
   - Examples: create_user, delete_connection, execute_query
   - Most privilege checks are against tools

2. **resource** - MCP resources (data endpoints)
   - Examples: connection_list, metrics_data
   - Less commonly used in current implementation

3. **prompt** - MCP prompts (template prompts)
   - Examples: troubleshooting_guide, query_optimizer
   - Future expansion area

**Registration**: Before a privilege can be granted, the identifier must be
registered in this table. Typically done during server initialization.

**Example Identifiers**:
```sql
INSERT INTO mcp_privilege_identifiers (identifier, item_type, description)
VALUES
    ('create_user', 'tool', 'Create a new user account'),
    ('delete_user', 'tool', 'Delete a user account'),
    ('update_user', 'tool', 'Update an existing user account'),
    ('create_user_group', 'tool', 'Create a new user group'),
    ('grant_mcp_privilege', 'tool', 'Grant MCP privileges to a group'),
    ('execute_query', 'tool', 'Execute arbitrary SQL queries'),
    ('list_connections', 'tool', 'List database connections');
```

### group_mcp_privileges

**Purpose**: Grant groups access to specific MCP items.

**Schema**:
```sql
CREATE TABLE group_mcp_privileges (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id INTEGER NOT NULL,
    privilege_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_group FOREIGN KEY (group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    CONSTRAINT fk_privilege FOREIGN KEY (privilege_id)
        REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
    UNIQUE (group_id, privilege_id)
);
```

**Access Pattern**:
1. User requests to use an MCP tool
2. System checks if user has is_superuser=true (bypass)
3. System finds all groups the user is in (including via hierarchy)
4. System checks if any of those groups has the required privilege
5. Access granted if privilege found, denied otherwise

**Example**:
```sql
-- Grant "developers" group access to create_user tool
INSERT INTO group_mcp_privileges (group_id, privilege_id)
SELECT g.id, p.id
FROM user_groups g, mcp_privilege_identifiers p
WHERE g.name = 'developers' AND p.identifier = 'create_user';
```

### group_connection_privileges

**Purpose**: Grant groups access to specific database connections.

**Schema**:
```sql
CREATE TABLE group_connection_privileges (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id INTEGER NOT NULL,
    connection_id INTEGER NOT NULL,
    access_level TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_group FOREIGN KEY (group_id)
        REFERENCES user_groups(id) ON DELETE CASCADE,
    CONSTRAINT fk_connection FOREIGN KEY (connection_id)
        REFERENCES connections(id) ON DELETE CASCADE,
    CONSTRAINT chk_access_level CHECK (access_level IN ('read', 'read_write')),
    UNIQUE (group_id, connection_id)
);
```

**Access Levels**:

1. **read** - Read-only access
   - SELECT queries only
   - View table structures
   - No data modification

2. **read_write** - Full access
   - All SELECT operations
   - INSERT, UPDATE, DELETE
   - DDL operations (CREATE, ALTER, DROP)
   - Administrative functions

**Access Pattern**:
1. User attempts to access a connection
2. System checks if user owns the connection (bypass)
3. System checks if connection is marked as shared
4. System finds all groups user is in
5. System checks if any group has connection privilege
6. Access granted with highest access level found

**Example**:
```sql
-- Grant "developers" read access to staging database
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
SELECT g.id, c.id, 'read'
FROM user_groups g, connections c
WHERE g.name = 'developers' AND c.name = 'staging-db';

-- Grant "dba-team" read_write access to production database
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
SELECT g.id, c.id, 'read_write'
FROM user_groups g, connections c
WHERE g.name = 'dba-team' AND c.name = 'production-db';
```

## Token Scoping

Token scoping provides an additional layer of restriction on top of user/group
privileges. Scoping can only REDUCE access, never expand it.

### user_token_connection_scope

**Purpose**: Restrict personal access tokens to specific connections.

**Schema**:
```sql
CREATE TABLE user_token_connection_scope (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_token_id INTEGER NOT NULL,
    connection_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_user_token FOREIGN KEY (user_token_id)
        REFERENCES user_tokens(id) ON DELETE CASCADE,
    CONSTRAINT fk_connection FOREIGN KEY (connection_id)
        REFERENCES connections(id) ON DELETE CASCADE,
    UNIQUE (user_token_id, connection_id)
);
```

**Behavior**:
- **No rows for token**: Token has access to ALL connections the user can
  access
- **Rows exist for token**: Token is restricted to ONLY those specific
  connections

**Use Cases**:
- Create a token for CI/CD that can only access test databases
- Create a mobile app token that can only access specific APIs
- Temporary contractor access to specific connections

**Example**:
```sql
-- Create token for user
INSERT INTO user_tokens (user_id, token_hash, expires_at)
VALUES (5, 'hash_of_token', NOW() + INTERVAL '30 days')
RETURNING id; -- Returns 100

-- Restrict token to only dev and staging connections
INSERT INTO user_token_connection_scope (user_token_id, connection_id)
VALUES
    (100, 1),  -- dev-db
    (100, 2);  -- staging-db
-- Now token 100 can ONLY access connections 1 and 2, even if user 5
-- has access to other connections
```

### user_token_mcp_scope

**Purpose**: Restrict personal access tokens to specific MCP tools/resources.

**Schema**:
```sql
CREATE TABLE user_token_mcp_scope (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_token_id INTEGER NOT NULL,
    privilege_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_user_token FOREIGN KEY (user_token_id)
        REFERENCES user_tokens(id) ON DELETE CASCADE,
    CONSTRAINT fk_privilege FOREIGN KEY (privilege_id)
        REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
    UNIQUE (user_token_id, privilege_id)
);
```

**Behavior**: Same as connection scoping but for MCP items.

**Use Cases**:
- Read-only token (only list/view tools, no create/update/delete)
- Monitoring token (only metrics collection tools)
- Specific workflow token (only tools needed for a particular automation)

**Example**:
```sql
-- Create a read-only token that can only list and view data
INSERT INTO user_token_mcp_scope (user_token_id, privilege_id)
SELECT 100, id FROM mcp_privilege_identifiers
WHERE identifier IN ('list_connections', 'list_user_groups', 'get_metrics');
```

### service_token_connection_scope

**Purpose**: Restrict service tokens to specific connections.

**Schema**: Same pattern as user_token_connection_scope but references
service_tokens.

**Use Cases**:
- Monitoring agent token (only access to monitored databases)
- Backup service token (only access to backup connections)
- Application-specific tokens

### service_token_mcp_scope

**Purpose**: Restrict service tokens to specific MCP tools/resources.

**Schema**: Same pattern as user_token_mcp_scope but references service_tokens.

**Use Cases**:
- Collector service token (only data collection tools)
- Metrics exporter token (only metrics-related tools)

## Authorization Flow

### Complete Authorization Check

When a user/token requests access to an MCP tool:

```
1. Extract authentication token from request

2. Identify principal (user or service token)

3. CHECK: Is principal a superuser?
   YES → GRANT ACCESS (bypass all other checks)
   NO  → Continue to step 4

4. CHECK: Is this a token with MCP scope restrictions?
   YES → Verify requested tool is in token's MCP scope
         NOT IN SCOPE → DENY ACCESS
         IN SCOPE → Continue to step 5
   NO  → Continue to step 5

5. CHECK: Does any group the user belongs to have the MCP privilege?
   For user principals:
     a. Find all direct group memberships
     b. Recursively find all parent groups (using WITH RECURSIVE)
     c. Check if any group has the privilege in group_mcp_privileges
   For service token principals:
     Skip (service tokens don't have group memberships)

6. RESULT:
   - Privilege found → GRANT ACCESS
   - No privilege found → DENY ACCESS
```

### Connection Access Check

When accessing a database connection:

```
1. Extract authentication token from request

2. Identify principal and target connection

3. CHECK: Does user/token own this connection?
   (connections.owner_username = user OR connections.owner_token = token)
   YES → GRANT ACCESS with read_write level (owner has full access)
   NO  → Continue to step 4

4. CHECK: Is principal a superuser?
   YES → GRANT ACCESS with read_write level
   NO  → Continue to step 5

5. CHECK: Is this a token with connection scope restrictions?
   YES → Verify connection is in token's connection scope
         NOT IN SCOPE → DENY ACCESS
         IN SCOPE → Continue to step 6
   NO  → Continue to step 6

6. CHECK: Does any group the user belongs to have connection privilege?
   a. Find all user's groups (direct + recursive hierarchy)
   b. Check group_connection_privileges for this connection
   c. Collect all access_level values found

7. RESULT:
   - No privileges found → DENY ACCESS
   - Only 'read' found → GRANT ACCESS with read level
   - Any 'read_write' found → GRANT ACCESS with read_write level
```

## Privilege Resolution

### Hierarchical Group Resolution

**Algorithm**: Recursive CTE to find all groups a user belongs to.

```sql
WITH RECURSIVE user_groups_recursive AS (
    -- Base case: direct memberships
    SELECT parent_group_id AS group_id
    FROM group_memberships
    WHERE member_user_id = :user_id

    UNION

    -- Recursive case: groups containing member groups
    SELECT gm.parent_group_id
    FROM group_memberships gm
    INNER JOIN user_groups_recursive ugr ON gm.member_group_id = ugr.group_id
)
SELECT DISTINCT group_id FROM user_groups_recursive;
```

**Example**:

Given hierarchy:
```
all-staff
  └─> engineering
      └─> backend-team
          └─> alice (user_id=5)
```

Query for alice returns: [backend-team, engineering, all-staff]

### Privilege Aggregation

**MCP Privileges**: Union of all privileges from all groups
- If user is in groups A, B, C
- User has access to tools granted to A ∪ B ∪ C

**Connection Privileges**: Highest access level wins
- If group A grants 'read' to connection X
- And group B grants 'read_write' to connection X
- User gets 'read_write' access

### Token Scope Intersection

**Token scoping acts as a filter:**
- User privileges: Set U
- Token connection scope: Set T
- Effective privileges: U ∩ T

**Example**:
- User has access to connections [1, 2, 3, 4, 5]
- Token is scoped to connections [3, 4, 6]
- Token can only access connections [3, 4] (intersection)

## Common Privilege Patterns

### Read-Only Analyst Group

```sql
-- 1. Create group
INSERT INTO user_groups (name, description)
VALUES ('analysts', 'Read-only data analysts');

-- 2. Grant read access to specific connections
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
SELECT g.id, c.id, 'read'
FROM user_groups g
CROSS JOIN connections c
WHERE g.name = 'analysts'
  AND c.name IN ('analytics-db', 'reporting-db');

-- 3. Grant limited MCP tools
INSERT INTO group_mcp_privileges (group_id, privilege_id)
SELECT g.id, p.id
FROM user_groups g
CROSS JOIN mcp_privilege_identifiers p
WHERE g.name = 'analysts'
  AND p.identifier IN (
    'execute_query',      -- Can run SELECT queries
    'list_connections',   -- Can see available connections
    'get_query_results'   -- Can retrieve query results
  );

-- 4. Add users to group
INSERT INTO group_memberships (parent_group_id, member_user_id)
SELECT g.id, u.id
FROM user_groups g, user_accounts u
WHERE g.name = 'analysts'
  AND u.username IN ('analyst1', 'analyst2', 'analyst3');
```

### Database Administrator Group

```sql
-- 1. Create DBA group
INSERT INTO user_groups (name, description)
VALUES ('dba-team', 'Database administrators with full access');

-- 2. Grant read_write access to all connections
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
SELECT g.id, c.id, 'read_write'
FROM user_groups g
CROSS JOIN connections c
WHERE g.name = 'dba-team';

-- 3. Grant all administrative MCP tools
INSERT INTO group_mcp_privileges (group_id, privilege_id)
SELECT g.id, p.id
FROM user_groups g
CROSS JOIN mcp_privilege_identifiers p
WHERE g.name = 'dba-team'
  AND p.item_type = 'tool';  -- Grant all tools

-- 4. Add DBAs to group
INSERT INTO group_memberships (parent_group_id, member_user_id)
SELECT g.id, u.id
FROM user_groups g, user_accounts u
WHERE g.name = 'dba-team'
  AND u.username IN ('dba1', 'dba2');
```

### Nested Team Structure

```sql
-- 1. Create organization structure
INSERT INTO user_groups (name, description) VALUES
    ('company', 'All company employees'),
    ('engineering', 'Engineering department'),
    ('backend-team', 'Backend developers'),
    ('frontend-team', 'Frontend developers');

-- 2. Create hierarchy
INSERT INTO group_memberships (parent_group_id, member_group_id)
SELECT parent.id, child.id
FROM user_groups parent, user_groups child
WHERE (parent.name = 'company' AND child.name = 'engineering')
   OR (parent.name = 'engineering' AND child.name = 'backend-team')
   OR (parent.name = 'engineering' AND child.name = 'frontend-team');

-- 3. Grant privileges at appropriate levels
-- All company: read access to shared resources
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
SELECT g.id, c.id, 'read'
FROM user_groups g, connections c
WHERE g.name = 'company' AND c.name = 'shared-analytics';

-- Engineering: access to dev/staging
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
SELECT g.id, c.id, 'read_write'
FROM user_groups g, connections c
WHERE g.name = 'engineering' AND c.name IN ('dev-db', 'staging-db');

-- Backend team: additional backend-specific access
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
SELECT g.id, c.id, 'read_write'
FROM user_groups g, connections c
WHERE g.name = 'backend-team' AND c.name = 'backend-services-db';

-- 4. Add users to leaf groups
INSERT INTO group_memberships (parent_group_id, member_user_id)
SELECT g.id, u.id
FROM user_groups g, user_accounts u
WHERE (g.name = 'backend-team' AND u.username IN ('alice', 'bob'))
   OR (g.name = 'frontend-team' AND u.username IN ('charlie', 'dana'));
```

Result:
- alice and bob: inherit from backend-team + engineering + company
- charlie and dana: inherit from frontend-team + engineering + company

### Scoped Service Token

```sql
-- 1. Create service token for monitoring agent
INSERT INTO service_tokens (name, token_hash, is_superuser)
VALUES ('monitoring-agent', 'hash_here', FALSE)
RETURNING id;  -- Returns 50

-- 2. Scope to only production databases
INSERT INTO service_token_connection_scope (service_token_id, connection_id)
SELECT 50, id FROM connections WHERE name LIKE 'prod-%';

-- 3. Scope to only monitoring-related tools
INSERT INTO service_token_mcp_scope (service_token_id, privilege_id)
SELECT 50, id FROM mcp_privilege_identifiers
WHERE identifier IN (
    'execute_query',
    'list_connections',
    'get_metrics',
    'collect_stats'
);
```

Result: Token can ONLY access production databases and ONLY use monitoring
tools.

## Performance Considerations

### Recursive Queries

Group hierarchy resolution uses recursive CTEs. Performance considerations:

1. **Depth Limit**: PostgreSQL default recursion limit is 100 levels
2. **Index Usage**: Ensure indexes exist on group_memberships foreign keys
3. **Caching**: Consider caching group membership results for active sessions
4. **Monitoring**: Watch for slow privilege check queries in pg_stat_statements

### Optimization Strategies

1. **Denormalization**: For very deep hierarchies, consider a
   materialized path or closure table
2. **Caching**: Cache computed privileges in application layer
3. **Batch Checks**: Check multiple privileges in single query
4. **Partial Indexes**: Add partial indexes on commonly queried privilege
   patterns

### Query Performance

**Check group membership** (should be fast with indexes):
```sql
EXPLAIN ANALYZE
WITH RECURSIVE user_groups AS (...)
SELECT COUNT(*) FROM user_groups;
```

Expected: < 5ms for typical hierarchies (< 10 levels deep)

**Check specific privilege** (should be fast):
```sql
EXPLAIN ANALYZE
SELECT 1
FROM group_mcp_privileges gmp
WHERE gmp.group_id IN (SELECT group_id FROM user_groups_recursive)
  AND gmp.privilege_id = (
    SELECT id FROM mcp_privilege_identifiers WHERE identifier = 'create_user'
  )
LIMIT 1;
```

Expected: < 10ms with proper indexes

## Security Considerations

### Privilege Escalation Prevention

1. **No Self-Granting**: Users cannot grant themselves privileges
   - Only superusers or users with grant_mcp_privilege can grant privileges
   - Privilege grants require separate privilege

2. **Circular Group Prevention**: Application validates group memberships
   - Prevents: A → B → C → A
   - Uses recursive CTE to detect cycles before insert

3. **Token Scoping is Restrictive**: Tokens can never have MORE access than
   the owning user/service

### Audit Recommendations

Consider logging:
1. All privilege grants/revokes
2. Group membership changes
3. Failed authorization attempts
4. Superuser privilege usage

### Least Privilege Principle

Best practices:
1. Grant privileges to groups, not individual users
2. Use token scoping for temporary/limited access
3. Prefer 'read' over 'read_write' where possible
4. Regularly audit and revoke unused privileges
5. Avoid superuser flags except for true administrators

## Troubleshooting

### User Cannot Access Tool

**Check hierarchy**:
```sql
-- See all groups user belongs to
WITH RECURSIVE user_groups AS (
    SELECT gm.parent_group_id, ug.name
    FROM group_memberships gm
    JOIN user_groups ug ON gm.parent_group_id = ug.id
    WHERE gm.member_user_id = :user_id
    UNION
    SELECT gm.parent_group_id, ug.name
    FROM group_memberships gm
    JOIN user_groups_recursive ugr ON gm.member_group_id = ugr.parent_group_id
    JOIN user_groups ug ON gm.parent_group_id = ug.id
)
SELECT * FROM user_groups;
```

**Check privilege grants**:
```sql
-- See all MCP privileges user has
SELECT DISTINCT p.identifier, p.item_type
FROM group_mcp_privileges gmp
JOIN mcp_privilege_identifiers p ON gmp.privilege_id = p.id
WHERE gmp.group_id IN (
    -- Replace with result of previous query
    SELECT group_id FROM user_groups_recursive
);
```

**Check token scope**:
```sql
-- If using a token, check its MCP scope
SELECT p.identifier
FROM user_token_mcp_scope ts
JOIN mcp_privilege_identifiers p ON ts.privilege_id = p.id
WHERE ts.user_token_id = :token_id;
```

### User Cannot Access Connection

**Check connection ownership**:
```sql
SELECT owner_username, owner_token, is_shared
FROM connections
WHERE id = :connection_id;
```

**Check group connection privileges**:
```sql
SELECT g.name, gcp.access_level
FROM group_connection_privileges gcp
JOIN user_groups g ON gcp.group_id = g.id
WHERE gcp.connection_id = :connection_id
  AND gcp.group_id IN (
    -- User's groups from previous query
    SELECT group_id FROM user_groups_recursive
  );
```

**Check token connection scope**:
```sql
SELECT c.name
FROM user_token_connection_scope ts
JOIN connections c ON ts.connection_id = c.id
WHERE ts.user_token_id = :token_id;
```

### Privilege Not Found

**Verify privilege is registered**:
```sql
SELECT * FROM mcp_privilege_identifiers
WHERE identifier = 'tool_name';
```

If not found, it needs to be registered before grants can be made.

## API Tools for Privilege Management

See `/Users/dpage/git/ai-workbench/docs/api-reference-privilege-tools.md` for
complete API documentation of the 29 privilege management tools.

Key tool categories:
- User management (create_user, update_user, delete_user)
- Token management (create_user_token, create_service_token)
- Group management (create_user_group, add_group_member)
- Privilege grants (grant_mcp_privilege, grant_connection_privilege)
- Scope management (set_token_connection_scope, set_token_mcp_scope)
- Listing/introspection (list_user_groups, list_group_members, etc.)
