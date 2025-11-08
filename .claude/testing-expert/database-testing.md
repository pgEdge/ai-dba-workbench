# Database Testing - pgEdge AI Workbench

This document describes how to test database-dependent code in the AI Workbench project.

## Overview

Database testing in the AI Workbench involves:
- Creating temporary test databases
- Running schema migrations
- Testing queries and data operations
- Ensuring proper cleanup

## Test Database Lifecycle

### 1. Database Creation

Each test run creates a unique temporary database:

```go
db, err := testutil.NewTestDatabase()
require.NoError(t, err)
defer db.Close()
```

**Database Naming**: `ai_workbench_test_<unix_timestamp>`

Example: `ai_workbench_test_1699564823`

### 2. Schema Setup

After creating the database, apply schema migrations:

```go
func runSchemaMigrations(db *testutil.TestDatabase) error {
    ctx := context.Background()

    // Create schema_version table
    _, err := db.Pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS schema_version (
            version INTEGER PRIMARY KEY,
            description TEXT NOT NULL,
            applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `)
    if err != nil {
        return fmt.Errorf("failed to create schema_version: %w", err)
    }

    // Apply migrations
    migrations := []struct {
        version     int
        description string
        sql         string
    }{
        {
            version:     1,
            description: "Create user_accounts table",
            sql: `
                CREATE TABLE user_accounts (
                    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                    username TEXT NOT NULL UNIQUE,
                    email TEXT NOT NULL,
                    is_superuser BOOLEAN NOT NULL DEFAULT FALSE,
                    full_name TEXT NOT NULL,
                    password_hash TEXT NOT NULL,
                    password_expires_at TIMESTAMP,
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
            `,
        },
        // More migrations...
    }

    for _, migration := range migrations {
        // Check if already applied
        var exists bool
        err = db.Pool.QueryRow(ctx,
            "SELECT EXISTS(SELECT 1 FROM schema_version WHERE version = $1)",
            migration.version).Scan(&exists)
        if err != nil {
            return fmt.Errorf("failed to check migration: %w", err)
        }

        if !exists {
            // Apply migration
            _, err = db.Pool.Exec(ctx, migration.sql)
            if err != nil {
                return fmt.Errorf("failed to apply migration %d: %w",
                    migration.version, err)
            }

            // Record migration
            _, err = db.Pool.Exec(ctx,
                "INSERT INTO schema_version (version, description) VALUES ($1, $2)",
                migration.version, migration.description)
            if err != nil {
                return fmt.Errorf("failed to record migration: %w", err)
            }
        }
    }

    return nil
}
```

### 3. Test Execution

Run tests against the prepared database:

```go
func TestUserOperations(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    // Now test with a fully prepared database
    testCreateUser(t, db)
    testListUsers(t, db)
    testDeleteUser(t, db)
}
```

### 4. Database Cleanup

Cleanup happens automatically in `defer db.Close()`:

```go
func (td *TestDatabase) Close() error {
    if td.Pool != nil {
        td.Pool.Close()
    }

    if td.keepDB {
        fmt.Printf("Keeping test database: %s\n", td.Name)
        return nil
    }

    // Terminate connections
    adminPool, err := pgxpool.New(ctx, td.AdminConnStr)
    if err != nil {
        return err
    }
    defer adminPool.Close()

    // Terminate all connections to test database
    _, err = adminPool.Exec(ctx, fmt.Sprintf(
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'",
        td.Name))

    // Drop database
    _, err = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", td.Name))

    return err
}
```

## Environment Variables

### TEST_AI_WORKBENCH_SERVER

PostgreSQL connection string for the admin database (where test databases are created).

**Default**: `postgres://postgres@localhost:5432/postgres`

**Usage**:
```bash
export TEST_AI_WORKBENCH_SERVER=postgres://user:password@hostname:5432/postgres
go test ./...
```

**Format**: Standard PostgreSQL connection URL
- `postgres://` - Scheme
- `user:password@` - Credentials (password optional)
- `hostname:5432` - Host and port
- `/postgres` - Database name (typically 'postgres' for admin database)

### TEST_AI_WORKBENCH_KEEP_DB

Keep test database after tests complete (for inspection).

**Default**: Not set (databases are dropped)

