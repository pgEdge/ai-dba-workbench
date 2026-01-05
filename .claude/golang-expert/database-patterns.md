/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Database Connection and Access Patterns
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Database Connection and Access Patterns

This document describes the database connection pooling, access patterns, and
best practices used in the pgEdge AI DBA Workbench Go backend.

## Connection Pooling with pgx/v5

Both the server and collector use `github.com/jackc/pgx/v5/pgxpool` for
PostgreSQL connection pooling. This is the recommended driver for Go PostgreSQL
applications.

### Why pgx over database/sql?

1. **Better Performance:** Native PostgreSQL protocol, no CGO overhead
2. **Rich Feature Set:** Binary protocol, COPY, LISTEN/NOTIFY, batch queries
3. **Context Support:** First-class context.Context integration
4. **Type Safety:** Strong typing for PostgreSQL types
5. **Connection Pooling:** Built-in production-ready connection pool

### Basic Pool Creation

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

func Connect(cfg *config.Config) (*pgxpool.Pool, error) {
    // Build connection string
    connStr := fmt.Sprintf(
        "host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
        cfg.GetPgHost(),
        cfg.GetPgPort(),
        cfg.GetPgDatabase(),
        cfg.GetPgUsername(),
        cfg.GetPgPassword(),
        cfg.GetPgSSLMode(),
    )

    // Parse configuration
    poolConfig, err := pgxpool.ParseConfig(connStr)
    if err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    // Configure pool settings
    poolConfig.MaxConns = 25
    poolConfig.MaxConnIdleTime = 5 * time.Minute
    poolConfig.HealthCheckPeriod = 1 * time.Minute

    // Create pool with context timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to create pool: %w", err)
    }

    // Test connectivity
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    return pool, nil
}
```

### Pool Configuration Options

```go
type PoolConfig struct {
    // Maximum number of connections in the pool
    MaxConns int32  // Default: 4

    // Minimum number of idle connections
    MinConns int32  // Default: 0

    // Maximum idle time before connection is closed
    MaxConnIdleTime time.Duration  // Default: 30 minutes

    // Maximum lifetime of a connection
    MaxConnLifetime time.Duration  // Default: 1 hour

    // How often to check connection health
    HealthCheckPeriod time.Duration  // Default: 1 minute

    // Connect timeout
    ConnectTimeout time.Duration  // Default: from connection string
}
```

**Recommended Settings:**

**Server (Datastore Pool):**
- MaxConns: 25 (handles concurrent MCP requests)
- MaxConnIdleTime: 5 minutes (datastore is always available)
- HealthCheckPeriod: 1 minute

**Collector (Datastore Pool):**
- MaxConns: 25 (handles concurrent probe storage)
- MaxConnIdleTime: 5 minutes
- HealthCheckPeriod: 1 minute

**Collector (Monitored Pools):**
- MaxConns: 10 (per monitored server, limited by semaphore)
- MaxConnIdleTime: 5 minutes (release unused connections)
- HealthCheckPeriod: 1 minute

## Server Connection Pattern

The server uses a single connection pool to the datastore database:

```go
type Server struct {
    dbPool *pgxpool.Pool
}

func (s *Server) HandleMCPRequest(w http.ResponseWriter, r *http.Request) {
    // Get connection from pool
    conn, err := s.dbPool.Acquire(r.Context())
    if err != nil {
        http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
        return
    }
    defer conn.Release()

    // Use connection for query
    var result string
    err = conn.QueryRow(r.Context(),
        "SELECT version()").Scan(&result)
    if err != nil {
        http.Error(w, "Query failed", http.StatusInternalServerError)
        return
    }

    fmt.Fprintf(w, "Result: %s", result)
}
```

**Key Points:**
- Acquire connection with request context (automatic cancellation)
- Always release connection with `defer`
- Context propagates cancellation to database operations
- Pool handles connection reuse automatically

## Collector Connection Pattern

The collector uses two types of connection pools:

### 1. Datastore Pool

Single pool for storing metrics:

```go
type Datastore struct {
    pool   *pgxpool.Pool
    config Config
}

