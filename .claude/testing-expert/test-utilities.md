# Test Utilities Reference - pgEdge AI Workbench

This document provides detailed information about available test utilities and how to use them.

## Overview

Test utilities are located in `/tests/testutil/` and provide common functionality for integration testing across the AI Workbench components.

## Module Structure

```
tests/testutil/
├── database.go    # Database lifecycle management
├── services.go    # Service process management
├── cli.go         # CLI command execution
├── config.go      # Configuration file management
└── common.go      # Common utilities
```

## Database Utilities

**File**: `/tests/testutil/database.go`

### TestDatabase

Manages the lifecycle of a temporary test database.

```go
type TestDatabase struct {
    Name          string         // Database name (ai_workbench_test_<timestamp>)
    ConnString    string         // Connection string for test database
    AdminConnStr  string         // Admin connection string
    Pool          *pgxpool.Pool  // Connection pool
    keepDB        bool           // Whether to keep DB after tests
}
```

### Functions

#### NewTestDatabase

Creates a new test database with a unique name.

```go
func NewTestDatabase() (*TestDatabase, error)
```

**Behavior**:
1. Generates unique database name: `ai_workbench_test_<unix_timestamp>`
2. Connects to PostgreSQL using `TEST_AI_WORKBENCH_SERVER` or default
3. Creates the test database
4. Returns connection pool to the test database

**Environment Variables**:
- `TEST_AI_WORKBENCH_SERVER`: PostgreSQL connection (default: `postgres://postgres@localhost:5432/postgres`)
- `TEST_AI_WORKBENCH_KEEP_DB=1`: Keep database after tests

**Returns**:
- `*TestDatabase`: Test database instance
- `error`: Error if database creation fails

**Example**:
```go
func TestDatabaseOperations(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err, "Failed to create test database")
    defer db.Close()

    // Use db.Pool for queries
    var count int
    err = db.Pool.QueryRow(context.Background(),
        "SELECT COUNT(*) FROM pg_database WHERE datname = $1",
        db.Name).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 1, count)
}
```

#### Close

Closes the connection pool and optionally drops the test database.

```go
func (td *TestDatabase) Close() error
```

**Behavior**:
1. Closes connection pool
2. If `TEST_AI_WORKBENCH_KEEP_DB=1`, prints database name and returns
3. Otherwise:
   - Connects to admin database
   - Terminates all connections to test database
   - Drops the test database

**Example**:
```go
func TestWithCleanup(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()  // Always call Close

    // Test code...
}
```

#### GetPool

Returns the connection pool for the test database.

```go
func (td *TestDatabase) GetPool() *pgxpool.Pool
```

**Example**:
```go
func TestQuery(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    pool := db.GetPool()
    var version string
    err = pool.QueryRow(context.Background(),
        "SELECT version()").Scan(&version)
    require.NoError(t, err)
    t.Logf("PostgreSQL version: %s", version)
}
```

## Service Management Utilities

**File**: `/tests/testutil/services.go`

### Service

Represents a running service process (collector or MCP server).

```go
type Service struct {
    Name    string       // Service name ("collector" or "mcp-server")
    Cmd     *exec.Cmd    // Command process
    LogFile *os.File     // Log file handle
    cancel  context.CancelFunc  // Cancellation function
}
```

### Functions

#### StartCollector

Starts the collector service with specified configuration.

```go
func StartCollector(configPath string) (*Service, error)
```

**Parameters**:
- `configPath`: Path to collector configuration file

**Behavior**:
1. Finds collector binary in `../collector/collector`
2. Creates log file: `tests/logs/collector-<timestamp>.log`
3. Starts collector with `-config` and `-v` flags
4. Redirects stdout/stderr to log file
5. Waits 2 seconds for startup

**Returns**:
- `*Service`: Service instance
- `error`: Error if startup fails

**Example**:
```go
func TestWithCollector(t *testing.T) {
    // Setup database and config first
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err)
    defer testutil.CleanupTestConfig(configPath)

    // Start collector
    collector, err := testutil.StartCollector(configPath)
    require.NoError(t, err, "Failed to start collector")
    defer collector.Stop()

    // Wait for collector to initialize
    time.Sleep(3 * time.Second)

    // Test collector functionality...
}
```

