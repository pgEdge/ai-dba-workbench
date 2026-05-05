# Testing and Development

This guide covers setting up a development
environment, contributing to the Collector, and
testing practices.

## Prerequisites

Before starting development, ensure you have the
following tools installed:

- [Go 1.24](https://go.dev/doc/install) or later
  is required for building the Collector.
- [PostgreSQL 14](https://www.postgresql.org/download/)
  or later is required for testing.
- [Git](https://git-scm.com/) is required for
  version control.
- Make is optional but recommended for build
  automation.
- [golangci-lint](https://golangci-lint.run/welcome/install/)
  is required for linting.

## Setting Up

Follow these steps to set up a development
environment.

### 1. Clone the Repository

In the following example, the commands clone the
repository and navigate to the Collector directory:

```bash
git clone \
    https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench/collector
```

### 2. Install Dependencies

In the following example, the commands download Go
module dependencies:

```bash
cd src
go mod download
```

### 3. Install Development Tools

In the following example, the command installs
`golangci-lint` for linting:

```bash
go install \
    github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

Ensure `$(go env GOPATH)/bin` is in your PATH. In
the following example, the export statement adds the
Go binary directory to PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### 4. Set Up Test Database

Create a test database. In the following example,
the SQL commands create the database and user:

```sql
CREATE DATABASE ai_workbench_dev;
CREATE USER collector_dev
    WITH PASSWORD 'dev-password';
GRANT ALL PRIVILEGES ON DATABASE ai_workbench_dev
    TO collector_dev;
```

### 5. Create Development Config

Copy and edit the example configuration. In the
following example, the command copies the example
configuration file:

```bash
cp ../examples/ai-dba-collector.yaml \
    ai-dba-collector-dev.yaml
```

Edit the configuration with your settings:

```yaml
datastore:
  host: localhost
  database: ai_workbench_dev
  username: ai_workbench_dev
  password_file: dev-password.txt
  sslmode: disable

secret_file: ./ai-dba-collector.secret
```

The collector does not auto-discover a YAML file in
the working directory or next to the binary; pass the
dev config explicitly with `--config` when you run
the collector, as shown in the
[Development Mode](#development-mode) section below.
The `secret_file:` entry above is a relative path
read from the YAML, so the collector resolves it
against the working directory at startup.

Create a development secret file. In the following
example, the commands generate a random secret:

```bash
openssl rand -base64 32 \
    > ./ai-dba-collector.secret
chmod 600 ./ai-dba-collector.secret
```

## Project Structure

The Collector follows this directory structure:

```
collector/
+-- src/
|   +-- main.go
|   +-- config.go
|   +-- constants.go
|   +-- garbage_collector.go
|   +-- database/
|   |   +-- datastore.go
|   |   +-- datastore_pool.go
|   |   +-- monitored_pool.go
|   |   +-- schema.go
|   |   +-- crypto.go
|   +-- probes/
|   |   +-- base.go
|   |   +-- constants.go
|   |   +-- pg_stat_*.go
|   +-- scheduler/
|   |   +-- scheduler.go
|   +-- utils/
+-- docs/
+-- Makefile
+-- README.md
```

## Building

This section describes how to build the Collector.

### Using Make

In the following example, the `make` commands perform
various build tasks:

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

# Run everything before committing
make check
```

### Using Go Directly

In the following example, the `go build` command
builds the Collector directly:

```bash
cd src
go build -o collector
```

## Running

This section describes how to run the Collector in
development mode.

### Development Mode

In the following example, the command starts the
Collector with a development configuration:

```bash
./ai-dba-collector \
    -config ai-dba-collector-dev.yaml
```

### With Verbose Logging

In the following example, the command captures all
output to a log file:

```bash
./ai-dba-collector \
    -config ai-dba-collector-dev.yaml \
    2>&1 | tee collector.log
```

## Test Types

The Collector includes several types of tests.

### Unit Tests

Unit tests verify individual functions and methods
in isolation. Test files reside in the same package
as the code being tested.

### Integration Tests

Integration tests verify interaction between
components and with a real database. These tests
are spread across packages.

## Running Tests

This section describes how to run the test suite.

### Using Make

In the following example, the `make` commands run
tests:

```bash
# Run all tests with formatting and linting
make check

# Run just tests
make test

# Run tests with coverage
make coverage
```

### Using Go

In the following example, the `go test` commands run
tests with various options:

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

Tests automatically create a temporary database for
each test run.

1. The test framework connects to PostgreSQL using
   `TEST_AI_WORKBENCH_SERVER` or the default URL.
2. The framework creates a database with a timestamp
   name like
   `ai_workbench_test_YYYYMMDD_HHMMSS_NNNNNN`.
3. All tests run against that database.
4. The framework drops the database when tests
   complete.

### Environment Variables

The following environment variables control test
behavior.

`TEST_AI_WORKBENCH_SERVER` specifies the PostgreSQL
connection URL:

```bash
export TEST_AI_WORKBENCH_SERVER=\
    "postgres://user:pass@localhost:5432/postgres"
go test ./...
```

`TEST_AI_WORKBENCH_KEEP_DB` keeps the test database
after tests complete:

```bash
export TEST_AI_WORKBENCH_KEEP_DB=1
go test ./...
```

`SKIP_DB_TESTS` skips all database tests:

```bash
export SKIP_DB_TESTS=1
go test ./...
```

## Writing Tests

This section describes test conventions for the
Collector.

### Test File Naming

Test files use the `*_test.go` suffix and reside in
the same package as the code being tested.

### Test Function Naming

Test function names should clearly describe the
behavior being tested:

```go
func TestFunctionName(t *testing.T) { }
func TestStructMethod(t *testing.T) { }
func TestFeatureDescription(t *testing.T) { }
```

### Basic Test Structure

In the following example, the test verifies a
datastore connection:

```go
func TestDatastoreConnection(t *testing.T) {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database tests")
    }

    config := NewTestConfig()
    ds, err := NewDatastore(config)
    if err != nil {
        t.Fatalf(
            "Failed to create datastore: %v", err)
    }
    defer ds.Close()

    conn, err := ds.GetConnection()
    if err != nil {
        t.Errorf(
            "Failed to get connection: %v", err)
    }
    defer ds.ReturnConnection(conn)

    if conn == nil {
        t.Error("Connection is nil")
    }
}
```

### Table-Driven Tests

Use table-driven tests for multiple test cases. In
the following example, the test verifies password
encryption with several inputs:

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
            encrypted, err :=
                crypto.EncryptPassword(
                    tt.password, tt.secret)
            if (err != nil) != tt.wantErr {
                t.Errorf(
                    "error = %v, wantErr %v",
                    err, tt.wantErr)
                return
            }
            if !tt.wantErr && encrypted == "" {
                t.Error(
                    "Encrypted password is empty")
            }
        })
    }
}
```

### Testing with Database

In the following example, the test verifies schema
migration:

```go
func TestSchemaManager(t *testing.T) {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database tests")
    }

    conn := getTestConnection(t)
    if conn == nil {
        return
    }
    defer conn.Release()

    sm := NewSchemaManager()
    err := sm.Migrate(conn)
    if err != nil {
        t.Fatalf("Failed to migrate: %v", err)
    }

    var count int
    err = conn.QueryRow(
        context.Background(), `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'connections'
    `).Scan(&count)

    if err != nil {
        t.Fatalf(
            "Failed to query tables: %v", err)
    }

    if count != 1 {
        t.Errorf(
            "Expected 1 table, got %d", count)
    }
}
```

## Coverage

This section describes how to generate and review
coverage reports.

### Generating Coverage Reports

In the following example, the commands generate and
view coverage reports:

```bash
# Generate coverage profile
go test -coverprofile=coverage.out ./...

