# Entity Relationships and Database Design

This document provides detailed documentation of the entity relationships,
foreign keys, and referential integrity rules in the pgEdge AI Workbench
database.

## Entity Relationship Overview

### Core Authentication and Authorization

```
┌─────────────────┐
│ user_accounts   │
│ - id (PK)       │
│ - username      │
│ - is_superuser  │
└────────┬────────┘
         │
         ├─────────────────────────────────────┐
         │                                     │
         │ (1:N)                               │ (1:N)
         ▼                                     ▼
┌─────────────────┐                   ┌──────────────────┐
│ user_tokens     │                   │ user_sessions    │
│ - id (PK)       │                   │ - session_token  │
│ - user_id (FK)  │                   │ - username (FK)  │
│ - token_hash    │                   │ - expires_at     │
└─────────────────┘                   └──────────────────┘

┌──────────────────┐
│ service_tokens   │
│ - id (PK)        │
│ - name           │
│ - token_hash     │
│ - is_superuser   │
└──────────────────┘
```

### Connection Ownership

```
┌─────────────────┐          ┌──────────────────┐
│ user_accounts   │          │ service_tokens   │
│ - username (PK) │          │ - name (PK)      │
└────────┬────────┘          └────────┬─────────┘
         │                            │
         │ (1:N)                      │ (1:N)
         │                            │
         └──────────────┬─────────────┘
                        │
                        ▼
                 ┌─────────────────┐
                 │ connections     │
                 │ - id (PK)       │
                 │ - owner_username│ (FK, nullable)
                 │ - owner_token   │ (FK, nullable)
                 │ - is_shared     │
                 │ - is_monitored  │
                 └────────┬────────┘
                          │
                          │ (1:N)
                          ▼
                 ┌─────────────────┐
                 │ probe_configs   │
                 │ - id (PK)       │
                 │ - connection_id │ (FK, nullable)
                 │ - name          │
                 │ - is_enabled    │
                 └─────────────────┘
```

**Key Constraint:** Exactly one of `owner_username` OR `owner_token` must be
set (enforced via CHECK constraint).

### RBAC System (Migration 6)

```
┌─────────────────┐
│ user_accounts   │
│ - id (PK)       │
└────────┬────────┘
         │
         │ (M:N via group_memberships)
         ▼
┌─────────────────────────┐
│ group_memberships       │
│ - id (PK)               │
│ - parent_group_id (FK)  │
│ - member_user_id (FK)   │ (nullable, XOR with member_group_id)
│ - member_group_id (FK)  │ (nullable, XOR with member_user_id)
└────────┬────────────────┘
         │
         │ (N:1)
         ▼
┌─────────────────┐
│ user_groups     │
│ - id (PK)       │
│ - name          │
└────────┬────────┘
         │
         ├──────────────────────┬────────────────────────┐
         │ (1:N)                │ (1:N)                  │
         ▼                      ▼                        │
┌──────────────────────┐  ┌──────────────────────────┐  │
│ group_mcp_privileges │  │ group_connection_        │  │
│ - id (PK)            │  │   privileges             │  │
│ - group_id (FK)      │  │ - id (PK)                │  │
│ - privilege_id (FK)  │  │ - group_id (FK)          │  │
└──────────┬───────────┘  │ - connection_id (FK)     │  │
           │              │ - access_level           │  │
           │              └──────────────────────────┘  │
           │                                            │
           │ (N:1)                                      │ (self-ref)
           ▼                                            │
┌──────────────────────────┐                           │
│ mcp_privilege_identifiers│                           │
│ - id (PK)                │                           │
│ - identifier             │                           │
│ - item_type              │                           │
└──────────────────────────┘                           │
                                                       │
        ┌──────────────────────────────────────────────┘
        │ Hierarchical self-reference:
        │ group_memberships.member_group_id -> user_groups.id
        └─> (allows groups to contain other groups)
```

### Token Scoping

