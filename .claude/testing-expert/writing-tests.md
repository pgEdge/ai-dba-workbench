# Writing Tests - Practical Guide

This document provides practical, step-by-step guidance for writing tests for new features in the AI DBA Workbench project.

## Quick Start Checklist

When implementing a new feature, follow this checklist:

- [ ] Identify test type needed (unit, integration, or both)
- [ ] Create test file (co-located for Go, in tests/ for integration)
- [ ] Write test cases covering success paths
- [ ] Write test cases covering error paths
- [ ] Write test cases covering edge cases
- [ ] Test security aspects (input validation, authorization)
- [ ] Run tests locally (`make test`)
- [ ] Check coverage (`make coverage`)
- [ ] Run linter (`make lint`)
- [ ] Fix any issues
- [ ] Commit tests with feature code

## Choosing Test Type

### Use Unit Tests When

- Testing a single function or method
- No external dependencies required (or can be mocked)
- Fast execution is important
- Testing logic in isolation

**Example**: Validating email format, parsing configuration, calculating values

### Use Integration Tests When

- Testing multiple components together
- Requires real database or services
- Testing end-to-end workflows
- Verifying component interactions

**Example**: User authentication flow, database migrations, API endpoints

### Use Both When

- Feature has testable logic (unit) and integration points (integration)
- Want fast feedback (unit) and confidence (integration)

**Example**: User creation (unit test validation logic, integration test full workflow)

## Writing Unit Tests

### Step 1: Create Test File

**Location**: Same directory as source file

**Naming**: `<source_file>_test.go`

```bash
# If source file is: collector/src/database/connection.go
# Create test file:  collector/src/database/connection_test.go
```

### Step 2: Set Up Test Function

```go
/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestFunctionName(t *testing.T) {
    // Test implementation
}
```

### Step 3: Write Test Cases

#### Simple Test

```go
func TestBuildConnectionString(t *testing.T) {
    // Arrange: Set up test data
    host := "localhost"
    port := 5432
    database := "testdb"

    // Act: Execute function
    result := BuildConnectionString(host, port, database)

    // Assert: Verify result
    assert.Contains(t, result, "host=localhost")
    assert.Contains(t, result, "port=5432")
    assert.Contains(t, result, "dbname=testdb")
}
```

#### Table-Driven Test

```go
func TestValidateEmail(t *testing.T) {
    tests := []struct {
        name    string
        email   string
        wantErr bool
    }{
        {
            name:    "valid email",
            email:   "user@example.com",
            wantErr: false,
        },
        {
            name:    "empty email",
            email:   "",
            wantErr: true,
        },
        {
            name:    "invalid format",
            email:   "notanemail",
            wantErr: true,
        },
        {
            name:    "missing @",
            email:   "userexample.com",
            wantErr: true,
        },
        {
            name:    "missing domain",
            email:   "user@",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateEmail(tt.email)

            if tt.wantErr {
                require.Error(t, err, "Expected error for email: %s", tt.email)
            } else {
                require.NoError(t, err, "Expected no error for email: %s", tt.email)
            }
        })
    }
}
```

#### Test with Mock

```go
// 1. Define interface (in production code)
type Database interface {
    Query(sql string) ([]Row, error)
}

// 2. Create mock (in test file)
type mockDatabase struct {
    queryResult []Row
    queryError  error
}

func (m *mockDatabase) Query(sql string) ([]Row, error) {
    return m.queryResult, m.queryError
}

// 3. Write test
func TestGetUsers(t *testing.T) {
    // Create mock with expected behavior
    mockDB := &mockDatabase{
        queryResult: []Row{
            {ID: 1, Name: "User 1"},
            {ID: 2, Name: "User 2"},
        },
        queryError: nil,
    }

    // Create service with mock
    service := NewUserService(mockDB)

    // Test
    users, err := service.GetUsers()

    // Verify
    require.NoError(t, err)
    assert.Len(t, users, 2)
    assert.Equal(t, "User 1", users[0].Name)
    assert.Equal(t, "User 2", users[1].Name)
}

func TestGetUsers_Error(t *testing.T) {
    // Create mock that returns error
    mockDB := &mockDatabase{
        queryError: errors.New("database error"),
    }

    service := NewUserService(mockDB)

    // Test
    users, err := service.GetUsers()

    // Verify error handling
    require.Error(t, err)
    assert.Nil(t, users)
    assert.Contains(t, err.Error(), "database error")
}
```

