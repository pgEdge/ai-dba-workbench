# Coverage and Code Quality - pgEdge AI Workbench

This document describes code coverage measurement and quality checks for the AI Workbench project.

## Code Coverage

### Overview

Code coverage measures which parts of the codebase are exercised by tests. The AI Workbench uses Go's built-in coverage tools for GoLang projects and Jest for React projects.

### Coverage Goals

**Project-Wide**:
- Overall coverage: >80%

**Critical Components**:
- Database operations: >90%
- User management: >90%
- Authentication/authorization: 100%
- Security functions (encryption, validation): 100%
- Connection management: >90%

**Lower Priority**:
- Main entry points: Lower coverage acceptable
- Logging utilities: Lower coverage acceptable
- Simple configuration getters: Lower coverage acceptable

## GoLang Coverage

### Running Coverage

#### Collector

```bash
cd collector
make coverage

# Or directly with go test
cd collector/src
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

**Output**:
- `coverage.out`: Coverage profile (text format)
- `coverage.html`: Coverage report (HTML format)

#### Server

```bash
cd server
make coverage

# Or directly with go test
cd server/src
SKIP_DB_TESTS=1 go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

#### Integration Tests

```bash
cd tests
make coverage

# Or directly with go test
cd tests
go test -coverprofile=coverage.out ./integration/... -timeout 30m
go tool cover -html=coverage.out -o coverage.html
```

### Viewing Coverage Reports

#### Terminal View

```bash
# Summary by package
go test -cover ./...

# Detailed function-level coverage
go tool cover -func=coverage.out

# Output example:
# github.com/pgedge/ai-workbench/database/datastore.go:45:    NewDatastore        100.0%
# github.com/pgedge/ai-workbench/database/datastore.go:67:    Close               100.0%
# github.com/pgedge/ai-workbench/database/datastore.go:89:    GetConnection       87.5%
```

#### HTML View

```bash
# Generate and open in browser
go tool cover -html=coverage.out

# Or generate file
go tool cover -html=coverage.out -o coverage.html
open coverage.html  # macOS
xdg-open coverage.html  # Linux
```

**HTML Report Features**:
- Color-coded lines (green = covered, red = not covered, gray = not executable)
- Click files in sidebar to navigate
- See exact lines missing coverage

### Coverage Profile Format

```
mode: set
github.com/pgedge/ai-workbench/database/datastore.go:45.41,47.16 2 1
github.com/pgedge/ai-workbench/database/datastore.go:47.16,49.3 1 1
github.com/pgedge/ai-workbench/database/datastore.go:50.2,50.26 1 1
```

**Format**: `file:line.col,line.col statements count`
- Last column: 0 = not covered, >0 = covered

### Coverage Modes

#### Set Mode (Default)

```bash
go test -covermode=set -coverprofile=coverage.out ./...
```

**Behavior**: Records whether each statement was executed (binary: yes/no)

**Use For**: Most testing scenarios

#### Count Mode

```bash
go test -covermode=count -coverprofile=coverage.out ./...
```

**Behavior**: Records how many times each statement was executed

**Use For**: Finding hot paths, performance analysis

#### Atomic Mode

```bash
go test -covermode=atomic -coverprofile=coverage.out ./...
```

**Behavior**: Like count mode, but thread-safe

**Use For**: Testing concurrent code with `-race` flag

### Combining Coverage from Multiple Packages

```bash
# Run tests with coverage for each package
go test -coverprofile=collector.out ./collector/...
go test -coverprofile=server.out ./server/...
go test -coverprofile=tests.out ./tests/...

# Merge coverage files
echo "mode: set" > coverage.out
tail -q -n +2 collector.out server.out tests.out >> coverage.out

# View combined coverage
go tool cover -html=coverage.out
```

### Coverage in CI/CD

Coverage is measured automatically in GitHub Actions:

```yaml
# .github/workflows/test-collector.yml
- name: Run tests with coverage
  env:
    TEST_AI_WORKBENCH_SERVER: postgres://postgres:postgres@localhost:5432/postgres
  run: |
    cd collector
    make coverage

- name: Upload coverage report
  uses: actions/upload-artifact@v4
  with:
    name: collector-coverage-go-${{ matrix.go-version }}
    path: collector/src/coverage.html
    retention-days: 30
```

**Artifacts**:
- Coverage HTML reports uploaded
- Available for 30 days
- Download from workflow run in GitHub Actions

### Improving Coverage

#### Find Uncovered Code

```bash
# View coverage by function
go tool cover -func=coverage.out | grep -v "100.0%"

# Find files with low coverage
go tool cover -func=coverage.out | awk '$3 < 80.0' | head -20
```

#### Coverage by Package

```bash
# Test individual package with coverage
go test -cover ./database/

# Output:
# ok      github.com/pgedge/ai-workbench/database    0.123s  coverage: 87.5% of statements
```

#### Identify Missing Test Cases