```
┌─────────────────┐                    ┌──────────────────┐
│ user_tokens     │                    │ service_tokens   │
│ - id (PK)       │                    │ - id (PK)        │
└────────┬────────┘                    └────────┬─────────┘
         │                                      │
         ├──────────────┬───────────────┐       ├──────────────┬───────────────┐
         │ (1:N)        │ (1:N)         │       │ (1:N)        │ (1:N)         │
         ▼              ▼               │       ▼              ▼               │
┌────────────────┐ ┌─────────────────┐ │  ┌────────────────┐ ┌─────────────────┐
│ user_token_    │ │ user_token_     │ │  │ service_token_ │ │ service_token_  │
│ connection_    │ │ mcp_scope       │ │  │ connection_    │ │ mcp_scope       │
│ scope          │ │ - id (PK)       │ │  │ scope          │ │ - id (PK)       │
│ - id (PK)      │ │ - user_token_id │ │  │ - id (PK)      │ │ - service_      │
│ - user_token_id│ │ - privilege_id  │ │  │ - service_     │ │   token_id (FK) │
│ - connection_id│ └─────────────────┘ │  │   token_id (FK)│ │ - privilege_id  │
└────────────────┘                     │  │ - connection_id│ └─────────────────┘
                                       │  └────────────────┘
                                       │
                                       │  All FK to:
                                       ├─> connections.id
                                       └─> mcp_privilege_identifiers.id
```

### Metrics Collection

```
┌─────────────────┐
│ connections     │
│ - id (PK)       │
└────────┬────────┘
         │
         │ (1:N) All metrics tables reference connection_id
         │
         ├──────────────────┬──────────────────┬─────────────────┐
         ▼                  ▼                  ▼                 ▼
┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐  ...
│ metrics.         │  │ metrics.         │  │ metrics.     │
│ pg_stat_activity │  │ pg_stat_database │  │ pg_settings  │
│ - connection_id  │  │ - connection_id  │  │ -connection_id
│ - collected_at   │  │ - collected_at   │  │ -collected_at│
│ - ...metrics...  │  │ - ...metrics...  │  │ -...metrics  │
└──────────────────┘  └──────────────────┘  └──────────────┘

Note: No explicit foreign keys on metrics tables to avoid
      performance overhead on high-volume inserts.
      Referential integrity maintained by application logic.
```

## Foreign Key Relationships

### CASCADE Rules Summary

| Parent Table | Child Table | Delete Rule | Update Rule | Purpose |
|--------------|-------------|-------------|-------------|---------|
| user_accounts | user_tokens | CASCADE | CASCADE | Delete tokens when user deleted |
| user_accounts | user_sessions | CASCADE | CASCADE | Delete sessions when user deleted |
| user_accounts | group_memberships | CASCADE | - | Remove user from groups |
| user_groups | group_memberships | CASCADE | - | Delete memberships when group deleted |
| user_groups | group_mcp_privileges | CASCADE | - | Delete privileges when group deleted |
| user_groups | group_connection_privileges | CASCADE | - | Delete connection access when group deleted |
| user_tokens | user_token_connection_scope | CASCADE | - | Delete scopes when token deleted |
| user_tokens | user_token_mcp_scope | CASCADE | - | Delete scopes when token deleted |
| service_tokens | service_token_connection_scope | CASCADE | - | Delete scopes when token deleted |
| service_tokens | service_token_mcp_scope | CASCADE | - | Delete scopes when token deleted |
| connections | probe_configs | CASCADE | - | Delete probe configs when connection deleted |
| connections | group_connection_privileges | CASCADE | - | Remove privileges when connection deleted |
| mcp_privilege_identifiers | group_mcp_privileges | CASCADE | - | Delete grants when privilege definition deleted |
| mcp_privilege_identifiers | user_token_mcp_scope | CASCADE | - | Delete scopes when privilege deleted |
| mcp_privilege_identifiers | service_token_mcp_scope | CASCADE | - | Delete scopes when privilege deleted |

### RESTRICT Rules Summary

| Parent Table | Child Table | Delete Rule | Purpose |
|--------------|-------------|-------------|---------|
| user_accounts | connections | RESTRICT | Cannot delete user who owns connections |
| service_tokens | connections | RESTRICT | Cannot delete service token that owns connections |

