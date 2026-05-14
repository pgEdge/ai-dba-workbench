#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench Top-Level Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all test coverage lint test-all test-e2e clean killall help

# Binary output directory
BIN_DIR := bin

# Default target - build all sub-projects
all:
	@echo "Building all sub-projects..."
	@mkdir -p $(BIN_DIR)
	@echo "Building collector..."
	@cd collector && $(MAKE) all BIN_DIR=../$(BIN_DIR)
	@echo "Building server..."
	@cd server && $(MAKE) all BIN_DIR=../$(BIN_DIR)
	@echo "Building alerter..."
	@cd alerter && $(MAKE) all BIN_DIR=../$(BIN_DIR)
	@echo "All sub-projects built successfully!"
	@echo "Binaries available in $(BIN_DIR)/"

# Run tests for all sub-projects
test:
	@echo "Running tests for all sub-projects..."
	@echo "Testing collector..."
	@cd collector && $(MAKE) test
	@echo "Testing server..."
	@cd server && $(MAKE) test
	@echo "Testing alerter..."
	@cd alerter && $(MAKE) test
	@echo "Testing client..."
	@cd client && $(MAKE) test
	@echo "All sub-project tests passed!"

# Run coverage for all sub-projects
coverage:
	@echo "Running coverage for all sub-projects..."
	@echo "Running coverage for collector..."
	@cd collector && $(MAKE) coverage
	@echo "Running coverage for server..."
	@cd server && $(MAKE) coverage
	@echo "Running coverage for alerter..."
	@cd alerter && $(MAKE) coverage
	@echo "Running coverage for client..."
	@cd client && $(MAKE) coverage
	@echo "Coverage reports generated for all sub-projects!"

# Run linting for all sub-projects
lint:
	@echo "Running linter for all sub-projects..."
	@echo "Linting collector..."
	@cd collector && $(MAKE) lint
	@echo "Linting server..."
	@cd server && $(MAKE) lint
	@echo "Linting alerter..."
	@cd alerter && $(MAKE) lint
	@echo "Linting client..."
	@cd client && $(MAKE) lint
	@echo "Linting completed for all sub-projects!"

# Run all tests (sub-project test-all)
test-all:
	@echo "Running all tests for sub-projects..."
	@echo "Running all tests for collector..."
	@cd collector && $(MAKE) test-all
	@echo "Running all tests for server..."
	@cd server && $(MAKE) test-all
	@echo "Running all tests for alerter..."
	@cd alerter && $(MAKE) test-all
	@echo "Running all tests for client..."
	@cd client && $(MAKE) test-all
	@echo "All tests passed!"

# Run E2E smoke tests (requires Docker for Postgres)
# Intentionally NOT a dependency of test-all: E2E is slow and needs Docker,
# so it is kept separate from the default per-sub-project test sweep.
test-e2e:
	@echo "Running E2E smoke tests (requires Docker for Postgres)..."
	$(MAKE) -C e2e test-local

# Clean all sub-projects
clean:
	@echo "Cleaning all sub-projects..."
	@echo "Cleaning collector..."
	@cd collector && $(MAKE) clean BIN_DIR=../$(BIN_DIR)
	@echo "Cleaning server..."
	@cd server && $(MAKE) clean BIN_DIR=../$(BIN_DIR)
	@echo "Cleaning alerter..."
	@cd alerter && $(MAKE) clean BIN_DIR=../$(BIN_DIR)
	@echo "Cleaning client..."
	@cd client && $(MAKE) clean
	@echo "All sub-projects cleaned!"

# Kill all running processes
killall:
	@echo "Killing all running processes..."
	@echo "Killing collector processes..."
	@cd collector && $(MAKE) killall
	@echo "Killing server processes..."
	@cd server && $(MAKE) killall
	@echo "Killing alerter processes..."
	@cd alerter && $(MAKE) killall
	@echo "All processes killed!"

# Show help
help:
	@echo "pgEdge AI DBA Workbench - Top-Level Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  all              - Build all sub-projects (default)"
	@echo "  test             - Run tests for all sub-projects"
	@echo "  coverage         - Run coverage for all sub-projects"
	@echo "  lint             - Run linter for all sub-projects"
	@echo "  test-all         - Run test-all for all sub-projects"
	@echo "  test-e2e         - Run E2E smoke tests (requires Docker)"
	@echo "  clean            - Clean all sub-projects"
	@echo "  killall          - Kill all running processes"
	@echo "  help             - Show this help message"
	@echo ""
	@echo "Sub-projects:"
	@echo "  collector        - PostgreSQL metrics collector"
	@echo "  server           - MCP server"
	@echo "  alerter          - Alert monitoring service"
	@echo "  client           - Web client application"
	@echo ""
	@echo "Binaries are built to the bin/ directory."
	@echo ""
	@echo "For sub-project specific help, run:"
	@echo "  cd <sub-project> && make help"
