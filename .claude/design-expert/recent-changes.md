# Recent Major Architectural Changes

This document tracks significant architectural changes and their implications
for future development.

## Migration 6: RBAC System (November 2025)

**Commits**:
- `2ca5ac2`: RBAC
- `9a98f0b`: RBAC, part deux
- `5d94ded`: Spec out RBAC (preliminary design)

### What Changed

**Major Addition**: Comprehensive Role-Based Access Control system with
hierarchical groups and fine-grained privileges.

**New Database Tables** (Migration 6):

1. **user_groups**: Hierarchical groups for organizing users
2. **group_memberships**: Many-to-many relationships (users and groups can be
   members of groups)
3. **connection_privileges**: Grant groups access to connections with
   read/read_write levels
4. **mcp_privilege_identifiers**: Registry of all MCP tools/resources/prompts
5. **group_mcp_privileges**: Grant groups access to specific MCP items
6. **token_connection_scope**: Restrict tokens to specific connections
7. **token_mcp_scope**: Restrict tokens to specific MCP items

**New Server Packages**:
- `/server/src/privileges`: Core privilege checking logic
- `/server/src/groupmgmt`: Group management operations
- `/server/src/integration`: Integration tests for privilege system

**New Documentation**:
- `/docs/user-guide-privileges.md`: User guide for privilege system
- `/docs/api-reference-privilege-tools.md`: API reference for privilege tools
- `/docs/privilege-workflows-examples.md`: Common workflow examples

### Design Rationale

**Problem**: Original design had authentication but minimal authorization.
All authenticated users had access to all connections and tools.

**Solution**: Implement RBAC with:
- Hierarchical groups (groups contain users and other groups)
- Two privilege types: connection access and MCP item access
- Token scoping to further restrict API tokens
- Fail-safe defaults (deny access unless explicitly granted)

**Key Design Decisions**:

1. **Groups Over Direct Grants**: Privileges granted to groups, never
   directly to users. Users gain privileges through group membership.

   **Rationale**: Scales better organizationally. Grant to group once instead
   of to each user individually.

2. **Hierarchical Groups**: Groups can contain other groups, creating
   inheritance hierarchies.

   **Rationale**: Models real organizational structures (e.g.,
   Engineering -> Backend -> Database Team).

3. **Recursive CTEs for Membership**: Transitive group membership resolved
   using PostgreSQL recursive CTEs.

   **Rationale**: Efficient database-side computation, handles arbitrary
   nesting depth.

4. **MCP Privilege Identifiers**: Explicit registry of all privileged
   operations.

   **Rationale**: Enables audit trail and fine-grained access control.
   Can grant access to specific tools without granting all.

5. **Token Scoping**: Tokens can be restricted to subsets of owner's
   privileges.

   **Rationale**: Supports least-privilege API tokens for automation
   (e.g., read-only monitoring token).

6. **Fail-Safe Defaults**: Shared connections with no groups DENIED,
   MCP items with no groups DENIED.

   **Rationale**: Security errors fail closed. Better to deny legitimate
   access than allow unauthorized access.

7. **Superuser Bypass**: Users/tokens with is_superuser=true bypass all
   privilege checks.

   **Rationale**: Bootstrap problem (need unrestricted access to set up
   groups), emergency access, administrative simplicity.

### Implementation Details

**Privilege Check Functions**:

```go
// Check connection access
func CanAccessConnection(ctx context.Context, pool *pgxpool.Pool,
    userID int, connectionID int, requestedLevel AccessLevel) (bool, error)

// Check MCP item access
func CanAccessMCPItem(ctx context.Context, pool *pgxpool.Pool,
    userID int, itemIdentifier string) (bool, error)

// Resolve user's groups (recursive)
func GetUserGroups(ctx context.Context, pool *pgxpool.Pool,
    userID int) ([]int, error)
```

**Privilege Resolution Algorithm**:

For connections:
1. If user.is_superuser -> ALLOW
2. If connection.is_shared = false -> Check ownership -> ALLOW/DENY
3. If connection.is_shared = true:
   - If no groups have privilege -> DENY (fail-safe)
   - Resolve user's groups (recursive)
   - If any user group has privilege with sufficient level -> ALLOW
   - Else -> DENY