**RESTRICT Behavior:** Prevents deletion if dependent rows exist. User must
first:
1. Transfer connection ownership, OR
2. Delete the connections, OR
3. Change connection owner to a different user/token

### UPDATE CASCADE Examples

**user_accounts.username**:
```sql
-- Rename user
UPDATE user_accounts SET username = 'alice_new' WHERE username = 'alice';

-- Automatically cascades to:
-- 1. user_sessions.username (all sessions updated)
-- 2. connections.owner_username (all owned connections updated)
```

This ensures referential integrity without manual updates.

## Detailed Foreign Key Definitions

### user_tokens

```sql
ALTER TABLE user_tokens
    ADD CONSTRAINT fk_user_tokens_user_id
    FOREIGN KEY (user_id)
    REFERENCES user_accounts(id)
    ON DELETE CASCADE;
```

**Behavior:**
- Deleting a user account deletes all their personal access tokens
- Prevents orphaned tokens

**Example:**
```sql
-- Delete user
DELETE FROM user_accounts WHERE id = 5;

-- Automatically deletes all rows from:
SELECT * FROM user_tokens WHERE user_id = 5;  -- Returns 0 rows
```

### user_sessions

```sql
ALTER TABLE user_sessions
    ADD CONSTRAINT fk_username
    FOREIGN KEY (username)
    REFERENCES user_accounts(username)
    ON DELETE CASCADE;
```

**Behavior:**
- Deleting a user deletes all their active sessions
- Renaming a user updates all session references

**Security Implication:** User deletion immediately invalidates all their
sessions, preventing further access.

### connections (dual ownership)

```sql
ALTER TABLE connections
    ADD CONSTRAINT fk_connections_owner_username
    FOREIGN KEY (owner_username)
    REFERENCES user_accounts(username)
    ON UPDATE CASCADE
    ON DELETE RESTRICT;

ALTER TABLE connections
    ADD CONSTRAINT fk_connections_owner_token
    FOREIGN KEY (owner_token)
    REFERENCES service_tokens(name)
    ON UPDATE CASCADE
    ON DELETE RESTRICT;

ALTER TABLE connections
    ADD CONSTRAINT chk_owner CHECK (
        (owner_username IS NOT NULL AND owner_token IS NULL) OR
        (owner_username IS NULL AND owner_token IS NOT NULL)
    );
```

**Behavior:**
- Each connection has exactly ONE owner (user XOR service token)
- Cannot delete owner if they have connections (must transfer or delete first)
- Renaming user/token updates all owned connections

**Transfer Ownership Example:**
```sql
-- Transfer connection from user to service token
UPDATE connections
SET owner_username = NULL,
    owner_token = 'monitoring-service'
WHERE id = 10;
```

### group_memberships (hierarchical)

```sql
ALTER TABLE group_memberships
    ADD CONSTRAINT fk_parent_group
    FOREIGN KEY (parent_group_id)
    REFERENCES user_groups(id)
    ON DELETE CASCADE;

ALTER TABLE group_memberships
    ADD CONSTRAINT fk_member_user
    FOREIGN KEY (member_user_id)
    REFERENCES user_accounts(id)
    ON DELETE CASCADE;

ALTER TABLE group_memberships
    ADD CONSTRAINT fk_member_group
    FOREIGN KEY (member_group_id)
    REFERENCES user_groups(id)
    ON DELETE CASCADE;

ALTER TABLE group_memberships
    ADD CONSTRAINT chk_exactly_one_member CHECK (
        (member_user_id IS NOT NULL AND member_group_id IS NULL) OR
        (member_user_id IS NULL AND member_group_id IS NOT NULL)
    );
```

**Behavior:**
- Deleting a group removes all its memberships (both as parent and member)
- Deleting a user removes them from all groups
- Self-referencing for hierarchical groups

**Cascade Chain Example:**
```sql
-- Delete a parent group
DELETE FROM user_groups WHERE name = 'engineering';

-- Cascades to:
-- 1. All rows in group_memberships where parent_group_id = engineering.id
-- 2. All rows where member_group_id = engineering.id (if it's nested in other
groups)
-- 3. All rows in group_mcp_privileges for this group
-- 4. All rows in group_connection_privileges for this group
```

