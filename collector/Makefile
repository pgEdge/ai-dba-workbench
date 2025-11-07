#-------------------------------------------------------------------------
#
# pgEdge AI Workbench Collector Makefile
#
# Copyright (c) 2025, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all build test lint clean vet fmt help killall test-all

# Default target
all: build

# Build the collector binary
build:
	@echo "Building collector..."
	cd src && go build -o ../collector

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

# Run tests with coverage
coverage: killall
	@echo "Running tests with coverage..."
	cd src && go test -coverprofile=coverage.out ./...
	cd src && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: src/coverage.html"
	@$(MAKE) --no-print-directory killall

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f collector
	rm -f src/coverage.out src/coverage.html

# Install dependencies
deps:
	@echo "Installing dependencies..."
	cd src && go mod download
	cd src && go mod tidy

# Check if everything is ready for commit
check: fmt vet test lint
	@echo "All checks passed!"

# Run all tests (test, coverage, lint)
test-all: test coverage lint
	@echo "All tests passed!"

# Help target
help:
	@echo "Available targets:"
	@echo "  all       - Build the collector binary (default)"
	@echo "  build     - Build the collector binary"
	@echo "  test      - Run tests"
	@echo "  lint      - Run golangci-lint"
	@echo "  vet       - Run go vet"
	@echo "  fmt       - Format code with gofmt"
	@echo "  coverage  - Generate test coverage report"
	@echo "  test-all  - Run test, coverage, and lint"
	@echo "  clean     - Remove build artifacts"
	@echo "  deps      - Install/update dependencies"
	@echo "  check     - Run all checks (fmt, vet, test, lint)"
	@echo "  killall   - Kill any running collector processes"
	@echo "  help      - Show this help message"

# Kill all running collector processes
killall:
	@pkill -9 collector 2>/dev/null || true
	@pkill -9 -f "go run.*collector" 2>/dev/null || true