### Step 4: Test Error Cases

Always test error paths:

```go
func TestCreateUser(t *testing.T) {
    t.Run("success", func(t *testing.T) {
        user, err := CreateUser("test@example.com", "Test User")
        require.NoError(t, err)
        assert.NotNil(t, user)
        assert.Equal(t, "Test User", user.Name)
    })

    t.Run("invalid email", func(t *testing.T) {
        _, err := CreateUser("invalid", "Test User")
        require.Error(t, err)
        assert.Contains(t, err.Error(), "invalid email")
    })

    t.Run("empty name", func(t *testing.T) {
        _, err := CreateUser("test@example.com", "")
        require.Error(t, err)
        assert.Contains(t, err.Error(), "empty name")
    })

    t.Run("nil input", func(t *testing.T) {
        _, err := CreateUser("", "")
        require.Error(t, err)
    })
}
```

### Step 5: Test Edge Cases

```go
func TestCalculateDiscount(t *testing.T) {
    tests := []struct {
        name     string
        price    float64
        discount float64
        want     float64
        wantErr  bool
    }{
        {"normal price", 100.0, 10.0, 90.0, false},
        {"zero price", 0, 10.0, 0, false},
        {"negative price", -10.0, 10.0, 0, true},
        {"100% discount", 100.0, 100.0, 0, false},
        {"over 100% discount", 100.0, 110.0, 0, true},
        {"max float", math.MaxFloat64, 10.0, 0, false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := CalculateDiscount(tt.price, tt.discount)

            if tt.wantErr {
                require.Error(t, err)
            } else {
                require.NoError(t, err)
                assert.Equal(t, tt.want, got)
            }
        })
    }
}
```

### Step 6: Run Tests

```bash
# Run tests for package
go test ./database/

# Run with verbose output
go test -v ./database/

# Run specific test
go test -run TestBuildConnectionString ./database/

# Run with coverage
go test -cover ./database/
```

## Writing Integration Tests

### Step 1: Determine Test Location

**Sub-Project Integration**: Co-located (e.g., `server/src/integration/`)
**Cross-Component**: `/tests/integration/`

### Step 2: Create Test File

```go
/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package integration

import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/pgedge/ai-workbench/tests/testutil"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

### Step 3: Set Up Test Environment

```go
func TestFeature(t *testing.T) {
    if os.Getenv("SKIP_INTEGRATION_TESTS") != "" {
        t.Skip("Skipping integration tests")
    }

    // Create test environment
    env := setupTestEnvironment(t)
    defer env.teardown(t)

    // Run tests...
}

type testEnv struct {
    db        *testutil.TestDatabase
    server    *testutil.Service
    cli       *testutil.CLIClient
    config    string
}

func setupTestEnvironment(t *testing.T) *testEnv {
    t.Helper()

    // Create database
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err, "Failed to create test database")

    // Run migrations
    err = runSchemaMigrations(db)
    require.NoError(t, err, "Failed to run migrations")

    // Create config
    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err, "Failed to create config")

    // Start server
    server, err := testutil.StartMCPServer(configPath, 18080)
    require.NoError(t, err, "Failed to start server")

    // Create CLI client
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err, "Failed to create CLI")

    // Wait for startup
    time.Sleep(3 * time.Second)

    return &testEnv{
        db:     db,
        server: server,
        cli:    cli,
        config: configPath,
    }
}