### group_mcp_privileges

```sql
ALTER TABLE group_mcp_privileges
    ADD CONSTRAINT fk_group
    FOREIGN KEY (group_id)
    REFERENCES user_groups(id)
    ON DELETE CASCADE;

ALTER TABLE group_mcp_privileges
    ADD CONSTRAINT fk_privilege
    FOREIGN KEY (privilege_id)
    REFERENCES mcp_privilege_identifiers(id)
    ON DELETE CASCADE;
```

**Behavior:**
- Deleting a group removes all its MCP privileges
- Deleting a privilege identifier removes all grants of that privilege

**Cleanup Example:**
```sql
-- Deprecate a tool (remove from privilege system)
DELETE FROM mcp_privilege_identifiers WHERE identifier = 'old_tool';

-- Cascades to:
-- 1. All group_mcp_privileges rows granting access to this tool
-- 2. All user_token_mcp_scope rows scoping tokens to this tool
-- 3. All service_token_mcp_scope rows scoping tokens to this tool
```

### Token Scoping Foreign Keys

```sql
-- User token connection scope
ALTER TABLE user_token_connection_scope
    ADD CONSTRAINT fk_user_token
    FOREIGN KEY (user_token_id)
    REFERENCES user_tokens(id)
    ON DELETE CASCADE;

ALTER TABLE user_token_connection_scope
    ADD CONSTRAINT fk_connection
    FOREIGN KEY (connection_id)
    REFERENCES connections(id)
    ON DELETE CASCADE;

-- Similar for user_token_mcp_scope, service_token_connection_scope,
-- service_token_mcp_scope
```

**Behavior:**
- Deleting a token removes all its scoping restrictions
- Deleting a connection removes it from all token scopes
- Deleting a privilege identifier removes it from all token scopes

**Automatic Cleanup:**
```sql
-- Delete a connection
DELETE FROM connections WHERE id = 5;

-- Automatically removes from:
-- 1. user_token_connection_scope (all tokens scoped to this connection)
-- 2. service_token_connection_scope (all tokens scoped to this connection)
-- 3. group_connection_privileges (all groups granted access to this connection)
```

## Unique Constraints

### Purpose and Enforcement

Unique constraints ensure data integrity and prevent duplicates.

#### user_accounts

```sql
UNIQUE (username)
```

**Purpose:** Usernames must be globally unique for authentication.

**Error Example:**
```sql
INSERT INTO user_accounts (username, email, full_name, password_hash)
VALUES ('alice', 'alice2@example.com', 'Alice Two', 'hash');

-- ERROR: duplicate key value violates unique constraint
-- "user_accounts_username_key"
```

#### service_tokens

```sql
UNIQUE (name)
UNIQUE (token_hash)
```

**Purpose:**
- `name` unique: Service token names are identifiers
- `token_hash` unique: Tokens must be globally unique for security

#### user_tokens

```sql
UNIQUE (token_hash)
```

**Purpose:** Personal access tokens must be globally unique.

#### user_groups

```sql
UNIQUE (name)
```

**Purpose:** Group names are identifiers and must be unique.

#### group_memberships

```sql
UNIQUE (parent_group_id, member_user_id)
UNIQUE (parent_group_id, member_group_id)
```

**Purpose:** Prevent duplicate memberships.

**Example:**
```sql
-- First insert succeeds
INSERT INTO group_memberships (parent_group_id, member_user_id)
VALUES (1, 5);

-- Duplicate insert fails
INSERT INTO group_memberships (parent_group_id, member_user_id)
VALUES (1, 5);

-- ERROR: duplicate key value violates unique constraint
-- "group_memberships_parent_group_id_member_user_id_key"
```

#### group_mcp_privileges

```sql
UNIQUE (group_id, privilege_id)
```

**Purpose:** Each group should only have each privilege once.

**Idempotent Grants:**
```sql
-- Use ON CONFLICT to make grants idempotent
INSERT INTO group_mcp_privileges (group_id, privilege_id)
VALUES (1, 10)
ON CONFLICT (group_id, privilege_id) DO NOTHING;
```

