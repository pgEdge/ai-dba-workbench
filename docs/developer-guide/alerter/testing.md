# Testing and Development

This guide explains how to set up a development environment for
the alerter, describes the project structure, and covers the
testing approach for different components.

## Prerequisites

Before developing the alerter, ensure you have the following
tools installed:

- [Go 1.21](https://go.dev/doc/install) or higher.
- A PostgreSQL 14+ instance for the datastore.
- Git for version control.
- Optionally, [Ollama](https://ollama.ai) for local LLM testing.

## Project Structure

The alerter source code is organized as follows:

```
alerter/
├── src/
│   ├── cmd/
│   │   └── ai-dba-alerter/
│   │       └── main.go           # Entry point
│   └── internal/
│       ├── config/
│       │   ├── config.go         # Configuration
│       │   └── config_test.go    # Config tests
│       ├── cron/
│       │   ├── cron.go           # Cron parsing
│       │   └── cron_test.go      # Cron tests
│       ├── database/
│       │   ├── datastore.go      # DB connection
│       │   ├── types.go          # Type definitions
│       │   ├── queries.go        # Alert queries
│       │   └── notification_queries.go
│       ├── engine/
│       │   ├── engine.go         # Core engine
│       │   └── engine_test.go    # Engine tests
│       ├── llm/
│       │   ├── llm.go            # Provider interfaces
│       │   ├── ollama.go         # Ollama provider
│       │   ├── openai.go         # OpenAI provider
│       │   ├── anthropic.go      # Anthropic provider
│       │   ├── voyage.go         # Voyage provider
│       │   └── retry.go          # Retry logic
│       └── notifications/
│           ├── manager.go        # Notification mgr
│           ├── slack.go          # Slack notifier
│           ├── mattermost.go     # Mattermost
│           ├── webhook.go        # Webhook notifier
│           ├── email.go          # Email notifier
│           └── template.go       # Templates
└── docs/                         # Documentation
```

## Setting Up the Development Environment

Clone the repository and navigate to the alerter directory:

```bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench/alerter
```

Install Go dependencies:

```bash
go mod download
```

Set up a development datastore with the AI DBA Workbench schema.
You can use the migrations from the collector to create the
schema.

## Building the Alerter

Build the alerter binary:

```bash
go build -o bin/ai-dba-alerter ./src
```

Build with race detection for development:

```bash
go build -race -o bin/ai-dba-alerter ./src
```

## Running in Development Mode

Create a development configuration file `dev-config.yaml`:

```yaml
datastore:
  host: localhost
  database: ai_workbench_dev
  username: postgres
  password: postgres

threshold:
  evaluation_interval_seconds: 30

anomaly:
  enabled: true
  tier1:
    enabled: true
    default_sensitivity: 3.0
  tier2:
    enabled: false  # Disable for faster iteration
  tier3:
    enabled: false
```

Run the alerter with debug logging:

```bash
./bin/ai-dba-alerter -config dev-config.yaml -debug
```

## Code Organization

### Configuration Package

The `config` package handles all configuration loading and
validation. Configuration sources are applied in order: defaults,
file, and command-line flags.

### Database Package

The `database` package provides datastore access. The `Datastore`
struct manages the connection pool. Query functions follow a
consistent naming pattern:

- `Get*` functions retrieve single records.
- `Get*s` functions retrieve multiple records.
- `Create*` functions insert new records.
- `Update*` functions modify existing records.
- `Delete*` functions remove records.

### Engine Package

The `engine` package contains the core alerter logic. The
`Engine` struct coordinates all background workers. Each worker
runs in its own goroutine and uses a ticker for periodic
execution.

### LLM Package

The `llm` package defines provider interfaces and
implementations. The `EmbeddingProvider` interface generates
vector embeddings. The `ReasoningProvider` interface performs
LLM classification.

### Notifications Package

The `notifications` package handles alert delivery. The `Manager`
struct coordinates notification processing. Each channel type has
a dedicated `Notifier` implementation.

## Running Tests

### Running All Tests

Run all tests from the alerter directory:

```bash
cd alerter
go test ./src/...
```

Run tests with verbose output:

```bash
go test -v ./src/...
```

### Running Specific Tests

Run tests for a specific package:

```bash
go test ./src/internal/engine/...
```

Run a specific test function:

```bash
go test -run TestCalculateStats ./src/internal/engine/...
```

Run tests matching a pattern:

```bash
go test -run TestCronMatches ./src/internal/engine/...
```

### Test Coverage

Run tests with coverage reporting:

```bash
go test -cover ./src/...
```

Generate a coverage profile:

```bash
go test -coverprofile=coverage.out ./src/...
```

View coverage in a browser:

```bash
go tool cover -html=coverage.out
```

### Race Detection

Run tests with race detection to find data races:

```bash
go test -race ./src/...
```

Enable race detection during development to catch concurrency
issues before they cause problems in production.

## Test Organization

### Unit Tests

Unit tests are located alongside the source files they test. Each
test file has a `_test.go` suffix. Unit tests verify individual
functions and methods in isolation.

### Test Files

The alerter includes the following test files:

| File | Description |
|------|-------------|
| `config/config_test.go` | Configuration loading and validation tests |
| `cron/cron_test.go` | Cron expression parsing tests |
| `database/datastore_test.go` | Database connection tests |
| `engine/engine_test.go` | Core engine function tests |

### Test Categories

Tests are organized into the following categories:

- Basic functionality tests verify correct behavior.
- Edge case tests verify handling of boundary conditions.
- Error handling tests verify graceful failure modes.
- Benchmark tests measure performance.

## Engine Tests

The engine package includes comprehensive tests for core
functionality.

### Statistical Functions

The `TestCalculateStats` test verifies mean and standard deviation
calculations:

- Empty slices return zero values.
- Single values return the value as mean with zero stddev.
- Multiple values return correct statistical calculations.
- Edge cases like negative values and large spreads are handled.

In the following example, the test verifies calculation with
typical database metrics:

```go
func TestCalculateStats(t *testing.T) {
    values := []float64{
        50.0, 55.0, 48.0, 52.0,
        49.0, 53.0, 51.0, 47.0,
    }
    mean, stddev := calculateStats(values)

    if math.Abs(mean-50.625) > 0.1 {
        t.Errorf(
            "mean = %v, expected 50.625", mean)
    }

    if math.Abs(stddev-2.5495) > 0.1 {
        t.Errorf(
            "stddev = %v, expected 2.5495", stddev)
    }
}
```

### Threshold Checking

The `TestCheckThreshold` test verifies all comparison operators:

- Greater than and greater than or equal.
- Less than and less than or equal.
- Equal and not equal.
- Edge cases like zero values and unknown operators.

In the following example, the test verifies a threshold
violation:

```go
func TestCheckThreshold(t *testing.T) {
    engine := &Engine{}

    result := engine.checkThreshold(85.5, ">", 80.0)
    if !result {
        t.Error(
            "expected threshold violation " +
            "for 85.5 > 80.0")
    }
}
```

### Cron Matching

The `TestCronMatches` test verifies cron expression evaluation:

- Invalid expressions return false.
- Exact time matches are detected.
- Step expressions work correctly.
- Weekday ranges are evaluated properly.
- Timezone handling is correct.

In the following example, the test verifies a 15-minute interval:

```go
func TestCronMatches(t *testing.T) {
    engine := &Engine{}
    testTime := time.Date(
        2025, 1, 15, 10, 15, 0, 0, time.UTC)

    result := engine.cronMatches(
        "*/15 * * * *", testTime, "UTC")
    if !result {
        t.Error(
            "expected match at minute 15 " +
            "for */15 expression")
    }
}
```

## Configuration Tests

The configuration package tests verify the following behaviors:

- Default values are applied correctly.
- Configuration files are loaded and parsed.
- Command-line flags override file values.
- Validation catches invalid configurations.

## Cron Tests

The cron package tests verify the following behaviors:

- Standard 5-field expressions are parsed.
- All syntax elements work (wildcards, ranges, lists, steps).
- Invalid expressions are rejected with errors.
- Timezone conversion is applied correctly.

## Writing New Tests

### Test Structure

Follow this structure for new tests:

```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name     string
        input    InputType
        expected OutputType
    }{
        {
            name:     "descriptive test case name",
            input:    someInput,
            expected: expectedOutput,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := FunctionUnderTest(tt.input)
            if result != tt.expected {
                t.Errorf(
                    "got %v, expected %v",
                    result, tt.expected)
            }
        })
    }
}
```

### Test Naming

Use descriptive names for test functions and cases:

- Test function names start with `Test` followed by the function
  name.
- Test case names describe the scenario being tested.
- Use lowercase with underscores for case names.

### Assertions

Use clear assertions with helpful error messages:

```go
if result != expected {
    t.Errorf(
        "FunctionName(%v) = %v, expected %v",
        input, result, expected)
}
```

### Benchmarks

Add benchmarks for performance-critical functions:

```go
func BenchmarkFunctionName(b *testing.B) {
    // Setup
    input := setupInput()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        FunctionUnderTest(input)
    }
}
```

Run benchmarks with the following command:

```bash
go test -bench=. ./src/...
```

## Database Tests

Database tests require a running PostgreSQL instance. These tests
verify the following behaviors:

- Connection pool management.
- Query execution and result parsing.
- Transaction handling.
- Error handling for database failures.

To run database tests, ensure the test database is configured and
provide the connection details through a configuration file.

## Mocking

For unit tests that need to isolate components, use
interface-based mocking. The alerter defines interfaces for the
following components:

- `EmbeddingProvider` for embedding generation.
- `ReasoningProvider` for LLM classification.
- `Notifier` for notification delivery.

Create mock implementations that record calls and return
configured responses for testing.

## Development Workflow

### Making Changes

1. Create a feature branch from `main`.
2. Make changes following the code style guidelines.
3. Write or update tests for the changes.
4. Run tests locally to verify correctness.
5. Submit a pull request for review.

### Code Style

Follow these code style guidelines:

- Use four spaces for indentation.
- Format code with `gofmt` before committing.
- Write clear, descriptive function and variable names.
- Include the copyright header in all source files.
- Add comments for exported functions and types.

### Adding New LLM Providers

To add a new LLM provider:

1. Create a new file in the `llm` package.
2. Implement the `EmbeddingProvider` or `ReasoningProvider`
   interface.
3. Add configuration options in `config/config.go`.
4. Register the provider in `llm/llm.go`.
5. Document the configuration options.

### Adding New Notification Channels

To add a new notification channel:

1. Define the channel type in
   `database/notification_types.go`.
2. Create a notifier implementation in the `notifications`
   package.
3. Register the notifier in `manager.go`.
4. Add configuration fields as needed.
5. Update the documentation.

## Debugging

### Debug Logging

Enable debug logging with the `-debug` flag. Debug output
includes:

- Rule evaluation progress and results.
- Baseline calculation details.
- Anomaly detection tier results.
- Notification processing status.

### Database Queries

Use the PostgreSQL logs to trace database queries. Set
`log_statement` to `all` in the development database for full
query logging.

### LLM Debugging

Enable debug logging to see LLM requests and responses. Check
the LLM provider logs for additional debugging information.

## Continuous Integration

The project runs tests automatically on pull requests. Ensure all
tests pass locally before submitting changes. The CI pipeline
runs the following checks:

- Unit tests with race detection.
- Coverage reporting.
- Code linting.

## Troubleshooting Tests

### Test Failures

When tests fail, check the following areas:

- The test output for specific assertion failures.
- Whether dependencies are properly initialized.
- Whether the configuration file is set correctly.
- Whether the database is accessible for integration tests.

### Flaky Tests

If tests fail intermittently, investigate these potential causes:

- Check for timing dependencies in the test.
- Use synchronization primitives for concurrent code.
- Ensure test isolation by resetting state.
- Consider using longer timeouts for slow operations.

### Coverage Gaps

If coverage is low, consider these approaches:

- Add tests for untested functions.
- Add edge case tests for existing functions.
- Consider adding integration tests for complex flows.

## Contributing

Before contributing, review the project's contribution guidelines
in `docs/developer-guide/contributing.md`. Ensure all tests pass and the code
follows the style guidelines before submitting a pull request.