For MCP items:
1. If user.is_superuser -> ALLOW
2. If identifier not registered -> DENY (fail-safe)
3. If no groups granted identifier -> DENY (fail-safe)
4. Resolve user's groups (recursive)
5. If any user group granted identifier -> ALLOW
6. Else -> DENY

**Token Scoping**:
- Empty scope tables (no rows) -> Token has full owner access
- Rows in scope tables -> Token restricted to listed items
- Scope cannot grant more than owner has

**Privilege Seeding**:
- Server startup calls `privileges.SeedMCPPrivileges()`
- Inserts any missing MCP privilege identifiers
- Idempotent operation (safe to run multiple times)
- Currently seeds 50+ tool/resource/prompt identifiers

### Impact on Future Development

**When Adding New MCP Tools/Resources/Prompts**:

1. Register privilege identifier in `privileges.SeedMCPPrivileges()`
2. Add privilege check in handler:
   ```go
   canAccess, err := privileges.CanAccessMCPItem(
       ctx, h.dbPool, h.userInfo.UserID, "tool:my_new_tool")
   if err != nil || !canAccess {
       return fmt.Errorf("access denied")
   }
   ```
3. Document privilege in API documentation
4. Add integration test for authorized/unauthorized access

**When Adding Features That Access Connections**:

1. Always check connection access before connecting:
   ```go
   canAccess, err := privileges.CanAccessConnection(
       ctx, h.dbPool, userInfo.UserID,
       connectionID, privileges.AccessLevelReadWrite)
   if err != nil || !canAccess {
       return fmt.Errorf("access denied")
   }
   ```
2. Use appropriate access level (read vs read_write)
3. Document privilege requirements
4. Add tests for both private and shared connections

**When Writing Tests**:

1. Create test user accounts with and without superuser status
2. Create test groups and group memberships
3. Grant specific privileges to test groups
4. Verify authorized users can access
5. Verify unauthorized users cannot access
6. Test token scoping restrictions

**Security Considerations**:

- ALWAYS check privileges, even for "internal" operations
- Default to DENY when unsure
- Superuser should be granted sparingly
- Token scoping is additional restriction, not replacement for privilege
  checks
- Audit logs should capture privilege denials

### Breaking Changes

**For Existing Installations**:
- Migration 6 is additive (no breaking schema changes)
- Existing connections and users unaffected
- New privilege system opt-in (superusers bypass all checks)
- Shared connections with no groups: Access DENIED (fail-safe change)

**For New Installations**:
- Must create groups and grant privileges for non-superusers
- Shared connections require explicit group assignment
- MCP tools require privilege grants (except for superusers)

### Migration Path

**For Existing Users**:
1. Apply Migration 6 (automatic on collector startup)
2. Create groups matching organizational structure
3. Add users to appropriate groups
4. Grant connection privileges to groups
5. Grant MCP privileges to groups
6. Test access with non-superuser accounts
7. Create scoped tokens as needed

**For New Users**:
1. Create initial superuser via CLI
2. Create group hierarchy
3. Create non-superuser accounts
4. Add users to groups
5. Create connections (superuser creates shared, users create private)
6. Grant privileges as needed

### Testing

**New Integration Tests**:
- `/server/src/integration/privileges_integration_test.go`: 600+ lines
- `/tests/integration/user_test.go`: User management integration tests

**Test Coverage**:
- Hierarchical group membership resolution
- Connection access with various privilege levels
- MCP item access with and without grants
- Token scoping restrictions
- Superuser bypass
- Fail-safe defaults (deny when no privileges)

**Test Pattern**:
```go
// Create test users and groups
superuser := createSuperuser(t, pool)
regularUser := createUser(t, pool)
group := createGroup(t, pool, "Test Group")
addUserToGroup(t, pool, regularUser.ID, group.ID)

// Test unauthorized access (should fail)
_, err := regularUser.ListConnections(ctx)
assert.Error(t, err)

// Grant privilege
grantConnectionPrivilege(t, pool, group.ID, connectionID, "read")

// Test authorized access (should succeed)
connections, err := regularUser.ListConnections(ctx)
assert.NoError(t, err)
assert.Contains(t, connections, connectionID)
```

### Documentation