#### StartMCPServer

Starts the MCP server with specified configuration and port.

```go
func StartMCPServer(configPath string, port int) (*Service, error)
```

**Parameters**:
- `configPath`: Path to server configuration file
- `port`: Port number for server to listen on

**Behavior**:
1. Finds server binary in `../server/mcp-server`
2. Creates log file: `tests/logs/mcp-server-<timestamp>.log`
3. Starts server with `-config` and `-port` flags
4. Redirects stdout/stderr to log file
5. Waits 2 seconds for startup

**Returns**:
- `*Service`: Service instance
- `error`: Error if startup fails

**Example**:
```go
func TestWithMCPServer(t *testing.T) {
    const testPort = 18080

    // Setup database and config
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err)
    defer testutil.CleanupTestConfig(configPath)

    // Start server
    server, err := testutil.StartMCPServer(configPath, testPort)
    require.NoError(t, err, "Failed to start MCP server")
    defer server.Stop()

    // Wait for server to be ready
    time.Sleep(3 * time.Second)

    // Test server functionality...
}
```

#### Stop

Stops a running service gracefully.

```go
func (s *Service) Stop() error
```

**Behavior**:
1. Calls cancellation function (sends SIGTERM to process)
2. Waits up to 10 seconds for graceful shutdown
3. Closes log file
4. If process doesn't exit, kills it forcefully

**Example**:
```go
func TestServiceLifecycle(t *testing.T) {
    service, err := testutil.StartMCPServer(configPath, 18080)
    require.NoError(t, err)

    // Use service...

    // Stop service
    err = service.Stop()
    require.NoError(t, err, "Failed to stop service")
}
```

### Helper Functions

#### getTestsDir

Returns the absolute path to the tests directory.

```go
func getTestsDir() (string, error)
```

**Internal use**: Used by service management functions to locate binaries and logs.

## CLI Execution Utilities

**File**: `/tests/testutil/cli.go`

### CLIClient

Client for executing CLI commands and MCP tools.

```go
type CLIClient struct {
    serverURL string  // MCP server URL
    cliPath   string  // Path to CLI binary
    token     string  // Authentication token
}
```

### Functions

#### NewCLIClient

Creates a new CLI client for the specified server.

```go
func NewCLIClient(serverURL string) (*CLIClient, error)
```

**Parameters**:
- `serverURL`: MCP server URL (e.g., `http://localhost:18080`)

**Behavior**:
1. Finds CLI binary in `../cli/workbench-cli`
2. Verifies binary exists
3. Returns CLI client instance

**Returns**:
- `*CLIClient`: CLI client instance
- `error`: Error if CLI binary not found

**Example**:
```go
func TestCLI(t *testing.T) {
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err, "Failed to create CLI client")

    // Use CLI...
}
```

#### RunTool

Executes an MCP tool with specified arguments.

```go
func (c *CLIClient) RunTool(toolName string, args map[string]interface{}) (interface{}, error)
```

**Parameters**:
- `toolName`: Name of MCP tool to execute
- `args`: Tool arguments as map (can be nil)

**Behavior**:
1. Builds CLI command: `workbench-cli tool <toolName> --server <serverURL>`
2. Adds `--token <token>` if token is set
3. Converts args to JSON and passes via stdin
4. Executes command and captures output
5. Parses JSON response

**Returns**:
- `interface{}`: Tool result (parse to expected type)
- `error`: Error if tool execution fails

**Example**:
```go
func TestToolExecution(t *testing.T) {
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err)
    cli.SetToken(adminToken)

    // Execute tool
    result, err := cli.RunTool("list_users", nil)
    require.NoError(t, err)

    // Parse result
    users, ok := result.([]interface{})
    require.True(t, ok, "Expected array of users")
    t.Logf("Found %d users", len(users))
}
```

#### Authenticate

Authenticates a user and returns a session token.

```go
func (c *CLIClient) Authenticate(username, password string) (string, error)
```

**Parameters**:
- `username`: User's username
- `password`: User's password

**Behavior**:
1. Executes `authenticate_user` tool
2. Extracts token from response
3. Returns token

**Returns**:
- `string`: Authentication token
- `error`: Error if authentication fails