# View in terminal
go tool cover -func=coverage.out

# View in browser
go tool cover -html=coverage.out
```

### Coverage Goals

The project targets the following coverage goals:

- Overall coverage should exceed 80 percent.
- Core packages such as database and scheduler
  should exceed 90 percent.
- Critical functions such as encryption and storage
  should have 100 percent coverage.

## Code Style

This section describes the code style conventions
for the Collector.

### Formatting

Always format code with `gofmt`. In the following
example, the commands format all Go files:

```bash
make fmt
# or
go fmt ./...
```

### Naming Conventions

The project follows these naming conventions:

- Packages use lowercase, single-word names such as
  `database`, `probes`, and `scheduler`.
- Types use PascalCase names such as
  `ProbeScheduler` and `Datastore`.
- Functions use PascalCase for exported functions
  and camelCase for private functions.
- Variables use camelCase names.
- Constants use PascalCase or SCREAMING_SNAKE_CASE
  names.

### Comments

Every exported type and function should have a doc
comment. In the following example, the comments
describe the type and method:

```go
// Datastore represents a connection to the
// PostgreSQL datastore. It manages the connection
// pool and provides methods for database access.
type Datastore struct {
    // ...
}

// GetConnection retrieves a connection from the
// pool with a default 5-second timeout.
func (ds *Datastore) GetConnection() (
    *pgxpool.Conn, error) {
    // ...
}
```

### Error Handling

Always check errors and provide context. In the
following example, the code wraps the error:

```go
conn, err := ds.GetConnection()
if err != nil {
    return fmt.Errorf(
        "failed to get datastore connection: %w",
        err)
}
```

Use the `%w` verb to wrap errors for error chains.

## Common Development Tasks

This section describes common tasks when developing
the Collector.

### Adding a Configuration Option

Follow these steps to add a configuration option:

1. Add a field to the `Config` struct in `config.go`.
2. Add a default value in `NewConfig()`.
3. Add parsing in `setConfigValue()`.
4. Add a getter method if needed.
5. Update the sample configuration file.

### Modifying the Schema

Follow these steps to modify the schema:

1. Add a new migration to `schema.go`.
2. Implement the `Up` function.
3. Make the migration idempotent.
4. Test on a clean database.
5. Test on a database with an existing schema.
6. Update the [Schema Management](schema-management.md)
   documentation.

### Adding a Probe

See the dedicated [Adding Probes](adding-probes.md)
guide for complete instructions.

## Debugging

This section describes debugging techniques for the
Collector.

### Using Delve Debugger

In the following example, the commands install and
run the Delve debugger:

```bash
go install \
    github.com/go-delve/delve/cmd/dlv@latest