#### group_connection_privileges

```sql
UNIQUE (group_id, connection_id)
```

**Purpose:** Each group should only have one access level per connection.

**Upsert Pattern:**
```sql
-- Update access level if already granted
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
VALUES (1, 5, 'read_write')
ON CONFLICT (group_id, connection_id)
DO UPDATE SET access_level = EXCLUDED.access_level;
```

#### probe_configs

```sql
CREATE UNIQUE INDEX probe_configs_name_global_key
    ON probe_configs(name)
    WHERE connection_id IS NULL;

CREATE UNIQUE INDEX probe_configs_name_connection_key
    ON probe_configs(name, COALESCE(connection_id, 0));
```

**Purpose:**
- Global configs (connection_id = NULL): One per probe name
- Per-connection configs: One per (probe name, connection) pair

**Example:**
```sql
-- Global config (OK)
INSERT INTO probe_configs (connection_id, name, description)
VALUES (NULL, 'pg_stat_activity', 'Global default');

-- Another global config for same probe (FAILS)
INSERT INTO probe_configs (connection_id, name, description)
VALUES (NULL, 'pg_stat_activity', 'Duplicate');
-- ERROR: violates unique constraint "probe_configs_name_global_key"

-- Connection-specific override (OK)
INSERT INTO probe_configs (connection_id, name, description)
VALUES (5, 'pg_stat_activity', 'Override for connection 5');
```

## Check Constraints

### Data Validation

Check constraints enforce business rules at the database level.

#### Non-Empty String Checks

```sql
-- user_accounts
CONSTRAINT chk_username_not_empty CHECK (username <> '')
CONSTRAINT chk_email_not_empty CHECK (email <> '')
CONSTRAINT chk_password_hash_not_empty CHECK (password_hash <> '')

-- service_tokens
CONSTRAINT chk_name_not_empty CHECK (name <> '')
CONSTRAINT chk_token_hash_not_empty CHECK (token_hash <> '')

-- user_tokens
CONSTRAINT chk_token_hash_not_empty CHECK (token_hash <> '')
```

**Purpose:** Prevent empty strings (as opposed to NULL) from being stored.

**Example:**
```sql
INSERT INTO user_accounts (username, email, full_name, password_hash)
VALUES ('', 'test@example.com', 'Test', 'hash');

-- ERROR: new row violates check constraint "chk_username_not_empty"
```

#### connections Constraints

```sql
-- Exactly one owner
CONSTRAINT chk_owner CHECK (
    (owner_username IS NOT NULL AND owner_token IS NULL) OR
    (owner_username IS NULL AND owner_token IS NOT NULL)
)

-- Valid port range
CONSTRAINT chk_port CHECK (port > 0 AND port <= 65535)
```

**Purpose:**
- Enforce ownership model
- Validate port numbers

**Example:**
```sql
-- Invalid: both owners set
INSERT INTO connections (owner_username, owner_token, ...)
VALUES ('alice', 'service-token', ...);
-- ERROR: violates check constraint "chk_owner"

-- Invalid: neither owner set
INSERT INTO connections (owner_username, owner_token, ...)
VALUES (NULL, NULL, ...);
-- ERROR: violates check constraint "chk_owner"

-- Invalid: port out of range
INSERT INTO connections (..., port, ...)
VALUES (..., 99999, ...);
-- ERROR: violates check constraint "chk_port"
```

#### probe_configs Constraints

```sql
CONSTRAINT chk_name_not_empty CHECK (name <> '')
CONSTRAINT chk_collection_interval_positive CHECK (collection_interval_seconds
> 0)
CONSTRAINT chk_retention_days_positive CHECK (retention_days > 0)
```

**Purpose:** Validate configuration parameters.

#### user_tokens Constraint

```sql
CONSTRAINT chk_expires_at_future CHECK (expires_at > created_at)
```

**Purpose:** Ensure tokens have future expiration dates when created.

