# Testing Expert Knowledge Base

This directory contains comprehensive documentation about testing infrastructure, patterns, and best practices for the pgEdge AI Workbench project.

## Purpose

This knowledge base provides:

1. **Testing Strategy**: Overview of testing approach across the project
2. **Test Patterns**: Proven patterns for unit and integration tests
3. **Test Utilities**: Documentation of available test helpers
4. **Practical Guides**: Step-by-step instructions for writing tests
5. **Quality Standards**: Coverage and linting requirements

## Documents

### [Testing Overview](testing-overview.md)
**Start here for a high-level understanding**

- Project structure and test organization
- Test types (unit, integration, E2E)
- Test execution commands
- Test frameworks and tools
- Coverage goals
- CI/CD integration
- Best practices summary

### [Unit Testing](unit-testing.md)
**Detailed guide to unit testing patterns**

- Unit test principles
- File organization
- GoLang test patterns
- React test patterns (planned)
- Mock strategies
- Testing private functions
- Performance testing (benchmarks)
- Common pitfalls

### [Integration Testing](integration-testing.md)
**Comprehensive integration testing guide**

- Integration test structure
- Test utilities (database, services, CLI, config)
- Test environment setup
- Example integration tests
- Sub-project integration tests
- Running integration tests
- Best practices
- Debugging techniques

### [Test Utilities](test-utilities.md)
**Reference for available test utilities**

- Database utilities (`testutil/database.go`)
- Service management (`testutil/services.go`)
- CLI execution (`testutil/cli.go`)
- Configuration management (`testutil/config.go`)
- Common utilities (`testutil/common.go`)
- Usage patterns
- Error handling
- Best practices

### [Database Testing](database-testing.md)
**Specialized guide for database testing**

- Test database lifecycle
- Environment variables
- Testing patterns (skip, setup, cleanup)
- Testing transactions
- Testing concurrent access
- Testing schema migrations
- Testing query results
- Connection pooling
- Debugging database tests

### [Coverage and Quality](coverage-and-quality.md)
**Code coverage and quality checks**

- Coverage measurement (GoLang and React)
- Running coverage reports
- Viewing coverage (terminal and HTML)
- Coverage goals by component
- Linting with golangci-lint
- Enabled linters and their checks
- Go vet and go fmt
- CI/CD quality checks
- Coverage thresholds
- Best practices

### [Writing Tests](writing-tests.md)
**Practical step-by-step guide**

- Quick start checklist
- Choosing test type
- Writing unit tests (step-by-step)
- Writing integration tests (step-by-step)
- Testing security aspects
- Common patterns
- Running and debugging tests
- Complete examples
- Best practices summary

## Quick Reference

### Running Tests

```bash
# Collector
cd collector
make test           # Run unit tests
make coverage       # Tests with coverage
make lint          # Run linter
make test-all      # Test + coverage + lint

# Server
cd server
make test           # Run unit tests
make coverage       # Tests with coverage
make lint          # Run linter
make test-all      # Test + coverage + lint

# Integration Tests
cd tests
make test           # Run integration tests
make coverage       # Tests with coverage
make run-test TEST=TestName  # Run specific test
make build-deps     # Build required binaries
```

### Environment Variables

```bash
# PostgreSQL connection for tests
export TEST_AI_WORKBENCH_SERVER=postgres://postgres@localhost:5432/postgres

# Keep test database for inspection
export TEST_AI_WORKBENCH_KEEP_DB=1

# Skip database-dependent tests
export SKIP_DB_TESTS=1

# Skip integration tests
export SKIP_INTEGRATION_TESTS=1
```

### Test File Locations

**Unit Tests**:
- GoLang: Co-located with source (`*_test.go`)
- React: `/client/tests/unit/` or co-located

**Integration Tests**:
- Cross-component: `/tests/integration/`
- Sub-project: `<project>/src/integration/`

**Test Utilities**:
- `/tests/testutil/`

## Test Writing Workflow

1. **Identify test type** (unit vs integration)
2. **Create test file** in appropriate location
3. **Write test cases**:
   - Success paths
   - Error paths
   - Edge cases
   - Security aspects
4. **Run tests locally**: `make test`
5. **Check coverage**: `make coverage`
6. **Run linter**: `make lint`
7. **Fix issues**
8. **Commit with feature code**

## Coverage Goals

- **Overall**: >80%
- **Critical Components**: >90%
  - Database operations
  - User management
  - Connection management
- **Security Functions**: 100%
  - Authentication/authorization
  - Encryption
  - Input validation

## Key Principles

1. **Write tests with features** - Not as an afterthought
2. **Test behavior, not implementation** - Focus on what, not how
3. **Test success and failure** - Don't just test happy path
4. **Test security thoroughly** - Validation, authorization, isolation
5. **Keep tests independent** - No shared state
6. **Clean up resources** - Use defer statements
7. **Use meaningful names** - Describe what is being tested
8. **Run tests before committing** - Catch issues early

## Testing Stack

### GoLang
- **Testing Framework**: Standard library `testing` package
- **Assertions**: `testify/assert` and `testify/require`
- **Mocking**: Interface-based (manual mocks)
- **Database**: `pgx/v5` with connection pooling
- **Coverage**: `go test -cover` and `go tool cover`
- **Linting**: `golangci-lint`

### React (Planned)
- **Testing Framework**: Jest
- **Component Testing**: React Testing Library
- **Mocking**: MSW (Mock Service Worker) for APIs
- **Coverage**: Jest coverage
- **Linting**: ESLint

## CI/CD

Tests run automatically on:
- Pull requests
- Commits to main branch

**Workflows**:
- `.github/workflows/test-collector.yml`
- `.github/workflows/test-server.yml`
- `.github/workflows/test-integration.yml`
- `.github/workflows/test-cli.yml`

**Artifacts**:
- Coverage reports (HTML)
- Retention: 30 days

## Getting Help

### Documentation
- Start with `testing-overview.md` for big picture
- Use `writing-tests.md` for practical guidance
- Refer to specific guides for deep dives

### Debugging
- Check service logs in `tests/logs/`
- Use `TEST_AI_WORKBENCH_KEEP_DB=1` to inspect test database
- Run tests with `-v` flag for verbose output
- Use `make killall` to clean up orphaned processes

### Common Issues
- **Binary not found**: Run `make build-deps`
- **Database connection failed**: Check PostgreSQL is running
- **Port in use**: Run `make killall` to clean up
- **Tests hang**: Check service logs for errors

## Additional Resources

- **Project Documentation**: `/docs/`
- **Collector Testing**: `/docs/collector/testing.md`
- **Integration Tests**: `/tests/README.md`
- **Project Instructions**: `/CLAUDE.md`
- **Design Document**: `/DESIGN.md`

## Contributing

When adding new tests:

1. Follow patterns documented here
2. Ensure >80% coverage for new code
3. Test security aspects thoroughly
4. Update documentation if adding new patterns
5. Run full test suite before committing

## Document Updates

Last Updated: 2025-11-08

These documents should be updated when:
- New testing patterns are introduced
- Test utilities are added or changed
- Coverage requirements change
- New testing tools are adopted

---

**Note**: This knowledge base is for the Testing Framework Architect agent and serves as a comprehensive reference for all testing-related guidance in the AI Workbench project.