cd src
dlv debug -- \
    -config ../ai-dba-collector-dev.yaml
```

Set breakpoints and step through code:

```
(dlv) break main.main
(dlv) continue
(dlv) next
(dlv) print config
```

### Database Inspection

Connect to the datastore and inspect the state. In
the following example, the queries check probe and
connection status:

```sql
SELECT * FROM probes WHERE is_enabled = TRUE;

SELECT * FROM connections
    WHERE is_monitored = TRUE;

SELECT connection_id, collected_at, COUNT(*)
FROM metrics.pg_stat_activity
WHERE collected_at > NOW() - INTERVAL '1 hour'
GROUP BY connection_id, collected_at
ORDER BY collected_at DESC;
```

## Performance Profiling

This section describes performance profiling
techniques.

### CPU Profiling

Add the `pprof` import to enable profiling. In the
following example, the import statement enables
HTTP profiling:

```go
import _ "net/http/pprof"
```

Start an HTTP server for profiling:

```go
go func() {
    log.Println(http.ListenAndServe(
        "localhost:6060", nil))
}()
```

Collect and analyze the profile. In the following
example, the commands run a 30-second profile:

```bash
go tool pprof \
    http://localhost:6060/debug/pprof/profile?seconds=30

(pprof) top
(pprof) list functionName
(pprof) web
```

### Memory Profiling

In the following example, the commands collect memory
profiles:

```bash
# Heap profile
go tool pprof \
    http://localhost:6060/debug/pprof/heap

# Allocation profile
go tool pprof \
    http://localhost:6060/debug/pprof/allocs
