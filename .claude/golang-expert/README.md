/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Go Backend Expert Documentation
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Go Backend Expert Documentation

This directory contains comprehensive documentation about the Go backend
architecture, patterns, and best practices for the pgEdge AI DBA Workbench project.

## Purpose

This documentation serves as a knowledge base for Go development on this
project, providing:

- Architectural overviews and design decisions
- Implementation patterns and best practices
- Security and authorization flows
- Testing strategies and examples
- Code conventions and style guide

## Documentation Structure

### [Architecture Overview](./architecture-overview.md)

**What it covers:**
- High-level architecture of server and collector components
- Component responsibilities and interactions
- Core architectural patterns (connection pooling, configuration, error
  handling)
- Database schema management
- Dependency injection patterns
- Graceful shutdown procedures
- Security considerations
- Performance optimizations

**When to use:**
- Understanding the overall system architecture
- Planning new features
- Debugging cross-component issues
- Optimizing performance
- Reviewing security implications

### [MCP Implementation](./mcp-implementation.md)

**What it covers:**
- JSON-RPC 2.0 protocol implementation
- MCP handler architecture and request flow
- All MCP protocol methods (initialize, ping, tools/call, etc.)
- MCP tool definitions and patterns
- Authentication flow for MCP requests
- Error handling in MCP context
- HTTP server integration
- Testing MCP handlers

**When to use:**
- Implementing new MCP tools
- Understanding MCP protocol flow
- Debugging MCP request issues
- Adding new resources or prompts
- Modifying authentication logic

### [Authentication and Authorization](./authentication-flow.md)

**What it covers:**
- Three token types (user passwords, service tokens, user tokens)
- Token storage and hashing (SHA256)
- Token generation and validation
- User token lifecycle management
- RBAC (Role-Based Access Control) model
- Group hierarchy and recursive membership
- Privilege checking for connections and MCP items
- Superuser privileges
- Connection ownership patterns
- Authorization flow in MCP handlers

**When to use:**
- Implementing authentication features
- Adding new privilege types
- Debugging access control issues
- Understanding RBAC model
- Implementing new authorization checks
- Reviewing security implications

### [Database Patterns](./database-patterns.md)

**What it covers:**
- Connection pooling with pgx/v5
- Server vs. collector connection patterns
- Semaphore-based concurrency control
- Connection string building
- Query execution patterns (simple, multiple rows, transactions, batches)
- Connection lifecycle management
- Password encryption for monitored connections
- Performance optimization
- Troubleshooting connection issues

**When to use:**
- Implementing database access code
- Optimizing query performance
- Debugging connection pool issues
- Understanding connection management
- Implementing new probes (collector)
- Troubleshooting timeouts or leaks

### [Testing Strategy](./testing-strategy.md)

**What it covers:**
- Testing philosophy and principles
- Unit tests, integration tests, table-driven tests
- Mocking patterns (interface-based, database mocking)
- Test utilities and helpers
- Test organization and naming conventions
- Running tests (coverage, race detector, benchmarks)
- Linting and static analysis
- CI/CD integration
- Common testing pitfalls

**When to use:**
- Writing tests for new features
- Improving test coverage
- Setting up test infrastructure
- Debugging test failures
- Optimizing test performance
- Implementing benchmarks

### [Code Conventions](./code-conventions.md)

**What it covers:**
- General Go principles
- Code formatting (four-space indentation)
- Naming conventions (packages, files, variables, functions, types)
- File structure and organization
- Import grouping
- Error handling patterns
- Context usage
- Concurrency patterns (goroutines, channels, mutexes)
- Comments and documentation
- Security best practices
- Performance optimization
- Common patterns (constructor, functional options, interface segregation)
- Common mistakes to avoid
- Code review checklist

**When to use:**
- Writing new code
- Code reviews
- Refactoring existing code
- Understanding project conventions
- Resolving linting issues
- Onboarding new developers

## Quick Reference

### Common Tasks

**Adding a new MCP tool:**
1. Review [MCP Implementation](./mcp-implementation.md) - Tool definition pattern
2. Review [Authentication Flow](./authentication-flow.md) - Authorization
   checks
3. Review [Database Patterns](./database-patterns.md) - Database access
4. Review [Code Conventions](./code-conventions.md) - Naming and structure
5. Review [Testing Strategy](./testing-strategy.md) - Test patterns