**Example:**
```sql
INSERT INTO user_tokens (user_id, token_hash, expires_at, created_at)
VALUES (5, 'hash', '2020-01-01', '2025-01-01');

-- ERROR: violates check constraint "chk_expires_at_future"
```

#### group_memberships Constraint

```sql
CONSTRAINT chk_exactly_one_member CHECK (
    (member_user_id IS NOT NULL AND member_group_id IS NULL) OR
    (member_user_id IS NULL AND member_group_id IS NOT NULL)
)
```

**Purpose:** Enforce member type (user XOR group).

#### mcp_privilege_identifiers Constraint

```sql
CONSTRAINT chk_item_type CHECK (item_type IN ('tool', 'resource', 'prompt'))
```

**Purpose:** Validate privilege item types.

#### group_connection_privileges Constraint

```sql
CONSTRAINT chk_access_level CHECK (access_level IN ('read', 'read_write'))
```

**Purpose:** Validate access level values.

## Referential Integrity Patterns

### Pattern 1: Simple Parent-Child

**Example:** user_accounts -> user_tokens

```
user_accounts (parent)
    ├─ DELETE: CASCADE to user_tokens (delete all tokens)
    └─ UPDATE: CASCADE to user_tokens (no practical effect since PK is id)
```

**Use Case:** Deleting a user should delete all their tokens.

### Pattern 2: Ownership with RESTRICT

**Example:** user_accounts -> connections

```
user_accounts (parent)
    └─ DELETE: RESTRICT if connections exist (prevent deletion)
```

**Use Case:** Cannot delete a user who owns connections (must transfer first).

**Workflow:**
```sql
-- 1. Attempt to delete user with connections (FAILS)
DELETE FROM user_accounts WHERE username = 'alice';
-- ERROR: violates foreign key constraint "fk_connections_owner_username"

-- 2. Transfer connections first
UPDATE connections SET owner_token = 'admin-service'
WHERE owner_username = 'alice';

-- 3. Now deletion succeeds
DELETE FROM user_accounts WHERE username = 'alice';
```

### Pattern 3: Hierarchical Self-Reference

**Example:** user_groups via group_memberships

```
user_groups (self-referential through group_memberships)
    ├─ DELETE: CASCADE removes all memberships where this group is parent or
member
    └─ Circular prevention: Application-enforced via recursive CTE
```

**Use Case:** Groups can contain other groups, forming a hierarchy.

**Circular Prevention (Application Logic):**
```go
func wouldCreateCircle(parentGroupID, memberGroupID int) bool {
    // Use recursive CTE to check if parentGroupID is already a descendant of
    // memberGroupID
    query := `
        WITH RECURSIVE group_hierarchy AS (
            SELECT parent_group_id FROM group_memberships
            WHERE member_group_id = $1
            UNION
            SELECT gm.parent_group_id FROM group_memberships gm
            JOIN group_hierarchy gh ON gm.member_group_id = gh.parent_group_id
        )
        SELECT 1 FROM group_hierarchy WHERE parent_group_id = $2
    `
    // If query returns a row, adding this membership would create a circle
}
```

### Pattern 4: Multi-Table CASCADE Chain

**Example:** Deleting a user_group

```
user_groups (DELETE)
    ├─> group_memberships (CASCADE DELETE)
    │   └─> No further cascades
    ├─> group_mcp_privileges (CASCADE DELETE)
    │   └─> No further cascades
    └─> group_connection_privileges (CASCADE DELETE)
        └─> No further cascades
```

**Result:** Single DELETE statement removes group and all associated data.

```sql
-- One DELETE statement
DELETE FROM user_groups WHERE id = 10;

-- Automatically deletes from:
-- 1. group_memberships (all memberships involving this group)
-- 2. group_mcp_privileges (all MCP privileges granted to this group)
-- 3. group_connection_privileges (all connection access granted to this group)
```

### Pattern 5: Token Scoping Cleanup

**Example:** Deleting a connection

```
connections (DELETE)
    ├─> user_token_connection_scope (CASCADE DELETE)
    ├─> service_token_connection_scope (CASCADE DELETE)
    ├─> group_connection_privileges (CASCADE DELETE)
    └─> probe_configs (CASCADE DELETE)
```

