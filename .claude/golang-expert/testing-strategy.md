/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Testing Strategy
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Testing Strategy

This document describes the testing approach, patterns, and best practices for
the pgEdge AI DBA Workbench Go backend.

## Testing Philosophy

The project follows these testing principles:

1. **Test Behavior, Not Implementation:** Focus on what the code does, not how
2. **Isolate Units:** Test components independently using mocking
3. **Test Realistic Scenarios:** Integration tests use real databases
4. **Maintain High Coverage:** Aim for >80% code coverage
5. **Fast Feedback:** Unit tests run in milliseconds
6. **Reliable Tests:** No flaky tests, consistent results

## Test Types

### 1. Unit Tests

Test individual functions and methods in isolation.

**Location:** Same directory as source code (Go convention)

**Example:** `/server/src/mcp/handler_test.go`

**Pattern:**
```go
package mcp

import (
    "testing"
)

func TestHandlerCreation(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    if handler == nil {
        t.Fatal("NewHandler returned nil")
    }

    if handler.serverName != "TestServer" {
        t.Errorf("serverName = %v, want TestServer", handler.serverName)
    }

    if handler.initialized {
        t.Error("Handler should not be initialized on creation")
    }
}

func TestHandleInitialize(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "initialize",
        "params": {}
    }`)

    resp, err := handler.HandleRequest(reqData, "")
    if err != nil {
        t.Fatalf("HandleRequest failed: %v", err)
    }

    if resp.Error != nil {
        t.Errorf("Expected no error, got: %v", resp.Error)
    }

    result, ok := resp.Result.(InitializeResult)
    if !ok {
        t.Fatalf("Result is not InitializeResult, got %T", resp.Result)
    }

    if result.ProtocolVersion != "2024-11-05" {
        t.Errorf("ProtocolVersion = %v, want 2024-11-05",
            result.ProtocolVersion)
    }
}
```

### 2. Integration Tests

Test components working together with real dependencies (database, etc.).

**Location:** `/tests/integration/`

**Example:** `/tests/integration/user_test.go`

**Pattern:**
```go
package integration

import (
    "context"
    "testing"

    "github.com/pgEdge/ai-workbench/tests/testutil"
)

func TestUserCreation(t *testing.T) {
    // Setup test database
    pool := testutil.SetupTestDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create user
    _, err := pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
    `, "testuser", "test@example.com", "Test User", "hash")

    if err != nil {
        t.Fatalf("Failed to create user: %v", err)
    }

    // Verify user exists
    var username string
    err = pool.QueryRow(ctx, `
        SELECT username FROM user_accounts WHERE username = $1
    `, "testuser").Scan(&username)

    if err != nil {
        t.Fatalf("Failed to query user: %v", err)
    }

    if username != "testuser" {
        t.Errorf("username = %v, want testuser", username)
    }
}
```

### 3. Table-Driven Tests

Test multiple scenarios with the same logic.

**Pattern:**
```go
func TestBuildConnectionString(t *testing.T) {
    tests := []struct {
        name     string
        config   Config
        expected string
    }{
        {
            name: "basic connection",
            config: Config{
                PgHost:     "localhost",
                PgPort:     5432,
                PgDatabase: "testdb",
                PgUsername: "user",
            },
            expected: "host=localhost port=5432 dbname=testdb user=user",
        },
        {
            name: "with password",
            config: Config{
                PgHost:     "localhost",
                PgPort:     5432,
                PgDatabase: "testdb",
                PgUsername: "user",
                PgPassword: "secret",
            },
            expected: "host=localhost port=5432 dbname=testdb user=user password=secret",
        },
        {
            name: "with SSL",
            config: Config{
                PgHost:     "localhost",
                PgPort:     5432,
                PgDatabase: "testdb",
                PgUsername: "user",
                PgSSLMode:  "require",
            },
            expected: "host=localhost port=5432 dbname=testdb user=user sslmode=require",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := BuildConnectionString(&tt.config)
            if !strings.Contains(result, tt.expected) {
                t.Errorf("BuildConnectionString() = %v, want to contain %v",
                    result, tt.expected)
            }
        })
    }
}
```

## Mocking

### Interface-Based Mocking

Define interfaces for dependencies:

```go
// Config interface for dependency injection
type Config interface {
    Validate() error
    GetPgHost() string
    GetPgPort() int
    GetPgDatabase() string
    // ... other methods
}

// Mock implementation for testing
type mockConfig struct {
    pgHost     string
    pgPort     int
    pgDatabase string
    validationError error
}

func (m *mockConfig) Validate() error {
    return m.validationError
}

func (m *mockConfig) GetPgHost() string {
    return m.pgHost
}

func (m *mockConfig) GetPgPort() int {
    return m.pgPort
}

func (m *mockConfig) GetPgDatabase() string {
    return m.pgDatabase
}

// Test using mock
func TestDatabaseConnection(t *testing.T) {
    config := &mockConfig{
        pgHost:     "testhost",
        pgPort:     5432,
        pgDatabase: "testdb",
    }

    // Test logic using config
    connStr := BuildConnectionString(config)
    // Assertions...
}
```

### Database Mocking

For unit tests that need database interaction, pass `nil` pool and test
without database:

```go
func TestHandlerWithoutDatabase(t *testing.T) {
    // Create handler with nil database pool
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // Test methods that don't require database
    reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "initialize",
        "params": {}
    }`)

    resp, err := handler.HandleRequest(reqData, "")
    if err != nil {
        t.Fatalf("HandleRequest failed: %v", err)
    }

    // Assertions...
}
```

For integration tests, use a real test database (see Test Utilities below).

## Test Utilities

### Test Database Setup

**Location:** `/tests/testutil/database.go`

```go
package testutil

