# Development Guidelines: pgEdge AI Workbench

This document provides guidelines for maintaining design consistency and
architectural integrity when developing features for the pgEdge AI Workbench.

## Purpose

These guidelines help ensure that new code:
1. Aligns with design philosophy and architectural decisions
2. Maintains security standards
3. Preserves component boundaries
4. Follows established patterns
5. Remains testable and maintainable

## Before You Code

### 1. Understand the Design Intent

**Always Start Here**:
- Read DESIGN.md for architectural context
- Review design-philosophy.md for core principles
- Check architecture-decisions.md for precedents
- Understand component-responsibilities.md boundaries

**Questions to Ask**:
- What design goal does this feature support?
- Which component(s) should this belong to?
- Does this fit existing architectural patterns?
- Are there security implications?
- How will this be tested?

### 2. Check for Existing Patterns

**Look for Similar Features**:
- How are similar operations implemented?
- What probe structure is used for similar metrics?
- How do existing tools handle similar operations?
- What authorization patterns apply?

**Example**: Adding a new probe? Look at existing probes in
`/collector/src/probes/` to understand the pattern.

### 3. Identify Design Compliance Points

**Security Checklist**:
- [ ] Will this handle user input? -> Parameterized queries required
- [ ] Will this access connections? -> Authorization check required
- [ ] Will this be an MCP tool? -> Privilege identifier required
- [ ] Will this handle credentials? -> Encryption required
- [ ] Will this store sensitive data? -> Consider security model

**Architectural Checklist**:
- [ ] Which component owns this functionality?
- [ ] Does this duplicate existing functionality?
- [ ] Does this violate component boundaries?
- [ ] Is this the right layer of abstraction?

## Design Compliance Guidelines

### Guideline 1: Component Boundaries Are Sacred

**Rule**: Never violate component responsibilities.

**Examples**:

**COMPLIANT**:
```go
// In Collector: Collect metrics and store
func (p *PgStatDatabaseProbe) Execute(ctx context.Context, ...) {
    metrics := executeQuery(...)
    p.Store(ctx, datastoreConn, connectionID, time.Now(), metrics)
}

// In Server: Retrieve and serve metrics
func (h *Handler) handleReadMetrics(params ...) {
    metrics := queryDatastore(...)
    return metrics
}
```

**VIOLATION**:
```go
// WRONG: Server collecting metrics (Collector's job)
func (h *Handler) handleCollectMetrics(params ...) {
    metrics := queryMonitoredDatabase(...)  // WRONG!
    storeInDatastore(metrics)                // WRONG!
}
```

**Why It Matters**: Violating boundaries creates tight coupling, duplicate
code, and unclear ownership.

### Guideline 2: Default to Deny

**Rule**: Access control must fail-safe (deny by default).

**Examples**:

**COMPLIANT**:
```go
// Explicit denial if no privileges granted
func CanAccessMCPItem(ctx context.Context, pool *pgxpool.Pool,
    userID int, itemIdentifier string) (bool, error) {

    // Superuser bypass
    if isSuperuser {
        return true, nil
    }

    // Check if privilege identifier exists
    var privilegeID int
    err := pool.QueryRow(ctx,
        "SELECT id FROM mcp_privilege_identifiers WHERE identifier = $1",
        itemIdentifier).Scan(&privilegeID)
    if err != nil {
        // NOT FOUND = DENY
        return false, fmt.Errorf("privilege identifier not found: %s",
            itemIdentifier)
    }

    // Check group privileges
    var groupCount int
    err = pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM group_mcp_privileges WHERE privilege_identifier_id = $1",
        privilegeID).Scan(&groupCount)
    if err != nil {
        return false, err
    }

    // NO GROUPS = DENY (fail-safe)
    if groupCount == 0 {
        return false, nil
    }

    // Check user's groups...
}
```

**VIOLATION**:
```go
// WRONG: Defaults to allowing access
func CanAccessConnection(connectionID int) bool {
    groups := getConnectionGroups(connectionID)
    if len(groups) == 0 {
        return true  // WRONG! Should deny when no groups assigned
    }
    // ...
}
```

**Why It Matters**: Security errors must fail closed. Better to deny legitimate
access than allow unauthorized access.

### Guideline 3: Always Parameterize SQL

**Rule**: Never construct SQL with string concatenation from user input.

**Examples**:

