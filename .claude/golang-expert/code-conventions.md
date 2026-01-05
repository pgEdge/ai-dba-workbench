/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Go Code Conventions
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Go Code Conventions and Best Practices

This document describes the Go coding conventions, patterns, and best practices
used in the pgEdge AI DBA Workbench project.

## General Principles

1. **Follow Go Idioms:** Write idiomatic Go code that feels natural to Go
   developers
2. **Readability First:** Code is read more often than written
3. **Explicit Over Clever:** Favor clarity over brevity
4. **Error Handling:** Handle errors explicitly, provide context
5. **Minimal Abstraction:** Don't over-engineer, solve actual problems
6. **Security:** Always consider security implications

## Code Formatting

### Indentation

**Use four spaces for indentation** (project-specific requirement):

```go
func ExampleFunction() error {
    if condition {
        result := doSomething()
        if result != nil {
            return result
        }
    }
    return nil
}
```

**Note:** This differs from standard Go convention (tabs), but is a project
requirement. Configure your editor:

**VS Code (.editorconfig):**
```ini
[*.go]
indent_style = space
indent_size = 4
```

### Line Length

Wrap lines at 80 characters where practical, but prioritize readability over
strict adherence.

### Formatting Tools

Use `gofmt` (with custom settings if needed) and `goimports`:

```bash
# Format code
gofmt -w -s .

# Organize imports
goimports -w .
```

## Naming Conventions

### Packages

- **Lowercase, single word:** `mcp`, `database`, `privileges`
- **No underscores or mixedCaps:** Avoid `user_mgmt`, prefer `usermgmt`
- **Descriptive:** Package name should describe its purpose

```go
package mcp        // Good
package userMgmt   // Bad
package user_mgmt  // Bad
```

### Files

- **Lowercase with underscores:** `connection_pool.go`
- **Test files:** `connection_pool_test.go`
- **Group related code:** Keep related types and functions in same file

```
database/
├── connection_pool.go
├── connection_pool_test.go
├── schema.go
└── schema_test.go
```

### Variables and Functions

**Variables:**
- **camelCase for private:** `connectionPool`, `userID`
- **PascalCase for exported:** `ConnectionPool`, `UserID`
- **Short names for limited scope:** `i`, `err`, `ctx`
- **Descriptive names for broader scope:** `monitoredConnectionPool`

```go
// Good
func ProcessUser(userID int) error {
    var result string
    for i := 0; i < 10; i++ {
        // i is fine for loop variable
    }
    return nil
}

// Bad
func ProcessUser(u int) error {  // u is too short
    var r string                 // r is unclear
    for userIndex := 0; userIndex < 10; userIndex++ {  // too verbose
    }
    return nil
}
```

**Functions:**
- **PascalCase for exported:** `NewHandler`, `GetConnection`
- **camelCase for private:** `buildConnectionString`, `validateToken`
- **Verbs for actions:** `Create`, `Update`, `Delete`, `Get`, `List`
- **Nouns for constructors:** `NewHandler`, `NewConfig`

```go
// Exported functions (PascalCase)
func NewHandler() *Handler
func CreateUser() error
func GetUserByID(id int) (*User, error)

// Private functions (camelCase)
func buildConnectionString() string
func validateToken(token string) error
```

### Constants

- **PascalCase for exported:** `MaxConnections`, `DefaultTimeout`
- **camelCase for private:** `maxRetries`, `defaultPort`
- **ALL_CAPS for special cases:** Rarely, only for true constants

```go
const (
    // Exported constants
    JSONRPCVersion = "2.0"
    MaxConnections = 100

    // Private constants
    defaultTimeout = 30 * time.Second
    maxRetries     = 3
)
```

### Types

- **PascalCase for exported:** `Handler`, `UserInfo`, `Config`
- **camelCase for private:** `connectionState`, `tokenCache`
- **Descriptive names:** Avoid abbreviations unless well-known

```go
// Good
type Handler struct {
    serverName string
    dbPool     *pgxpool.Pool
}

type UserInfo struct {
    UserID      int
    Username    string
    IsSuperuser bool
}

// Bad
type Hdlr struct {  // Unclear abbreviation
    sn string
    dp *pgxpool.Pool
}

type UsrInf struct {  // Unnecessary abbreviation
    UID int
    UN  string
    ISU bool
}
```

