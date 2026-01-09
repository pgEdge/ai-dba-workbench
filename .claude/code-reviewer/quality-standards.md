# Quality Standards

This document defines code quality standards for the AI DBA Workbench project.

## General Standards

### Formatting

| Rule | Standard |
|------|----------|
| Indentation | Four spaces (all languages) |
| Line length | 79 characters (docs), reasonable for code |
| Trailing whitespace | None |
| Final newline | Required |

### Naming Conventions

**Go:**

| Element | Convention | Example |
|---------|------------|---------|
| Package | lowercase, short | `auth`, `mcp` |
| Exported function | PascalCase | `CreateUser` |
| Unexported function | camelCase | `validateInput` |
| Constant | PascalCase or ALL_CAPS | `MaxRetries`, `DEFAULT_TIMEOUT` |
| Interface | PascalCase, often -er | `Reader`, `TokenValidator` |

**TypeScript/React:**

| Element | Convention | Example |
|---------|------------|---------|
| Component | PascalCase | `UserProfile` |
| Hook | camelCase, use prefix | `useAuth`, `useConnections` |
| Function | camelCase | `fetchData` |
| Constant | SCREAMING_SNAKE_CASE | `MAX_RETRIES` |
| Type/Interface | PascalCase | `UserData`, `ConnectionConfig` |

### Documentation

**Required documentation:**

- All exported functions/methods
- All exported types
- Complex algorithms
- Non-obvious business logic
- Configuration options

**Documentation style:**

```go
// CreateUser creates a new user account with the given username and password.
// It returns the created user or an error if the username already exists.
func CreateUser(ctx context.Context, username, password string) (*User, error)
```

## Code Organization

### Function Length

| Length | Assessment |
|--------|------------|
| < 20 lines | Ideal |
| 20-50 lines | Acceptable |
| 50-100 lines | Consider splitting |
| > 100 lines | Must refactor |

### Cyclomatic Complexity

| Complexity | Assessment |
|------------|------------|
| 1-5 | Simple, easy to test |
| 6-10 | Moderate, acceptable |
| 11-20 | Complex, consider simplifying |
| > 20 | Too complex, must refactor |

### Nesting Depth

| Depth | Assessment |
|-------|------------|
| 1-2 | Ideal |
| 3 | Acceptable |
| 4 | Maximum recommended |
| > 4 | Refactor with early returns or extraction |

**Reducing nesting:**

```go
// BAD - Deep nesting
func process(data *Data) error {
    if data != nil {
        if data.Valid {
            if data.Ready {
                // actual logic here
            }
        }
    }
    return nil
}

// GOOD - Early returns
func process(data *Data) error {
    if data == nil {
        return nil
    }
    if !data.Valid {
        return nil
    }
    if !data.Ready {
        return nil
    }
    // actual logic here
    return nil
}
```

## Error Handling

### Go Error Handling

**Always check errors:**

```go
// BAD
result, _ := someFunction()

// GOOD
result, err := someFunction()
if err != nil {
    return fmt.Errorf("someFunction failed: %w", err)
}
```

**Wrap errors with context:**

```go
// BAD - No context
if err != nil {
    return err
}

// GOOD - Context added
if err != nil {
    return fmt.Errorf("failed to create user %s: %w", username, err)
}
```

**Don't ignore cleanup errors (but don't fail on them):**

```go
defer func() {
    if err := file.Close(); err != nil {
        log.Warn("failed to close file", "error", err)
    }
}()
```

### TypeScript Error Handling

**Handle promise rejections:**

```typescript
// BAD
const data = await fetchData();

// GOOD
try {
    const data = await fetchData();
} catch (error) {
    // Handle error appropriately
}
```

**Type errors properly:**

```typescript
// BAD
catch (error) {
    console.log(error.message);  // error is unknown
}

// GOOD
catch (error) {
    if (error instanceof Error) {
        console.log(error.message);
    }
}
```

## Testing Standards

### Coverage Requirements

| Component Type | Minimum Coverage |
|----------------|------------------|
| Business logic | 90% |
| API handlers | 85% |
| Utilities | 80% |
| UI components | 70% |

### Test Organization

**Go:**

- Unit tests: `*_test.go` co-located with source
- Integration tests: `/tests/integration/`
- Table-driven tests for multiple scenarios

**React:**

- Unit tests: Co-located or in `/tests/unit/`
- Component tests: Test behavior, not implementation
- Integration tests: `/tests/integration/`

### Test Naming

```go
// Go - Descriptive function names
func TestCreateUser_WithValidInput_ReturnsUser(t *testing.T)
func TestCreateUser_WithDuplicateUsername_ReturnsError(t *testing.T)
```

```typescript
// TypeScript - describe/it blocks
describe('CreateUser', () => {
    it('returns user when input is valid', () => {})
    it('returns error when username is duplicate', () => {})
})
```

## Code Duplication

### DRY Principle

- Extract repeated code into functions
- Use interfaces for common behavior
- Create shared utilities for common operations

**When duplication is acceptable:**

- Test setup code (clarity over DRY)
- Two occurrences that may diverge
- Premature abstraction would be worse

**When duplication must be eliminated:**

- Three or more identical code blocks
- Business logic repeated
- Error-prone operations repeated

## Performance Standards

### Avoid Obvious Issues

- No N+1 queries
- No unbounded allocations in loops
- No synchronous operations that should be async
- Use connection pooling

### Resource Management

```go
// Always close resources
defer file.Close()
defer rows.Close()
defer conn.Release()
```

### Memory Allocation

```go
// Pre-allocate slices when size is known
items := make([]Item, 0, expectedCount)

// Reuse buffers when appropriate
var buf bytes.Buffer
buf.Reset()
```

## Dependency Management

### Go Dependencies

- Use specific versions in `go.mod`
- Minimize external dependencies
- Prefer standard library
- Vet new dependencies for maintenance/security

### Node Dependencies

- Lock versions with `package-lock.json`
- Audit regularly
- Minimize production dependencies
- Keep dev dependencies separate

## Code Review Expectations

### Author Responsibilities

- Self-review before requesting review
- Ensure tests pass
- Ensure linters pass
- Provide context in PR description
- Respond to feedback promptly

### Reviewer Responsibilities

- Review within reasonable timeframe
- Provide constructive feedback
- Distinguish between blockers and suggestions
- Approve when concerns are addressed
