# Testing MCP Components

This document describes the testing strategy and best practices for the pgEdge AI DBA Workbench MCP server.

## Testing Philosophy

The MCP server follows these testing principles:

1. **Comprehensive coverage** - All functionality should have tests
2. **Test pyramid** - More unit tests than integration tests
3. **Fast feedback** - Unit tests should run quickly
4. **Isolation** - Tests should be independent and repeatable
5. **Realistic scenarios** - Integration tests should mirror production use

## Test Organization

### Directory Structure

```
server/
├── src/
│   ├── mcp/
│   │   ├── handler.go
│   │   ├── handler_test.go          # Unit tests for MCP handlers
│   │   ├── protocol.go
│   │   └── protocol_test.go         # Unit tests for protocol types
│   ├── privileges/
│   │   ├── privileges.go
│   │   ├── privileges_test.go       # Unit tests for privilege logic
│   │   ├── seed.go
│   │   └── seed_test.go
│   ├── integration/
│   │   └── privileges_integration_test.go  # Integration tests
│   └── ...
└── ...
```

### Test Types

1. **Unit Tests** - Test individual functions in isolation
    - File pattern: `*_test.go` in same directory as source
    - Run with: `go test ./src/mcp/...`

2. **Integration Tests** - Test components working together with database
    - Directory: `/server/src/integration/`
    - Run with: `go test ./src/integration/... -v`

3. **Linting** - Static code analysis
    - Run with: `make lint`

4. **Coverage** - Measure test coverage
    - Run with: `make coverage`

---

## Unit Testing MCP Handlers

### Basic Handler Test Pattern

```go
func TestHandleToolName(t *testing.T) {
    // 1. CREATE HANDLER
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // 2. MOCK AUTHENTICATION (if needed)
    handler.userInfo = &UserInfo{
        IsAuthenticated: true,
        IsSuperuser:     true,
        Username:        "testuser",
        IsServiceToken:  false,
    }

    // 3. PREPARE ARGUMENTS
    args := map[string]interface{}{
        "param1": "value1",
        "param2": 42,
    }

    // 4. CALL HANDLER
    result, err := handler.handleToolName(args)

    // 5. VERIFY RESULTS
    if err != nil {
        t.Fatalf("handleToolName failed: %v", err)
    }

    // Verify result structure
    resultMap, ok := result.(map[string]interface{})
    if !ok {
        t.Fatalf("Result is not a map")
    }

    // Verify specific fields
    content, ok := resultMap["content"].([]map[string]interface{})
    if !ok || len(content) == 0 {
        t.Fatalf("Invalid content structure")
    }
}
```

### Testing Protocol Methods

```go
func TestHandleInitialize(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // Create JSON-RPC request
    reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "initialize",
        "params": {}
    }`)

    // Process request
    resp, err := handler.HandleRequest(reqData, "")
    if err != nil {
        t.Fatalf("HandleRequest failed: %v", err)
    }

    // Verify no error in response
    if resp.Error != nil {
        t.Errorf("Expected no error, got: %v", resp.Error)
    }

    // Verify response ID matches
    if resp.ID != "test-1" {
        t.Errorf("Response ID = %v, want test-1", resp.ID)
    }

    // Verify result structure
    result, ok := resp.Result.(InitializeResult)
    if !ok {
        t.Fatalf("Result is not InitializeResult, got %T", resp.Result)
    }

    if result.ProtocolVersion != "2024-11-05" {
        t.Errorf("ProtocolVersion = %v, want 2024-11-05", result.ProtocolVersion)
    }

    // Verify handler state changed
    if !handler.initialized {
        t.Error("Handler should be initialized after initialize method")
    }
}
```

### Testing Error Cases

```go
func TestHandleInvalidJSON(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // Invalid JSON
    reqData := []byte(`{invalid json}`)

    resp, err := handler.HandleRequest(reqData, "")
    if err != nil {
        t.Fatalf("HandleRequest failed: %v", err)
    }

    // Should return parse error
    if resp.Error == nil {
        t.Fatal("Expected error response for invalid JSON")
    }

    if resp.Error.Code != ParseError {
        t.Errorf("Error code = %v, want %v (ParseError)",
            resp.Error.Code, ParseError)
    }
}
```

### Testing Authentication

```go
func TestToolRequiresAuthentication(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // No userInfo set (unauthenticated)
    handler.userInfo = nil

    args := map[string]interface{}{
        "param1": "value1",
    }

    _, err := handler.handleProtectedTool(args)
    if err == nil {
        t.Error("Expected authentication error")
    }

    if !strings.Contains(err.Error(), "authentication required") {
        t.Errorf("Wrong error message: %v", err)
    }
}
```

### Testing Authorization

```go
func TestToolRequiresSuperuser(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // Authenticated but not superuser
    handler.userInfo = &UserInfo{
        IsAuthenticated: true,
        IsSuperuser:     false,
        Username:        "regularuser",
        IsServiceToken:  false,
    }

    args := map[string]interface{}{
        "param1": "value1",
    }

    _, err := handler.handleSuperuserOnlyTool(args)
    if err == nil {
        t.Error("Expected permission denied error")
    }

    if !strings.Contains(err.Error(), "permission denied") {
        t.Errorf("Wrong error message: %v", err)
    }
}
```

### Testing with Mock Database

For unit tests that need database access, you can use a mock:

```go
// Option 1: Set dbPool to nil (bypass database checks)
handler := NewHandler("TestServer", "1.0.0", nil, nil)

