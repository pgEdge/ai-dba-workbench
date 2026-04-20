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

This document describes the testing approach, patterns, and best practices
for the pgEdge AI DBA Workbench Go backend.

## Testing Philosophy

1. **Test Behavior, Not Implementation:** Focus on what the code does.
2. **Isolate Units:** Test components independently using mocking.
3. **Test Realistic Scenarios:** Integration tests use real databases.
4. **Maintain High Coverage:** Aim for >80% overall; >90% critical; 100%
   security.
5. **Fast Feedback:** Unit tests run in milliseconds.
6. **Reliable Tests:** No flaky tests; consistent results.

## Test Organization

### Directory Structure

```
collector/src/
├── database/
│   ├── datastore.go
│   ├── datastore_test.go       # Unit tests (co-located)
│   ├── schema.go
│   └── schema_test.go
├── probes/
│   ├── base.go
│   └── pg_settings_probe_test.go

server/src/
├── mcp/
│   ├── handler.go
│   ├── handler_test.go
│   ├── protocol.go
│   └── protocol_test.go
├── internal/auth/
│   ├── store.go
│   └── store_test.go
├── internal/api/
│   └── rbac_integration_test.go # Integration tests (co-located)
├── internal/resources/
│   └── integration_test.go
```

### Naming Conventions

```go
func TestFunctionName(t *testing.T)              // Basic test
func TestFunctionName_SpecificCase(t *testing.T) // Specific scenario
func TestFunctionName_Error(t *testing.T)        // Error case
```

## Running Tests

### Makefile Commands

```bash
# Per sub-project
cd collector && make test        # Unit tests
cd collector && make coverage    # Tests with coverage
cd collector && make lint        # Linter
cd collector && make test-all    # Test + coverage + lint

cd server && make test           # Unit + integration tests
cd server && make coverage
cd server && make lint
cd server && make test-all

cd alerter && make test          # Unit tests
cd alerter && make coverage
cd alerter && make lint
cd alerter && make test-all

# All projects from root
make test-all
```

### Environment Variables

```bash
# PostgreSQL connection for tests
export TEST_AI_WORKBENCH_SERVER=postgres://postgres@localhost:5432/postgres

# Keep test database for debugging (name printed to console)
export TEST_AI_WORKBENCH_KEEP_DB=1

# Skip database-dependent tests
export SKIP_DB_TESTS=1

# Skip integration tests
export SKIP_INTEGRATION_TESTS=1
```

### Go Test Commands

```bash
go test ./...                              # All tests
go test -v ./...                           # Verbose
go test -run TestHandleInitialize ./...    # Specific test
go test -race ./...                        # Race detector
go test -short ./...                       # Skip long tests
go test -coverprofile=coverage.out ./...   # Coverage profile
go tool cover -html=coverage.out           # HTML coverage report
go tool cover -func=coverage.out           # Per-function coverage
go test -bench=. -benchmem ./...           # Benchmarks
```

## Unit Test Patterns

### Table-Driven Tests

Preferred pattern for testing multiple scenarios:

```go
func TestBuildConnectionString(t *testing.T) {
    tests := []struct {
        name     string
        config   Config
        expected string
        wantErr  bool
    }{
        {
            name:     "basic connection",
            config:   Config{PgHost: "localhost", PgPort: 5432},
            expected: "host=localhost port=5432",
        },
        {
            name:     "with SSL",
            config:   Config{PgHost: "localhost", PgSSLMode: "require"},
            expected: "sslmode=require",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := BuildConnectionString(&tt.config)
            if !strings.Contains(result, tt.expected) {
                t.Errorf("got %v, want to contain %v",
                    result, tt.expected)
            }
        })
    }
}
```

### Interface-Based Mocking

Define interfaces for dependencies; create manual mock implementations:

```go
// Production code
type Database interface {
    Query(sql string) ([]Row, error)
    Execute(sql string) error
}

type UserRepository struct {
    db Database
}

// Test code
type mockDatabase struct {
    queryResult []Row
    queryError  error
    execError   error
}

func (m *mockDatabase) Query(sql string) ([]Row, error) {
    return m.queryResult, m.queryError
}

func (m *mockDatabase) Execute(sql string) error {
    return m.execError
}

func TestUserRepository_GetUser(t *testing.T) {
    mockDB := &mockDatabase{
        queryResult: []Row{{ID: 1, Name: "Test User"}},
    }
    repo := &UserRepository{db: mockDB}

    user, err := repo.GetUser(1)
    require.NoError(t, err)
    assert.Equal(t, "Test User", user.Name)
}
```

### Testing Without Database

Pass `nil` pool for methods that do not require database access:

```go
func TestHandlerWithoutDatabase(t *testing.T) {
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
}
```

### Skip Pattern for Database Tests

```go
func skipIfNoDatabase(t *testing.T) *pgxpool.Pool {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
    }

    connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
    if connStr == "" {
        t.Skip("TEST_AI_WORKBENCH_SERVER not set")
    }

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        t.Skipf("Could not connect: %v", err)
    }

    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        t.Skipf("Ping failed: %v", err)
    }

    return pool
}
```

## Database Testing

### Test Database Lifecycle

Each test run creates a unique temporary database via `testutil`:

```go
db, err := testutil.NewTestDatabase()
require.NoError(t, err)
defer db.Close()
```

- **Naming**: `ai_workbench_test_<unix_timestamp>`
- **Connection**: Uses `TEST_AI_WORKBENCH_SERVER` (default:
  `postgres://postgres@localhost:5432/postgres`)
- **Cleanup**: `Close()` terminates connections and drops the database
  unless `TEST_AI_WORKBENCH_KEEP_DB=1`

### TestDatabase Struct

```go
type TestDatabase struct {
    Name         string         // ai_workbench_test_<timestamp>
    ConnString   string         // Connection string for test database
    AdminConnStr string         // Admin connection string
    Pool         *pgxpool.Pool  // Connection pool
    keepDB       bool           // Whether to keep DB after tests
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
    tx, err := db.Pool.Begin(ctx)
    require.NoError(t, err)

    _, err = tx.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
    `, "txuser", "tx@example.com", "TX User", "hash")
    require.NoError(t, err)

    err = tx.Rollback(ctx)
    require.NoError(t, err)

    var count int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM user_accounts WHERE username = $1",
        "txuser").Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 0, count, "Data should not exist after rollback")
}
```

### Testing Schema Migrations

```go
func TestMigrationIdempotency(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    err = runSchemaMigrations(db)
    require.NoError(t, err)

    var count1 int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM schema_version").Scan(&count1)
    require.NoError(t, err)

    // Run again; verify no duplicates
    err = runSchemaMigrations(db)
    require.NoError(t, err)

    var count2 int
    err = db.Pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM schema_version").Scan(&count2)
    require.NoError(t, err)
    assert.Equal(t, count1, count2)
}
```

### Testing Constraint Violations

```go
func TestDatabaseErrors(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()
    err = runSchemaMigrations(db)
    require.NoError(t, err)
    ctx := context.Background()

    t.Run("duplicate key", func(t *testing.T) {
        _, err := db.Pool.Exec(ctx, `
            INSERT INTO user_accounts (username, email, full_name, password_hash)
            VALUES ($1, $2, $3, $4)
        `, "dup", "dup@example.com", "Dup", "hash")
        require.NoError(t, err)

        _, err = db.Pool.Exec(ctx, `
            INSERT INTO user_accounts (username, email, full_name, password_hash)
            VALUES ($1, $2, $3, $4)
        `, "dup", "another@example.com", "Another", "hash")
        require.Error(t, err)
        assert.Contains(t, err.Error(), "unique")
    })

    t.Run("not null violation", func(t *testing.T) {
        _, err := db.Pool.Exec(ctx, `
            INSERT INTO user_accounts (username, email, full_name, password_hash)
            VALUES ($1, $2, $3, $4)
        `, "nulltest", nil, "Null Test", "hash")
        require.Error(t, err)
    })
}
```

## Integration Testing

### Integration Test Approach

Integration tests live alongside the code they test (co-located).
Each integration test file connects to a real PostgreSQL instance
using the `TEST_AI_WORKBENCH_SERVER` environment variable. Tests use
the `skipIfNoDatabase` helper to skip when no database is available.

For examples, see:

- `server/src/internal/api/rbac_integration_test.go`
- `server/src/internal/api/cluster_handlers_test.go` (PUT
  cluster-groups update tests)
- `server/src/internal/resources/integration_test.go`

### HTTP Handler Integration Tests: Bearer + Context

Handlers in `internal/api/` that require authentication and RBAC use
TWO independent auth surfaces:

1. `getUserInfoCompat(r, h.authStore)` validates a bearer token from
   the `Authorization` header via `authStore.ValidateSessionToken`.
2. `rbacChecker.HasAdminPermission(r.Context(), perm)` reads
   `auth.UserIDContextKey` / `auth.IsSuperuserContextKey` from the
   request context.

In production, middleware extracts the bearer token and populates the
context. Handler-level tests bypass that middleware, so you must set
BOTH: a real session token on the request AND the corresponding user
context values. Setting only one causes confusing 401s or 403s that
look like ownership or permission bugs but are actually test-harness
gaps.

Use the existing helpers together. The `rbac_handlers_test.go`
helpers `withUser(req, userID)` and `withSuperuser(req)` set
context. A handful of `cluster_handlers_test.go` helpers show the
combined pattern:

```go
// Create a user with the required admin permission, authenticate
// them, and return BOTH the userID and the raw session token.
handler, _, userID, token, cleanup := setupGroupUpdateHandler(t, ds)
defer cleanup()