```bash
# Open HTML report
go tool cover -html=coverage.out

# Look for:
# - Red lines (uncovered code)
# - Error handling paths
# - Edge cases
# - Rarely used code paths
```

### Coverage Best Practices

#### 1. Focus on Meaningful Coverage

```go
// Don't write tests just for coverage
func GetName() string {
    return "name"  // Simple getter - test through usage
}

// Do test critical logic
func CalculateDiscount(price float64, user User) (float64, error) {
    // Critical business logic - needs thorough testing
    if price < 0 {
        return 0, errors.New("invalid price")
    }
    // ... complex logic ...
}
```

#### 2. Test Error Paths

```go
func TestErrorPaths(t *testing.T) {
    // Test success path
    result, err := DoSomething(validInput)
    require.NoError(t, err)
    assert.Equal(t, expected, result)

    // Test error paths (often missed in coverage)
    _, err = DoSomething(invalidInput)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "invalid input")

    _, err = DoSomething(nil)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "nil input")
}
```

#### 3. Test Edge Cases

```go
func TestEdgeCases(t *testing.T) {
    tests := []struct {
        name    string
        input   int
        wantErr bool
    }{
        {"zero", 0, false},           // Edge case
        {"negative", -1, true},       // Edge case
        {"max int", math.MaxInt, false},  // Edge case
        {"normal", 42, false},        // Normal case
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := Process(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

#### 4. Don't Test External Code

```go
// Bad: Testing pgx library (external)
func TestPgxQuery(t *testing.T) {
    conn, _ := pgx.Connect(ctx, connStr)
    rows, err := conn.Query(ctx, "SELECT 1")
    // Testing pgx, not our code
}

// Good: Testing our wrapper
func TestDatabaseQuery(t *testing.T) {
    mockDB := &mockDatabase{
        queryResult: []Row{{ID: 1}},
    }
    repo := NewRepository(mockDB)
    result, err := repo.GetByID(1)
    // Testing our code with mocked database
}
```

## Code Quality Checks

### Linting with golangci-lint

#### Configuration

**File**: `/tests/.golangci.yml` (also used by collector and server)

```yaml
linters-settings:
    errcheck:
        check-type-assertions: true
        check-blank: true
    govet:
        enable-all: true
        disable:
            - fieldalignment  # Often not worth the complexity
            - shadow          # Can be noisy
    misspell:
        locale: US

linters:
    enable:
        - errcheck      # Check for unchecked errors
        - govet         # Standard Go vet checks
        - ineffassign   # Detect ineffectual assignments
        - staticcheck   # Advanced static analysis
        - unused        # Check for unused code
        - misspell      # Find commonly misspelled words
        - gosec         # Security-focused linter

issues:
    exclude-dirs:
        - vendor
    exclude-rules:
        # Less strict in tests
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

#### Running Linter

```bash
# Collector
cd collector
make lint

# Server
cd server
make lint

# Integration tests
cd tests
make lint

# Direct usage
golangci-lint run ./...

# With auto-fix
golangci-lint run --fix ./...
```

#### Enabled Linters

**errcheck**: Checks for unchecked errors
```go
// Bad
func badExample() {
    file, _ := os.Open("file.txt")  // Ignoring error
    defer file.Close()              // Ignoring error
}

// Good
func goodExample() error {
    file, err := os.Open("file.txt")
    if err != nil {
        return fmt.Errorf("failed to open file: %w", err)
    }
    defer func() {
        if err := file.Close(); err != nil {
            log.Printf("failed to close file: %v", err)
        }
    }()
    return nil
}
```

**govet**: Standard Go static analysis
```go
// Detects:
// - Printf format string mismatches
// - Unreachable code
// - Invalid struct tags
// - Lost context
// - Suspicious boolean conditions
```

**ineffassign**: Detects ineffectual assignments
```go
// Bad
func ineffectual() int {
    x := 5  // ineffectual assignment
    x = 10
    return x
}

// Good
func effectual() int {
    x := 10
    return x
}
```

**staticcheck**: Advanced static analysis
```go
// Detects:
// - Unnecessary conversions
// - Deprecated function usage
// - Unreachable code
// - Context misuse
// - Empty branches
```

**unused**: Checks for unused code
```go
// Detects:
// - Unused variables
// - Unused functions
// - Unused constants
// - Unused types
```

**misspell**: Finds spelling errors
```go
// Detects spelling mistakes in:
// - Comments
// - String literals
// - Function names
// - Variable names
```

**gosec**: Security-focused checks
```go
// Detects:
// - Weak crypto usage
// - Command injection risks
// - SQL injection risks
// - File permission issues
// - Unsafe defer patterns
```

### Go Vet

Built-in static analysis tool:

```bash
# Run go vet
go vet ./...

# Through Makefile
make vet
```

**Checks**:
- Printf argument mismatches
- Struct tag issues
- Unreachable code
- Lost cancel function context
- Suspicious shifts
- Misuse of unsafe
- Composite literals