import (
    "context"
    "fmt"
    "os"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
)

// SetupTestDatabase creates a test database connection pool
func SetupTestDatabase(t *testing.T) *pgxpool.Pool {
    t.Helper()

    // Get test database connection string from environment
    connStr := os.Getenv("TEST_DATABASE_URL")
    if connStr == "" {
        connStr = "host=localhost port=5432 dbname=ai_workbench_test " +
            "user=postgres password=postgres sslmode=disable"
    }

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        t.Fatalf("Failed to create test database pool: %v", err)
    }

    // Verify connection
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        t.Fatalf("Failed to ping test database: %v", err)
    }

    // Clean database before test
    CleanTestDatabase(t, pool)

    return pool
}

// CleanTestDatabase removes all data from test database
func CleanTestDatabase(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()

    ctx := context.Background()
    tables := []string{
        "group_mcp_privileges",
        "connection_privileges",
        "group_memberships",
        "user_tokens",
        "service_tokens",
        "groups",
        "connections",
        "user_accounts",
    }

    for _, table := range tables {
        _, err := pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
        if err != nil {
            t.Logf("Warning: failed to clean table %s: %v", table, err)
        }
    }
}

// CreateTestUser creates a test user account
func CreateTestUser(
    t *testing.T,
    pool *pgxpool.Pool,
    username string,
    isSuperuser bool,
) int {
    t.Helper()

    var userID int
    err := pool.QueryRow(context.Background(), `
        INSERT INTO user_accounts
            (username, email, full_name, password_hash, is_superuser)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id
    `, username, username+"@example.com", "Test User",
        "test_hash", isSuperuser).Scan(&userID)

    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }

    return userID
}
```

### Test Configuration

**Location:** `/tests/testutil/config.go`

```go
package testutil

// CreateTestConfig creates a test configuration
func CreateTestConfig() *Config {
    return &Config{
        PgHost:     "localhost",
        PgPort:     5432,
        PgDatabase: "ai_workbench_test",
        PgUsername: "postgres",
        PgPassword: "postgres",
        PgSSLMode:  "disable",
    }
}
```

## Test Organization

### Project Structure

```
/server/src/
├── mcp/
│   ├── handler.go
│   ├── handler_test.go       # Unit tests
│   ├── protocol.go
│   └── protocol_test.go      # Unit tests
├── privileges/
│   ├── privileges.go
│   └── privileges_test.go    # Unit tests
└── ...

/collector/src/
├── database/
│   ├── datastore.go
│   ├── datastore_test.go     # Unit tests
│   ├── schema.go
│   └── schema_test.go        # Unit tests
├── probes/
│   ├── base.go
│   ├── pg_settings_probe.go
│   └── pg_settings_probe_test.go  # Unit tests
└── ...

/tests/
├── integration/
│   ├── user_test.go          # Integration tests
│   └── connection_test.go    # Integration tests
└── testutil/
    ├── database.go           # Test utilities
    ├── config.go             # Test utilities
    └── common.go             # Common test helpers
