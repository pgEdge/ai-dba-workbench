# Quality Checklist

Go-specific quality standards, anti-patterns, and review checklists for the
AI DBA Workbench project.

## Go Anti-Patterns

### Error Handling

- Never ignore errors with `_`; always check and handle.
- Wrap errors with context using `fmt.Errorf("description: %w", err)`.
- Never swallow errors in empty `if err != nil {}` blocks.
- Never replace the original error with `errors.New()`; use `%w` wrapping.
- Log cleanup errors from `defer` but do not fail on them.

### Resource Management

- Always `defer Close()` immediately after acquiring a resource.
- Never `defer` inside a loop; extract to a helper function instead.
- Close `rows` with `defer rows.Close()` and check `rows.Err()` after
  iteration.

### Concurrency

- Protect shared state with `sync.Mutex` or `atomic` operations.
- Prevent goroutine leaks; use `context` or `select` with timeout.
- Never create `context.Background()` inside a handler that already
  receives a context; propagate the parent context.

### Database Queries

- Always parameterize SQL queries (`$1`, `$2`); never concatenate input.
- Avoid N+1 queries; use JOINs or `IN` clauses.
- Always check errors from `db.Query` and `rows.Scan`.
- Pass `context.Context` to all database calls (`QueryContext`,
  `ExecContext`).

## Project Naming Conventions

| Element             | Convention      | Example                     |
|---------------------|-----------------|-----------------------------|
| Package             | lowercase short | `auth`, `mcp`               |
| Exported function   | PascalCase      | `CreateUser`                |
| Unexported function | camelCase       | `validateInput`             |
| Constant            | PascalCase      | `MaxRetries`                |
| Interface           | PascalCase, -er | `Reader`, `TokenValidator`  |

## Code Organization Standards

- Functions under 50 lines; refactor anything over 100 lines.
- Cyclomatic complexity under 10; refactor anything over 20.
- Nesting depth at most 3; use early returns to flatten logic.
- Four-space indentation in all files.
- Include the project copyright header in every new source file.
- Document all exported functions, types, and complex algorithms.
- Pre-allocate slices with `make([]T, 0, expectedCount)` when the size
  is known.
- Prefer the standard library over external dependencies.

## Testing Standards

- Use table-driven tests for multiple scenarios.
- Name tests descriptively:
  `TestCreateUser_WithDuplicateUsername_ReturnsError`.
- Always assert expected behavior; no tests without assertions.
- Tests must be independent; no shared mutable state between tests.
- Test both success and error paths.
- Avoid flaky tests: mock external services, seed randomness, use
  relative time comparisons.

## Review Checklists

### New Feature

- Code follows project naming conventions and style.
- Functions are under 50 lines; complexity under 10.
- All errors checked and wrapped with context.
- No sensitive data in error messages or logs.
- Input validation present for all external data.
- Authorization checks in place.
- SQL queries parameterized; no injection vectors.
- Unit tests added with edge cases and error paths.
- Resources released with `defer`.
- No N+1 queries or unbounded allocations.
- Public APIs documented; changelog updated.

### Bug Fix

- Root cause identified; fix addresses cause, not symptom.
- Fix is minimal and focused; no unrelated changes.
- Test added that reproduces the bug without the fix.
- All existing tests still pass.
- Changelog updated.

### Refactoring

- No behavior changes; all existing tests pass.
- Complexity reduced or unchanged.
- Duplication reduced.
- Performance not degraded.
- Changes are incremental and easy to revert.

### Database Change

- Migration is reversible and handles existing data.
- Indexes added for new query patterns.
- All queries parameterized.
- Results filtered by authorization.
- Constraints, foreign keys, and null handling are correct.
- `COMMENT ON` used to describe new objects.
- Schema version incremented.

### API Change

- Backward compatible or properly versioned.
- Request/response format follows existing conventions.
- Input validation complete.
- Authorization checked.
- Error responses consistent with existing patterns.
- Endpoint tests and authorization tests added.
- API documentation and changelog updated.

### Quick Review (< 50 lines)

- Change does what it claims.
- No obvious bugs or security issues.
- Error handling present.
- Tests added or updated.
- Code style consistent with surroundings.