**Example**:
```go
func TestAuthentication(t *testing.T) {
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err)

    // Authenticate
    token, err := cli.Authenticate("admin", "password")
    require.NoError(t, err, "Authentication failed")
    assert.NotEmpty(t, token)

    // Use token for subsequent requests
    cli.SetToken(token)
}
```

#### SetToken

Sets the authentication token for subsequent requests.

```go
func (c *CLIClient) SetToken(token string)
```

**Parameters**:
- `token`: Authentication token

**Example**:
```go
func TestWithToken(t *testing.T) {
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err)

    // Set token
    cli.SetToken("admin-token-from-database")

    // Now all RunTool calls will use this token
    result, err := cli.RunTool("list_users", nil)
    require.NoError(t, err)
}
```

#### Ping

Pings the server to verify it's responding.

```go
func (c *CLIClient) Ping() error
```

**Behavior**:
1. Executes `ping` tool
2. Verifies response

**Returns**:
- `error`: Error if ping fails

**Example**:
```go
func TestServerHealth(t *testing.T) {
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err)

    // Verify server is responding
    err = cli.Ping()
    require.NoError(t, err, "Server is not responding")
}
```

## Configuration Utilities

**File**: `/tests/testutil/config.go`

### Functions

#### CreateTestConfig

Creates a test configuration file from template.

```go
func CreateTestConfig(dbName string) (string, error)
```

**Parameters**:
- `dbName`: Test database name

**Behavior**:
1. Reads template from `tests/config/test.conf.template`
2. Replaces `{{DB_NAME}}` placeholder with actual database name
3. Generates unique config filename: `test-ai_workbench_test_<timestamp>.conf`
4. Writes configuration to `tests/config/`
5. Returns path to created config file

**Returns**:
- `string`: Path to created configuration file
- `error`: Error if config creation fails

**Template Variables**:
- `{{DB_NAME}}`: Replaced with test database name

**Example**:
```go
func TestWithConfig(t *testing.T) {
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    defer db.Close()

    // Create config for this database
    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err, "Failed to create config")
    defer testutil.CleanupTestConfig(configPath)

    // Use config to start services
    server, err := testutil.StartMCPServer(configPath, 18080)
    require.NoError(t, err)
    defer server.Stop()
}
```

#### CleanupTestConfig

Removes a test configuration file.

```go
func CleanupTestConfig(configPath string) error
```

**Parameters**:
- `configPath`: Path to configuration file to remove

**Behavior**:
1. Removes configuration file
2. Removes associated password file (if exists)

**Returns**:
- `error`: Error if cleanup fails

**Example**:
```go
func TestConfigCleanup(t *testing.T) {
    configPath, err := testutil.CreateTestConfig("testdb")
    require.NoError(t, err)

    // Use config...

    // Cleanup
    err = testutil.CleanupTestConfig(configPath)
    require.NoError(t, err, "Failed to cleanup config")

    // Verify file removed
    _, err = os.Stat(configPath)
    assert.True(t, os.IsNotExist(err))
}
```

## Common Utilities

**File**: `/tests/testutil/common.go`

### Functions

#### GetProjectRoot

Returns the absolute path to the project root directory.

```go
func GetProjectRoot() (string, error)
```

**Returns**:
- `string`: Absolute path to project root
- `error`: Error if unable to determine project root

**Example**:
```go
func TestProjectStructure(t *testing.T) {
    root, err := testutil.GetProjectRoot()
    require.NoError(t, err)

    // Verify expected directories exist
    collectorDir := filepath.Join(root, "collector")
    assert.DirExists(t, collectorDir)

    serverDir := filepath.Join(root, "server")
    assert.DirExists(t, serverDir)
}
```

## Usage Patterns

### Complete Test Environment Setup

```go
func TestCompleteWorkflow(t *testing.T) {
    // 1. Create test database
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err, "Failed to create database")
    defer db.Close()

    // 2. Run schema migrations
    err = runSchemaMigrations(db)
    require.NoError(t, err, "Failed to run migrations")

    // 3. Create admin user and get token
    adminToken, err := createAdminUser(db)
    require.NoError(t, err, "Failed to create admin")

    // 4. Create test configuration
    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err, "Failed to create config")
    defer testutil.CleanupTestConfig(configPath)

    // 5. Start MCP server
    server, err := testutil.StartMCPServer(configPath, 18080)
    require.NoError(t, err, "Failed to start server")
    defer server.Stop()

    // 6. Create CLI client
    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err, "Failed to create CLI client")
    cli.SetToken(adminToken)

    // 7. Wait for services to be ready
    time.Sleep(3 * time.Second)

    // 8. Verify server is responding
    err = cli.Ping()
    require.NoError(t, err, "Server not responding")

    // 9. Run your tests
    result, err := cli.RunTool("list_users", nil)
    require.NoError(t, err)
    t.Logf("Result: %v", result)
}
```