```

### Test Naming Conventions

**Test Functions:**
```go
func TestFunctionName(t *testing.T)              // Basic test
func TestFunctionName_SpecificCase(t *testing.T) // Specific scenario
func TestFunctionName_Error(t *testing.T)        // Error case
```

**Table-Driven Tests:**
```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name     string  // Descriptive test name
        input    Type
        expected Type
        wantErr  bool
    }{
        // Test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test logic...
        })
    }
}
```

## Running Tests

### Run All Tests

```bash
# Server tests
cd server/src
go test ./...

# Collector tests
cd collector/src
go test ./...

# Integration tests
cd tests/integration
go test -v
```

### Run Specific Package

```bash
go test github.com/pgEdge/ai-workbench/server/src/mcp
```

### Run Specific Test

```bash
go test -run TestHandleInitialize
```

### Run with Verbose Output

```bash
go test -v ./...
```

### Run with Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View coverage in terminal
go tool cover -func=coverage.out

# Generate HTML coverage report
go tool cover -html=coverage.out -o coverage.html
```

### Run with Race Detector

```bash
go test -race ./...
```

## Coverage Reporting

Current coverage targets:

- **Overall:** >80%
- **Critical Paths:** >90% (authentication, authorization, connection handling)
- **Utility Functions:** >70%

### Measuring Coverage

```bash
# Generate coverage for all packages
go test -coverprofile=coverage.out ./...

# View summary
go tool cover -func=coverage.out | grep total

# View detailed HTML report
go tool cover -html=coverage.out
```

### Coverage in CI/CD

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# Check coverage threshold
go tool cover -func=coverage.out | grep total | \
    awk '{print substr($3, 1, length($3)-1)}' | \
    awk '{if ($1 < 80) exit 1}'
```

## Benchmarking

### Writing Benchmarks

```go
func BenchmarkConnectionPooling(b *testing.B) {
    pool := setupTestPool(b)
    defer pool.Close()

    ctx := context.Background()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        conn, err := pool.Acquire(ctx)
        if err != nil {
            b.Fatalf("Failed to acquire connection: %v", err)
        }
        conn.Release()
    }
}

func BenchmarkQueryExecution(b *testing.B) {
    pool := setupTestPool(b)
    defer pool.Close()

    ctx := context.Background()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        var result int
        err := pool.QueryRow(ctx, "SELECT 1").Scan(&result)
        if err != nil {
            b.Fatalf("Query failed: %v", err)
        }
    }
}
```

### Running Benchmarks

```bash
# Run all benchmarks
go test -bench=.

# Run specific benchmark
go test -bench=BenchmarkConnectionPooling

# Run with memory profiling
go test -bench=. -benchmem

# Run with CPU profiling
go test -bench=. -cpuprofile=cpu.prof
```

## Linting and Static Analysis

### golangci-lint Configuration

**File:** `.golangci.yml`

```yaml
linters:
  enable:
    - errcheck      # Check for unchecked errors
    - gosimple      # Simplify code
    - govet         # Go vet
    - ineffassign   # Detect ineffectual assignments
    - staticcheck   # Static analysis
    - unused        # Detect unused code
    - gosec         # Security issues
    - gofmt         # Format check
    - misspell      # Spelling errors

linters-settings:
  gosec:
    excludes:
      - G304  # File inclusion via variable (config files)
      - G115  # Integer overflow (bounds checked)
```

### Running Linters

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run ./...

# Run specific linter
golangci-lint run --disable-all --enable=gosec ./...

# Auto-fix issues
golangci-lint run --fix ./...
```

## Test Best Practices

### 1. Use t.Helper()

Mark helper functions to improve error reporting:

```go
func createTestUser(t *testing.T, username string) int {
    t.Helper()  // Errors reported at caller's line

    var userID int
    err := pool.QueryRow(ctx, `
        INSERT INTO user_accounts (username, ...) VALUES ($1, ...)
        RETURNING id
    `, username).Scan(&userID)

    if err != nil {
        t.Fatalf("Failed to create user: %v", err)
    }

    return userID
}
```

### 2. Use t.Cleanup()

Register cleanup functions for better resource management:

```go
func TestWithCleanup(t *testing.T) {
    pool := setupTestPool(t)
    t.Cleanup(func() {
        pool.Close()
    })

    // Test logic...
    // Pool automatically closed after test
}
```

### 3. Use Subtests

Organize related tests:

```go
func TestUserManagement(t *testing.T) {
    t.Run("CreateUser", func(t *testing.T) {
        // Test user creation
    })

    t.Run("UpdateUser", func(t *testing.T) {
        // Test user update
    })

    t.Run("DeleteUser", func(t *testing.T) {
        // Test user deletion
    })
}
```

### 4. Avoid Sleeps

Use synchronization primitives instead:

```go
// Bad
time.Sleep(100 * time.Millisecond)
if !condition {
    t.Error("Condition not met")
}

// Good
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()

ticker := time.NewTicker(10 * time.Millisecond)
defer ticker.Stop()

for {
    select {
    case <-ctx.Done():
        t.Error("Timeout waiting for condition")
        return
    case <-ticker.C:
        if condition {
            return
        }
    }
}
```

### 5. Test Error Conditions

Don't just test happy paths:

```go
func TestDivision(t *testing.T) {
    tests := []struct {
        name      string
        numerator int
        denominator int
        expected  float64
        wantErr   bool
    }{
        {
            name:        "valid division",
            numerator:   10,
            denominator: 2,
            expected:    5.0,
            wantErr:     false,
        },
        {
            name:        "division by zero",
            numerator:   10,
            denominator: 0,
            wantErr:     true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Divide(tt.numerator, tt.denominator)

            if (err != nil) != tt.wantErr {
                t.Errorf("Divide() error = %v, wantErr %v",
                    err, tt.wantErr)
                return
            }

            if !tt.wantErr && result != tt.expected {
                t.Errorf("Divide() = %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### 6. Use testdata Directory

Store test fixtures in `testdata/`:

```
pkg/
├── handler.go
├── handler_test.go
└── testdata/
    ├── valid_request.json
    ├── invalid_request.json
    └── expected_response.json
```

```go
func TestHandleRequest(t *testing.T) {
    data, err := os.ReadFile("testdata/valid_request.json")
    if err != nil {
        t.Fatalf("Failed to read test data: %v", err)
    }

    // Use data in test...
}
```

### 7. Parallel Tests

Run independent tests in parallel:

```go
func TestParallelExecution(t *testing.T) {
    t.Parallel()  // Mark test as parallel

    // Test logic...
}
```

**Note:** Don't use `t.Parallel()` for tests that:
- Share mutable state
- Use the same database
- Depend on specific execution order

## Continuous Integration

### GitHub Actions Example

```yaml
name: Tests

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
          POSTGRES_DB: ai_workbench_test
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 5432:5432

    steps:
    - uses: actions/checkout@v3

    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'

    - name: Run tests
      env:
        TEST_DATABASE_URL: "host=localhost port=5432 dbname=ai_workbench_test user=postgres password=postgres sslmode=disable"
      run: |
        cd server/src
        go test -v -race -coverprofile=coverage.out ./...
        cd ../../collector/src
        go test -v -race -coverprofile=coverage.out ./...

    - name: Check coverage
      run: |
        cd server/src
        go tool cover -func=coverage.out | grep total
```

## Common Testing Pitfalls

### 1. Not Cleaning Up Resources

Always clean up:

```go
// Bad
func TestWithoutCleanup(t *testing.T) {
    file, _ := os.Create("test.txt")
    // File never closed
}

// Good
func TestWithCleanup(t *testing.T) {
    file, err := os.Create("test.txt")
    if err != nil {
        t.Fatalf("Failed to create file: %v", err)
    }
    defer os.Remove("test.txt")
    defer file.Close()
}
```

### 2. Ignoring Errors in Tests

Check all errors:

```go
// Bad
result, _ := SomeFunction()

// Good
result, err := SomeFunction()
if err != nil {
    t.Fatalf("SomeFunction() failed: %v", err)
}
```

### 3. Brittle Assertions

Test behavior, not implementation:

```go
// Bad - too specific
if response == "User alice created at 2024-01-01 12:00:00" {
    t.Error("Response doesn't match")
}

// Good - test essential parts
if !strings.Contains(response, "User alice created") {
    t.Error("Response doesn't indicate user creation")
}
```

### 4. Global State

Avoid global variables in tests:

```go
// Bad
var testDB *pgxpool.Pool

func TestSomething(t *testing.T) {
    testDB = setupDB()  // Shared global state
}

// Good
func TestSomething(t *testing.T) {
    db := setupDB(t)  // Test-local state
    defer db.Close()
}
```
