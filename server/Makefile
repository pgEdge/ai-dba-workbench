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

# Minimum acceptable total statement coverage, in percent; the coverage
# target fails below it. The project floor is 90% (see CLAUDE.md, Tests);
# this default is pinned just below the total measured on 11 September
# 2026 so the gate holds the line today, and it should be raised towards
# 90 as coverage grows. Override for local experiments with
# `make coverage COVERAGE_MIN=80`.
COVERAGE_MIN ?= 55

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
# Use -p=1 to serialize per-package test binaries. Integration tests in
# internal/database, internal/api, and internal/tools share Postgres schema
# (DROP/CREATE the same tables), so running their test binaries concurrently
# races.
# -race enables the data-race detector; CI runs the same target.
test:
	@echo "Running tests..."
	cd src && go test -race -p=1 -v ./...

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
# Use -p=1 to serialize per-package test binaries (see note on the test target).
# -race enables the data-race detector; CI runs the same target.
# -v emits per-test output so this single invocation can replace the
# separate `test` target inside `test-all` and halve total test time.
coverage:
	@echo "Running tests with coverage..."
	cd src && go test -race -p=1 -v -coverprofile=coverage.out ./...
	cd src && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: src/coverage.html"
	@cd src && total=$$(go tool cover -func=coverage.out | tail -1 | awk '{gsub(/%/, "", $$3); print $$3}'); \
	echo "Total coverage: $${total}% (minimum $(COVERAGE_MIN)%)"; \
	awk -v t="$$total" -v m="$(COVERAGE_MIN)" 'BEGIN { exit (t + 0 < m + 0) ? 1 : 0 }' \
	|| { echo "ERROR: total coverage $${total}% is below the $(COVERAGE_MIN)% floor"; exit 1; }

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

# Run all tests (fmt-check, coverage, lint)
# coverage now emits verbose output and runs the full suite once, so
# the separate `test` target is no longer included; this halves the
# wall-clock time previously spent running every package twice.
test-all: fmt-check coverage lint
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
	@echo "  coverage  - Generate test coverage report and enforce COVERAGE_MIN"
	@echo "  test-all  - Run fmt-check, coverage, and lint (verbose, single pass)"
	@echo "  clean     - Remove build artifacts"
	@echo "  deps      - Install/update dependencies"
	@echo "  check     - Run all checks (fmt, vet, test, lint)"
	@echo "  killall   - SIGTERM every matching process on the host, including"
	@echo "              any dev server started from bin/; use deliberately"
	@echo "  help      - Show this help message"
	@echo ""
	@echo "Binary is built to: $(ABS_BIN_DIR)/$(BINARY)"

# Kill every running server process on the host, including any started
# from bin/ by a developer. Never wired into test targets; invoke
# deliberately.
killall:
	@pkill ai-dba-server 2>/dev/null || true
	@pkill -f "[g]o run.*mcp-server" 2>/dev/null || true