// Option 2: Use a mock database pool (more complex)
// This requires a mocking library like testify/mock
```

---

## Integration Testing

Integration tests use a real database and test the full stack.

### Setup and Teardown

```go
func TestIntegrationScenario(t *testing.T) {
    // SETUP
    ctx := context.Background()

    // Connect to test database
    cfg := config.NewConfig()
    cfg.PgHost = "localhost"
    cfg.PgPort = 5432
    cfg.PgDatabase = "ai_workbench_test"
    cfg.PgUsername = "testuser"
    cfg.PgPassword = "testpass"

    pool, err := database.Connect(cfg)
    if err != nil {
        t.Fatalf("Failed to connect to test database: %v", err)
    }
    defer pool.Close()

    // Seed privilege identifiers
    if err := privileges.SeedMCPPrivileges(ctx, pool); err != nil {
        t.Fatalf("Failed to seed privileges: %v", err)
    }

    // Create test data
    // ...

    // EXECUTE TEST
    // ...

    // CLEANUP
    // Delete test data
    _, err = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username LIKE 'test_%'")
    if err != nil {
        t.Logf("Warning: Failed to clean up test users: %v", err)
    }
}
```

### Testing Full Request Flow

```go
func TestCreateUserIntegration(t *testing.T) {
    // Setup database connection and handler
    pool, cfg := setupTestDatabase(t)
    defer pool.Close()

    handler := NewHandler("TestServer", "1.0.0", pool, cfg)

    // Step 1: Initialize
    initReq := []byte(`{
        "jsonrpc": "2.0",
        "id": "1",
        "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "TestClient", "version": "1.0"}
        }
    }`)
    _, err := handler.HandleRequest(initReq, "")
    if err != nil {
        t.Fatalf("Initialize failed: %v", err)
    }

    // Step 2: Authenticate via HTTP API (get token)
    authBody := []byte(`{"username": "admin", "password": "adminpass"}`)
    authReq, _ := http.NewRequest("POST", server.URL+"/api/auth/login", bytes.NewBuffer(authBody))
    authReq.Header.Set("Content-Type", "application/json")
    authResp, err := http.DefaultClient.Do(authReq)
    if err != nil {
        t.Fatalf("Authentication failed: %v", err)
    }
    defer authResp.Body.Close()

    // Extract token from response
    var authResult struct {
        SessionToken string `json:"session_token"`
    }
    json.NewDecoder(authResp.Body).Decode(&authResult)
    token := authResult.SessionToken

    // Step 3: Create user with token
    createReq := []byte(`{
        "jsonrpc": "2.0",
        "id": "3",
        "method": "tools/call",
        "params": {
            "name": "create_user",
            "arguments": {
                "username": "test_alice",
                "email": "alice@test.com",
                "fullName": "Alice Test",
                "password": "TestPass123!"
            }
        }
    }`)
    createResp, err := handler.HandleRequest(createReq, token)
    if err != nil {
        t.Fatalf("Create user failed: %v", err)
    }

    // Verify success
    if createResp.Error != nil {
        t.Errorf("Expected success, got error: %v", createResp.Error)
    }

    // Step 4: Verify user exists in database
    ctx := context.Background()
    var username string
    err = pool.QueryRow(ctx,
        "SELECT username FROM user_accounts WHERE username = $1",
        "test_alice").Scan(&username)
    if err != nil {
        t.Errorf("User not found in database: %v", err)
    }

    // Cleanup
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", "test_alice")
}
```

### Testing Privileges

```go
func TestPrivilegeEnforcement(t *testing.T) {
    pool, cfg := setupTestDatabase(t)
    defer pool.Close()
    ctx := context.Background()

    // Create test user (non-superuser)
    userID, err := createTestUser(pool, "test_user", false)
    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }
    defer deleteTestUser(pool, userID)

    // Create test group
    groupID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_group", "Test group")
    if err != nil {
        t.Fatalf("Failed to create test group: %v", err)
    }
    defer deleteTestGroup(pool, groupID)

    // Add user to group
    err = groupmgmt.AddGroupMember(ctx, pool, groupID, &userID, nil)
    if err != nil {
        t.Fatalf("Failed to add user to group: %v", err)
    }

    // Test 1: User cannot access without privilege
    canAccess, err := privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
    if err != nil {
        t.Fatalf("CanAccessMCPItem failed: %v", err)
    }
    if canAccess {
        t.Error("User should not have access without privilege grant")
    }

    // Grant privilege to group
    err = privileges.GrantMCPPrivilege(ctx, pool, groupID, "create_user")
    if err != nil {
        t.Fatalf("Failed to grant privilege: %v", err)
    }

    // Test 2: User can now access with privilege
    canAccess, err = privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
    if err != nil {
        t.Fatalf("CanAccessMCPItem failed: %v", err)
    }
    if !canAccess {
        t.Error("User should have access after privilege grant")
    }

    // Revoke privilege
    err = privileges.RevokeMCPPrivilege(ctx, pool, groupID, "create_user")
    if err != nil {
        t.Fatalf("Failed to revoke privilege: %v", err)
    }

    // Test 3: User loses access after revoke
    canAccess, err = privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
    if err != nil {
        t.Fatalf("CanAccessMCPItem failed: %v", err)
    }
    if canAccess {
        t.Error("User should not have access after privilege revoke")
    }
}
```

---

## Test Helpers

### Common Test Setup Functions

```go
// Helper to set up test database
func setupTestDatabase(t *testing.T) (*pgxpool.Pool, *config.Config) {
    cfg := config.NewConfig()
    cfg.PgHost = os.Getenv("TEST_DB_HOST")
    if cfg.PgHost == "" {
        cfg.PgHost = "localhost"
    }
    cfg.PgPort = 5432
    cfg.PgDatabase = "ai_workbench_test"
    cfg.PgUsername = "testuser"
    cfg.PgPassword = os.Getenv("TEST_DB_PASSWORD")

    pool, err := database.Connect(cfg)
    if err != nil {
        t.Fatalf("Failed to connect to test database: %v", err)
    }

    // Seed privileges
    ctx := context.Background()
    if err := privileges.SeedMCPPrivileges(ctx, pool); err != nil {
        t.Fatalf("Failed to seed privileges: %v", err)
    }

    return pool, cfg
}

