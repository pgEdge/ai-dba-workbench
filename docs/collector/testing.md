# Testing Guide

This guide covers testing practices for the Collector.

## Test Types

The Collector includes several types of tests:

### Unit Tests

Test individual functions and methods in isolation.

**Location**: Same package as code being tested

**Example**: `database/datastore_test.go`

### Integration Tests

Test interaction between components and with a real database.

**Location**: Spread across packages

**Example**: Schema migration tests, connection pool tests

## Running Tests

### Using Make

```bash
# Run all tests with formatting and linting
make check

# Run just tests
make test

# Run tests with coverage
make coverage
```

### Using Go

```bash
# All tests
cd src
go test ./...

# Specific package
go test ./database/...

# Verbose output
go test -v ./...

# Specific test
go test -run TestDatastoreConnection ./database/

# With coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Database

Tests automatically create a temporary database:

1. Connect to PostgreSQL using TEST_AI_WORKBENCH_SERVER or default
2. Create database with timestamp: `ai_workbench_test_YYYYMMDD_HHMMSS_NNNNNN`
3. Run all tests against that database
4. Drop the database when tests complete

### Environment Variables

**TEST_AI_WORKBENCH_SERVER**: PostgreSQL connection URL

```bash
export TEST_AI_WORKBENCH_SERVER="postgres://user:pass@localhost:5432/postgres"
go test ./...
```

**TEST_AI_WORKBENCH_KEEP_DB**: Keep test database after tests

```bash
export TEST_AI_WORKBENCH_KEEP_DB=1
go test ./...
# Database will remain for inspection
```

**SKIP_DB_TESTS**: Skip all database tests

```bash
export SKIP_DB_TESTS=1
go test ./...
```

## Writing Tests

### Test File Naming

- Test files: `*_test.go`
- Same package as code being tested

### Test Function Naming

```go
func TestFunctionName(t *testing.T) { }
func TestStructMethod(t *testing.T) { }
func TestFeatureDescription(t *testing.T) { }
```

### Basic Test Structure

```go
func TestDatastoreConnection(t *testing.T) {
    // Skip if no database
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database tests")
    }
    
    // Setup
    config := NewTestConfig()
    ds, err := NewDatastore(config)
    if err != nil {
        t.Fatalf("Failed to create datastore: %v", err)
    }
    defer ds.Close()
    
    // Test
    conn, err := ds.GetConnection()
    if err != nil {
        t.Errorf("Failed to get connection: %v", err)
    }
    defer ds.ReturnConnection(conn)
    
    // Verify
    if conn == nil {
        t.Error("Connection is nil")
    }
}
```

### Table-Driven Tests

```go
func TestPasswordEncryption(t *testing.T) {
    tests := []struct {
        name     string
        password string
        secret   string
        wantErr  bool
    }{
        {
            name:     "valid encryption",
            password: "mypassword",
            secret:   "mysecret",
            wantErr:  false,
        },
        {
            name:     "empty secret",
            password: "mypassword",
            secret:   "",
            wantErr:  true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            encrypted, err := crypto.EncryptPassword(tt.password, tt.secret)
            if (err != nil) != tt.wantErr {
                t.Errorf("EncryptPassword() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && encrypted == "" {
                t.Error("Encrypted password is empty")
            }
        })
    }
}
```

### Testing with Database

```go
func TestSchemaManager(t *testing.T) {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database tests")
    }
    
    // Get test database connection
    conn := getTestConnection(t)
    if conn == nil {
        return // Skip if no database
    }
    defer conn.Release()
    
    // Create schema manager
    sm := NewSchemaManager()
    
    // Apply migrations
    err := sm.Migrate(conn)
    if err != nil {
        t.Fatalf("Failed to migrate: %v", err)
    }
    
    // Verify tables exist
    var count int
    err = conn.QueryRow(context.Background(), `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'connections'
    `).Scan(&count)
    
    if err != nil {
        t.Fatalf("Failed to query tables: %v", err)
    }
    
    if count != 1 {
        t.Errorf("Expected 1 connections table, got %d", count)
    }
}
```

## Test Utilities

### Helper Functions

```go
// getTestConnection gets a connection to the test database
func getTestConnection(t *testing.T) *pgxpool.Conn {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database tests")
        return nil
    }
    
    config := NewTestConfig()
    ds, err := NewDatastore(config)
    if err != nil {
        t.Skipf("Failed to create datastore: %v", err)
        return nil
    }
    
    conn, err := ds.GetConnection()
    if err != nil {
        t.Skipf("Failed to get connection: %v", err)
        return nil
    }
    
    return conn
}
```

### Test Fixtures

```go
// createTestConnection creates a test connection record
func createTestConnection(t *testing.T, conn *pgxpool.Conn) int {
    var id int
    err := conn.QueryRow(context.Background(), `
        INSERT INTO connections (
            name, host, port, database_name, username,
            is_shared, is_monitored, owner_token
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id
    `, "test-server", "localhost", 5432, "postgres", "testuser",
       true, true, "test-token").Scan(&id)
    
    if err != nil {
        t.Fatalf("Failed to create test connection: %v", err)
    }
    
    return id
}
```

## Coverage

### Generating Coverage Reports

```bash
# Generate coverage profile
go test -coverprofile=coverage.out ./...