**COMPLIANT**:
```go
// Parameterized query
_, err := conn.Exec(ctx,
    "SELECT * FROM users WHERE username = $1 AND email = $2",
    username, email)
```

**VIOLATION**:
```go
// WRONG: SQL injection vulnerability
query := fmt.Sprintf("SELECT * FROM users WHERE username = '%s'", username)
_, err := conn.Exec(ctx, query)  // WRONG!
```

**Exception**: Table/column names from probe definitions (not user input).
Use `#nosec G201` comment for linter.

**ACCEPTABLE** (with caution):
```go
// Table name from probe definition, NOT user input
// #nosec G201 - table name is from probe definition
query := fmt.Sprintf("SELECT * FROM metrics.%s WHERE connection_id = $1",
    tableName)
_, err := conn.Exec(ctx, query, connectionID)
```

**Why It Matters**: SQL injection is the #1 web application vulnerability.
Parameterization prevents it completely.

### Guideline 4: Check Privileges at Every Access Point

**Rule**: Validate authorization before every privileged operation.

**Pattern**:
```go
func (h *Handler) handleQueryDatabase(params QueryParams) (*Response, error) {
    // 1. Extract user identity from handler context
    userInfo := h.userInfo
    if userInfo == nil {
        return nil, fmt.Errorf("authentication required")
    }

    // 2. Check connection access
    canAccess, err := privileges.CanAccessConnection(
        ctx, h.dbPool, userInfo.UserID,
        params.ConnectionID, privileges.AccessLevelReadWrite)
    if err != nil {
        return nil, fmt.Errorf("privilege check failed: %w", err)
    }
    if !canAccess {
        return nil, fmt.Errorf("access denied to connection %d",
            params.ConnectionID)
    }

    // 3. Check MCP item access
    canAccess, err = privileges.CanAccessMCPItem(
        ctx, h.dbPool, userInfo.UserID, "tool:query_database")
    if err != nil {
        return nil, fmt.Errorf("privilege check failed: %w", err)
    }
    if !canAccess {
        return nil, fmt.Errorf("access denied to tool query_database")
    }

    // 4. Check token scoping (if applicable)
    if userInfo.TokenID != nil {
        // Check token_connection_scope and token_mcp_scope
        // ...
    }

    // 5. NOW safe to execute operation
    result := executeQuery(...)
    return result, nil
}
```

**Why It Matters**: Defense in depth. Even if API-level auth is bypassed,
operation-level checks prevent unauthorized access.

### Guideline 5: Migrations Are Immutable

**Rule**: Never modify existing migrations. Always add new migrations.

**Examples**:

**COMPLIANT**:
```go
// Adding a new column: NEW migration
sm.migrations = append(sm.migrations, Migration{
    Version:     7,
    Description: "Add description column to user_groups",
    Up: func(conn *pgxpool.Conn) error {
        _, err := conn.Exec(ctx, `
            ALTER TABLE user_groups
            ADD COLUMN description TEXT;
        `)
        return err
    },
})
```

**VIOLATION**:
```go
// WRONG: Modifying existing Migration 6
sm.migrations = append(sm.migrations, Migration{
    Version:     6,  // WRONG! This migration already applied
    Description: "User groups and privilege management",
    Up: func(conn *pgxpool.Conn) error {
        // Adding new columns to existing migration
        // WRONG! This won't run on existing installations
    },
})
```

**Why It Matters**: Existing installations have already applied old migrations.
Changing them breaks repeatability and can cause data loss.

### Guideline 6: Comprehensive COMMENT ON Statements

**Rule**: Every table, column, constraint, and index must have a COMMENT ON
statement.

**Example**:
```sql
CREATE TABLE user_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE user_groups IS
    'Hierarchical groups for organizing users and permissions';
COMMENT ON COLUMN user_groups.id IS
    'Unique identifier for the group';
COMMENT ON COLUMN user_groups.name IS
    'Unique name for the group';
COMMENT ON COLUMN user_groups.description IS
    'Optional description of the group purpose';
COMMENT ON COLUMN user_groups.created_at IS
    'Timestamp when the group was created';

CREATE INDEX idx_user_groups_name ON user_groups(name);
COMMENT ON INDEX idx_user_groups_name IS
    'Index for fast lookup of groups by name';
```

**Why It Matters**: Comments serve as inline documentation and help future
developers understand schema intent.

### Guideline 7: Test Before You Commit

