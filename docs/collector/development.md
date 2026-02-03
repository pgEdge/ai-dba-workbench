# Development Guide

This guide covers setting up a development environment and contributing to the
Collector.

## Prerequisites

- Go 1.23 or later
- PostgreSQL 12 or later (for testing)
- Git
- Make (optional, for build automation)
- golangci-lint (for linting)

## Setting Up

### 1. Clone the Repository

```bash
git clone https://github.com/pgedge/ai-workbench.git
cd ai-workbench/collector
```

### 2. Install Dependencies

```bash
cd src
go mod download
```

### 3. Install Development Tools

Install golangci-lint for linting:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

Ensure `$(go env GOPATH)/bin` is in your PATH:

```bash
# Add to your ~/.bashrc, ~/.zshrc, or ~/.zprofile
export PATH="$PATH:$(go env GOPATH)/bin"
```

### 4. Set Up Test Database

Create a test database:

```sql
CREATE DATABASE ai_workbench_dev;
CREATE USER collector_dev WITH PASSWORD 'dev-password';
GRANT ALL PRIVILEGES ON DATABASE ai_workbench_dev TO collector_dev;
```

### 5. Create Development Config

Copy and edit the example config:

```bash
cp ../examples/ai-dba-collector.yaml ai-dba-collector-dev.yaml
```

Edit with your settings:

```yaml
datastore:
  host: localhost
  database: ai_workbench_dev
  username: collector_dev
  password_file: dev-password.txt
  sslmode: disable

secret_file: ./ai-dba-collector.secret
```

Create a development secret file:

```bash
openssl rand -base64 32 > ./ai-dba-collector.secret
chmod 600 ./ai-dba-collector.secret
```

## Project Structure

```
collector/
├── src/                    # Source code
│   ├── main.go            # Entry point
│   ├── config.go          # Configuration
│   ├── constants.go       # Constants
│   ├── garbage_collector.go
│   ├── database/          # Database package
│   │   ├── datastore.go
│   │   ├── datastore_pool.go
│   │   ├── monitored_pool.go
│   │   ├── schema.go
│   │   ├── crypto.go
│   │   └── ...
│   ├── probes/            # Probes package
│   │   ├── base.go
│   │   ├── constants.go
│   │   ├── pg_stat_*.go  # Probe implementations
│   │   └── ...
│   ├── scheduler/         # Scheduler package
│   │   └── scheduler.go
│   └── utils/             # Utility package
├── docs/                  # Documentation
├── Makefile              # Build automation
└── README.md             # Quick start guide
```

## Building

### Using Make

```bash
# Build the collector
make build

# Build and format code
make fmt build

# Run linter
make lint

# Run tests
make test

# Run tests with coverage
make coverage

# Run tests, coverage, and linting
make test-all

# Run everything (fmt, vet, test, lint - recommended before committing)
make check

# Kill any running collector processes
make killall
```

### Using Go Directly

```bash
cd src
go build -o collector
```

## Running

### Development Mode

```bash
./ai-dba-collector -config ai-dba-collector-dev.yaml
```

### With Verbose Logging

The collector uses Go's standard log package. To see all output:

```bash
./ai-dba-collector -config ai-dba-collector-dev.yaml 2>&1 | tee collector.log
```

## Testing

See the dedicated [Testing Guide](testing.md) for comprehensive testing
information.

### Quick Test Commands

```bash
# Run all tests
make test

# Run specific package tests
cd src
go test ./database/...

# Run with verbose output
go test -v ./...

# Run specific test
go test -v -run TestDatastoreConnection ./database/

# Run with coverage
make coverage
```

## Code Style

### Formatting

Always format code with `go fmt`:

```bash
make fmt
# or
go fmt ./...
```

### Indentation

Use 4 spaces, not tabs (configured in .editorconfig).

### Naming Conventions

- Packages use lowercase, single word names (database, probes, scheduler).
- Types use PascalCase names (ProbeScheduler, Datastore).
- Functions use PascalCase for exported, camelCase for private.
- Variables use camelCase names.
- Constants use PascalCase or SCREAMING_SNAKE_CASE names.

### Comments

Every exported type and function should have a doc comment:

```go
// Datastore represents a connection to the PostgreSQL datastore.
// It manages the connection pool and provides methods for accessing
// the database.
type Datastore struct {
    // ...
}

// GetConnection retrieves a connection from the pool with a default
// 5-second timeout.
func (ds *Datastore) GetConnection() (*pgxpool.Conn, error) {
    // ...
}
```

### Error Handling

Always check errors and provide context:

```go
// Good
conn, err := ds.GetConnection()
if err != nil {
    return fmt.Errorf("failed to get datastore connection: %w", err)
}

// Bad
conn, _ := ds.GetConnection()
```

Use `%w` verb to wrap errors for error chains.

## Adding Features

### 1. Plan the Change

- Review existing code
- Consider impact on other components
- Check if schema changes needed
- Write design notes if complex

### 2. Implement

- Create new files or modify existing ones
- Follow existing patterns
- Add appropriate error handling
- Include logging where needed

### 3. Test

- Write unit tests for new functions
- Write integration tests if needed
- Ensure existing tests still pass
- Achieve good code coverage

### 4. Document

