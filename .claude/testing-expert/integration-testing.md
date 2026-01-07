# Integration Testing - pgEdge AI DBA Workbench

This document describes the integration testing structure and approach for the AI DBA Workbench project.

## Overview

Integration tests verify that multiple components work together correctly. The AI DBA Workbench has two levels of integration testing:

1. **Sub-Project Integration Tests**: Tests within a sub-project (e.g., database + schema manager in collector)
2. **Cross-Component Integration Tests**: Tests across collector, server, and CLI (in `/tests/`)

## Integration Test Directory

```
tests/
├── integration/              # Integration test files
│   └── user_test.go         # User management E2E tests
├── testutil/                # Test utilities
│   ├── database.go          # Database lifecycle management
│   ├── services.go          # Service process management
│   ├── cli.go               # CLI command execution
│   ├── config.go            # Configuration file management
│   └── common.go            # Common utilities
├── config/                  # Test configuration files
│   └── test.conf.template   # Configuration template
├── logs/                    # Service logs (created during tests)
├── go.mod                   # Test module dependencies
├── Makefile                 # Test runner commands
└── README.md                # Integration test documentation
```

## Test Utilities

### Database Lifecycle Management

**File**: `/tests/testutil/database.go`

**Purpose**: Create and manage temporary test databases

```go
// Create a new test database
func NewTestDatabase() (*TestDatabase, error)

// Close and optionally drop the test database
func (td *TestDatabase) Close() error

// Get connection pool
func (td *TestDatabase) GetPool() *pgxpool.Pool
```

**Usage**:
```go
func TestUserOperations(t *testing.T) {
    // Create test database
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    // Database name: ai_workbench_test_<timestamp>
    // Connection pool available: db.Pool

    // Run tests...
}
```

**Environment Variables**:
- `TEST_AI_WORKBENCH_SERVER`: PostgreSQL connection string (default: `postgres://postgres@localhost:5432/postgres`)
- `TEST_AI_WORKBENCH_KEEP_DB=1`: Keep database after tests for inspection

**Features**:
- Unique database name per test run
- Automatic cleanup (unless `TEST_AI_WORKBENCH_KEEP_DB=1`)
- Connection pooling
- Terminates connections before cleanup

### Service Management

**File**: `/tests/testutil/services.go`

**Purpose**: Start and stop collector and MCP server processes

```go
// Start collector service
func StartCollector(configPath string) (*Service, error)

// Start MCP server
func StartMCPServer(configPath string, port int) (*Service, error)

// Stop service gracefully
func (s *Service) Stop() error
```

**Usage**:
```go
func TestWithServices(t *testing.T) {
    // Start collector
    collector, err := testutil.StartCollector(configPath)
    require.NoError(t, err)
    defer collector.Stop()

    // Start MCP server
    server, err := testutil.StartMCPServer(configPath, 18080)
    require.NoError(t, err)
    defer server.Stop()

    // Services running, logs in tests/logs/
    // Run tests...
}
```

**Features**:
- Process management with context cancellation
- Log file capture (tests/logs/<service>-<timestamp>.log)
- Graceful shutdown with timeout
- stdout/stderr redirection to logs
- Startup delay to ensure readiness

### CLI Execution

**File**: `/tests/testutil/cli.go`

**Purpose**: Execute CLI commands and MCP tools

```go
// Create CLI client
func NewCLIClient(serverURL string) (*CLIClient, error)

// Execute MCP tool
func (c *CLIClient) RunTool(toolName string, args map[string]interface{}) (interface{}, error)

// Authenticate user
func (c *CLIClient) Authenticate(username, password string) (string, error)

// Set authentication token
func (c *CLIClient) SetToken(token string)

// Ping server
func (c *CLIClient) Ping() error
```

**Usage**:
```go
func TestCLICommands(t *testing.T) {
    // Create CLI client
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err)

    // Authenticate
    token, err := cli.Authenticate("admin", "password")
    require.NoError(t, err)
    cli.SetToken(token)

    // Run MCP tool
    result, err := cli.RunTool("create_user", map[string]interface{}{
        "username": "testuser",
        "email":    "test@example.com",
    })
    require.NoError(t, err)

    // Verify result...
}
```

### Configuration Management

**File**: `/tests/testutil/config.go`

**Purpose**: Generate test configuration files

```go
// Create test configuration from template
func CreateTestConfig(dbName string) (string, error)

// Cleanup test configuration
func CleanupTestConfig(configPath string) error
```

