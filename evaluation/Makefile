#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench evaluation harness Makefile
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

.PHONY: all run clean help

# Extra flags for "ai-dba-evaluate run", e.g. EVALUATE_ARGS="-runs 5".
EVALUATE_ARGS ?=

all: help

# Bring up PostgreSQL in Docker and the server, replay the incidents and
# write results/report.md. Needs EVALUATE_TARGETS and the API keys of the
# providers it names.
run:
	./scripts/run-evaluation.sh $(EVALUATE_ARGS)

# Remove results and runtime files.
clean:
	rm -rf results .runtime

help:
	@echo "pgEdge AI DBA Workbench evaluation harness - Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  run    - Replay incidents against Ellie (requires Docker,"
	@echo "           EVALUATE_TARGETS and the providers' API keys)"
	@echo "  clean  - Remove results and runtime files"
