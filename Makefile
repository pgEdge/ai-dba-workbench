#-------------------------------------------------------------------------
#
# pgEdge AI Workbench Top-Level Makefile
#
# Copyright (c) 2025, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all test coverage lint test-integration test-all clean killall help

# Default target - build all sub-projects
all:
	@echo "Building all sub-projects..."
	@echo "Building collector..."
	@cd collector && $(MAKE) all
	@echo "Building server..."
	@cd server && $(MAKE) all
	@echo "Building CLI..."
	@cd cli && $(MAKE) all
	@echo "All sub-projects built successfully!"

# Run tests for all sub-projects
test:
	@echo "Running tests for all sub-projects..."
	@echo "Testing collector..."
	@cd collector && $(MAKE) test
	@echo "Testing server..."
	@cd server && $(MAKE) test
	@echo "Testing CLI..."
	@cd cli && $(MAKE) test
	@echo "All sub-project tests passed!"

# Run coverage for all sub-projects
coverage:
	@echo "Running coverage for all sub-projects..."
	@echo "Running coverage for collector..."
	@cd collector && $(MAKE) coverage
	@echo "Running coverage for server..."
	@cd server && $(MAKE) coverage
	@echo "Running coverage for CLI..."
	@cd cli && $(MAKE) coverage
	@echo "Coverage reports generated for all sub-projects!"

# Run linting for all sub-projects
lint:
	@echo "Running linter for all sub-projects..."
	@echo "Linting collector..."
	@cd collector && $(MAKE) lint
	@echo "Linting server..."
	@cd server && $(MAKE) lint
	@echo "Linting CLI..."
	@cd cli && $(MAKE) lint
	@echo "Linting completed for all sub-projects!"

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	@cd tests && $(MAKE) test
	@echo "Integration tests passed!"

# Run all tests (sub-project test-all + integration tests)
test-all:
	@echo "Running all tests for sub-projects..."
	@echo "Running all tests for collector..."
	@cd collector && $(MAKE) test-all
	@echo "Running all tests for server..."
	@cd server && $(MAKE) test-all
	@echo "Running all tests for CLI..."
	@cd cli && $(MAKE) test-all
	@echo "Running integration tests..."
	@cd tests && $(MAKE) test-all
	@echo "All tests passed!"

# Clean all sub-projects and integration tests
clean:
	@echo "Cleaning all sub-projects..."
	@echo "Cleaning collector..."
	@cd collector && $(MAKE) clean
	@echo "Cleaning server..."
	@cd server && $(MAKE) clean
	@echo "Cleaning CLI..."
	@cd cli && $(MAKE) clean
	@echo "Cleaning integration tests..."
	@cd tests && $(MAKE) clean
	@echo "All sub-projects cleaned!"

# Kill all running processes
killall:
	@echo "Killing all running processes..."
	@echo "Killing collector processes..."
	@cd collector && $(MAKE) killall
	@echo "Killing server processes..."
	@cd server && $(MAKE) killall
	@echo "Killing CLI processes..."
	@cd cli && $(MAKE) killall
	@echo "Killing test processes..."
	@cd tests && $(MAKE) killall
	@echo "All processes killed!"

# Show help
help:
	@echo "pgEdge AI Workbench - Top-Level Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  all              - Build all sub-projects (default)"
	@echo "  test             - Run tests for all sub-projects"
	@echo "  coverage         - Run coverage for all sub-projects"
	@echo "  lint             - Run linter for all sub-projects"
	@echo "  test-integration - Run integration tests"
	@echo "  test-all         - Run test-all for all sub-projects and integration tests"
	@echo "  clean            - Clean all sub-projects and integration tests"
	@echo "  killall          - Kill all running processes"
	@echo "  help             - Show this help message"
	@echo ""
	@echo "Sub-projects:"
	@echo "  collector        - PostgreSQL metrics collector"
	@echo "  server           - MCP server"
	@echo "  cli              - Command-line interface"
	@echo "  tests            - Integration tests"
	@echo ""
	@echo "For sub-project specific help, run:"
	@echo "  cd <sub-project> && make help"
