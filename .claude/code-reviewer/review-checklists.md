# Review Checklists

This document provides checklists for reviewing different types of changes.

## New Feature Checklist

### Code Quality

- [ ] Code follows naming conventions
- [ ] Functions are appropriately sized (< 50 lines)
- [ ] Complexity is manageable (cyclomatic < 10)
- [ ] No deep nesting (< 4 levels)
- [ ] Code is well-documented
- [ ] No unused code or dead code paths

### Error Handling

- [ ] All errors are checked
- [ ] Errors are wrapped with context
- [ ] Error messages are helpful
- [ ] No sensitive data in error messages
- [ ] Errors propagate correctly

### Testing

- [ ] Unit tests added
- [ ] Edge cases tested
- [ ] Error paths tested
- [ ] Integration tests if needed
- [ ] Tests are deterministic
- [ ] Coverage meets standards (> 80%)

### Security

- [ ] Input validation present
- [ ] Authorization checks in place
- [ ] No SQL injection possible
- [ ] No sensitive data logged
- [ ] Credentials handled properly

### Documentation

- [ ] Public APIs documented
- [ ] Complex logic explained
- [ ] README updated if needed
- [ ] Changelog updated

### Performance

- [ ] No N+1 queries
- [ ] Appropriate use of caching
- [ ] Resources properly released
- [ ] No unbounded operations

## Bug Fix Checklist

### Understanding

- [ ] Root cause identified
- [ ] Fix addresses root cause (not symptom)
- [ ] No regression in related functionality
- [ ] Edge cases considered

### Implementation

- [ ] Fix is minimal and focused
- [ ] No unrelated changes included
- [ ] Existing code style maintained
- [ ] Error handling improved if applicable

### Testing

- [ ] Test reproduces the bug (without fix)
- [ ] Test passes with fix
- [ ] Existing tests still pass
- [ ] Related edge cases tested

### Documentation

- [ ] Fix documented in code if complex
- [ ] Changelog updated
- [ ] Related documentation updated

## Refactoring Checklist

### Scope

- [ ] Refactoring is focused
- [ ] No behavior changes intended
- [ ] Changes are well-bounded

### Verification

- [ ] All existing tests pass
- [ ] No new test failures
- [ ] Functionality verified manually if needed
- [ ] Performance not degraded

### Code Quality

- [ ] Code is cleaner than before
- [ ] Complexity reduced or unchanged
- [ ] Duplication reduced
- [ ] Naming improved if needed

### Safety

- [ ] Changes are incremental
- [ ] Easy to revert if needed
- [ ] No hidden behavior changes

## Performance Change Checklist

### Measurement

- [ ] Baseline performance measured
- [ ] Improvement measured
- [ ] Improvement is significant
- [ ] No regression elsewhere

### Implementation

- [ ] Change is necessary (not premature optimization)
- [ ] Change is well-documented
- [ ] Complexity increase is justified
- [ ] Maintainability not sacrificed

### Testing

- [ ] Correctness verified
- [ ] Benchmark tests added
- [ ] Edge cases still work
- [ ] Load testing if applicable

### Trade-offs

- [ ] Memory vs. CPU trade-off documented
- [ ] Readability impact acceptable
- [ ] Maintenance cost considered

## Database Change Checklist

### Schema Changes

- [ ] Migration is reversible
- [ ] Migration handles existing data
- [ ] Indexes added for query patterns
- [ ] No breaking changes (or migration path exists)

### Query Changes

- [ ] Query is parameterized
- [ ] Query performance acceptable
- [ ] Indexes support the query
- [ ] Results filtered by authorization

### Data Integrity

- [ ] Constraints are appropriate
- [ ] Foreign keys maintained
- [ ] Default values sensible
- [ ] Null handling correct

### Documentation

- [ ] Schema changes documented
- [ ] Migration history updated
- [ ] API changes documented

## API Change Checklist

### Design

- [ ] Change is backward compatible (or versioned)
- [ ] Naming is consistent
- [ ] Request/response format follows conventions
- [ ] Error responses are consistent

### Implementation

- [ ] Input validation complete
- [ ] Authorization checked
- [ ] Rate limiting appropriate
- [ ] Errors handled properly

### Documentation

- [ ] API documentation updated
- [ ] Examples provided
- [ ] Migration guide if breaking change
- [ ] Changelog updated

### Testing

- [ ] Endpoint tests added
- [ ] Error cases tested
- [ ] Authorization tests added
- [ ] Performance acceptable

## UI Component Checklist

### Functionality

- [ ] Component works as expected
- [ ] State management correct
- [ ] Props interface clear
- [ ] Events handled properly

### Accessibility

- [ ] ARIA labels present
- [ ] Keyboard navigation works
- [ ] Focus management correct
- [ ] Screen reader compatible

### Styling

- [ ] Uses theme system
- [ ] Responsive design
- [ ] Consistent with design system
- [ ] No inline styles (unless necessary)

### Performance

- [ ] Unnecessary re-renders prevented
- [ ] Heavy computations memoized
- [ ] Lazy loading if appropriate
- [ ] Bundle size impact acceptable

### Testing

- [ ] Component tests added
- [ ] User interactions tested
- [ ] Edge cases covered
- [ ] Accessibility tested

## Quick Review (< 50 lines)

For small changes, focus on:

- [ ] Change does what it claims
- [ ] No obvious bugs
- [ ] Error handling present
- [ ] Tests added/updated
- [ ] No security issues
- [ ] Code style consistent