req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster-groups/42",
    bytes.NewReader(body))
req.Header.Set("Content-Type", "application/json")
req = withBearer(req, token)     // satisfies getUserInfoCompat
req = withUser(req, userID)      // satisfies HasAdminPermission
```

When a test does not care about the user identity (only that
permission gating fires), `withSuperuser(req)` plus no bearer is NOT
sufficient if the handler calls `getUserInfoCompat`. Either skip that
test at the level that does not use bearer auth (for example, the
auto-detected-group branch only uses context-based RBAC and no
bearer), or pair `withBearer` and `withSuperuser`. Inspect the
handler source to see which surfaces it touches.

### Datastore-Backed Handler Tests

When a handler touches the datastore, construct a `*database.Datastore`
from a test pool via `database.NewTestDatastore(pool)` (defined in
`server/src/internal/database/test_helpers.go`). The fields on
`Datastore` are unexported, so tests in other packages cannot build
one directly. `NewTestDatastore` wraps a caller-owned pool and leaves
`Close` responsibility with the caller.

Minimal schemas work fine: `cluster_handlers_test.go` uses
`clusterGroupsTestSchema` which creates only the `cluster_groups`
table the exercised code path needs, not the full collector schema.
This keeps the tests fast and decoupled from unrelated migrations.

## Security Testing Patterns

### Input Validation

```go
func TestInputValidation(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid input", "valid_username", false},
        {"SQL injection", "admin' OR '1'='1", true},
        {"XSS attempt", "<script>alert('xss')</script>", true},
        {"command injection", "user; rm -rf /", true},
        {"oversized input", strings.Repeat("a", 10000), true},
        {"null bytes", "user\x00admin", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateUsername(tt.input)
            if tt.wantErr {
                require.Error(t, err)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

### Authorization

```go
func TestAuthorization(t *testing.T) {
    env := SetupTestEnvironment(t)
    defer env.Teardown(t)

    regularToken := createTestUser(t, env, "regular", false)
    superToken := createTestUser(t, env, "admin", true)

    t.Run("regular user denied", func(t *testing.T) {
        env.CLI.SetToken(regularToken)
        _, err := env.CLI.RunTool("create_user", map[string]interface{}{
            "username": "newuser", "email": "new@example.com",
        })
        require.Error(t, err)
        assert.Contains(t, err.Error(), "permission denied")
    })

    t.Run("superuser allowed", func(t *testing.T) {
        env.CLI.SetToken(superToken)
        result, err := env.CLI.RunTool("create_user", map[string]interface{}{
            "username": "newuser", "email": "new@example.com",
            "full_name": "New", "password": "Password123!",
        })
        require.NoError(t, err)
        assert.Contains(t, result, "created")
    })
}
```

## Coverage and Quality

### Coverage Targets

- **Overall**: >80%
- **Critical paths** (auth, RBAC, connection handling): >90%
- **Security functions** (encryption, validation): 100%
- **Lower priority** (main entry points, logging, simple getters):
  acceptable lower coverage

### Coverage Commands

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep total        # Summary
go tool cover -html=coverage.out -o coverage.html     # HTML report
go tool cover -func=coverage.out | awk '$3 < 80.0'   # Low-coverage files
```

### Coverage Modes

- `set` (default): Binary covered/not-covered.
- `count`: How many times each statement executed.
- `atomic`: Thread-safe counting; use with `-race`.

### Linting with golangci-lint

**Config files**: Each sub-project has its own config file
(`server/src/.golangci.yml`, `collector/.golangci.yml`,
`alerter/.golangci.yml`).

```yaml
linters-settings:
    errcheck:
        check-type-assertions: true
        check-blank: true
    govet:
        enable-all: true
        disable:
            - fieldalignment
            - shadow
    misspell:
        locale: US

linters:
    enable:
        - errcheck        # Unchecked errors
        - govet           # Standard Go vet checks
        - ineffassign     # Ineffectual assignments
        - staticcheck     # Advanced static analysis
        - unused          # Unused code
        - misspell        # Spelling errors
        - gosec           # Security-focused checks

issues:
    exclude-dirs:
        - vendor
    exclude-rules:
        - path: _test\.go
          linters:
              - gosec
        - path: _test\.go
          linters:
              - errcheck
          text: "Error return value of.*Close.*not checked"

run:
    timeout: 5m
    tests: true
```

### Running Linters

```bash
golangci-lint run ./...           # Run all enabled linters
golangci-lint run --fix ./...     # Auto-fix where possible
gofmt -w ./...                    # Format all Go files
go vet ./...                      # Built-in static analysis
```

### Suppressing Linter Warnings

```go
// Specific linter with explanation
//nolint:gosec // G204: Command execution - input is validated
func executeCommand(cmd string) { ... }
```

## CI/CD

### GitHub Actions Workflows

- `.github/workflows/ci-collector.yml`
- `.github/workflows/ci-server.yml`
- `.github/workflows/ci-alerter.yml`
- `.github/workflows/ci-client.yml`
- `.github/workflows/ci-docs.yml`

All workflows:

1. **Matrix**: Go 1.23, 1.24
2. **Services**: PostgreSQL 14, 15, 16, 17, 18
3. **Steps**: Build, test with coverage, lint, upload coverage HTML
4. **Artifacts**: Coverage reports retained 7 days

### CI Example (Server)

```yaml
services:
  postgres:
    image: postgres:${{ matrix.postgres-version }}
    env:
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: postgres
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
    go-version: ${{ matrix.go-version }}
- name: Run tests
  env:
    TEST_AI_WORKBENCH_SERVER: postgres://postgres:postgres@localhost:5432/postgres
  run: cd server && make coverage
- name: Upload coverage
  uses: actions/upload-artifact@v4
  with:
    name: server-coverage
    path: server/src/coverage.html
    retention-days: 7
```

## Best Practices

### Use t.Helper()

Mark helper functions for better error reporting:

```go
func createTestUser(t *testing.T, username string) int {
    t.Helper()
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

### Use t.Cleanup()

```go
func TestWithCleanup(t *testing.T) {
    pool := setupTestPool(t)
    t.Cleanup(func() { pool.Close() })
    // Pool automatically closed after test
}
```

### Use Subtests for Related Operations

```go
func TestUserManagement(t *testing.T) {
    t.Run("CreateUser", func(t *testing.T) { ... })
    t.Run("UpdateUser", func(t *testing.T) { ... })
    t.Run("DeleteUser", func(t *testing.T) { ... })
}
```

### Avoid Sleeps; Use Synchronization

```go
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
        if condition { return }
    }
}
```

### Do Not Use t.Parallel() With Shared Database

Tests sharing a database or mutable state must not use `t.Parallel()`.

### Test Error Conditions

Always test failure paths alongside success paths:

```go
func TestDivision(t *testing.T) {
    tests := []struct {
        name    string
        a, b    int
        want    float64
        wantErr bool
    }{
        {"valid", 10, 2, 5.0, false},
        {"divide by zero", 10, 0, 0, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Divide(tt.a, tt.b)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && result != tt.want {
                t.Errorf("got %v, want %v", result, tt.want)
            }
        })
    }
}
```

### Use testdata Directory for Fixtures

```
pkg/
├── handler.go
├── handler_test.go
└── testdata/
    ├── valid_request.json
    └── invalid_request.json
```

### Testing Stack

- **Framework**: Standard library `testing` package
- **Assertions**: `testify/assert` and `testify/require`
- **Mocking**: Interface-based (manual mocks; no external framework)
- **Database**: `pgx/v5` with connection pooling
- **Coverage**: `go test -cover` and `go tool cover`
- **Linting**: `golangci-lint`

## Debugging Tests

### Keep Test Database for Inspection

```bash
TEST_AI_WORKBENCH_KEEP_DB=1 go test -v ./...
# Output: "Keeping test database: ai_workbench_test_1699564823"
psql postgres://postgres@localhost:5432/ai_workbench_test_1699564823
\dt
SELECT * FROM user_accounts;
```

### Kill Orphaned Processes

```bash
cd server && make killall
# or manually:
pkill -f ai-dba-server
pkill -f ai-dba-collector
```

## Common Pitfalls

1. **Not cleaning up resources**: Always use `defer db.Close()`,
   `defer file.Close()`, `defer os.Remove(path)`.
2. **Ignoring errors in tests**: Check all errors with `require.NoError`.
3. **Brittle assertions**: Test essential behavior, not exact timestamps
   or formatting.
4. **Global state**: Use test-local variables, not package-level globals.
5. **Testing external libraries**: Test your wrappers, not pgx itself.
6. **Disabling linters without reason**: Always add an explanation in
   `//nolint` comments.