func (ds *Datastore) GetConnection() (*pgxpool.Conn, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    return ds.pool.Acquire(ctx)
}

func (ds *Datastore) GetConnectionWithContext(
    ctx context.Context,
) (*pgxpool.Conn, error) {
    return ds.pool.Acquire(ctx)
}

func (ds *Datastore) ReturnConnection(conn *pgxpool.Conn) {
    if conn != nil {
        conn.Release()
    }
}
```

**Usage Pattern:**
```go
// Quick operations (5 second timeout)
conn, err := ds.GetConnection()
if err != nil {
    return err
}
defer ds.ReturnConnection(conn)

// Or with custom timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
conn, err := ds.GetConnectionWithContext(ctx)
if err != nil {
    return err
}
defer ds.ReturnConnection(conn)
```

### 2. Monitored Connection Pools

Separate pools per monitored database with semaphore-based concurrency control:

```go
type MonitoredConnectionPoolManager struct {
    pools          map[int]*pgxpool.Pool           // connectionID -> pool
    semaphores     map[int]chan struct{}           // connectionID -> semaphore
    maxConnections int                             // Max concurrent per server
    maxIdleSeconds int
    mu             sync.RWMutex
}

func (m *MonitoredConnectionPoolManager) GetConnection(
    ctx context.Context,
    conn MonitoredConnection,
    serverSecret string,
) (*pgxpool.Conn, error) {
    // 1. Acquire semaphore slot (blocks if limit reached)
    if err := m.acquireSlot(ctx, conn.ID); err != nil {
        return nil, fmt.Errorf("failed to acquire slot: %w", err)
    }

    // 2. Get or create pool
    pool, err := m.getOrCreatePool(conn, serverSecret)
    if err != nil {
        m.releaseSlot(conn.ID)
        return nil, err
    }

    // 3. Acquire connection from pool
    pgxConn, err := pool.Acquire(ctx)
    if err != nil {
        m.releaseSlot(conn.ID)
        return nil, err
    }

    return pgxConn, nil
}

func (m *MonitoredConnectionPoolManager) ReturnConnection(
    connectionID int,
    conn *pgxpool.Conn,
) {
    if conn != nil {
        conn.Release()
    }
    m.releaseSlot(connectionID)
}
```

**Semaphore Pattern:**
```go
// Semaphore implemented as buffered channel
func (m *MonitoredConnectionPoolManager) getSemaphore(
    connectionID int,
) chan struct{} {
    m.mu.Lock()
    defer m.mu.Unlock()

    sem, exists := m.semaphores[connectionID]
    if !exists {
        sem = make(chan struct{}, m.maxConnections)
        m.semaphores[connectionID] = sem
    }
    return sem
}