### Go Format

Ensure consistent code formatting:

```bash
# Format all code
go fmt ./...

# Through Makefile
make fmt

# Check formatting without changes
gofmt -l .
```

**Standards**:
- 4-space indentation (per project conventions)
- Tab characters converted to spaces
- Consistent bracket placement
- No trailing whitespace

### Pre-Commit Checks

Run all quality checks before committing:

```bash
# Collector
cd collector
make check  # Runs: fmt, vet, test, lint

# Server
cd server
make check  # Runs: fmt, vet, test, lint

# All projects
make test-all  # From root
```

## CI/CD Quality Checks

### GitHub Actions Workflows

All workflows include:
1. Build
2. Test with coverage
3. Lint
4. Upload coverage artifacts

**Example**: `.github/workflows/test-collector.yml`

```yaml
- name: Run tests with coverage
  env:
    TEST_AI_WORKBENCH_SERVER: postgres://postgres:postgres@localhost:5432/postgres
  run: |
    cd collector
    make coverage

- name: Upload coverage report
  uses: actions/upload-artifact@v4
  with:
    name: collector-coverage-go-${{ matrix.go-version }}
    path: collector/src/coverage.html
    retention-days: 30
```

### Coverage Thresholds

**Current**: No automated threshold enforcement

**Recommended**: Add coverage threshold checks

```bash
# Check if coverage meets threshold
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//' | \
    awk '{if ($1 < 80) exit 1}'
```

**GitHub Action Example**:
```yaml
- name: Check coverage threshold
  run: |
    cd collector/src
    go test -coverprofile=coverage.out ./...
    COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
    echo "Coverage: $COVERAGE%"
    if (( $(echo "$COVERAGE < 80" | bc -l) )); then
      echo "Coverage $COVERAGE% is below threshold 80%"
      exit 1
    fi
```

## React Coverage (Planned)

### Jest Configuration

```javascript
// jest.config.js
module.exports = {
    collectCoverageFrom: [
        'src/**/*.{js,jsx,ts,tsx}',
        '!src/**/*.test.{js,jsx,ts,tsx}',
        '!src/index.tsx',
        '!src/**/*.d.ts',
    ],
    coverageThreshold: {
        global: {
            branches: 80,
            functions: 80,
            lines: 80,
            statements: 80,
        },
    },
    coverageReporters: ['text', 'html', 'lcov'],
};
```

### Running React Coverage

```bash
cd client
npm test -- --coverage

# Generate HTML report
npm test -- --coverage --coverageReporters=html

# Open report
open coverage/index.html
```

### Coverage Metrics

**Lines**: Percentage of code lines executed
**Branches**: Percentage of conditional branches taken
**Functions**: Percentage of functions called
**Statements**: Percentage of statements executed

## Best Practices

### 1. Run Coverage Locally

```bash
# Before committing
cd collector
make test-all  # Runs tests, coverage, and lint

cd server
make test-all

cd tests
make test-all
```

### 2. Review Coverage Reports

```bash
# Generate HTML report
make coverage

# Open in browser
open src/coverage.html  # macOS
xdg-open src/coverage.html  # Linux

# Identify uncovered code
# Add tests for critical paths
```

### 3. Fix Linting Issues Promptly

```bash
# Check for issues
make lint

# Auto-fix where possible
golangci-lint run --fix ./...

# Manually fix remaining issues
```

### 4. Don't Disable Linters Without Reason

```go
// Bad: Disabling without explanation
//nolint:all
func riskyFunction() {
    // ...
}

// Good: Specific linter with explanation
//nolint:gosec // G204: Command execution - input is validated
func executeCommand(cmd string) {
    exec.Command("bash", "-c", cmd).Run()
}
```

### 5. Test Critical Paths Thoroughly

Focus coverage efforts on:
- Security-sensitive code
- Data integrity operations
- Error handling
- Business logic
- Edge cases

Less critical:
- Simple getters/setters
- Logging statements
- Trivial wrappers

## Troubleshooting

### Coverage Shows 0%

```bash
# Ensure tests are running
go test -v ./...

# Check for build tags
go test -v -tags=integration ./...

# Verify test files are named correctly
ls *_test.go
```

### Linter Takes Too Long

```bash
# Increase timeout
golangci-lint run --timeout=10m ./...

# Run on specific packages
golangci-lint run ./database/...
```

### Linter False Positives

```go
// Disable specific linter for line
//nolint:errcheck // Explanation why it's safe
file.Close()

// Disable for block
//nolint:gosec
func handleUserInput(input string) {
    // G204: Subprocess launched with variable - validated above
    exec.Command("bash", "-c", input).Run()
}
```

## Related Documents

- `testing-overview.md` - Overall testing strategy
- `unit-testing.md` - Unit test patterns
- `integration-testing.md` - Integration test patterns
- `writing-tests.md` - Practical guide for new tests