**Rule**: All tests must pass before committing. No exceptions.

**Required Tests**:
```bash
# Run ALL tests
cd /Users/dpage/git/ai-workbench
make test-all

# This includes:
# - Unit tests for all components
# - Integration tests
# - Linting
# - Coverage checks
```

**Check for Errors**:
- Read test output carefully (don't miss errors in long output)
- Check for warnings in addition to failures
- Verify coverage hasn't decreased
- Ensure linter is happy

**Why It Matters**: Broken tests indicate broken code. Committing broken tests
makes it hard to track when regression was introduced.

### Guideline 8: Tests Must Clean Up

**Rule**: Tests must clean up all resources (databases, files, connections).

**Pattern**:
```go
func TestSomething(t *testing.T) {
    // Create temporary database
    testDB := createTestDatabase(t)
    defer dropTestDatabase(t, testDB)  // ALWAYS clean up

    // Create temporary file
    tmpFile, err := os.CreateTemp("", "test-*.log")
    require.NoError(t, err)
    defer os.Remove(tmpFile.Name())  // ALWAYS clean up

    // Run test...
}
```

**Exception**: Keep debug logs if test fails (for debugging).

**Why It Matters**: Resource leaks slow down test suite and pollute development
environment.

### Guideline 9: Four Spaces, Always

**Rule**: Use four spaces for indentation. Never tabs. Never two spaces.

**Example**:
```go
func Example() {
····if condition {
········doSomething()
········if nested {
············doMore()
········}
····}
}
```

**Why It Matters**: Consistent indentation improves readability and prevents
merge conflicts.

**Enforcement**: Use `gofmt` (for Go) and configure editor to use 4 spaces.

### Guideline 10: Modularize and DRY

**Rule**: Don't repeat yourself. Extract common functionality.

**COMPLIANT**:
```go
// Common function
func validateToken(ctx context.Context, pool *pgxpool.Pool,
    token string) (*UserInfo, error) {
    // Token validation logic (used by all handlers)
}

// Handlers use common function
func (h *Handler) handleFoo(req Request) (*Response, error) {
    userInfo, err := validateToken(ctx, h.dbPool, token)
    // ...
}

func (h *Handler) handleBar(req Request) (*Response, error) {
    userInfo, err := validateToken(ctx, h.dbPool, token)
    // ...
}
```

**VIOLATION**:
```go
// WRONG: Duplicated token validation in each handler
func (h *Handler) handleFoo(req Request) (*Response, error) {
    // Token validation logic duplicated
    hash := sha256.Sum256([]byte(token))
    var userID int
    err := h.dbPool.QueryRow(ctx, "SELECT user_id FROM user_sessions...")
    // ...
}

func (h *Handler) handleBar(req Request) (*Response, error) {
    // Same logic duplicated again - WRONG!
    hash := sha256.Sum256([]byte(token))
    var userID int
    err := h.dbPool.QueryRow(ctx, "SELECT user_id FROM user_sessions...")
    // ...
}
```

**Why It Matters**: Duplication leads to bugs when one copy is updated and
others aren't.

## Pattern Library

### Pattern: Adding a New Probe

**Steps**:
1. Create new file in `/collector/src/probes/`
2. Implement MetricsProbe interface:
   - GetName() string
   - GetTableName() string
   - GetQuery() string
   - Execute(...) ([]map[string]interface{}, error)
   - Store(...) error
   - EnsurePartition(...) error
   - IsDatabaseScoped() bool
3. Add migration to create metrics table (partitioned by inserted_at)
4. Add probe registration in probe registry
5. Add default probe_config entry
6. Write unit tests
7. Update documentation

**Example**: See `/collector/src/probes/pg_stat_database_probe.go`

### Pattern: Adding a New MCP Tool

**Steps**:
1. Define tool parameters struct
2. Implement handler function in `/server/src/mcp/handler.go`
3. Register tool in handleListTools()
4. Add privilege identifier to SeedMCPPrivileges()
5. Implement authorization checks
6. Write unit tests
7. Write integration tests
8. Update API documentation

**Handler Pattern**:
```go
func (h *Handler) handleToolFoo(params map[string]interface{}) (*Response, error) {
    // 1. Parse and validate parameters
    var p FooParams
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, fmt.Errorf("invalid parameters: %w", err)
    }

    // 2. Check privileges
    canAccess, err := privileges.CanAccessMCPItem(
        ctx, h.dbPool, h.userInfo.UserID, "tool:foo")
    if err != nil || !canAccess {
        return nil, fmt.Errorf("access denied")
    }

    // 3. Execute operation
    result := doSomething(p)

    // 4. Return response
    return &Response{
        JSONRPC: JSONRPCVersion,
        ID:      requestID,
        Result:  result,
    }, nil
}
```

### Pattern: Adding a New Migration

**Steps**:
1. Determine next migration version number
2. Add migration to registerMigrations() in `/collector/src/database/schema.go`
3. Include COMMENT ON statements for all objects
4. Test migration on fresh database
5. Test migration on database with existing data
6. Update schema documentation

**Migration Pattern**:
```go
sm.migrations = append(sm.migrations, Migration{
    Version:     7,
    Description: "Brief description of what this migration does",
    Up: func(conn *pgxpool.Conn) error {
        ctx := context.Background()

        // Execute DDL
        _, err := conn.Exec(ctx, `
            CREATE TABLE new_table (
                id SERIAL PRIMARY KEY,
                name TEXT NOT NULL
            );

            COMMENT ON TABLE new_table IS
                'Purpose of this table';
            COMMENT ON COLUMN new_table.id IS
                'Purpose of this column';
            COMMENT ON COLUMN new_table.name IS
                'Purpose of this column';
        `)
        if err != nil {
            return fmt.Errorf("failed to create new_table: %w", err)
        }

        return nil
    },
})
```

### Pattern: Adding a New Privilege Check

**Steps**:
1. Identify the resource being protected
2. Determine access level needed (read vs read_write for connections)
3. Add privilege identifier to SeedMCPPrivileges() if MCP item
4. Call appropriate privilege check function
5. Handle denial gracefully (return error, don't crash)
6. Write test for authorized and unauthorized access

**Connection Access Pattern**:
```go
canAccess, err := privileges.CanAccessConnection(
    ctx, h.dbPool,
    userInfo.UserID,
    connectionID,
    privileges.AccessLevelReadWrite)
if err != nil {
    logger.Errorf("Privilege check failed: %v", err)
    return fmt.Errorf("privilege check failed: %w", err)
}
if !canAccess {
    logger.Warnf("Access denied: user=%d connection=%d",
        userInfo.UserID, connectionID)
    return fmt.Errorf("access denied to connection")
}
```

**MCP Item Access Pattern**:
```go
canAccess, err := privileges.CanAccessMCPItem(
    ctx, h.dbPool,
    userInfo.UserID,
    "tool:my_new_tool")
if err != nil {
    logger.Errorf("Privilege check failed: %v", err)
    return fmt.Errorf("privilege check failed: %w", err)
}
if !canAccess {
    logger.Warnf("Access denied: user=%d tool=my_new_tool",
        userInfo.UserID)
    return fmt.Errorf("access denied to tool")
}
```

### Pattern: Error Handling

**Rules**:
1. Wrap errors with context using fmt.Errorf with %w
2. Log errors before returning
3. Don't leak sensitive information in error messages
4. Return appropriate error codes in MCP responses

**Example**:
```go
func doSomething(id int) error {
    result, err := database.Query(ctx, "SELECT ...", id)
    if err != nil {
        // Log with full details
        logger.Errorf("Database query failed: id=%d err=%v", id, err)

        // Return wrapped error with context
        return fmt.Errorf("failed to query database: %w", err)
    }

    if result == nil {
        // Log the issue
        logger.Warnf("No result found for id=%d", id)

        // Return appropriate error (don't leak SQL details)
        return fmt.Errorf("resource not found")
    }

    return nil
}
```

## Code Review Checklist

Use this checklist when reviewing code (your own or others'):

### Design Compliance
- [ ] Changes align with design goals in DESIGN.md
- [ ] Component boundaries respected
- [ ] No duplicate functionality
- [ ] Appropriate abstraction level
- [ ] Follows established patterns

### Security
- [ ] All SQL queries parameterized
- [ ] Input validation at API boundary
- [ ] Authorization checks before privileged operations
- [ ] No credentials in logs or error messages
- [ ] Sensitive data encrypted appropriately
- [ ] Fail-safe defaults (deny by default)

### Testing
- [ ] Unit tests for new functions
- [ ] Integration tests for new features
- [ ] All tests pass (make test-all)
- [ ] Coverage maintained or improved
- [ ] Tests clean up resources
- [ ] Edge cases tested

### Code Quality
- [ ] Four-space indentation
- [ ] No code duplication
- [ ] Functions are focused and modular
- [ ] Error handling is comprehensive
- [ ] Logging is appropriate (not too much, not too little)
- [ ] Comments explain "why" not "what"
- [ ] Copyright notice in new files

### Documentation
- [ ] COMMENT ON for new database objects
- [ ] README updated if needed
- [ ] API documentation updated
- [ ] Migration description is clear
- [ ] Complex logic has explanatory comments

### Git Hygiene
- [ ] Commit message explains "why"
- [ ] Commits are logical units
- [ ] No commented-out code committed
- [ ] No temporary debug code committed
- [ ] No sensitive data in commit history

## Common Anti-Patterns to Avoid

### Anti-Pattern 1: Direct Database Access from Client

**WRONG**:
```javascript
// Client code directly querying database
const result = await pg.query("SELECT * FROM connections");
```

**RIGHT**:
```javascript
// Client uses MCP protocol
const result = await mcpClient.callTool("list_connections", {});
```

**Why**: Client should never have database credentials. All access via MCP
server.

### Anti-Pattern 2: Bypassing Privilege Checks

**WRONG**:
```go
func (h *Handler) handleSuperUserOnlyOperation(params Params) (*Response, error) {
    // WRONG: Assuming user is authorized because they're authenticated
    result := doPrivilegedOperation(params)
    return result, nil
}
```

**RIGHT**:
```go
func (h *Handler) handleSuperUserOnlyOperation(params Params) (*Response, error) {
    // Check superuser status explicitly
    var isSuperuser bool
    err := h.dbPool.QueryRow(ctx,
        "SELECT is_superuser FROM user_accounts WHERE id = $1",
        h.userInfo.UserID).Scan(&isSuperuser)
    if err != nil {
        return nil, fmt.Errorf("failed to check superuser status: %w", err)
    }
    if !isSuperuser {
        return nil, fmt.Errorf("superuser privileges required")
    }

    result := doPrivilegedOperation(params)
    return result, nil
}
```

**Why**: Authentication != Authorization. Always check privileges explicitly.

### Anti-Pattern 3: Storing Passwords in Plaintext

**WRONG**:
```go
// Storing password directly
_, err := conn.Exec(ctx,
    "INSERT INTO user_accounts (username, password) VALUES ($1, $2)",
    username, password)  // WRONG!
```

**RIGHT**:
```go
// Hash password before storage
hash := sha256.Sum256([]byte(password))
passwordHash := hex.EncodeToString(hash[:])

_, err := conn.Exec(ctx,
    "INSERT INTO user_accounts (username, password_hash) VALUES ($1, $2)",
    username, passwordHash)
```

**Why**: Password breaches are catastrophic. Always hash.

### Anti-Pattern 4: Logging Sensitive Data

**WRONG**:
```go
logger.Infof("User login: username=%s password=%s", username, password)  // WRONG!
logger.Infof("Token created: %s", token)  // WRONG!
```

**RIGHT**:
```go
logger.Infof("User login: username=%s", username)
logger.Infof("Token created: token_id=%d", tokenID)
```

**Why**: Logs may be viewed by unauthorized users. Never log credentials.

### Anti-Pattern 5: Ignoring Errors

**WRONG**:
```go
result, _ := doSomething()  // WRONG! Ignoring error
```

**RIGHT**:
```go
result, err := doSomething()
if err != nil {
    logger.Errorf("Operation failed: %v", err)
    return fmt.Errorf("failed to do something: %w", err)
}
```

**Why**: Ignored errors become silent failures that are hard to debug.

## Getting Help

If you're unsure whether your implementation aligns with design goals:

1. **Read DESIGN.md first** - Often answers the question
2. **Look for similar code** - Follow established patterns
3. **Ask in design review** - Better to ask than implement wrong
4. **Write design doc** - For significant features, document design first
5. **Start with tests** - TDD often reveals design issues early

## Continuous Improvement

These guidelines evolve as we learn. If you discover:
- A common mistake not covered here
- A useful pattern worth documenting
- A guideline that doesn't work in practice
- An inconsistency in the codebase

Please update this document and share your findings.

---

**Version**: 1.0
**Last Updated**: 2025-11-08
**Status**: Living Document