- Add doc comments to exported items
- Update relevant documentation files
- Add examples if helpful

### 5. Submit

- Run `make check` to ensure quality
- Create a pull request
- Address review feedback

## Common Development Tasks

### Adding a Configuration Option

1. Add field to `Config` struct in `config.go`
2. Add default value in `NewConfig()`
3. Add parsing in `setConfigValue()`
4. Add getter method if needed
5. Update sample config file
6. Document in [Configuration Reference](config-reference.md)

### Modifying the Schema

1. Add new migration to `schema.go`
2. Implement `Up` function
3. Make it idempotent
4. Test on clean database
5. Test on database with existing schema
6. Update [Schema Management](schema-management.md) documentation

### Adding a Probe

See the dedicated [Adding Probes](adding-probes.md) guide.

### Modifying Connection Pooling

1. Update pool configuration in `database/datastore_pool.go` or
   `database/monitored_pool.go`
2. Consider impact on concurrency
3. Test with multiple connections
4. Update configuration documentation

## Debugging

### Using Delve Debugger

Install Delve:

```bash
go install github.com/go-delve/delve/cmd/dlv@latest
```

Debug the collector:

```bash
cd src
dlv debug -- -config ../ai-dba-collector-dev.yaml
```

Set breakpoints and run:

```
(dlv) break main.main
(dlv) continue
(dlv) next
(dlv) print config
```

### Logging

Add debug logging where needed:

```go
log.Printf("DEBUG: Connection pool size: %d", pool.Stat().TotalConns())
```

### Database Inspection

Connect to the datastore and inspect:

```sql
-- Check probe status
SELECT * FROM probes WHERE is_enabled = TRUE;

-- Check connections
SELECT * FROM connections WHERE is_monitored = TRUE;

-- Check recent metrics
SELECT connection_id, collected_at, COUNT(*)
FROM metrics.pg_stat_activity
WHERE collected_at > NOW() - INTERVAL '1 hour'
GROUP BY connection_id, collected_at
ORDER BY collected_at DESC;

-- Check connection pool stats (from PostgreSQL side)
SELECT application_name, state, COUNT(*)
FROM pg_stat_activity
WHERE application_name LIKE 'pgEdge%'
GROUP BY application_name, state;
```

## Performance Profiling

### CPU Profiling

Add import:

```go
import _ "net/http/pprof"
```

Start HTTP server:

```go
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

Collect profile:

```bash
# Run for 30 seconds
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Analyze
(pprof) top
(pprof) list functionName
(pprof) web  # requires graphviz
```

### Memory Profiling

```bash
# Heap profile
go tool pprof http://localhost:6060/debug/pprof/heap

# Allocation profile
go tool pprof http://localhost:6060/debug/pprof/allocs
```

### Goroutine Profiling

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

## Contributing

### Before Submitting

1. Run `make check` - ensures formatting, linting, and tests pass
2. Update documentation
3. Add tests for new functionality
4. Follow existing code style
5. Keep commits focused and atomic

### Commit Messages

Follow this format:

```
Short summary (50 chars or less)

More detailed explanation if needed. Wrap at 72 characters.
Explain what changed and why, not how (code shows how).

- Bullet points are okay
- Use present tense ("Add feature" not "Added feature")
- Reference issues: "Fixes #123"
```

### Pull Request Process

1. Create a feature branch
2. Make your changes
3. Push to your fork
4. Open a pull request
5. Address review comments
6. Wait for approval and merge

## Troubleshooting

### "go.mod out of sync"

```bash
go mod tidy
```

### "golangci-lint not found"

```bash
# Install or update
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### "Test database connection failed"

Check environment variables:

```bash
export TEST_AI_WORKBENCH_SERVER="postgres://user:pass@localhost/postgres"
go test ./...
```

### Build fails with "undefined: ..."

```bash
# Re-download dependencies
go mod download
go mod verify
```

## Resources

### Go Resources

- [Effective Go][effective-go] provides best practices for Go programming.
- [Go Code Review Comments][go-review] provides community-maintained guidelines.
- [pgx Documentation][pgx] covers the PostgreSQL driver library.

[effective-go]: https://golang.org/doc/effective_go.html
[go-review]: https://github.com/golang/go/wiki/CodeReviewComments
[pgx]: https://pkg.go.dev/github.com/jackc/pgx/v5

### PostgreSQL Resources

- [PostgreSQL Documentation][pg-docs] provides the official reference.
- [Statistics Views][pg-stats] describes monitoring statistics.
- [Partitioning][pg-part] explains table partitioning.

[pg-docs]: https://www.postgresql.org/docs/
[pg-stats]: https://www.postgresql.org/docs/current/monitoring-stats.html
[pg-part]: https://www.postgresql.org/docs/current/ddl-partitioning.html

### Project Resources

- [DESIGN.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/DESIGN.md) -
  Overall system design
- [CLAUDE.md](https://github.com/pgEdge/ai-dba-workbench/blob/main/CLAUDE.md) -
  Project guidelines
- [Testing Guide](testing.md) - Detailed testing information

## See Also

- [Testing Guide](testing.md) - Running and writing tests
- [Adding Probes](adding-probes.md) - Creating new probes
- [Architecture](architecture.md) - System design