// Helper to create test user
func createTestUser(pool *pgxpool.Pool, username string, isSuperuser bool) (int, error) {
    ctx := context.Background()
    passwordHash := usermgmt.HashPassword("TestPassword123!")

    var userID int
    err := pool.QueryRow(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id
    `, username, username+"@test.com", "Test User", passwordHash, isSuperuser).Scan(&userID)

    return userID, err
}

// Helper to delete test user
func deleteTestUser(pool *pgxpool.Pool, userID int) error {
    ctx := context.Background()
    _, err := pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID)
    return err
}

// Helper to extract token from authentication response
func extractTokenFromResponse(resp *Response) string {
    resultMap, ok := resp.Result.(map[string]interface{})
    if !ok {
        return ""
    }

    content, ok := resultMap["content"].([]map[string]interface{})
    if !ok || len(content) == 0 {
        return ""
    }

    text, ok := content[0]["text"].(string)
    if !ok {
        return ""
    }

    // Extract token from text like "Session token: abc123..."
    parts := strings.Split(text, "Session token: ")
    if len(parts) < 2 {
        return ""
    }

    tokenParts := strings.Split(parts[1], "\n")
    return tokenParts[0]
}
```

---

## Running Tests

### Run All Tests

```bash
cd /Users/dpage/git/ai-workbench/server
go test ./...
```

### Run Specific Package

```bash
go test ./src/mcp/...
```

### Run Specific Test

```bash
go test ./src/mcp/... -run TestHandleInitialize
```

### Run with Verbose Output

```bash
go test ./src/mcp/... -v
```

### Run Integration Tests

```bash
go test ./src/integration/... -v
```

### Run with Coverage

```bash
go test ./src/mcp/... -cover
```

### Generate Coverage Report

```bash
go test ./src/mcp/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Run Linting

```bash
make lint
```

### Run All Checks (via Makefile)

```bash
make test
```

---

## Continuous Integration

### GitHub Actions (Example)

```yaml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_DB: ai_workbench_test
          POSTGRES_USER: testuser
          POSTGRES_PASSWORD: testpass
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

      - name: Run linter
        run: make lint

      - name: Run unit tests
        run: go test ./src/mcp/... -v

      - name: Run integration tests
        env:
          TEST_DB_HOST: localhost
          TEST_DB_PASSWORD: testpass
        run: go test ./src/integration/... -v

      - name: Generate coverage
        run: go test ./... -coverprofile=coverage.out

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./coverage.out
```

---

## Test Coverage Goals

### Current Coverage

Check coverage with:

```bash
make coverage
```

### Coverage Targets

- **Overall:** 80% minimum
- **Critical paths:** 90%+ (authentication, authorization, privilege checks)
- **Handler functions:** 85%+
- **Protocol functions:** 90%+
- **Helper functions:** 75%+

### Improving Coverage

1. **Identify uncovered code:**
    ```bash
    go test ./src/mcp/... -coverprofile=coverage.out
    go tool cover -html=coverage.out
    ```

2. **Write tests for uncovered branches:**
    - Error cases
    - Edge cases
    - Boundary conditions

3. **Focus on critical code:**
    - Authentication logic
    - Authorization checks
    - Database operations
    - Input validation

---

## Best Practices

### Test Naming

```go
// Good
func TestHandleCreateUserSuccess(t *testing.T)
func TestHandleCreateUserUnauthorized(t *testing.T)
func TestHandleCreateUserInvalidEmail(t *testing.T)

// Bad
func TestCreateUser(t *testing.T)
func Test1(t *testing.T)
```

### Test Organization

```go
func TestFeature(t *testing.T) {
    // Setup
    handler := setupHandler()

    // Execute
    result, err := handler.doSomething()

    // Verify
    if err != nil {
        t.Errorf("Unexpected error: %v", err)
    }

    // Cleanup (if needed)
    cleanup()
}
```

### Table-Driven Tests

```go
func TestValidation(t *testing.T) {
    tests := []struct {
        name      string
        input     string
        wantError bool
        errorMsg  string
    }{
        {"valid email", "user@example.com", false, ""},
        {"missing @", "userexample.com", true, "invalid email"},
        {"missing domain", "user@", true, "invalid email"},
        {"empty", "", true, "email required"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateEmail(tt.input)
            if (err != nil) != tt.wantError {
                t.Errorf("validateEmail() error = %v, wantError %v", err, tt.wantError)
            }
            if err != nil && !strings.Contains(err.Error(), tt.errorMsg) {
                t.Errorf("Error message = %v, want to contain %v", err.Error(), tt.errorMsg)
            }
        })
    }
}
```

### Assertions

```go
// Good - specific messages
if result.Username != "alice" {
    t.Errorf("Username = %q, want %q", result.Username, "alice")
}

// Bad - generic messages
if result.Username != "alice" {
    t.Error("Wrong username")
}
```

### Cleanup

```go
// Good - defer cleanup
func TestWithDatabase(t *testing.T) {
    pool := setupDatabase(t)
    defer pool.Close()

    userID := createTestUser(t, pool)
    defer deleteTestUser(t, pool, userID)

    // Test code...
}

// Bad - manual cleanup (might not run if test fails)
func TestWithDatabase(t *testing.T) {
    pool := setupDatabase(t)
    userID := createTestUser(t, pool)

    // Test code...

    deleteTestUser(t, pool, userID)
    pool.Close()
}
```

---

## Debugging Tests

### Print Debug Info

```go
func TestDebug(t *testing.T) {
    result := doSomething()

    // Temporarily print for debugging
    t.Logf("Result: %+v", result)

    // Assertions...
}
```

### Run Single Test with Verbose

```bash
go test ./src/mcp/... -run TestSpecificTest -v
```

### Use Debugger

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug test
dlv test ./src/mcp/... -- -test.run TestSpecificTest
```

---

## Checklist for New Tests

When adding tests for new functionality:

- [ ] Unit tests for happy path
- [ ] Unit tests for error cases
- [ ] Unit tests for edge cases
- [ ] Unit tests for authentication
- [ ] Unit tests for authorization
- [ ] Integration test for full flow
- [ ] Tests run successfully locally
- [ ] Tests pass in CI
- [ ] Coverage meets targets
- [ ] No flaky tests (consistent results)
- [ ] Tests clean up after themselves
- [ ] Test names are descriptive
