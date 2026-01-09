#!/bin/bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench CLI - Development Startup Script
#
# Portions copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# This script builds and starts the CLI in development mode.
# It copies the example config to bin/ if no config exists there.
#

set -e

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BIN_DIR="${SCRIPT_DIR}/bin"
CONFIG_FILE="${BIN_DIR}/ai-dba-cli.yaml"
EXAMPLE_CONFIG="${SCRIPT_DIR}/examples/ai-dba-cli.yaml"
CLI_DIR="${SCRIPT_DIR}/cli/src"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}pgEdge AI DBA Workbench CLI - Development Mode${NC}"
echo "=========================================="

# Check if CLI source directory exists
if [ ! -d "${CLI_DIR}" ]; then
    echo -e "${RED}Error: CLI source directory not found: ${CLI_DIR}${NC}"
    exit 1
fi

# Create bin directory if it doesn't exist
mkdir -p "${BIN_DIR}"

# Copy example config if no config exists
if [ ! -f "${CONFIG_FILE}" ]; then
    if [ -f "${EXAMPLE_CONFIG}" ]; then
        echo -e "${YELLOW}No config found, copying example config to ${CONFIG_FILE}${NC}"
        cp "${EXAMPLE_CONFIG}" "${CONFIG_FILE}"
        echo -e "${YELLOW}Please edit ${CONFIG_FILE} with your settings${NC}"
    else
        echo -e "${RED}Error: Example config not found: ${EXAMPLE_CONFIG}${NC}"
        exit 1
    fi
fi

# Display configuration
echo -e "${YELLOW}Configuration:${NC}"
echo "  Config file: ${CONFIG_FILE}"
echo "  Bin dir: ${BIN_DIR}"
echo ""

# Build the CLI
echo -e "${GREEN}Building CLI...${NC}"
cd "${CLI_DIR}"
go build -o "${BIN_DIR}/ai-dba-cli" ./cmd/ai-cli

# Run the CLI
echo -e "${GREEN}Starting CLI...${NC}"
echo ""

exec "${BIN_DIR}/ai-dba-cli" --config="${CONFIG_FILE}" "$@"