func (env *testEnv) teardown(t *testing.T) {
    t.Helper()

    if env.server != nil {
        env.server.Stop()
    }
    if env.config != "" {
        testutil.CleanupTestConfig(env.config)
    }
    if env.db != nil {
        env.db.Close()
    }
}
```

### Step 4: Write Test Workflow

```go
func TestUserWorkflow(t *testing.T) {
    env := setupTestEnvironment(t)
    defer env.teardown(t)

    username := "testuser"
    email := "test@example.com"
    password := "TestPassword123!"

    // Step 1: Create user
    t.Run("CreateUser", func(t *testing.T) {
        result, err := env.cli.RunTool("create_user", map[string]interface{}{
            "username":  username,
            "email":     email,
            "full_name": "Test User",
            "password":  password,
        })
        require.NoError(t, err, "Failed to create user")
        assert.Contains(t, result, "created")
    })

    // Step 2: Authenticate
    var token string
    t.Run("Authenticate", func(t *testing.T) {
        var err error
        token, err = env.cli.Authenticate(username, password)
        require.NoError(t, err, "Failed to authenticate")
        assert.NotEmpty(t, token)
        env.cli.SetToken(token)
    })

    // Step 3: List users
    t.Run("ListUsers", func(t *testing.T) {
        result, err := env.cli.RunTool("list_users", nil)
        require.NoError(t, err, "Failed to list users")

        users, ok := result.([]interface{})
        require.True(t, ok, "Expected array of users")

        found := false
        for _, u := range users {
            user := u.(map[string]interface{})
            if user["username"] == username {
                found = true
                assert.Equal(t, email, user["email"])
                break
            }
        }
        assert.True(t, found, "Created user not found in list")
    })

    // Step 4: Delete user
    t.Run("DeleteUser", func(t *testing.T) {
        result, err := env.cli.RunTool("delete_user", map[string]interface{}{
            "username": username,
        })
        require.NoError(t, err, "Failed to delete user")
        assert.Contains(t, result, "deleted")
    })

    // Step 5: Verify deletion
    t.Run("VerifyDeletion", func(t *testing.T) {
        result, err := env.cli.RunTool("list_users", nil)
        require.NoError(t, err, "Failed to list users")

        users, ok := result.([]interface{})
        require.True(t, ok, "Expected array of users")

        for _, u := range users {
            user := u.(map[string]interface{})
            if user["username"] == username {
                t.Error("User still exists after deletion")
            }
        }
    })
}
```

### Step 5: Run Integration Tests

```bash
# From tests directory
cd tests
make test

# With coverage
make coverage

# Specific test
make run-test TEST=TestUserWorkflow