**Usage**:
```go
func TestWithConfig(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    // Create test config for this database
    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err)
    defer testutil.CleanupTestConfig(configPath)

    // Use config to start services...
}
```

## Integration Test Structure

### Test Environment Setup

```go
const (
    testServerPort = 18080
    testServerURL  = "http://localhost:18080"
)

// TestEnvironment holds all test infrastructure
type TestEnvironment struct {
    DB         *testutil.TestDatabase
    Collector  *testutil.Service
    Server     *testutil.Service
    CLI        *testutil.CLIClient
    Config     string
    AdminToken string
}

// SetupTestEnvironment initializes the test environment
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
    // Create logs directory
    logsDir := filepath.Join("..", "logs")
    if err := os.MkdirAll(logsDir, 0755); err != nil {
        t.Fatalf("Failed to create logs directory: %v", err)
    }

    // Create test database
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err, "Failed to create test database")

    // Run schema migrations
    err = runSchemaMigrations(db)
    require.NoError(t, err, "Failed to run schema migrations")

    // Create admin user
    adminToken, err := createAdminUser(db)
    require.NoError(t, err, "Failed to create admin user")

    // Create test configuration
    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err, "Failed to create test config")

    // Start MCP server
    server, err := testutil.StartMCPServer(configPath, testServerPort)
    require.NoError(t, err, "Failed to start MCP server")

    // Create CLI client
    cli, err := testutil.NewCLIClient(testServerURL)
    require.NoError(t, err, "Failed to create CLI client")
    cli.SetToken(adminToken)

    // Wait for services to be ready
    time.Sleep(3 * time.Second)

    // Verify server is responding
    err = cli.Ping()
    require.NoError(t, err, "Server ping failed")

    return &TestEnvironment{
        DB:         db,
        Collector:  nil, // Start if needed
        Server:     server,
        CLI:        cli,
        Config:     configPath,
        AdminToken: adminToken,
    }
}

// TeardownTestEnvironment cleans up the test environment
func (env *TestEnvironment) TeardownTestEnvironment(t *testing.T) {
    if env.Server != nil {
        if err := env.Server.Stop(); err != nil {
            t.Logf("Warning: Failed to stop MCP server: %v", err)
        }
    }

    if env.Collector != nil {
        if err := env.Collector.Stop(); err != nil {
            t.Logf("Warning: Failed to stop collector: %v", err)
        }
    }

    if env.Config != "" {
        if err := testutil.CleanupTestConfig(env.Config); err != nil {
            t.Logf("Warning: Failed to cleanup config: %v", err)
        }
    }

    if env.DB != nil {
        if err := env.DB.Close(); err != nil {
            t.Logf("Warning: Failed to close database: %v", err)
        }
    }
}
```

### Example Integration Test

```go
func TestUserCRUD(t *testing.T) {
    if os.Getenv("SKIP_INTEGRATION_TESTS") != "" {
        t.Skip("Skipping integration tests")
    }

    // Setup test environment
    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)

    // Test data
    username := "testuser"
    email := "testuser@example.com"
    fullName := "Test User"

    // Create user
    t.Run("Create", func(t *testing.T) {
        result, err := env.CLI.RunTool("create_user", map[string]interface{}{
            "username":  username,
            "email":     email,
            "full_name": fullName,
            "password":  "TestPassword123!",
        })
        require.NoError(t, err)
        assert.Contains(t, result, "successfully created")
    })

    // List users and verify creation
    t.Run("List", func(t *testing.T) {
        result, err := env.CLI.RunTool("list_users", nil)
        require.NoError(t, err)

        users := result.([]map[string]interface{})
        found := false
        for _, user := range users {
            if user["username"] == username {
                found = true
                assert.Equal(t, email, user["email"])
                assert.Equal(t, fullName, user["full_name"])
                break
            }
        }
        assert.True(t, found, "Created user not found in list")
    })

    // Update user
    t.Run("Update", func(t *testing.T) {
        newEmail := "newemail@example.com"
        result, err := env.CLI.RunTool("update_user", map[string]interface{}{
            "username": username,
            "email":    newEmail,
        })
        require.NoError(t, err)
        assert.Contains(t, result, "successfully updated")

        // Verify update
        result, err = env.CLI.RunTool("list_users", nil)
        require.NoError(t, err)

        users := result.([]map[string]interface{})
        for _, user := range users {
            if user["username"] == username {
                assert.Equal(t, newEmail, user["email"])
                break
            }
        }
    })

    // Delete user
    t.Run("Delete", func(t *testing.T) {
        result, err := env.CLI.RunTool("delete_user", map[string]interface{}{
            "username": username,
        })
        require.NoError(t, err)
        assert.Contains(t, result, "successfully deleted")

        // Verify deletion
        result, err = env.CLI.RunTool("list_users", nil)
        require.NoError(t, err)

        users := result.([]map[string]interface{})
        for _, user := range users {
            if user["username"] == username {
                t.Error("User still exists after deletion")
            }
        }
    })
}
```

