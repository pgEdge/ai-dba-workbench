#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench Shared Package Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all test lint vet fmt fmt-check test-all clean help

# pkg is a library module with no command, so there is no build target
# and no binary; `all` is an alias for the full check sweep.
all: test-all

# Run tests
# -race enables the data-race detector; CI runs the same target.
test:
	@echo "Running tests..."
	go test -race -v ./...

# Run linting
lint:
	@echo "Running linter..."
	golangci-lint run

# Run go vet (built-in static analysis)
vet:
	@echo "Running go vet..."
	go vet ./...

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Check code formatting (fails if files are not formatted)
fmt-check:
	@echo "Checking code formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1)

# Run all checks (formatting, tests, linting)
test-all: fmt-check test lint
	@echo "All tests passed!"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f coverage.out coverage.html

# Help target
help:
	@echo "Available targets:"
	@echo "  all       - Run the full check sweep (default, same as test-all)"
	@echo "  test      - Run tests"
	@echo "  lint      - Run golangci-lint"
	@echo "  vet       - Run go vet"
	@echo "  fmt       - Format code with gofmt"
	@echo "  fmt-check - Check code formatting (fails if not formatted)"
	@echo "  test-all  - Run fmt-check, test and lint"
	@echo "  clean     - Remove build artifacts"
	@echo "  help      - Show this help message"