**Usage**:
```bash
export TEST_AI_WORKBENCH_KEEP_DB=1
go test ./...
# Database remains, name printed to console
```

**Use Cases**:
- Debugging test failures
- Inspecting test data
- Verifying schema migrations
- Understanding test behavior

### SKIP_DB_TESTS

Skip all tests that require database access.

**Default**: Not set (tests run normally)

**Usage**:
```bash
export SKIP_DB_TESTS=1
go test ./...
```

**Use Cases**:
- Running tests without PostgreSQL
- CI environments without database
- Quick unit test runs

## Testing Patterns

### Skip Tests Without Database

```go
func skipIfNoDatabase(t *testing.T) *pgxpool.Pool {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
    }

    connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
    if connStr == "" {
        t.Skip("Skipping database test (TEST_AI_WORKBENCH_SERVER not set)")
    }

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        t.Skipf("Skipping database test (could not connect: %v)", err)
    }

    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        t.Skipf("Skipping database test (ping failed: %v)", err)
    }

    return pool
}

func TestWithDatabase(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    // Test code...
}
```

### Test Data Setup and Cleanup

```go
func TestUserCRUD(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()

    // Setup: Create test user
    var userID int
    err = db.Pool.QueryRow(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
        RETURNING id
    `, "testuser", "test@example.com", "Test User", "hashed_password").Scan(&userID)
    require.NoError(t, err)

    // Cleanup: Delete test user
    defer func() {
        _, err := db.Pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID)
        if err != nil {
            t.Logf("Warning: Failed to cleanup test user: %v", err)
        }
    }()

    // Test: Verify user exists
    var count int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM user_accounts WHERE id = $1",
        userID).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 1, count)
}
```

### Testing Transactions

```go
func TestTransactionRollback(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()

    // Start transaction
    tx, err := db.Pool.Begin(ctx)
    require.NoError(t, err)

    // Insert data
    _, err = tx.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
    `, "txuser", "tx@example.com", "TX User", "hash")
    require.NoError(t, err)

    // Rollback
    err = tx.Rollback(ctx)
    require.NoError(t, err)

    // Verify data was not persisted
    var count int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM user_accounts WHERE username = $1",
        "txuser").Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 0, count, "Data should not exist after rollback")
}

func TestTransactionCommit(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()

    // Start transaction
    tx, err := db.Pool.Begin(ctx)
    require.NoError(t, err)
    defer tx.Rollback(ctx)  // Rollback if not committed

    // Insert data
    _, err = tx.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
    `, "txuser", "tx@example.com", "TX User", "hash")
    require.NoError(t, err)

    // Commit
    err = tx.Commit(ctx)
    require.NoError(t, err)

    // Verify data was persisted
    var count int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM user_accounts WHERE username = $1",
        "txuser").Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 1, count, "Data should exist after commit")

    // Cleanup
    _, err = db.Pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", "txuser")
    require.NoError(t, err)
}
```

### Testing Concurrent Access

```go
func TestConcurrentDatabaseAccess(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()
    var wg sync.WaitGroup
    errors := make(chan error, 10)

    // Create 10 users concurrently
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()

            username := fmt.Sprintf("user%d", idx)
            email := fmt.Sprintf("user%d@example.com", idx)

            _, err := db.Pool.Exec(ctx, `
                INSERT INTO user_accounts (username, email, full_name, password_hash)
                VALUES ($1, $2, $3, $4)
            `, username, email, "Test User", "hash")

            if err != nil {
                errors <- err
            }
        }(i)
    }

    wg.Wait()
    close(errors)

    // Check for errors
    for err := range errors {
        t.Errorf("Concurrent insert failed: %v", err)
    }

    // Verify all users created
    var count int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM user_accounts WHERE username LIKE 'user%'").Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 10, count)
}
```

### Testing Schema Migrations

```go
func TestSchemaVersion(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    ctx := context.Background()

    // Initial state: no schema_version table
    var exists bool
    err = db.Pool.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT FROM information_schema.tables
            WHERE table_name = 'schema_version'
        )
    `).Scan(&exists)
    require.NoError(t, err)
    assert.False(t, exists)

    // Run migrations
    err = runSchemaMigrations(db)
    require.NoError(t, err)

    // Verify schema_version table exists
    err = db.Pool.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT FROM information_schema.tables
            WHERE table_name = 'schema_version'
        )
    `).Scan(&exists)
    require.NoError(t, err)
    assert.True(t, exists)

    // Verify migrations recorded
    var count int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM schema_version").Scan(&count)
    require.NoError(t, err)
    assert.Greater(t, count, 0, "At least one migration should be recorded")

    // Verify expected tables exist
    tables := []string{
        "user_accounts",
        "user_tokens",
        "connections",
        "probes",
    }

    for _, table := range tables {
        err = db.Pool.QueryRow(ctx, `
            SELECT EXISTS (
                SELECT FROM information_schema.tables
                WHERE table_name = $1
            )
        `, table).Scan(&exists)
        require.NoError(t, err)
        assert.True(t, exists, "Table %s should exist", table)
    }
}