## Sub-Project Integration Tests

### Server Integration Tests

**Location**: `/server/src/integration/`

**Example**: `privileges_integration_test.go`

```go
func TestPrivilegesIntegration(t *testing.T) {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database integration tests")
    }

    // Get database connection
    connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
    if connStr == "" {
        t.Skip("TEST_AI_WORKBENCH_SERVER not set")
    }

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    defer pool.Close()

    // Test privilege seeding
    err = SeedPrivileges(pool)
    require.NoError(t, err)

    // Verify privileges exist
    var count int
    err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM privileges").Scan(&count)
    require.NoError(t, err)
    assert.Greater(t, count, 0)

    // Test privilege assignment
    err = AssignPrivilege(pool, "admin", "manage_users")
    require.NoError(t, err)

    // Verify assignment
    hasPrivilege, err := CheckPrivilege(pool, "admin", "manage_users")
    require.NoError(t, err)
    assert.True(t, hasPrivilege)
}
```

### Collector Integration Tests

**Location**: Co-located in collector packages (e.g., `/collector/src/database/`)

**Example**: Schema migration tests

```go
func TestSchemaMigrations(t *testing.T) {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database integration tests")
    }

    // Create test database
    db := createTestDatabase(t)
    defer cleanupTestDatabase(t, db)

    // Create schema manager
    sm := NewSchemaManager()

    // Apply migrations
    err := sm.Migrate(db)
    require.NoError(t, err)

    // Verify schema version
    var version int
    err = db.QueryRow(context.Background(),
        "SELECT MAX(version) FROM schema_version").Scan(&version)
    require.NoError(t, err)
    assert.Equal(t, LatestSchemaVersion, version)

    // Verify tables exist
    tables := []string{
        "connections",
        "user_accounts",
        "user_tokens",
        "probes",
    }

    for _, table := range tables {
        var exists bool
        err = db.QueryRow(context.Background(), `
            SELECT EXISTS (
                SELECT FROM information_schema.tables
                WHERE table_name = $1
            )
        `, table).Scan(&exists)
        require.NoError(t, err)
        assert.True(t, exists, "table %s should exist", table)
    }
}
```

## Running Integration Tests

### Using Make

```bash
# Run all integration tests
cd tests
make test

# Run with coverage
make coverage

# Run specific test
make run-test TEST=TestUserCRUD

# Build dependencies first
make build-deps

# Kill orphaned processes
make killall
```

### Using Go Directly

```bash
# All integration tests
cd tests
go test -v ./integration/... -timeout 30m

# Specific test
go test -v ./integration/... -run TestUserCRUD

# With environment variables
TEST_AI_WORKBENCH_SERVER=postgres://user:pass@host/db \
TEST_AI_WORKBENCH_KEEP_DB=1 \
go test -v ./integration/...
```

### Environment Variables

**TEST_AI_WORKBENCH_SERVER**: Database connection string
```bash
export TEST_AI_WORKBENCH_SERVER=postgres://postgres:password@localhost:5432/postgres
```

**TEST_AI_WORKBENCH_KEEP_DB**: Keep test database after tests
```bash
export TEST_AI_WORKBENCH_KEEP_DB=1
```

**SKIP_INTEGRATION_TESTS**: Skip all integration tests
```bash
export SKIP_INTEGRATION_TESTS=1
```

**SKIP_DB_TESTS**: Skip database-dependent tests
```bash
export SKIP_DB_TESTS=1
```

## Best Practices

### 1. Test Isolation

Each test should be independent:

```go
func TestFeature1(t *testing.T) {
    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)
    // Test uses its own environment
}

func TestFeature2(t *testing.T) {
    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)
    // Completely separate environment
}
```

### 2. Cleanup

Always clean up resources:

