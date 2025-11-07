# pgEdge AI Workbench Integration Tests

This directory contains integration tests that exercise all AI Workbench
components working together: the collector, MCP server, and CLI.

## Overview

The integration tests:

- Start a temporary PostgreSQL test database
- Run database schema migrations
- Start the collector service
- Start the MCP server
- Execute CLI commands to test functionality
- Verify results and clean up

## Prerequisites

- Go 1.21 or later
- PostgreSQL 12 or later (running and accessible)
- All AI Workbench components built (collector, server, CLI)

## Getting Started

### Build Dependencies

Before running tests, ensure all components are built:

```bash
make build-deps
```

This will build the collector, MCP server, and CLI binaries.

### Run All Tests

```bash
make test
```

### Run Tests with Coverage

```bash
make coverage
```

This generates an HTML coverage report at `coverage.html`.

### Run Specific Test

```bash
make run-test TEST=TestUserCRUD
```

## Test Structure

```
tests/
├── config/              # Test configuration files
│   └── test.conf.template  # Configuration template
├── integration/         # Integration test files
│   └── user_test.go    # User management tests
├── testutil/           # Test utilities
│   ├── cli.go         # CLI command execution utilities
│   ├── config.go      # Configuration file management
│   ├── database.go    # Database lifecycle management
│   └── services.go    # Service (collector, server) management
├── logs/              # Test execution logs (created during tests)
├── go.mod             # Go module definition
├── Makefile           # Test runner and utilities
└── README.md          # This file
```

## Test Suites

### User Management Tests

Located in [integration/user_test.go](integration/user_test.go), these tests
cover:

#### TestUserCRUD

Tests create, read, update, and delete operations for users:

- Create a new user via CLI
- List users and verify creation
- Update user details
- Verify update by listing users again
- Delete user
- Verify deletion

#### TestPasswordExpiry

Tests password expiry functionality:

- Create user with expired password
- Attempt authentication with expired password (should fail)
- Update password expiry to future date
- Authenticate again (should succeed)

#### TestSuperuserFlag

Tests is_superuser flag enforcement:

- Create regular (non-superuser) user
- Authenticate as regular user
- Verify regular user cannot:
  - Create users
  - List users
  - Update other users
  - Delete users
- Create superuser
- Authenticate as superuser
- Verify superuser can create users

## Environment Variables

- `TEST_DB_URL`: PostgreSQL connection string for test database server
  - Default: `postgres://postgres@localhost:5432/postgres`
  - Example: `postgres://user:password@hostname:5432/postgres`

- `TEST_DB_KEEP`: Keep test database after tests complete
  - Default: Not set (databases are cleaned up)
  - Set to `1` to keep database for inspection

- `SKIP_INTEGRATION_TESTS`: Skip integration tests entirely
  - Default: Not set (tests run normally)
  - Set to `1` to skip all integration tests

## Examples

### Run tests with default settings

```bash
make test
```

### Keep test database for inspection

```bash
TEST_DB_KEEP=1 make test
```

The test database name will be printed during test execution.

### Use custom PostgreSQL server

```bash
TEST_DB_URL=postgres://myuser:mypass@dbhost:5432/postgres make test
```

### Run only password expiry tests

```bash
make run-test TEST=TestPasswordExpiry
```

### Run tests with verbose output and coverage

```bash
make coverage
```

## Test Utilities

### Database Management (testutil/database.go)

- `NewTestDatabase()`: Creates a new test database with unique name
- `Close()`: Cleans up test database (unless TEST_DB_KEEP=1)
- `GetPool()`: Returns connection pool for test database

### Service Management (testutil/services.go)

- `StartCollector()`: Starts collector service with test config
- `StartMCPServer()`: Starts MCP server on test port
- `Stop()`: Gracefully stops service with timeout

### CLI Execution (testutil/cli.go)

- `NewCLIClient()`: Creates CLI client for testing
- `RunTool()`: Executes MCP tool via CLI
- `Authenticate()`: Authenticates user and returns session token
- `SetToken()`: Sets bearer token for authenticated requests

### Configuration (testutil/config.go)

- `CreateTestConfig()`: Creates test configuration from template
- `CleanupTestConfig()`: Removes test configuration file

## Logs

Test execution logs are stored in the `logs/` directory:

- `collector-<timestamp>.log`: Collector service logs
- `mcp-server-<timestamp>.log`: MCP server logs

These logs are useful for debugging test failures.

## Troubleshooting

### Tests fail with "failed to connect to PostgreSQL"

Ensure PostgreSQL is running and accessible. Check your connection string:

```bash
TEST_DB_URL=postgres://postgres@localhost:5432/postgres make test
```

### Tests fail with "binary not found"

Build all dependencies:

```bash
make build-deps
```

### Tests hang or timeout

Check the service logs in the `logs/` directory for errors. You may need to
manually clean up orphaned processes:

```bash
pkill -f collector
pkill -f mcp-server
```

### Database cleanup fails

If test databases are not being cleaned up, you can manually drop them:

```sql
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname LIKE 'ai_workbench_test_%';

DROP DATABASE ai_workbench_test_<timestamp>;
```

## Contributing

When adding new integration tests:

1. Add test functions to appropriate files in `integration/`
2. Use the test utilities in `testutil/` for common operations
3. Follow the existing test structure and naming conventions
4. Clean up test data in defer statements or cleanup functions
5. Update this README with new test descriptions

## License

This software is released under The PostgreSQL License. See
[LICENSE.md](../LICENSE.md) for details.