### Interfaces

- **-er suffix for single-method:** `Reader`, `Writer`, `Handler`
- **Descriptive for multi-method:** `MetricsProbe`, `ConnectionManager`

```go
// Single-method interfaces
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Closer interface {
    Close() error
}

// Multi-method interfaces
type MetricsProbe interface {
    GetName() string
    Execute(ctx context.Context, conn *pgxpool.Conn) ([]map[string]interface{}, error)
    Store(ctx context.Context, conn *pgxpool.Conn, data []map[string]interface{}) error
}
```

## File Structure

### Standard File Organization

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

// Package doc comment
package mcp

import (
    // Standard library
    "context"
    "fmt"
    "time"

    // Third-party libraries
    "github.com/jackc/pgx/v5/pgxpool"

    // Project imports
    "github.com/pgEdge/ai-workbench/server/src/config"
    "github.com/pgEdge/ai-workbench/server/src/logger"
)

// Constants
const (
    JSONRPCVersion = "2.0"
    MaxRetries     = 3
)

// Package-level variables (avoid when possible)
var (
    defaultConfig *Config
)

// Types
type Handler struct {
    serverName string
    config     *Config
}

// Constructor
func NewHandler(serverName string, cfg *Config) *Handler {
    return &Handler{
        serverName: serverName,
        config:     cfg,
    }
}

// Methods
func (h *Handler) HandleRequest(data []byte) (*Response, error) {
    // Implementation...
    return nil, nil
}

// Helper functions
func validateRequest(req *Request) error {
    // Implementation...
    return nil
}
```

### Import Grouping

Group imports in this order:

1. Standard library
2. Third-party libraries
3. Project imports

Separate groups with blank lines:

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "golang.org/x/crypto/bcrypt"

    "github.com/pgEdge/ai-workbench/server/src/config"
    "github.com/pgEdge/ai-workbench/server/src/logger"
)
```

## Error Handling

### Error Creation

**Use `fmt.Errorf` with `%w` to wrap errors:**

```go
func GetUser(id int) (*User, error) {
    user, err := db.QueryUser(id)
    if err != nil {
        return nil, fmt.Errorf("failed to query user %d: %w", id, err)
    }
    return user, nil
}
```

**Custom errors for sentinel values:**

```go
var (
    ErrUserNotFound = fmt.Errorf("user not found")
    ErrInvalidToken = fmt.Errorf("invalid token")
)

func Authenticate(token string) error {
    if !isValid(token) {
        return ErrInvalidToken
    }
    return nil
}
```

### Error Checking

**Check errors immediately:**

```go
// Good
conn, err := pool.Acquire(ctx)
if err != nil {
    return fmt.Errorf("failed to acquire connection: %w", err)
}
defer conn.Release()

// Bad
conn, _ := pool.Acquire(ctx)  // Ignoring error
defer conn.Release()
```

**Don't panic in library code:**

```go
// Good
func Divide(a, b int) (int, error) {
    if b == 0 {
        return 0, fmt.Errorf("division by zero")
    }
    return a / b, nil
}

// Bad
func Divide(a, b int) int {
    if b == 0 {
        panic("division by zero")  // Don't panic in libraries
    }
    return a / b
}
```

### Defer for Cleanup

**Use defer for cleanup, even in error paths:**

```go
func ProcessFile(filename string) error {
    file, err := os.Open(filename)
    if err != nil {
        return fmt.Errorf("failed to open file: %w", err)
    }
    defer file.Close()

    // Process file...
    // If error occurs, file still gets closed
    return nil
}
```

**Check errors in deferred functions:**

```go
func SaveData(data []byte) (err error) {
    file, err := os.Create("data.txt")
    if err != nil {
        return fmt.Errorf("failed to create file: %w", err)
    }
    defer func() {
        if cerr := file.Close(); cerr != nil && err == nil {
            err = fmt.Errorf("failed to close file: %w", cerr)
        }
    }()

    _, err = file.Write(data)
    if err != nil {
        return fmt.Errorf("failed to write data: %w", err)
    }

    return nil
}
```