```

### Goroutine Profiling

In the following example, the command collects a
goroutine profile:

```bash
go tool pprof \
    http://localhost:6060/debug/pprof/goroutine
```

## Benchmarks

Write benchmarks to measure performance-critical
code. In the following example, the benchmark tests
encryption performance:

```go
func BenchmarkEncryption(b *testing.B) {
    password := "test-password"
    secret := "test-secret"

    for i := 0; i < b.N; i++ {
        _, err := crypto.EncryptPassword(
            password, secret)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

In the following example, the commands run
benchmarks:

```bash
go test -bench=. ./...
go test -bench=BenchmarkEncryption ./database/
```

## Best Practices

Follow these best practices when writing tests.

### Test Isolation

Each test should be independent. In the following
example, the test creates and cleans up its own
resources:

```go
func TestConnection(t *testing.T) {
    conn := createTestConnection(t)
    defer deleteTestConnection(t, conn)
    // test...
}
```

### Cleanup

Always clean up resources using defer statements. In
the following example, the test closes the datastore
on completion:

```go
func TestDatastore(t *testing.T) {
    ds, err := NewDatastore(config)
    if err != nil {
        t.Fatal(err)
    }
    defer ds.Close()
    // test...
}
```

### Meaningful Assertions

Provide context in error messages. In the following
example, the assertion includes both actual and
expected values:

```go
if got != want {
    t.Errorf(
        "GetValue() = %v, want %v", got, want)
}
```

### Skip Appropriately

Skip tests that cannot run in the current
environment:

```go
if os.Getenv("SKIP_DB_TESTS") != "" {
    t.Skip("Database not available")
}
```

## Contributing

This section describes the contribution workflow.

### Before Submitting

Follow these steps before submitting a change:

1. Run `make check` to ensure formatting, linting,
   and tests pass.
2. Update relevant documentation.
3. Add tests for new functionality.
4. Follow existing code style.
5. Keep commits focused and atomic.

### Commit Messages

Follow this format for commit messages:

```
Short summary (50 chars or less)

More detailed explanation if needed. Wrap at 72
characters. Explain what changed and why, not how.

- Use present tense ("Add feature" not "Added")
- Reference issues: "Fixes #123"
```

## Troubleshooting

This section covers common development issues.

### "go.mod out of sync"

Run the following command to synchronize the module
file:

```bash
go mod tidy
```

### "golangci-lint not found"

Install or update `golangci-lint` with the following
command:

```bash
go install \
    github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### "Test database connection failed"

Check the environment variables. In the following
example, the export statement sets the connection
URL:

```bash
export TEST_AI_WORKBENCH_SERVER=\
    "postgres://user:pass@localhost/postgres"
go test ./...
```

### Build Fails with "undefined: ..."

Re-download dependencies with the following
commands:

```bash
go mod download
go mod verify
```

### Tests Hang

If tests hang, check for the following issues:

- Missing cleanup in defer statements.
- Goroutine leaks in concurrent code.
- Missing timeout contexts.

### Tests Flaky

If tests are flaky, check for the following issues:

- Race conditions; run `go test -race` to detect
  them.
- Test isolation; ensure tests do not share state.
- Time-dependent assertions.

## Resources

This section provides links to external resources.

### Go Resources

The following resources are helpful for Go
development:

- [Effective Go](https://golang.org/doc/effective_go.html)
  provides best practices for Go programming.
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
  provides community-maintained guidelines.
- [pgx Documentation](https://pkg.go.dev/github.com/jackc/pgx/v5)
  covers the PostgreSQL driver library.

### PostgreSQL Resources

The following resources are helpful for PostgreSQL
development:

- [PostgreSQL Documentation](https://www.postgresql.org/docs/)
  provides the official reference.
- [Statistics Views](https://www.postgresql.org/docs/current/monitoring-stats.html)
  describes monitoring statistics.
- [Partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html)
  explains table partitioning.

## See Also

The following resources provide additional details.

- [Adding Probes](adding-probes.md) covers how to
  create new probes.
- [Architecture](architecture.md) describes the
  system design.
- [Schema Management](schema-management.md) covers
  the migration system.
