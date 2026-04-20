#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench Client Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all build test test-all coverage lint format clean help

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

# Run all tests (test + lint)
test-all: test lint

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	npm run test:coverage

# Run linter (ESLint)
lint:
	@echo "Running ESLint..."
	npm run lint

# Format code with Prettier
format:
	@echo "Formatting code with Prettier..."
	npm run format

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
	@echo "  test-all         - Run all tests and lint"
	@echo "  coverage         - Run tests with coverage"
	@echo "  lint             - Run ESLint"
	@echo "  format           - Format code with Prettier"
	@echo "  clean            - Clean build artifacts"
	@echo "  help             - Show this help message"
	@echo ""
	@echo "Development commands (run directly with npm):"
	@echo "  npm run dev      - Start development server"
	@echo "  npm run preview  - Preview production build"
	@echo "  npm run test:watch - Run tests in watch mode"
	@echo "  npm run test:ui  - Run tests with UI"
	@echo "  npm run lint:fix - Run ESLint with auto-fix"
	@echo "  npm run format:check - Check formatting without changes"
