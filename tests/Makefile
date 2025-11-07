#-------------------------------------------------------------------------
#
# pgEdge AI Workbench Integration Tests Makefile
#
# Copyright (c) 2025, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all test clean build-deps logs help

# Default target
all: test

# Run integration tests
test: build-deps
	@echo "Running integration tests..."
	@mkdir -p logs config
	@go test -v ./integration/... -timeout 30m

# Run tests with coverage
coverage: build-deps
	@echo "Running integration tests with coverage..."
	@mkdir -p logs config
	@go test -v -coverprofile=coverage.out ./integration/... -timeout 30m
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Build all required binaries
build-deps:
	@echo "Building collector..."
	@cd ../collector && make build
	@echo "Building MCP server..."
	@cd ../server && make build
	@echo "Building CLI..."
	@cd ../cli && make build

# Run specific test
run-test:
	@echo "Running specific test: $(TEST)"
	@mkdir -p logs config
	@go test -v ./integration/... -run $(TEST) -timeout 30m

# Clean up test artifacts
clean:
	@echo "Cleaning up test artifacts..."
	@rm -f coverage.out coverage.html
	@rm -f config/test-*.conf
	@rm -f logs/*.log
	@go clean -testcache

# Create necessary directories
logs:
	@mkdir -p logs config

# Show help
help:
	@echo "pgEdge AI Workbench Integration Tests"
	@echo ""
	@echo "Usage:"
	@echo "  make test         - Run all integration tests"
	@echo "  make coverage     - Run tests with coverage report"
	@echo "  make run-test     - Run specific test (e.g., make run-test TEST=TestUserCRUD)"
	@echo "  make build-deps   - Build all required binaries (collector, server, CLI)"
	@echo "  make clean        - Clean up test artifacts"
	@echo "  make help         - Show this help message"
	@echo ""
	@echo "Environment variables:"
	@echo "  TEST_DB_URL            - PostgreSQL connection string (default: postgres://postgres@localhost:5432/postgres)"
	@echo "  TEST_DB_KEEP=1         - Keep test database after tests complete"
	@echo "  SKIP_INTEGRATION_TESTS=1 - Skip integration tests"
	@echo ""
	@echo "Examples:"
	@echo "  make test                                    # Run all tests with default settings"
	@echo "  TEST_DB_KEEP=1 make test                     # Run tests and keep test database"
	@echo "  TEST_DB_URL=postgres://user:pass@host/db make test  # Use custom database"
	@echo "  make run-test TEST=TestUserCRUD              # Run only TestUserCRUD test"