# View in terminal
go tool cover -func=coverage.out

# View in browser
go tool cover -html=coverage.out
```

### Coverage Goals

Aim for:

- Overall coverage should exceed 80 percent.
- Core packages (database, scheduler) should exceed 90 percent.
- Critical functions (encryption, storage) should have 100 percent coverage.

## Continuous Integration

### GitHub Actions

Tests run automatically on:

- Pull requests
- Commits to main branch

Configuration in `.github/workflows/test.yml`.

## Best Practices

### 1. Test Isolation

Each test should be independent:

```go
// Good - independent test
func TestConnection(t *testing.T) {
    conn := createTestConnection(t)
    defer deleteTestConnection(t, conn)
    // test...
}

// Bad - depends on other tests
var globalConnection int
func TestConnection1(t *testing.T) {
    globalConnection = createTestConnection(t)
}
func TestConnection2(t *testing.T) {
    // Uses globalConnection - BAD
}
```

### 2. Cleanup

Always clean up resources:

```go
func TestDatastore(t *testing.T) {
    ds, err := NewDatastore(config)
    if err != nil {
        t.Fatal(err)
    }
    defer ds.Close()  // Always close
    
    // test...
}
```

### 3. Meaningful Assertions

```go
// Good - clear error message
if got != want {
    t.Errorf("GetValue() = %v, want %v", got, want)
}

// Bad - no context
if got != want {
    t.Error("values don't match")
}
```

### 4. Table-Driven Tests

Use for multiple test cases:

```go
tests := []struct {
    name string
    input string
    want string
}{
    {"empty", "", ""},
    {"single", "a", "A"},
    {"multiple", "abc", "ABC"},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got := ToUpper(tt.input)
        if got != tt.want {
            t.Errorf("got %v, want %v", got, tt.want)
        }
    })
}
```

### 5. Skip Appropriately

Skip tests that can't run:

```go
if os.Getenv("SKIP_DB_TESTS") != "" {
    t.Skip("Database not available")
}
```

## Common Testing Patterns

### Testing Errors

```go
func TestInvalidConfig(t *testing.T) {
    config := &Config{Port: -1}
    err := config.Validate()
    if err == nil {
        t.Error("Expected error for invalid port, got nil")
    }
}
```

### Testing Timeouts

```go
func TestTimeout(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()
    
    err := longRunningOperation(ctx)
    if err != context.DeadlineExceeded {
        t.Errorf("Expected timeout error, got %v", err)
    }
}
```

### Testing Concurrent Code

```go
func TestConcurrentAccess(t *testing.T) {
    manager := NewPoolManager()
    var wg sync.WaitGroup
    
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            conn, err := manager.GetConnection(context.Background(), testConn, "secret")
            if err != nil {
                t.Errorf("Failed to get connection: %v", err)
                return
            }
            defer manager.ReturnConnection(testConn.ID, conn)
            // use connection...
        }()
    }
    
    wg.Wait()
}
```

## Debugging Test Failures

### Verbose Output

```bash
go test -v ./...
```

### Run Single Test

```bash
go test -run TestSpecificFunction ./package/
```

### Print Debug Info

```go
func TestDebug(t *testing.T) {
    result := calculate()
    t.Logf("Intermediate result: %v", result)
    
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

### Inspect Test Database

```bash
# Run tests with TEST_AI_WORKBENCH_KEEP_DB
TEST_AI_WORKBENCH_KEEP_DB=1 go test ./...

# Connect to test database
psql ai_workbench_test_20251105_120000_123456

# Inspect state
SELECT * FROM connections;
SELECT * FROM probes;
```

## Performance Testing

### Benchmarks

```go
func BenchmarkEncryption(b *testing.B) {
    password := "test-password"
    secret := "test-secret"

    for i := 0; i < b.N; i++ {
        _, err := crypto.EncryptPassword(password, secret)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

Run benchmarks:

```bash
go test -bench=. ./...
go test -bench=BenchmarkEncryption ./database/
```

## Troubleshooting

### Tests Hang

- Check for missing cleanup (defers)
- Look for goroutine leaks
- Verify timeout contexts are used

### Tests Flaky

- Check for race conditions (`go test -race`)
- Ensure test isolation
- Avoid time-dependent assertions

### Database Tests Fail

- Verify TEST_AI_WORKBENCH_SERVER is correct
- Check PostgreSQL is running
- Ensure user has CREATE DATABASE permission
- Try with SKIP_DB_TESTS=1 to isolate

## See Also

- [Development Guide](development.md) - Setting up development environment
- [Architecture](architecture.md) - Understanding the codebase