func (m *MonitoredConnectionPoolManager) acquireSlot(
    ctx context.Context,
    connectionID int,
) error {
    sem := m.getSemaphore(connectionID)
    select {
    case sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (m *MonitoredConnectionPoolManager) releaseSlot(connectionID int) {
    m.mu.RLock()
    sem, exists := m.semaphores[connectionID]
    m.mu.RUnlock()

    if exists {
        <-sem
    }
}
```

**Why Semaphores?**

The semaphore pattern limits total concurrent connections to each monitored
server, even though the pool itself might allow more. This prevents
overwhelming monitored databases during high-frequency probe execution.

**Example:**
- Pool configured with MaxConns: 10
- Semaphore configured with maxConnections: 5
- Result: Maximum 5 concurrent connections to this server

## Connection String Building

### Server Pattern

```go
func GetConnectionString(cfg *config.Config) string {
    connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s",
        cfg.GetPgHost(), cfg.GetPgPort(),
        cfg.GetPgDatabase(), cfg.GetPgUsername())

    if cfg.GetPgPassword() != "" {
        connStr += fmt.Sprintf(" password=%s", cfg.GetPgPassword())
    }

    if cfg.GetPgHostAddr() != "" {
        connStr += fmt.Sprintf(" hostaddr=%s", cfg.GetPgHostAddr())
    }

    if cfg.GetPgSSLMode() != "" {
        connStr += fmt.Sprintf(" sslmode=%s", cfg.GetPgSSLMode())
    }

    if cfg.GetPgSSLCert() != "" {
        connStr += fmt.Sprintf(" sslcert=%s", cfg.GetPgSSLCert())
    }

    if cfg.GetPgSSLKey() != "" {
        connStr += fmt.Sprintf(" sslkey=%s", cfg.GetPgSSLKey())
    }

    if cfg.GetPgSSLRootCert() != "" {
        connStr += fmt.Sprintf(" sslrootcert=%s", cfg.GetPgSSLRootCert())
    }

    connStr += " application_name='pgEdge AI DBA Workbench - MCP Server'"

    return connStr
}
```

### Collector Pattern (with Encryption)

```go
func buildMonitoredConnectionString(
    conn MonitoredConnection,
    serverSecret string,
) (string, error) {
    // Decrypt password
    password, err := DecryptPassword(conn.PasswordEncrypted, serverSecret)
    if err != nil {
        return "", fmt.Errorf("failed to decrypt password: %w", err)
    }

    // Build connection string
    params := make(map[string]string)
    params["dbname"] = conn.DatabaseName
    params["user"] = conn.Username
    params["password"] = password

    if conn.HostAddr != nil && *conn.HostAddr != "" {
        params["hostaddr"] = *conn.HostAddr
    } else if conn.Host != nil && *conn.Host != "" {
        params["host"] = *conn.Host
    }

    if conn.Port != nil {
        params["port"] = fmt.Sprintf("%d", *conn.Port)
    }

    if conn.SSLMode != nil {
        params["sslmode"] = *conn.SSLMode
    }

    if conn.SSLCert != nil {
        params["sslcert"] = *conn.SSLCert
    }

    if conn.SSLKey != nil {
        params["sslkey"] = *conn.SSLKey
    }

    if conn.SSLRootCert != nil {
        params["sslrootcert"] = *conn.SSLRootCert
    }

    params["application_name"] = fmt.Sprintf(
        "pgEdge AI DBA Workbench - Collector [%s]", conn.Name)

    return buildPostgresConnectionString(params), nil
}

func buildPostgresConnectionString(params map[string]string) string {
    var parts []string
    for key, value := range params {
        parts = append(parts, fmt.Sprintf("%s=%s", key, value))
    }
    return strings.Join(parts, " ")
}
```

## Query Execution Patterns

### Simple Query

```go
func GetUserByID(
    ctx context.Context,
    pool *pgxpool.Pool,
    userID int,
) (*User, error) {
    var user User
    err := pool.QueryRow(ctx, `
        SELECT id, username, email, is_superuser
        FROM user_accounts
        WHERE id = $1
    `, userID).Scan(&user.ID, &user.Username, &user.Email, &user.IsSuperuser)

    if err != nil {
        return nil, fmt.Errorf("failed to query user: %w", err)
    }

    return &user, nil
}
```

### Multiple Rows

```go
func GetAllUsers(
    ctx context.Context,
    pool *pgxpool.Pool,
) ([]User, error) {
    rows, err := pool.Query(ctx, `
        SELECT id, username, email, is_superuser
        FROM user_accounts
        ORDER BY username
    `)
    if err != nil {
        return nil, fmt.Errorf("failed to query users: %w", err)
    }
    defer rows.Close()

    users := make([]User, 0)
    for rows.Next() {
        var user User
        if err := rows.Scan(&user.ID, &user.Username,
            &user.Email, &user.IsSuperuser); err != nil {
            return nil, fmt.Errorf("failed to scan user: %w", err)
        }
        users = append(users, user)
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("error iterating users: %w", err)
    }

    return users, nil
}
```

### Transaction Pattern

```go
func CreateUserAndGroup(
    ctx context.Context,
    pool *pgxpool.Pool,
    username string,
    groupName string,
) error {
    // Begin transaction
    tx, err := pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer func() {
        if err != nil {
            if rerr := tx.Rollback(ctx); rerr != nil {
                logger.Errorf("Error rolling back transaction: %v", rerr)
            }
        }
    }()

    // Create user
    var userID int
    err = tx.QueryRow(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ($1, $2, $3, $4)
        RETURNING id
    `, username, "email@example.com", "Full Name", "hash").Scan(&userID)
    if err != nil {
        return fmt.Errorf("failed to create user: %w", err)
    }

    // Create group
    var groupID int
    err = tx.QueryRow(ctx, `
        INSERT INTO groups (name, description)
        VALUES ($1, $2)
        RETURNING id
    `, groupName, "Description").Scan(&groupID)
    if err != nil {
        return fmt.Errorf("failed to create group: %w", err)
    }

    // Add user to group
    _, err = tx.Exec(ctx, `
        INSERT INTO group_memberships (parent_group_id, member_user_id)
        VALUES ($1, $2)
    `, groupID, userID)
    if err != nil {
        return fmt.Errorf("failed to add user to group: %w", err)
    }

    // Commit transaction
    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("failed to commit transaction: %w", err)
    }

    return nil
}
```

**Transaction Best Practices:**
1. Always use `defer` to handle rollback
2. Set `err` variable in outer scope for defer to check
3. Check rollback errors but don't return them (transaction already failed)
4. Use explicit `Commit()` to finalize transaction
5. Keep transactions short to minimize lock contention

### Batch Insert Pattern

```go
func StoreProbeBatch(
    ctx context.Context,
    conn *pgxpool.Conn,
    tableName string,
    columns []string,
    values [][]interface{},
) error {
    if len(values) == 0 {
        return nil
    }

    const batchSize = 100

    tx, err := conn.Begin(ctx)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer func() {
        if err != nil {
            if rerr := tx.Rollback(ctx); rerr != nil {
                logger.Errorf("Rollback error: %v", rerr)
            }
        }
    }()

    for i := 0; i < len(values); i += batchSize {
        end := i + batchSize
        if end > len(values) {
            end = len(values)
        }
        batch := values[i:end]

        // Build multi-value INSERT
        query := buildBatchInsertQuery(tableName, columns, len(batch))
        args := flattenBatch(batch)

        if _, err := tx.Exec(ctx, query, args...); err != nil {
            return fmt.Errorf("failed to insert batch: %w", err)
        }
    }

    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("failed to commit: %w", err)
    }

    return nil
}

func buildBatchInsertQuery(
    tableName string,
    columns []string,
    rowCount int,
) string {
    columnList := strings.Join(columns, ", ")

    var valueClauses []string
    placeholderNum := 1
    for i := 0; i < rowCount; i++ {
        var placeholders []string
        for range columns {
            placeholders = append(placeholders,
                fmt.Sprintf("$%d", placeholderNum))
            placeholderNum++
        }
        valueClauses = append(valueClauses,
            fmt.Sprintf("(%s)", strings.Join(placeholders, ", ")))
    }

    return fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
        tableName, columnList, strings.Join(valueClauses, ", "))
}

func flattenBatch(batch [][]interface{}) []interface{} {
    args := make([]interface{}, 0, len(batch)*len(batch[0]))
    for _, row := range batch {
        args = append(args, row...)
    }
    return args
}
```

## Connection Lifecycle Management

### Graceful Shutdown

```go
func (s *Server) Shutdown(ctx context.Context) error {
    // Close the pool
    if s.dbPool != nil {
        s.dbPool.Close()
    }

    return nil
}
```

### Pool Health Monitoring

```go
func MonitorPoolStats(pool *pgxpool.Pool) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        stat := pool.Stat()
        logger.Infof("Pool stats: Total=%d Idle=%d Acquired=%d",
            stat.TotalConns(),
            stat.IdleConns(),
            stat.AcquiredConns())
    }
}
```

### Connection Timeout Handling

```go
func QueryWithTimeout(
    pool *pgxpool.Pool,
    query string,
    args ...interface{},
) error {
    // Create context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    _, err := pool.Exec(ctx, query, args...)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("query timed out after 5 seconds")
        }
        return fmt.Errorf("query failed: %w", err)
    }

    return nil
}
```

## Password Encryption for Monitored Connections

The collector encrypts monitored connection passwords using AES-256-GCM:

```go
func EncryptPassword(password string, secret string) (string, error) {
    // Create cipher from secret
    key := sha256.Sum256([]byte(secret))
    block, err := aes.NewCipher(key[:])
    if err != nil {
        return "", err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }

    // Generate nonce
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }

    // Encrypt
    ciphertext := gcm.Seal(nonce, nonce, []byte(password), nil)

    // Encode as base64
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func DecryptPassword(encrypted string, secret string) (string, error) {
    // Decode base64
    ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
    if err != nil {
        return "", err
    }

    // Create cipher from secret
    key := sha256.Sum256([]byte(secret))
    block, err := aes.NewCipher(key[:])
    if err != nil {
        return "", err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }

    // Extract nonce
    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return "", fmt.Errorf("ciphertext too short")
    }

    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

    // Decrypt
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", err
    }

    return string(plaintext), nil
}
```

## Best Practices

1. **Always Use Contexts:** Pass context for cancellation and timeouts
2. **Release Connections:** Use `defer` to ensure connections are released
3. **Handle Errors Explicitly:** Check and wrap errors with context
4. **Use Transactions:** For operations modifying multiple tables
5. **Set Appropriate Timeouts:** Balance responsiveness vs. query complexity
6. **Monitor Pool Stats:** Track pool utilization in production
7. **Use Prepared Statements:** For frequently executed queries (via pgx)
8. **Batch Operations:** Group INSERT/UPDATE operations for efficiency
9. **Close Rows:** Always call `rows.Close()` (or use `defer`)
10. **Check rows.Err():** After iteration to catch iteration errors

## Performance Optimization

### Connection Pool Sizing

```
Optimal pool size = (core_count * 2) + effective_spindle_count
```

For CPU-bound workloads (web servers):
- MaxConns: 25-50 connections
- MinConns: 5-10 connections

For I/O-bound workloads (batch processors):
- MaxConns: 100+ connections
- MinConns: 10-20 connections

### Query Optimization

1. **Use Indexes:** Ensure queries use appropriate indexes
2. **Limit Result Sets:** Use LIMIT for pagination
3. **Avoid SELECT \*:** Select only needed columns
4. **Use Prepared Statements:** Reduce parsing overhead
5. **Batch Operations:** Reduce round trips

### Connection Reuse

1. **Keep Connections Alive:** Set appropriate MaxConnIdleTime
2. **Health Checks:** Detect and remove stale connections
3. **Application Name:** Identify connection sources
4. **Statement Timeout:** Prevent runaway queries

## Troubleshooting

### Connection Pool Exhaustion

**Symptoms:**
- Timeouts acquiring connections
- Slow API responses

**Solutions:**
- Increase MaxConns
- Check for connection leaks (missing Release calls)
- Add monitoring for pool stats

### Connection Timeouts

**Symptoms:**
- context deadline exceeded errors

**Solutions:**
- Increase timeout duration
- Optimize slow queries
- Add indexes

### Connection Leaks

**Symptoms:**
- Pool stats show high AcquiredConns that never decrease

**Solutions:**
- Audit code for missing `defer conn.Release()`
- Add connection tracking in development
- Review error handling paths