```go
func TestWithCleanup(t *testing.T) {
    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)  // Always cleanup

    // Create test data
    userID := createTestUser(t, env)
    defer deleteTestUser(t, env, userID)  // Cleanup test data

    // Run test...
}
```

### 3. Use Subtests for Related Operations

```go
func TestUserManagement(t *testing.T) {
    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)

    var userID int

    t.Run("Create", func(t *testing.T) {
        // Create user
        userID = createUser(t, env)
    })

    t.Run("Read", func(t *testing.T) {
        // Read user created above
        user := getUser(t, env, userID)
        assert.NotNil(t, user)
    })

    t.Run("Update", func(t *testing.T) {
        // Update user
        updateUser(t, env, userID)
    })

    t.Run("Delete", func(t *testing.T) {
        // Delete user
        deleteUser(t, env, userID)
    })
}
```

### 4. Verify State Changes

```go
func TestStateChange(t *testing.T) {
    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)

    // Verify initial state
    initialCount := getUserCount(t, env)

    // Perform operation
    createUser(t, env, "testuser")

    // Verify state changed
    newCount := getUserCount(t, env)
    assert.Equal(t, initialCount+1, newCount)
}
```

### 5. Test Both Success and Failure Paths

```go
func TestAuthentication(t *testing.T) {
    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)

    t.Run("valid credentials", func(t *testing.T) {
        token, err := env.CLI.Authenticate("admin", "correct_password")
        require.NoError(t, err)
        assert.NotEmpty(t, token)
    })

    t.Run("invalid credentials", func(t *testing.T) {
        _, err := env.CLI.Authenticate("admin", "wrong_password")
        require.Error(t, err)
        assert.Contains(t, err.Error(), "authentication failed")
    })

    t.Run("non-existent user", func(t *testing.T) {
        _, err := env.CLI.Authenticate("nonexistent", "password")
        require.Error(t, err)
    })
}
```

## Debugging Integration Tests

### Check Service Logs

```bash
# Logs are in tests/logs/
ls -la tests/logs/

# View collector log
tail -f tests/logs/collector-<timestamp>.log

# View server log
tail -f tests/logs/mcp-server-<timestamp>.log
```

### Keep Test Database

```bash
TEST_AI_WORKBENCH_KEEP_DB=1 make test

# Connect to test database
psql postgres://postgres@localhost:5432/ai_workbench_test_<timestamp>

# Inspect data
SELECT * FROM user_accounts;
SELECT * FROM connections;
```

### Run Single Test with Verbose Output

```bash
make run-test TEST=TestUserCRUD
# or
go test -v ./integration/... -run TestUserCRUD
```

### Kill Orphaned Processes

```bash
make killall
# or manually
pkill -f mcp-server
pkill -f collector
```

## Common Issues

### Tests Hang

**Causes**:
- Service didn't start (check logs)
- Missing cleanup (defer statements)
- Deadlock in service code

**Solutions**:
- Check service logs in tests/logs/
- Use timeout: `go test -timeout 30m`
- Add startup verification (ping server)

### Database Connection Errors

**Causes**:
- PostgreSQL not running
- Wrong connection string
- Insufficient permissions

**Solutions**:
- Verify PostgreSQL is running: `psql postgres://...`
- Check TEST_AI_WORKBENCH_SERVER variable
- Ensure user can CREATE DATABASE

### Flaky Tests

**Causes**:
- Race conditions
- Insufficient startup delay
- Shared state between tests

**Solutions**:
- Increase startup sleep time
- Use proper synchronization
- Ensure test isolation
- Run with `-race` flag

## CI/CD Integration

### GitHub Actions

```yaml
# .github/workflows/test-integration.yml
name: Integration Tests

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_PASSWORD: postgres
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 5432:5432

    steps:
    - uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: '1.23'

    - name: Build dependencies
      run: |
        cd tests
        make build-deps

    - name: Run integration tests
      env:
        TEST_AI_WORKBENCH_SERVER: postgres://postgres:postgres@localhost:5432/postgres
      run: |
        cd tests
        make coverage

    - name: Upload coverage
      uses: actions/upload-artifact@v4
      with:
        name: integration-coverage
        path: tests/coverage.html
```

## Related Documents

- `testing-overview.md` - Overall testing strategy
- `unit-testing.md` - Unit test patterns
- `test-utilities.md` - Detailed utility documentation
- `database-testing.md` - Database testing specifics
- `writing-tests.md` - Practical guide for new tests
