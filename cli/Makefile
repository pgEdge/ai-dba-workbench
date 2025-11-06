#-------------------------------------------------------------------------
#
# pgEdge AI Workbench CLI Makefile
#
# Copyright (c) 2025, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all build test lint clean vet fmt help

# Default target
all: build

# Build the CLI binary
build:
	@echo "Building CLI..."
	cd src && go build -o ../ai-cli

# Run tests
test:
	@echo "Running tests..."
	cd src && go test -v ./...

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
coverage:
	@echo "Running tests with coverage..."
	cd src && go test -coverprofile=coverage.out ./...
	cd src && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: src/coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f ai-cli
	rm -f src/coverage.out src/coverage.html

# Install dependencies
deps:
	@echo "Installing dependencies..."
	cd src && go mod download
	cd src && go mod tidy

# Check if everything is ready for commit
check: fmt vet test lint
	@echo "All checks passed!"

# Help target
help:
	@echo "Available targets:"
	@echo "  all       - Build the CLI binary (default)"
	@echo "  build     - Build the CLI binary"
	@echo "  test      - Run tests"
	@echo "  lint      - Run golangci-lint"
	@echo "  vet       - Run go vet"
	@echo "  fmt       - Format code with gofmt"
	@echo "  coverage  - Generate test coverage report"
	@echo "  clean     - Remove build artifacts"
	@echo "  deps      - Install/update dependencies"
	@echo "  check     - Run all checks (fmt, vet, test, lint)"
	@echo "  help      - Show this help message"