### Reusable Test Environment

```go
type TestEnv struct {
    DB        *testutil.TestDatabase
    Server    *testutil.Service
    CLI       *testutil.CLIClient
    Config    string
    AdminToken string
}

func setupTestEnv(t *testing.T) *TestEnv {
    t.Helper()

    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)

    adminToken, err := createAdminUser(db)
    require.NoError(t, err)

    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err)

    server, err := testutil.StartMCPServer(configPath, 18080)
    require.NoError(t, err)

    cli, err := testutil.NewCLIClient("http://localhost:18080")
    require.NoError(t, err)
    cli.SetToken(adminToken)

    time.Sleep(3 * time.Second)

    return &TestEnv{
        DB:         db,
        Server:     server,
        CLI:        cli,
        Config:     configPath,
        AdminToken: adminToken,
    }
}

func (env *TestEnv) Cleanup(t *testing.T) {
    t.Helper()

    if env.Server != nil {
        env.Server.Stop()
    }
    if env.Config != "" {
        testutil.CleanupTestConfig(env.Config)
    }
    if env.DB != nil {
        env.DB.Close()
    }
}

func TestWithEnvironment(t *testing.T) {
    env := setupTestEnv(t)
    defer env.Cleanup(t)

    // Run tests with env...
}
```

## Error Handling

### Common Errors and Solutions

#### Binary Not Found

```
Error: collector binary not found at ../collector/collector
```

**Solution**: Build dependencies first
```bash
cd tests
make build-deps
```

#### Database Connection Failed

```
Error: failed to connect to PostgreSQL: connection refused
```

**Solution**: Ensure PostgreSQL is running and connection string is correct
```bash
export TEST_AI_WORKBENCH_SERVER=postgres://postgres@localhost:5432/postgres
```

#### Port Already in Use

```
Error: failed to start MCP server: address already in use
```

**Solution**: Kill orphaned processes
```bash
cd tests
make killall
# or manually
pkill -f mcp-server
```

#### Service Startup Timeout

```
Error: server ping failed: connection refused
```

**Solution**: Increase startup delay
```go
time.Sleep(5 * time.Second)  // Increase from 3 to 5 seconds
```

## Best Practices

### 1. Always Use defer for Cleanup

```go
db, err := testutil.NewTestDatabase()
require.NoError(t, err)
defer db.Close()  // Always cleanup
```

### 2. Check Errors from Utilities

```go
// Good
server, err := testutil.StartMCPServer(configPath, port)
require.NoError(t, err, "Failed to start server")

// Bad - ignoring error
server, _ := testutil.StartMCPServer(configPath, port)
```

### 3. Use Helper Functions

```go
func setupDatabase(t *testing.T) *testutil.TestDatabase {
    t.Helper()  // Marks as helper for better error reporting

    db, err := testutil.NewTestDatabase()
    require.NoError(t, err)
    return db
}
```

### 4. Wait for Services to Be Ready

```go
server, err := testutil.StartMCPServer(configPath, port)
require.NoError(t, err)

// Don't immediately use server, wait for startup
time.Sleep(3 * time.Second)

// Verify it's ready
err = cli.Ping()
require.NoError(t, err)
```

### 5. Use Meaningful Test Data

```go
// Good - clear what's being tested
db, err := testutil.NewTestDatabase()
require.NoError(t, err)

// Bad - unclear purpose
db1, _ := testutil.NewTestDatabase()
db2, _ := testutil.NewTestDatabase()
```

## Related Documents

- `testing-overview.md` - Overall testing strategy
- `integration-testing.md` - Integration test patterns
- `database-testing.md` - Database testing specifics
- `writing-tests.md` - Practical guide for new tests