## Context Usage

### Context as First Parameter

**Always pass context as first parameter:**

```go
// Good
func QueryUser(ctx context.Context, pool *pgxpool.Pool, id int) (*User, error) {
    var user User
    err := pool.QueryRow(ctx, "SELECT * FROM users WHERE id = $1", id).Scan(&user)
    return &user, err
}

// Bad
func QueryUser(pool *pgxpool.Pool, id int, ctx context.Context) (*User, error) {
    // ctx should be first parameter
}
```

### Context Propagation

**Propagate context through call chain:**

```go
func HandleRequest(ctx context.Context, req *Request) (*Response, error) {
    // Pass context to next function
    user, err := authenticateUser(ctx, req.Token)
    if err != nil {
        return nil, err
    }

    // Pass context to database operation
    result, err := queryDatabase(ctx, user.ID)
    if err != nil {
        return nil, err
    }

    return &Response{Result: result}, nil
}
```

### Context Timeout

**Use context for timeouts:**

```go
func QueryWithTimeout(query string) error {
    // Create context with 5-second timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    _, err := pool.Exec(ctx, query)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("query timed out")
        }
        return fmt.Errorf("query failed: %w", err)
    }

    return nil
}
```

## Concurrency

### Goroutine Management

**Track goroutines with WaitGroup:**

```go
type Worker struct {
    wg sync.WaitGroup
}

func (w *Worker) Start() {
    w.wg.Add(1)
    go w.work()
}

func (w *Worker) work() {
    defer w.wg.Done()

    // Do work...
}

func (w *Worker) Stop() {
    // Signal workers to stop...
    w.wg.Wait()  // Wait for all goroutines
}
```

### Channel Patterns

**Buffered channels for known capacity:**

```go
// Semaphore pattern
sem := make(chan struct{}, maxConcurrent)

for i := 0; i < total; i++ {
    sem <- struct{}{}  // Acquire
    go func(idx int) {
        defer func() { <-sem }()  // Release
        processItem(idx)
    }(i)
}

// Wait for all to complete
for i := 0; i < maxConcurrent; i++ {
    sem <- struct{}{}
}
```

**Select for multiplexing:**

```go
func worker(ctx context.Context, jobs <-chan Job, results chan<- Result) {
    for {
        select {
        case <-ctx.Done():
            return
        case job := <-jobs:
            result := processJob(job)
            results <- result
        }
    }
}
```

### Mutex Usage

**Minimize critical sections:**

```go
type Cache struct {
    mu    sync.RWMutex
    items map[string]interface{}
}

func (c *Cache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    item, ok := c.items[key]
    return item, ok
}

func (c *Cache) Set(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = value
}
```

## Comments

### Package Comments

**Every package should have a package comment:**

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

// Package mcp implements the Model Context Protocol for the MCP server.
//
// The MCP protocol provides a JSON-RPC 2.0 interface for AI models to
// interact with PostgreSQL databases through tools, resources, and prompts.
package mcp
```

### Exported Identifiers

**Document all exported types, functions, and constants:**

```go
// Handler processes MCP requests and manages protocol state.
type Handler struct {
    serverName    string
    serverVersion string
}

// NewHandler creates a new MCP handler with the given server information.
func NewHandler(serverName, serverVersion string) *Handler {
    return &Handler{
        serverName:    serverName,
        serverVersion: serverVersion,
    }
}