**User Documentation**:
- Complete user guide with examples
- API reference for all privilege management tools
- Common workflow examples (setting up teams, creating scoped tokens)

**Developer Documentation**:
- Privilege checking patterns
- How to add new privileged operations
- Testing guidelines

---

## Migration 5: User Tokens (November 2025)

**Commit**: `5c022a9`: Support user tokens

### What Changed

**Enhancement**: Extended user_tokens table to support user-owned API tokens
in addition to session tokens.

**Schema Changes**:
- Made `expires_at` nullable (supports indefinite lifetime tokens)
- Removed `chk_expires_at_future` constraint
- Added `name` column (optional token description)
- Added `note` column (additional token information)
- Updated table comment to reflect user-owned API tokens

### Design Rationale

**Problem**: Original design had only session tokens (24-hour lifetime,
deleted on logout). No support for long-lived API tokens for automation.

**Solution**: Extend user_tokens to support both session tokens and
user-owned API tokens with optional expiry.

**Key Decisions**:

1. **Nullable Expiry**: Allow tokens without expiry for long-lived automation.

   **Rationale**: Some automation needs indefinite token lifetime.

2. **Name and Note Fields**: Allow users to document token purpose.

   **Rationale**: Users may have multiple tokens; need way to identify them.

3. **User Ownership**: Tokens owned by user account (FK to user_accounts).

   **Rationale**: Token privileges derived from user privileges.

### Impact on Future Development

**Token Management Tools**:
- create_user_token: Create new user-owned token
- list_user_tokens: List user's tokens
- revoke_user_token: Revoke specific token
- refresh_session_token: Extend session lifetime

**Token Scoping** (added in Migration 6):
- User tokens can be scoped to subset of user's privileges
- Enables least-privilege automation tokens

### Breaking Changes

**For Existing Installations**:
- Migration 5 is backward compatible
- Existing session tokens continue to work
- New token creation uses updated schema

---

## Migration 4: Configuration Tracking Probes (November 2025)

**Commit**: `5c25da4`: Probes for pg_hba.conf and pg_ident.conf

### What Changed

**Addition**: New probes for tracking PostgreSQL authentication configuration.

**New Probes**:
- pg_hba_file_rules: Tracks pg_hba.conf changes over time
- pg_ident_file_mappings: Tracks pg_ident.conf changes over time

**Probe Type**: Change-tracked (latest partition must never be dropped)

### Design Rationale

**Problem**: Security-relevant configuration changes need to be tracked for
audit and compliance.

**Solution**: Create probes that snapshot authentication configuration and
preserve history.

**Key Decisions**:

1. **Change-Tracked Probes**: Special handling to preserve latest data.

   **Rationale**: Compliance requirements to track security configuration.

2. **Point-in-Time Recovery**: Can query "what was the configuration at time T".

   **Rationale**: Incident response and forensic analysis.

### Impact on Future Development

**When Adding Change-Tracked Probes**:
1. Mark probe as change-tracked in probe definition
2. Garbage collector will preserve latest partition per connection
3. Document compliance/security rationale
4. Test that latest partition is never dropped

**Pattern for Change-Tracked Probes**:
```go
// In DropExpiredPartitions()
if tableName == "pg_hba_file_rules" || tableName == "pg_ident_file_mappings" {
    // Find most recent partition for each connection
    protectedPartitions := findMostRecentPartitions(...)

    // Exclude from deletion
    if protectedPartitions[partitionName] {
        continue  // Don't drop
    }
}
```

---

## Migration 3: pg_settings Probe (November 2025)

**Commit**: `097816f`: Add a pg_settings probe

### What Changed

**Addition**: New probe for tracking PostgreSQL configuration parameter
changes.

**New Probe**: pg_settings

**Probe Type**: Change-tracked (latest partition must never be dropped)

### Design Rationale

**Problem**: Configuration changes impact performance and behavior. Need
historical record of when parameters changed.

**Solution**: Create probe that snapshots pg_settings and tracks changes.

**Benefits**:
- Correlate performance issues with configuration changes
- Track who changed what and when (via ALTER SYSTEM tracking)
- Point-in-time configuration recovery

---

## Migration 2: User Sessions (November 2025)

**Commit**: Earlier in development

### What Changed

**Addition**: user_sessions table for session token management.