func TestMigrationIdempotency(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    // Run migrations first time
    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()

    // Get migration count
    var count1 int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM schema_version").Scan(&count1)
    require.NoError(t, err)

    // Run migrations again
    err = runSchemaMigrations(db)
    require.NoError(t, err)

    // Verify no duplicate migrations
    var count2 int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM schema_version").Scan(&count2)
    require.NoError(t, err)
    assert.Equal(t, count1, count2, "Running migrations twice should not create duplicates")
}
```

### Testing Query Results

```go
func TestQueryResults(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()

    // Insert test data
    users := []struct {
        username string
        email    string
        fullName string
    }{
        {"user1", "user1@example.com", "User One"},
        {"user2", "user2@example.com", "User Two"},
        {"user3", "user3@example.com", "User Three"},
    }

    for _, user := range users {
        _, err := db.Pool.Exec(ctx, `
            INSERT INTO user_accounts (username, email, full_name, password_hash)
            VALUES ($1, $2, $3, $4)
        `, user.username, user.email, user.fullName, "hash")
        require.NoError(t, err)
    }

    // Query all users
    rows, err := db.Pool.Query(ctx, `
        SELECT username, email, full_name
        FROM user_accounts
        ORDER BY username
    `)
    require.NoError(t, err)
    defer rows.Close()

    // Verify results
    results := []struct {
        username string
        email    string
        fullName string
    }{}

    for rows.Next() {
        var u struct {
            username string
            email    string
            fullName string
        }
        err := rows.Scan(&u.username, &u.email, &u.fullName)
        require.NoError(t, err)
        results = append(results, u)
    }

    require.NoError(t, rows.Err())
    assert.Equal(t, len(users), len(results))

    for i, expected := range users {
        assert.Equal(t, expected.username, results[i].username)
        assert.Equal(t, expected.email, results[i].email)
        assert.Equal(t, expected.fullName, results[i].fullName)
    }
}
```

### Testing Error Cases

```go
func TestDatabaseErrors(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()

    t.Run("duplicate key violation", func(t *testing.T) {
        // Insert user
        _, err := db.Pool.Exec(ctx, `
            INSERT INTO user_accounts (username, email, full_name, password_hash)
            VALUES ($1, $2, $3, $4)
        `, "duplicate", "dup@example.com", "Duplicate User", "hash")
        require.NoError(t, err)

        // Try to insert duplicate username
        _, err = db.Pool.Exec(ctx, `
            INSERT INTO user_accounts (username, email, full_name, password_hash)
            VALUES ($1, $2, $3, $4)
        `, "duplicate", "another@example.com", "Another User", "hash")
        require.Error(t, err)
        assert.Contains(t, err.Error(), "unique", "Should be unique constraint violation")
    })

    t.Run("not null violation", func(t *testing.T) {
        _, err := db.Pool.Exec(ctx, `
            INSERT INTO user_accounts (username, email, full_name, password_hash)
            VALUES ($1, $2, $3, $4)
        `, "nulltest", nil, "Null Test", "hash")
        require.Error(t, err)
        assert.Contains(t, err.Error(), "null", "Should be NOT NULL violation")
    })

    t.Run("foreign key violation", func(t *testing.T) {
        // Assuming there's a foreign key relationship
        _, err := db.Pool.Exec(ctx, `
            INSERT INTO user_tokens (user_id, token_hash, name)
            VALUES ($1, $2, $3)
        `, 99999, "token_hash", "Test Token")
        require.Error(t, err)
        assert.Contains(t, err.Error(), "foreign key", "Should be foreign key violation")
    })
}
```

## Connection Pooling

### Using Connection Pools

```go
func TestConnectionPool(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    ctx := context.Background()

    // Pool is already configured by testutil
    pool := db.Pool

    // Pool stats
    stat := pool.Stat()
    t.Logf("Pool stats - Total: %d, Idle: %d, Acquired: %d",
        stat.TotalConns(), stat.IdleConns(), stat.AcquiredConns())

    // Acquire connections concurrently
    var wg sync.WaitGroup
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()

            conn, err := pool.Acquire(ctx)
            require.NoError(t, err)
            defer conn.Release()

            var result int
            err = conn.QueryRow(ctx, "SELECT $1::int", idx).Scan(&result)
            require.NoError(t, err)
            assert.Equal(t, idx, result)

            time.Sleep(100 * time.Millisecond)
        }(i)
    }

    wg.Wait()

    // Verify pool is healthy
    err = pool.Ping(ctx)
    require.NoError(t, err)
}
```

## Best Practices

### 1. Always Clean Up Test Data

```go
func TestWithCleanup(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()  // Database cleanup

    ctx := context.Background()

    // Create test data
    var userID int
    err = db.Pool.QueryRow(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
        RETURNING id
    `, "testuser", "test@example.com", "Test User", "hash").Scan(&userID)
    require.NoError(t, err)

    // Cleanup test data
    defer func() {
        _, err := db.Pool.Exec(ctx,
            "DELETE FROM user_accounts WHERE id = $1", userID)
        if err != nil {
            t.Logf("Warning: cleanup failed: %v", err)
        }
    }()

    // Test code...
}
```

### 2. Use Context with Timeout

```go
func TestWithTimeout(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    // Create context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Query with timeout
    var result int
    err = db.Pool.QueryRow(ctx, "SELECT pg_sleep(1), 42").Scan(&result)
    require.NoError(t, err)
    assert.Equal(t, 42, result)
}
```

### 3. Test Isolation

```go
func TestIsolation(t *testing.T) {
    // Each test gets its own database
    t.Run("test1", func(t *testing.T) {
        db1, err := testutil.NewTestDatabase()
        require.NoError(t, err)
        defer db1.Close()

        // Data in db1 doesn't affect other tests
    })

    t.Run("test2", func(t *testing.T) {
        db2, err := testutil.NewTestDatabase()
        require.NoError(t, err)
        defer db2.Close()

        // Completely separate database
    })
}
```

### 4. Verify Constraints

```go
func TestConstraints(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    ctx := context.Background()

    // Test UNIQUE constraint
    _, err = db.Pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
    `, "testuser", "test@example.com", "Test", "hash")
    require.NoError(t, err)

    _, err = db.Pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
    `, "testuser", "different@example.com", "Test", "hash")
    require.Error(t, err, "Duplicate username should fail")

    // Test NOT NULL constraint
    _, err = db.Pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
    `, nil, "test@example.com", "Test", "hash")
    require.Error(t, err, "NULL username should fail")
}
```

## Debugging Database Tests

### Inspect Test Database

```bash
# Run tests with TEST_AI_WORKBENCH_KEEP_DB
TEST_AI_WORKBENCH_KEEP_DB=1 go test -v ./...

# Output will show database name:
# Keeping test database: ai_workbench_test_1699564823

# Connect to it
psql postgres://postgres@localhost:5432/ai_workbench_test_1699564823

# Inspect tables
\dt

# Query data
SELECT * FROM user_accounts;
SELECT * FROM schema_version;
```

### Enable Query Logging

```go
func TestWithQueryLogging(t *testing.T) {
    // Enable query logging in PostgreSQL
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    ctx := context.Background()

    // Enable statement logging
    _, err = db.Pool.Exec(ctx, "SET log_statement = 'all'")
    require.NoError(t, err)

    // Queries will now be logged to PostgreSQL logs
    // Check: tail -f /var/log/postgresql/postgresql-*.log
}
```

## Related Documents

- `testing-overview.md` - Overall testing strategy
- `integration-testing.md` - Integration test patterns
- `test-utilities.md` - Test utility reference
- `writing-tests.md` - Practical guide for new tests