**Implementing a new probe (collector):**
1. Review [Architecture Overview](./architecture-overview.md) - Probe
   architecture
2. Review [Database Patterns](./database-patterns.md) - Query patterns
3. Review collector probe examples in `/collector/src/probes/`
4. Review [Code Conventions](./code-conventions.md) - Code structure
5. Review [Testing Strategy](./testing-strategy.md) - Test patterns

**Debugging connection issues:**
1. Review [Database Patterns](./database-patterns.md) - Troubleshooting section
2. Review [Architecture Overview](./architecture-overview.md) - Connection
   pooling
3. Check pool statistics and logs
4. Review semaphore configuration (collector)

**Adding authentication/authorization:**
1. Review [Authentication Flow](./authentication-flow.md) - Complete flow
2. Review [MCP Implementation](./mcp-implementation.md) - Integration with MCP
3. Review database schema for auth tables
4. Review [Testing Strategy](./testing-strategy.md) - Test auth scenarios

**Optimizing performance:**
1. Review [Architecture Overview](./architecture-overview.md) - Performance
   optimizations
2. Review [Database Patterns](./database-patterns.md) - Performance section
3. Review [Code Conventions](./code-conventions.md) - Performance patterns
4. Run benchmarks (see [Testing Strategy](./testing-strategy.md))

## Code Examples

Each documentation file includes practical code examples. Here are some key
patterns:

### Connection Pool Usage

```go
func QueryUser(ctx context.Context, pool *pgxpool.Pool, id int) (*User, error) {
    var user User
    err := pool.QueryRow(ctx,
        "SELECT id, username FROM users WHERE id = $1",
        id).Scan(&user.ID, &user.Username)
    if err != nil {
        return nil, fmt.Errorf("failed to query user: %w", err)
    }
    return &user, nil
}
```

### Error Handling

```go
result, err := someOperation()
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}
```

### Transaction Pattern

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return fmt.Errorf("failed to begin transaction: %w", err)
}
defer func() {
    if err != nil {
        tx.Rollback(ctx)
    }
}()

// ... operations ...

if err := tx.Commit(ctx); err != nil {
    return fmt.Errorf("failed to commit: %w", err)
}
```

### Goroutine Management

```go
type Worker struct {
    wg sync.WaitGroup
    shutdownChan chan struct{}
}

func (w *Worker) Start() {
    w.wg.Add(1)
    go w.work()
}

func (w *Worker) work() {
    defer w.wg.Done()
    // ... work ...
}

func (w *Worker) Stop() {
    close(w.shutdownChan)
    w.wg.Wait()
}
```

## Best Practices Summary

1. **Security First:** Always consider security implications, validate input,
   prevent SQL injection
2. **Explicit Error Handling:** Check all errors, provide context with
   `fmt.Errorf`
3. **Resource Management:** Use `defer` for cleanup, release connections,
   close files
4. **Context Propagation:** Pass context as first parameter, respect
   cancellation
5. **Testing:** Write tests for all functionality, aim for >80% coverage
6. **Idiomatic Go:** Follow Go conventions, write readable code
7. **Documentation:** Document exported identifiers, explain complex logic
8. **Performance:** Profile before optimizing, batch operations, minimize
   allocations
9. **Concurrency:** Manage goroutines properly, avoid race conditions
10. **Code Reviews:** Use the checklist in
    [Code Conventions](./code-conventions.md)

## Contributing to Documentation

When updating these documents:

1. Maintain the copyright header
2. Update the relevant section, don't create new files
3. Include practical code examples
4. Keep line length to 79 characters where practical
5. Update this README if adding new sections
6. Test all code examples
7. Use four-space indentation (project standard)

## Additional Resources

### External Documentation

- [Effective Go](https://go.dev/doc/effective_go) - Official Go best practices
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
  - Community standards
- [pgx Documentation](https://pkg.go.dev/github.com/jackc/pgx/v5) - PostgreSQL
  driver
- [golangci-lint](https://golangci-lint.run/) - Linting tool

### Project Files

- `/DESIGN.md` - Overall project design and architecture
- `/CLAUDE.md` - Standing instructions for Claude Code
- `/server/README.md` - MCP server overview
- `/collector/README.md` - Collector overview

## Feedback

If you find errors, have suggestions, or need clarification on any topic,
please update the documentation or discuss with the team.

---

**Last Updated:** 2025-11-08

**Maintained By:** Development Team
