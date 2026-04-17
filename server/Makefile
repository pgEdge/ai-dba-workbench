#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench MCP Server Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all build openapi test lint clean vet fmt fmt-check help killall test-all

# Binary output directory (can be overridden by parent Makefile)
BIN_DIR ?= .

# Binary name
BINARY := ai-dba-server

# Get absolute path to output directory
ABS_BIN_DIR := $(abspath $(BIN_DIR))

# Default target
all: build

# Build the server binary
build:
	@echo "Building ai-dba-server..."
	@mkdir -p $(ABS_BIN_DIR)
	cd src && go build -ldflags="-s -w" -o $(ABS_BIN_DIR)/$(BINARY) ./cmd/mcp-server

# Generate static OpenAPI specification
openapi:
	@echo "Generating OpenAPI spec..."
	cd src && go run ./cmd/mcp-server -openapi ../../docs/admin-guide/api/openapi.json

# Run tests
test: killall
	@echo "Running tests..."
	cd src && go test -v ./...
	@$(MAKE) --no-print-directory killall

# Run linting
lint:
	@echo "Running linter..."
	cd src && golangci-lint run

# Run go vet (built-in static analysis)
vet:
	@echo "Running go vet..."
	cd src && go vet ./...

# Format code
fmt:
	@echo "Formatting code..."
	cd src && go fmt ./...

# Check code formatting (fails if files are not formatted)
fmt-check:
	@echo "Checking code formatting..."
	@test -z "$$(cd src && gofmt -l .)" || (echo "Files not formatted:"; cd src && gofmt -l .; exit 1)

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	cd src && go test -coverprofile=coverage.out ./...
	cd src && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: src/coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(ABS_BIN_DIR)/$(BINARY)
	rm -f src/coverage.out src/coverage.html

# Install dependencies
deps:
	@echo "Installing dependencies..."
	cd src && go mod download
	cd src && go mod tidy

# Check if everything is ready for commit
check: fmt vet test lint
	@echo "All checks passed!"

# Run all tests (fmt-check, test, coverage, lint)
test-all: fmt-check test coverage lint
	@echo "All tests passed!"

# Help target
help:
	@echo "Available targets:"
	@echo "  all       - Build the server binary (default)"
	@echo "  build     - Build the server binary"
	@echo "  openapi   - Generate static OpenAPI specification"
	@echo "  test      - Run tests"
	@echo "  lint      - Run golangci-lint"
	@echo "  vet       - Run go vet"
	@echo "  fmt       - Format code with gofmt"
	@echo "  fmt-check - Check code formatting (fails if not formatted)"
	@echo "  coverage  - Generate test coverage report"
	@echo "  test-all  - Run fmt-check, test, coverage, and lint"
	@echo "  clean     - Remove build artifacts"
	@echo "  deps      - Install/update dependencies"
	@echo "  check     - Run all checks (fmt, vet, test, lint)"
	@echo "  killall   - Kill any running server processes"
	@echo "  help      - Show this help message"
	@echo ""
	@echo "Binary is built to: $(ABS_BIN_DIR)/$(BINARY)"

# Kill all running server processes
killall:
	@pkill -9 ai-dba-server 2>/dev/null || true
	@pkill -9 -f "go run.*mcp-server" 2>/dev/null || true
