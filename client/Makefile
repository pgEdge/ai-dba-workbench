#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench Client Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all build test test-all coverage lint clean help

# Default target - build the client
all: build

# Build the client
build:
	@echo "Building client..."
	npm run build

# Run tests
test:
	@echo "Running tests..."
	npm run test

# Run all tests (same as test for client)
test-all: test

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	npm run test:coverage

# Run linter (TypeScript type checking)
lint:
	@echo "Running TypeScript type check..."
	npx tsc --noEmit

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf dist
	rm -rf coverage

# Show help
help:
	@echo "pgEdge AI DBA Workbench Client - Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  all              - Build the client (default)"
	@echo "  build            - Build the client"
	@echo "  test             - Run tests"
	@echo "  test-all         - Run all tests"
	@echo "  coverage         - Run tests with coverage"
	@echo "  lint             - Run TypeScript type check"
	@echo "  clean            - Clean build artifacts"
	@echo "  help             - Show this help message"
	@echo ""
	@echo "Development commands (run directly with npm):"
	@echo "  npm run dev      - Start development server"
	@echo "  npm run preview  - Preview production build"
	@echo "  npm run test:watch - Run tests in watch mode"
	@echo "  npm run test:ui  - Run tests with UI"