# Keep database for inspection
TEST_AI_WORKBENCH_KEEP_DB=1 make test
```

## Testing Security Aspects

### Input Validation

```go
func TestInputValidation(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
        errMsg  string
    }{
        {
            name:    "valid input",
            input:   "valid_username",
            wantErr: false,
        },
        {
            name:    "SQL injection attempt",
            input:   "admin' OR '1'='1",
            wantErr: true,
            errMsg:  "invalid characters",
        },
        {
            name:    "XSS attempt",
            input:   "<script>alert('xss')</script>",
            wantErr: true,
            errMsg:  "invalid characters",
        },
        {
            name:    "command injection attempt",
            input:   "user; rm -rf /",
            wantErr: true,
            errMsg:  "invalid characters",
        },
        {
            name:    "oversized input",
            input:   strings.Repeat("a", 10000),
            wantErr: true,
            errMsg:  "too long",
        },
        {
            name:    "null bytes",
            input:   "user\x00admin",
            wantErr: true,
            errMsg:  "invalid characters",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateUsername(tt.input)

            if tt.wantErr {
                require.Error(t, err)
                if tt.errMsg != "" {
                    assert.Contains(t, err.Error(), tt.errMsg)
                }
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

### Authorization Testing

```go
func TestAuthorization(t *testing.T) {
    env := setupTestEnvironment(t)
    defer env.teardown(t)

    // Create regular user (non-superuser)
    regularToken, err := createTestUser(t, env, "regular", false)
    require.NoError(t, err)

    // Create superuser
    superToken, err := createTestUser(t, env, "admin", true)
    require.NoError(t, err)

    t.Run("regular user cannot create users", func(t *testing.T) {
        env.cli.SetToken(regularToken)

        _, err := env.cli.RunTool("create_user", map[string]interface{}{
            "username": "newuser",
            "email":    "new@example.com",
        })

        require.Error(t, err)
        assert.Contains(t, err.Error(), "permission denied")
    })

    t.Run("superuser can create users", func(t *testing.T) {
        env.cli.SetToken(superToken)

        result, err := env.cli.RunTool("create_user", map[string]interface{}{
            "username":  "newuser",
            "email":     "new@example.com",
            "full_name": "New User",
            "password":  "Password123!",
        })

        require.NoError(t, err)
        assert.Contains(t, result, "created")
    })

    t.Run("user cannot access other user's data", func(t *testing.T) {
        env.cli.SetToken(regularToken)

        _, err := env.cli.RunTool("get_user", map[string]interface{}{
            "username": "admin",
        })

        require.Error(t, err)
        assert.Contains(t, err.Error(), "access denied")
    })
}
```

### Session Isolation Testing

```go
func TestSessionIsolation(t *testing.T) {
    env := setupTestEnvironment(t)
    defer env.teardown(t)

    // Create two users
    user1Token, err := createTestUser(t, env, "user1", false)
    require.NoError(t, err)

    user2Token, err := createTestUser(t, env, "user2", false)
    require.NoError(t, err)

    // User 1 creates a connection
    env.cli.SetToken(user1Token)
    _, err = env.cli.RunTool("create_connection", map[string]interface{}{
        "name":     "user1_connection",
        "host":     "localhost",
        "database": "testdb",
    })
    require.NoError(t, err)

    // User 2 should not see user 1's connection
    env.cli.SetToken(user2Token)
    result, err := env.cli.RunTool("list_connections", nil)
    require.NoError(t, err)

    connections := result.([]interface{})
    for _, conn := range connections {
        c := conn.(map[string]interface{})
        if c["name"] == "user1_connection" {
            t.Error("User 2 can see User 1's connection - isolation breach!")
        }
    }
}
```

## Common Patterns

### Testing with Context Timeout

```go
func TestWithTimeout(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    result, err := LongOperation(ctx)
    require.NoError(t, err)
    assert.NotNil(t, result)
}
```

### Testing Concurrent Operations

```go
func TestConcurrency(t *testing.T) {
    var wg sync.WaitGroup
    errors := make(chan error, 10)

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()

            err := DoWork(idx)
            if err != nil {
                errors <- err
            }
        }(i)
    }

    wg.Wait()
    close(errors)

    // Check for errors
    errorCount := 0
    for err := range errors {
        t.Errorf("Operation failed: %v", err)
        errorCount++
    }

    assert.Equal(t, 0, errorCount, "Expected no errors")
}
```

### Testing Cleanup

```go
func TestCleanup(t *testing.T) {
    // Create resource
    resource := createResource(t)

    // Register cleanup
    t.Cleanup(func() {
        if err := resource.Close(); err != nil {
            t.Logf("Warning: cleanup failed: %v", err)
        }
    })

    // Or use defer
    defer func() {
        if err := resource.Close(); err != nil {
            t.Logf("Warning: cleanup failed: %v", err)
        }
    }()

    // Test code...
}
```

## Running and Debugging Tests

### Run Tests

```bash
# All tests in package
go test ./package/

# Verbose
go test -v ./package/

# Specific test
go test -run TestFunctionName ./package/

# With coverage
go test -cover ./package/

# With race detection
go test -race ./package/
```

### Debug Test Failures

```bash
# Run with verbose output
go test -v ./package/

# Run single test
go test -v -run TestFailingTest ./package/

# Add debug output in test
func TestDebug(t *testing.T) {
    result := calculate()
    t.Logf("Intermediate result: %v", result)

    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

### Keep Test Database

```bash
# Keep database for inspection
TEST_AI_WORKBENCH_KEEP_DB=1 go test -v ./...

# Connect to test database
psql postgres://postgres@localhost:5432/ai_workbench_test_<timestamp>

# Inspect data
SELECT * FROM user_accounts;
```

## Best Practices Summary

1. **Write tests with the feature** - Don't leave them for later
2. **Test success and failure paths** - Don't just test happy path
3. **Test edge cases** - Null, empty, maximum values
4. **Test security** - Validation, authorization, isolation
5. **Use meaningful test names** - Describe what is being tested
6. **Keep tests independent** - No shared state between tests
7. **Clean up resources** - Use defer for cleanup
8. **Use table-driven tests** - For multiple similar test cases
9. **Mock external dependencies** - For unit tests
10. **Run tests before committing** - Catch issues early

## Complete Example

Here's a complete example of testing a new feature:

```go
// Feature: User password validation

// 1. Production code: server/src/usermgmt/password.go
package usermgmt

import (
    "errors"
    "unicode"
)

func ValidatePassword(password string) error {
    if len(password) < 8 {
        return errors.New("password must be at least 8 characters")
    }

    if len(password) > 128 {
        return errors.New("password must not exceed 128 characters")
    }

    hasUpper := false
    hasLower := false
    hasDigit := false
    hasSpecial := false

    for _, char := range password {
        switch {
        case unicode.IsUpper(char):
            hasUpper = true
        case unicode.IsLower(char):
            hasLower = true
        case unicode.IsDigit(char):
            hasDigit = true
        case unicode.IsPunct(char) || unicode.IsSymbol(char):
            hasSpecial = true
        }
    }

    if !hasUpper {
        return errors.New("password must contain uppercase letter")
    }
    if !hasLower {
        return errors.New("password must contain lowercase letter")
    }
    if !hasDigit {
        return errors.New("password must contain digit")
    }
    if !hasSpecial {
        return errors.New("password must contain special character")
    }

    return nil
}

// 2. Test code: server/src/usermgmt/password_test.go
package usermgmt

import (
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestValidatePassword(t *testing.T) {
    tests := []struct {
        name     string
        password string
        wantErr  bool
        errMsg   string
    }{
        {
            name:     "valid password",
            password: "ValidPass123!",
            wantErr:  false,
        },
        {
            name:     "too short",
            password: "Short1!",
            wantErr:  true,
            errMsg:   "at least 8 characters",
        },
        {
            name:     "too long",
            password: strings.Repeat("a", 129) + "A1!",
            wantErr:  true,
            errMsg:   "not exceed 128 characters",
        },
        {
            name:     "no uppercase",
            password: "lowercase123!",
            wantErr:  true,
            errMsg:   "uppercase letter",
        },
        {
            name:     "no lowercase",
            password: "UPPERCASE123!",
            wantErr:  true,
            errMsg:   "lowercase letter",
        },
        {
            name:     "no digit",
            password: "NoDigits!",
            wantErr:  true,
            errMsg:   "digit",
        },
        {
            name:     "no special character",
            password: "NoSpecial123",
            wantErr:  true,
            errMsg:   "special character",
        },
        {
            name:     "minimum valid",
            password: "Pass123!",
            wantErr:  false,
        },
        {
            name:     "with symbols",
            password: "P@ssw0rd!",
            wantErr:  false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidatePassword(tt.password)

            if tt.wantErr {
                require.Error(t, err, "Expected error for password: %s", tt.password)
                assert.Contains(t, err.Error(), tt.errMsg)
            } else {
                require.NoError(t, err, "Expected no error for password: %s", tt.password)
            }
        })
    }
}

// 3. Run tests
// cd server
// make test
// make coverage
// make lint
```

## Related Documents

- `testing-overview.md` - Overall testing strategy
- `unit-testing.md` - Unit test patterns in depth
- `integration-testing.md` - Integration test patterns in depth
- `test-utilities.md` - Test utility reference
- `database-testing.md` - Database testing specifics
- `coverage-and-quality.md` - Coverage and linting