**Result:** Deleting a connection automatically removes it from all token
scopes, group privileges, and probe configurations.

## Orphan Prevention

### Strategies Used

1. **Foreign Key Constraints**: Prevent orphans at database level
2. **CASCADE Rules**: Automatically clean up dependent rows
3. **Application Validation**: Additional checks before INSERT/UPDATE

### No Orphans Guarantee

With the current foreign key design, **orphaned rows are impossible**:

- All child rows have FK constraints with CASCADE or RESTRICT
- No child row can exist without a valid parent
- Deleting a parent either deletes children (CASCADE) or prevents deletion
  (RESTRICT)

### Metrics Tables (Special Case)

**Note:** Metrics tables in the `metrics` schema do NOT have foreign key
constraints to `connections`.

**Reason:** Performance. Foreign keys would add significant overhead to
high-volume INSERT operations.

**Orphan Handling:**
- Application logic ensures connection_id validity before INSERT
- Old metrics from deleted connections are harmless (connection_id becomes a
  stale reference)
- Metrics cleanup is time-based (retention), not referential

## Relationship Cardinality

### One-to-Many (1:N)

| Parent (1) | Child (N) | Notes |
|------------|-----------|-------|
| user_accounts | user_tokens | One user, many personal access tokens |
| user_accounts | user_sessions | One user, many active sessions |
| user_groups | group_memberships (as parent) | One group, many members |
| user_groups | group_mcp_privileges | One group, many privileges |
| user_groups | group_connection_privileges | One group, many connection grants |
| connections | probe_configs | One connection, many probe overrides |
| connections | metrics.* | One connection, many metric records |
| mcp_privilege_identifiers | group_mcp_privileges | One privilege, many group grants |
| user_tokens | user_token_connection_scope | One token, many connection restrictions |
| user_tokens | user_token_mcp_scope | One token, many MCP restrictions |
| service_tokens | service_token_connection_scope | One token, many connection restrictions |
| service_tokens | service_token_mcp_scope | One token, many MCP restrictions |

### Many-to-Many (M:N)

Implemented via junction tables:

| Entity A | Junction Table | Entity B | Relationship |
|----------|----------------|----------|--------------|
| user_accounts | group_memberships | user_groups | Users belong to groups |
| user_groups | group_memberships | user_groups | Groups contain groups |
| user_groups | group_mcp_privileges | mcp_privilege_identifiers | Groups have privileges |
| user_groups | group_connection_privileges | connections | Groups access connections |

### One-to-One (1:1)

Not explicitly used in the current schema, but some relationships are
effectively 1:1:

- `user_sessions.session_token` is unique (1:1 with session)
- `service_tokens.name` is unique (1:1 identifier)

## Data Integrity Rules Summary

1. **No orphans**: All relationships enforced via FK constraints
2. **Referential integrity**: Cascading deletes maintain consistency
3. **Unique constraints**: Prevent duplicates where needed
4. **Check constraints**: Validate data at insert/update time
5. **NOT NULL constraints**: Enforce required fields
6. **Application validation**: Additional business logic (circular group
   prevention)
7. **Idempotent operations**: Use ON CONFLICT for safe upserts

## Database Diagram

See ASCII diagrams above for visual representation of relationships.

For production documentation, consider generating ER diagrams using tools like:
- pgAdmin (built-in ER diagram generator)
- dbdiagram.io (online ER diagram tool)
- SchemaSpy (generates HTML documentation with diagrams)

### Example dbdiagram.io Syntax

```
Table user_accounts {
  id integer [pk, increment]
  username text [unique, not null]
  email text [not null]
  is_superuser boolean [not null, default: false]
}

Table user_tokens {
  id integer [pk, increment]
  user_id integer [not null, ref: > user_accounts.id]
  token_hash text [unique, not null]
  expires_at timestamp [not null]
}

Table connections {
  id integer [pk, increment]
  owner_username varchar [ref: > user_accounts.username]
  owner_token varchar [ref: > service_tokens.name]
  name varchar [not null]
}

// Paste into dbdiagram.io for visual diagram
```
