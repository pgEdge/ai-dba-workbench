# Testing Guide

This guide explains how to run tests for the alerter and describes the
testing approach for different components.

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

Enable race detection during development to catch concurrency issues
before they cause problems in production.

## Test Organization

### Unit Tests

Unit tests are located alongside the source files they test. Each test
file has a `_test.go` suffix. Unit tests verify individual functions
and methods in isolation.

### Test Files

The alerter includes the following test files:

| File | Description |
|------|-------------|
| `config/config_test.go` | Configuration loading and validation tests |
| `cron/cron_test.go` | Cron expression parsing tests |
| `database/datastore_test.go` | Database connection tests |
| `engine/engine_test.go` | Core engine function tests |

### Test Categories

Tests are organized into categories:

- Basic functionality tests verify correct behavior.
- Edge case tests verify handling of boundary conditions.
- Error handling tests verify graceful failure modes.
- Benchmark tests measure performance.

## Engine Tests

The engine package includes comprehensive tests for core functionality.

### Statistical Functions

The `TestCalculateStats` test verifies mean and standard deviation
calculations:

- Empty slices return zero values.
- Single values return the value as mean with zero stddev.
- Multiple values return correct statistical calculations.
- Edge cases like negative values and large spreads are handled.

In the following example, the test verifies calculation with typical
database metrics:

```go
func TestCalculateStats(t *testing.T) {
    values := []float64{50.0, 55.0, 48.0, 52.0, 49.0, 53.0, 51.0, 47.0}
    mean, stddev := calculateStats(values)

    if math.Abs(mean-50.625) > 0.1 {
        t.Errorf("mean = %v, expected 50.625", mean)
    }

    if math.Abs(stddev-2.5495) > 0.1 {
        t.Errorf("stddev = %v, expected 2.5495", stddev)
    }
}
```

### Threshold Checking

The `TestCheckThreshold` test verifies all comparison operators:

- Greater than and greater than or equal.
- Less than and less than or equal.
- Equal and not equal.
- Edge cases like zero values and unknown operators.

In the following example, the test verifies a threshold violation:

```go
func TestCheckThreshold(t *testing.T) {
    engine := &Engine{}

    result := engine.checkThreshold(85.5, ">", 80.0)
    if !result {
        t.Error("expected threshold violation for 85.5 > 80.0")
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
    testTime := time.Date(2025, 1, 15, 10, 15, 0, 0, time.UTC)

    result := engine.cronMatches("*/15 * * * *", testTime, "UTC")
    if !result {
        t.Error("expected match at minute 15 for */15 expression")
    }
}
```

## Configuration Tests

The configuration package tests verify:

- Default values are applied correctly.
- Configuration files are loaded and parsed.
- Environment variables override file values.
- Validation catches invalid configurations.

## Cron Tests

The cron package tests verify:

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
                t.Errorf("got %v, expected %v", result, tt.expected)
            }
        })
    }
}
```

### Test Naming

Use descriptive names for test functions and cases:

- Test function names start with `Test` followed by the function name.
- Test case names describe the scenario being tested.
- Use lowercase with underscores for case names.

### Assertions

Use clear assertions with helpful error messages:

```go
if result != expected {
    t.Errorf("FunctionName(%v) = %v, expected %v", input, result, expected)
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

Run benchmarks with:

```bash
go test -bench=. ./src/...
```

## Database Tests

Database tests require a running PostgreSQL instance. These tests verify:

- Connection pool management.
- Query execution and result parsing.
- Transaction handling.
- Error handling for database failures.

To run database tests, ensure the test database is configured and set
the appropriate environment variables.

## Mocking

For unit tests that need to isolate components, use interface-based
mocking. The alerter defines interfaces for:

- `EmbeddingProvider` for embedding generation.
- `ReasoningProvider` for LLM classification.
- `Notifier` for notification delivery.

Create mock implementations that record calls and return configured
responses for testing.

## Continuous Integration

The project runs tests automatically on pull requests. Ensure all tests
pass locally before submitting changes. The CI pipeline runs:

- Unit tests with race detection.
- Coverage reporting.
- Code linting.

## Troubleshooting Tests

### Test Failures

When tests fail, check:

- The test output for specific assertion failures.
- Whether dependencies are properly initialized.
- Whether environment variables are set correctly.
- Whether the database is accessible for integration tests.

### Flaky Tests

If tests fail intermittently:

- Check for timing dependencies in the test.
- Use synchronization primitives for concurrent code.
- Ensure test isolation by resetting state.
- Consider using longer timeouts for slow operations.

### Coverage Gaps

If coverage is low:

- Add tests for untested functions.
- Add edge case tests for existing functions.
- Consider adding integration tests for complex flows.
