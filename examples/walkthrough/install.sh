#!/usr/bin/env bash
set -euo pipefail

# Entrypoint for curl-pipe installation:
#   curl -fsSL https://raw.githubusercontent.com/pgEdge/ai-dba-workbench/main/examples/walkthrough/install.sh | bash

# --- Codespaces check ---

if [ -n "${CODESPACES:-}" ]; then
  echo ""
  echo "  Running in Codespaces — everything is already set up."
  echo "  Continue with the walkthrough or run: bash examples/walkthrough/guide.sh"
  echo ""
  exit 0
fi

WORK_DIR="${WALKTHROUGH_DIR:-pgedge-workbench-walkthrough}"
BRANCH="${PGEDGE_BRANCH:-main}"
BASE_URL="https://raw.githubusercontent.com/pgEdge/ai-dba-workbench/${BRANCH}"

# --- Header ---

echo ""
echo "  pgEdge AI DBA Workbench Walkthrough"
echo "  ===================================="
echo ""

# --- Download walkthrough files (mirrors repo layout) ---

echo "  Downloading walkthrough files..."

mkdir -p "$WORK_DIR/examples/walkthrough/config"
mkdir -p "$WORK_DIR/examples/walkthrough/helper"
mkdir -p "$WORK_DIR/examples/walkthrough/nginx/walkthrough/images"
mkdir -p "$WORK_DIR/examples/walkthrough/seed"
mkdir -p "$WORK_DIR/examples/walkthrough/secret"

FILES=(
  examples/walkthrough/docker-compose.yml
  examples/walkthrough/guide.sh
  examples/walkthrough/runner.sh
  examples/walkthrough/setup.sh
  examples/walkthrough/config/ai-dba-server.yaml
  examples/walkthrough/config/ai-dba-collector.yaml
  examples/walkthrough/config/ai-dba-alerter.yaml
  examples/walkthrough/helper/Dockerfile
  examples/walkthrough/helper/server.py
  examples/walkthrough/nginx/nginx.conf
  examples/walkthrough/nginx/walkthrough/driver.min.css
  examples/walkthrough/nginx/walkthrough/driver.min.js
  examples/walkthrough/nginx/walkthrough/loader.js
  examples/walkthrough/nginx/walkthrough/tour.css
  examples/walkthrough/seed/demo-schema.sql
  examples/walkthrough/seed/workload.sh
)

FAILED=0
for file in "${FILES[@]}"; do
  if ! curl -fsSL "$BASE_URL/$file" -o "$WORK_DIR/$file"; then
    echo "  Warning: failed to download $file" >&2
    FAILED=$((FAILED + 1))
  fi
done

if [[ $FAILED -gt 0 ]]; then
  echo ""
  echo "  Error: $FAILED file(s) failed to download." >&2
  echo "  Check your network connection and try again." >&2
  echo ""
  exit 1
fi

chmod +x "$WORK_DIR/examples/walkthrough/guide.sh" \
         "$WORK_DIR/examples/walkthrough/setup.sh" \
         "$WORK_DIR/examples/walkthrough/seed/workload.sh"

echo "  Downloaded ${#FILES[@]} files."
echo ""

# --- Download Dockerfiles and source needed for build ---
# The compose file builds from ../../ context, so we need the
# project Dockerfiles. For the curl-pipe install these are fetched
# individually.

echo "  Downloading build files..."

BUILD_DIRS=(
  server
  collector
  alerter
  client
)

for dir in "${BUILD_DIRS[@]}"; do
  mkdir -p "$WORK_DIR/$dir"
  if ! curl -fsSL "$BASE_URL/$dir/Dockerfile" -o "$WORK_DIR/$dir/Dockerfile"; then
    echo "  Warning: failed to download $dir/Dockerfile" >&2
  fi
done

echo "  Done."
echo ""

cd "$WORK_DIR"

# --- Run the interactive guide ---

echo "  Starting the interactive walkthrough..."
echo ""
exec bash examples/walkthrough/guide.sh