**New Table**: user_sessions
- Session tokens with 24-hour expiry
- Explicit logout support
- Multiple concurrent sessions per user

### Design Rationale

**Problem**: Need stateful session management for interactive use.

**Solution**: Separate session tokens from long-lived API tokens.

**Benefits**:
- Clear separation of session vs. API token lifecycle
- Automatic cleanup of expired sessions
- Support for multiple concurrent sessions

---

## Migration 1: Schema Consolidation (November 2025)

**Commit**: Earlier in development

### What Changed

**Major Refactoring**: Consolidated 43 original migrations into single
Migration 1.

**Changes**:
- Reorganized column order (PK, FK, flags, data, timestamps)
- Comprehensive COMMENT ON statements
- Cleaner initial schema for new installations

### Design Rationale

**Problem**: 43 migrations were complex and historically evolved.

**Solution**: Consolidate into single authoritative schema for new
installations.

**Benefits**:
- Easier to understand schema structure
- Logical column ordering
- Faster initial setup (fewer migration steps)
- Preserved for existing installations (no re-migration required)

### Impact on Future Development

**For New Migrations**:
- Never modify Migration 1 (immutable)
- Add new migrations starting with version 2, 3, etc.
- Follow column ordering pattern established in Migration 1
- Include comprehensive COMMENT ON statements

---

## Lessons Learned

### From RBAC Implementation

**What Went Well**:
- Comprehensive design spec before implementation (`5d94ded`)
- Extensive integration testing
- Thorough documentation

**Challenges**:
- Recursive CTE performance for deep group hierarchies
- Complexity of privilege resolution algorithm
- Testing all edge cases (deeply nested groups, circular references)

**Best Practices**:
1. Design complex features in separate commits (spec, implementation,
   documentation)
2. Integration tests caught issues unit tests missed
3. Fail-safe defaults prevented security issues

### From Token System Evolution

**What Went Well**:
- Incremental evolution (session tokens -> user tokens -> token scoping)
- Backward compatibility maintained
- Clear separation of concerns

**Challenges**:
- Token type proliferation (session vs user vs service)
- Consistent behavior across token types

**Best Practices**:
1. Nullable columns enable feature evolution without breaking changes
2. Clear documentation of token type differences essential
3. Testing across all token types required

### From Probe System Evolution

**What Went Well**:
- Change-tracked probe pattern emerged naturally
- Partition management scales well
- Easy to add new probes

**Challenges**:
- Special handling for change-tracked probes adds complexity
- Retention policy implications for compliance

**Best Practices**:
1. Distinguish change-tracked from time-series probes early
2. Document compliance rationale for change-tracked probes
3. Test partition management thoroughly

---

## Future Architectural Changes (Anticipated)

### Multi-Collector Support

**Status**: Designed but not implemented

**Design Considerations**:
- Leader election for probe scheduling
- Probe assignment across collectors
- Failover and load balancing

**Impact**: Will require changes to scheduler and probe assignment logic

### High Availability PostgreSQL

**Status**: Designed but not implemented

**Design Considerations**:
- Support for PostgreSQL streaming replication
- Support for Spock multi-master replication
- Read replica awareness

**Impact**: Will require connection pooling enhancements and probe scoping

### Rate Limiting

**Status**: Not designed

**Anticipated Need**: Prevent brute-force authentication attacks

**Design Considerations**:
- Per-IP rate limiting
- Per-user rate limiting
- Token bucket algorithm

**Impact**: Will require middleware in MCP server

### Key Rotation

**Status**: Not designed

**Anticipated Need**: Rotate server_secret without re-encrypting all
passwords

**Design Considerations**:
- Multiple active encryption keys
- Key version tracking
- Gradual re-encryption

**Impact**: Will require changes to crypto.go and password encryption

---

## Tracking Future Changes

When making significant architectural changes:

1. **Document in this file** with:
   - What changed (schema, code, behavior)
   - Why it changed (problem and solution)
   - How it impacts future development
   - Breaking changes and migration path

2. **Update design documents** if architecture or philosophy changes

3. **Add integration tests** to prevent regression

4. **Update user documentation** for user-facing changes

5. **Update API documentation** for MCP changes

---

**Version**: 1.0
**Last Updated**: 2025-11-08
**Status**: Living Document
