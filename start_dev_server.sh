#!/bin/bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench Server - Development Startup Script
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# This script builds and starts the MCP server in development mode.
# It copies the example config to bin/ if no config exists there.
#

set -e

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BIN_DIR="${SCRIPT_DIR}/bin"
CONFIG_FILE="${BIN_DIR}/ai-dba-server.yaml"
EXAMPLE_CONFIG="${SCRIPT_DIR}/examples/ai-dba-server.yaml"
SERVER_DIR="${SCRIPT_DIR}/server/src"
TRACE_FILE="${BIN_DIR}/mcp-trace.jsonl"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}pgEdge AI DBA Workbench Server - Development Mode${NC}"
echo "=========================================="

# Check if server source directory exists
if [ ! -d "${SERVER_DIR}" ]; then
    echo -e "${RED}Error: Server source directory not found: ${SERVER_DIR}${NC}"
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
echo "  Trace file: ${TRACE_FILE}"
echo "  Bin dir: ${BIN_DIR}"
echo ""

# Check if rebuild is needed
SERVER_BINARY="${BIN_DIR}/ai-dba-server"
NEEDS_REBUILD=false

if [ ! -f "${SERVER_BINARY}" ]; then
    NEEDS_REBUILD=true
    echo -e "${YELLOW}Server binary not found, building...${NC}"
else
    # Check if any Go source files are newer than the binary
    NEWEST_SOURCE=$(find "${SCRIPT_DIR}/server/src" -name "*.go" -newer "${SERVER_BINARY}" 2>/dev/null | head -1)
    if [ -n "${NEWEST_SOURCE}" ]; then
        NEEDS_REBUILD=true
        echo -e "${YELLOW}Source files changed, rebuilding...${NC}"
    fi
fi

# Build the server if needed
if [ "${NEEDS_REBUILD}" = true ]; then
    echo -e "${GREEN}Building server...${NC}"
    cd "${SERVER_DIR}"
    go build -o "${SERVER_BINARY}" ./cmd/mcp-server
else
    echo -e "${GREEN}Server binary is up to date${NC}"
fi

# Run the server
echo -e "${GREEN}Starting server...${NC}"
echo ""

exec "${BIN_DIR}/ai-dba-server" --config="${CONFIG_FILE}" --trace-file="${TRACE_FILE}"
