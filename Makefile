#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench Top-Level Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all test coverage lint test-all clean killall help

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
	@echo "Building cli..."
	@cd cli && $(MAKE) all BIN_DIR=../$(BIN_DIR)
	@echo "All sub-projects built successfully!"
	@echo "Binaries available in $(BIN_DIR)/"

# Run tests for all sub-projects
test:
	@echo "Running tests for all sub-projects..."
	@echo "Testing collector..."
	@cd collector && $(MAKE) test
	@echo "Testing server..."
	@cd server && $(MAKE) test
	@echo "Testing cli..."
	@cd cli && $(MAKE) test
	@echo "All sub-project tests passed!"

# Run coverage for all sub-projects
coverage:
	@echo "Running coverage for all sub-projects..."
	@echo "Running coverage for collector..."
	@cd collector && $(MAKE) coverage
	@echo "Running coverage for server..."
	@cd server && $(MAKE) coverage
	@echo "Running coverage for cli..."
	@cd cli && $(MAKE) coverage
	@echo "Coverage reports generated for all sub-projects!"

# Run linting for all sub-projects
lint:
	@echo "Running linter for all sub-projects..."
	@echo "Linting collector..."
	@cd collector && $(MAKE) lint
	@echo "Linting server..."
	@cd server && $(MAKE) lint
	@echo "Linting cli..."
	@cd cli && $(MAKE) lint
	@echo "Linting completed for all sub-projects!"

# Run all tests (sub-project test-all)
test-all:
	@echo "Running all tests for sub-projects..."
	@echo "Running all tests for collector..."
	@cd collector && $(MAKE) test-all
	@echo "Running all tests for server..."
	@cd server && $(MAKE) test-all
	@echo "Running all tests for cli..."
	@cd cli && $(MAKE) test-all
	@echo "All tests passed!"

# Clean all sub-projects
clean:
	@echo "Cleaning all sub-projects..."
	@echo "Cleaning collector..."
	@cd collector && $(MAKE) clean BIN_DIR=../$(BIN_DIR)
	@echo "Cleaning server..."
	@cd server && $(MAKE) clean BIN_DIR=../$(BIN_DIR)
	@echo "Cleaning cli..."
	@cd cli && $(MAKE) clean BIN_DIR=../$(BIN_DIR)
	@echo "Removing bin directory..."
	@rm -rf $(BIN_DIR)
	@echo "All sub-projects cleaned!"

# Kill all running processes
killall:
	@echo "Killing all running processes..."
	@echo "Killing collector processes..."
	@cd collector && $(MAKE) killall
	@echo "Killing server processes..."
	@cd server && $(MAKE) killall
	@echo "Killing cli processes..."
	@cd cli && $(MAKE) killall
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
	@echo "  clean            - Clean all sub-projects"
	@echo "  killall          - Kill all running processes"
	@echo "  help             - Show this help message"
	@echo ""
	@echo "Sub-projects:"
	@echo "  collector        - PostgreSQL metrics collector"
	@echo "  server           - MCP server"
	@echo "  cli              - AI CLI client"
	@echo ""
	@echo "Binaries are built to the bin/ directory."
	@echo ""
	@echo "For sub-project specific help, run:"
	@echo "  cd <sub-project> && make help"