// HandleRequest processes an MCP JSON-RPC 2.0 request and returns a response.
// It validates the request format, authenticates the user, and routes to the
// appropriate handler method.
func (h *Handler) HandleRequest(data []byte, bearerToken string) (*Response, error) {
    // Implementation...
    return nil, nil
}
```

### Implementation Comments

**Comment complex logic, not obvious code:**

```go
// Good - explains why, not what
func (m *Manager) acquireSlot(ctx context.Context, id int) error {
    sem := m.getSemaphore(id)
    // Use select to support context cancellation during semaphore acquisition.
    // This prevents goroutines from blocking indefinitely when the context
    // is cancelled during shutdown.
    select {
    case sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

// Bad - restates what code does
func Add(a, b int) int {
    // Add a and b together
    result := a + b
    // Return the result
    return result
}
```

### TODO Comments

**Format TODO comments consistently:**

```go
// TODO(username): Description of what needs to be done
// TODO(alice): Implement connection pooling for monitored databases
// TODO(bob): Add retry logic for transient database errors
```

## Security

### SQL Injection Prevention

**Always use parameterized queries:**

```go
// Good
func GetUser(ctx context.Context, pool *pgxpool.Pool, username string) (*User, error) {
    var user User
    err := pool.QueryRow(ctx,
        "SELECT id, username FROM users WHERE username = $1",
        username).Scan(&user.ID, &user.Username)
    return &user, err
}

// Bad
func GetUser(ctx context.Context, pool *pgxpool.Pool, username string) (*User, error) {
    query := fmt.Sprintf("SELECT id, username FROM users WHERE username = '%s'", username)
    // SQL injection vulnerability!
    var user User
    err := pool.QueryRow(ctx, query).Scan(&user.ID, &user.Username)
    return &user, err
}
```

**Exception for dynamic table names (use #nosec with justification):**

```go
// Partition name is validated against known probe names, not user input
// #nosec G201 - table name from probe definition, not user input
query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
    partitionName, tableName, startDate, endDate)
```

### Input Validation

**Validate all user input:**

```go
func CreateUser(username, email string) error {
    // Validate username
    if len(username) == 0 || len(username) > 50 {
        return fmt.Errorf("username must be 1-50 characters")
    }

    // Validate email format
    if !isValidEmail(email) {
        return fmt.Errorf("invalid email format")
    }

    // Sanitize input (if needed)
    username = strings.TrimSpace(username)

    // Proceed with creation...
    return nil
}
```

### Credentials Handling

**Never log credentials:**

```go
// Good
logger.Infof("Connecting to database: host=%s, port=%d, database=%s",
    host, port, database)

// Bad
logger.Infof("Connection string: %s", connString)  // May contain password!
```

**Use secure random generation:**

```go
func GenerateToken() (string, error) {
    bytes := make([]byte, 32)
    // Use crypto/rand, not math/rand
    if _, err := rand.Read(bytes); err != nil {
        return "", fmt.Errorf("failed to generate token: %w", err)
    }
    return base64.URLEncoding.EncodeToString(bytes), nil
}
```

## Performance

### Avoid Allocations in Hot Paths

**Reuse buffers:**

```go
type Worker struct {
    buffer bytes.Buffer
}

func (w *Worker) ProcessData(data []byte) string {
    w.buffer.Reset()
    w.buffer.Write(data)
    // Process buffer...
    return w.buffer.String()
}
```

**Use sync.Pool for expensive objects:**

```go
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func ProcessData(data []byte) string {
    buf := bufferPool.Get().(*bytes.Buffer)
    defer bufferPool.Put(buf)

    buf.Reset()
    buf.Write(data)
    return buf.String()
}
```

### Prefer Slices Over Maps

**When possible, use slices:**

```go
// Better performance for small collections
type Config struct {
    values []struct {
        key   string
        value string
    }
}

// Good for larger collections, O(1) lookup
type FastConfig struct {
    values map[string]string
}
```

### Batch Database Operations

**Batch INSERTs:**

```go
// Good - batch insert
func InsertUsers(users []User) error {
    const batchSize = 100
    for i := 0; i < len(users); i += batchSize {
        end := i + batchSize
        if end > len(users) {
            end = len(users)
        }
        if err := insertBatch(users[i:end]); err != nil {
            return err
        }
    }
    return nil
}

// Bad - individual inserts
func InsertUsers(users []User) error {
    for _, user := range users {
        if err := insertUser(user); err != nil {
            return err
        }
    }
    return nil
}
```

## Testing Conventions

See `testing-strategy.md` for comprehensive testing patterns.

**Quick Reference:**

```go
// Test function naming
func TestFunctionName(t *testing.T)
func TestFunctionName_EdgeCase(t *testing.T)
func TestFunctionName_ErrorHandling(t *testing.T)

// Use t.Helper() in test utilities
func createTestUser(t *testing.T, name string) *User {
    t.Helper()
    // Implementation...
}

// Use subtests for organization
func TestUserOperations(t *testing.T) {
    t.Run("Create", func(t *testing.T) { /* ... */ })
    t.Run("Update", func(t *testing.T) { /* ... */ })
    t.Run("Delete", func(t *testing.T) { /* ... */ })
}

// Table-driven tests
func TestValidation(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid input", "test", false},
        {"empty input", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := Validate(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("got error %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## Common Patterns

### Constructor Pattern

```go
// NewHandler creates and initializes a Handler
func NewHandler(name string, pool *pgxpool.Pool) *Handler {
    return &Handler{
        name:   name,
        pool:   pool,
        cache:  make(map[string]interface{}),
    }
}
```

### Functional Options Pattern

```go
type ServerOption func(*Server)

func WithTimeout(d time.Duration) ServerOption {
    return func(s *Server) {
        s.timeout = d
    }
}

func WithMaxConns(n int) ServerOption {
    return func(s *Server) {
        s.maxConns = n
    }
}

func NewServer(addr string, opts ...ServerOption) *Server {
    s := &Server{
        addr:     addr,
        timeout:  30 * time.Second,  // default
        maxConns: 10,                // default
    }

    for _, opt := range opts {
        opt(s)
    }

    return s
}

// Usage
server := NewServer(":8080",
    WithTimeout(60*time.Second),
    WithMaxConns(100))
```

### Interface Segregation

```go
// Small, focused interfaces
type Reader interface {
    Read(ctx context.Context) ([]byte, error)
}

type Writer interface {
    Write(ctx context.Context, data []byte) error
}

type Closer interface {
    Close() error
}

// Compose interfaces
type ReadWriteCloser interface {
    Reader
    Writer
    Closer
}
```

## Common Mistakes to Avoid

### 1. Ignoring Error Returns

```go
// Bad
conn, _ := pool.Acquire(ctx)

// Good
conn, err := pool.Acquire(ctx)
if err != nil {
    return fmt.Errorf("failed to acquire connection: %w", err)
}
```

### 2. Not Releasing Resources

```go
// Bad
func GetData() ([]byte, error) {
    conn, err := pool.Acquire(ctx)
    if err != nil {
        return nil, err
    }
    // Connection leaked if query fails
    return conn.Query(...)
}

// Good
func GetData() ([]byte, error) {
    conn, err := pool.Acquire(ctx)
    if err != nil {
        return nil, err
    }
    defer conn.Release()
    return conn.Query(...)
}
```

### 3. Mutating Shared State Without Locking

```go
// Bad
type Cache struct {
    items map[string]string
}

func (c *Cache) Set(key, value string) {
    c.items[key] = value  // Race condition!
}

// Good
type Cache struct {
    mu    sync.RWMutex
    items map[string]string
}

func (c *Cache) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = value
}
```

### 4. Copying Mutexes

```go
// Bad
func (h Handler) Process() {  // Receiver by value - copies mutex!
    h.mu.Lock()
    defer h.mu.Unlock()
}

// Good
func (h *Handler) Process() {  // Receiver by pointer
    h.mu.Lock()
    defer h.mu.Unlock()
}
```

### 5. Ignoring Context Cancellation

```go
// Bad
func ProcessItems(ctx context.Context, items []Item) {
    for _, item := range items {
        processItem(item)  // Doesn't check context
    }
}

// Good
func ProcessItems(ctx context.Context, items []Item) error {
    for _, item := range items {
        if ctx.Err() != nil {
            return ctx.Err()
        }
        processItem(item)
    }
    return nil
}
```

## Code Review Checklist

- [ ] Code follows four-space indentation
- [ ] All errors are checked and handled
- [ ] Resources are properly released (defer, Close, etc.)
- [ ] Context is propagated through function calls
- [ ] No SQL injection vulnerabilities
- [ ] No credentials in logs or error messages
- [ ] Exported identifiers have comments
- [ ] Tests are included for new functionality
- [ ] No race conditions (run with -race)
- [ ] Code passes golangci-lint
- [ ] No unnecessary allocations in hot paths
- [ ] Goroutines are properly managed (WaitGroup, channels)
- [ ] Transaction errors are checked and rolled back
- [ ] No panic() in library code
- [ ] TODO comments include assignee
