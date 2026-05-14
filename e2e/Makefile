#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench E2E Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all test test-local install-browsers clean help

all: help

# Run the suite. Assumes the stack is already up.
test:
	@echo "Running Playwright tests..."
	npx playwright test

# Run the suite locally (brings up Postgres in Docker, builds, runs).
test-local:
	@echo "Running E2E with local stack bring-up..."
	./run-local.sh

# Install the Playwright browsers (with OS deps).
install-browsers:
	@echo "Installing Playwright browsers..."
	npx playwright install --with-deps

# Remove transient artefacts.
clean:
	@echo "Cleaning E2E artefacts..."
	rm -rf node_modules .auth playwright-report test-results .runtime

help:
	@echo "pgEdge AI DBA Workbench E2E - Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  test              - Run Playwright tests (stack must be up)"
	@echo "  test-local        - Bring up the stack via Docker and run tests"
	@echo "  install-browsers  - Install Playwright browsers"
	@echo "  clean             - Remove transient artefacts"
