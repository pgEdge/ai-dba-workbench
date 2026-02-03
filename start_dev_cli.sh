#!/bin/bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench CLI - Development Startup Script
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# This script builds and starts both the MCP server and CLI in development
# mode. It copies the example configs to bin/ if no configs exist there.
#

set -e

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BIN_DIR="${SCRIPT_DIR}/bin"

# CLI configuration
CLI_CONFIG="${BIN_DIR}/ai-dba-cli.yaml"
CLI_EXAMPLE_CONFIG="${SCRIPT_DIR}/examples/ai-dba-cli.yaml"
CLI_DIR="${SCRIPT_DIR}/cli/src"
CLI_BIN="${BIN_DIR}/ai-dba-cli"

# Server configuration
SERVER_CONFIG="${BIN_DIR}/ai-dba-server.yaml"
SERVER_EXAMPLE_CONFIG="${SCRIPT_DIR}/examples/ai-dba-server.yaml"
SERVER_SRC_DIR="${SCRIPT_DIR}/server/src"
SERVER_BIN="${BIN_DIR}/ai-dba-server"
SERVER_LOG="/tmp/ai-dba-server.log"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# PID for cleanup
SERVER_PID=""

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Shutting down services...${NC}"

    if [ ! -z "$SERVER_PID" ]; then
        echo "Stopping MCP server (PID: $SERVER_PID)..."
        kill $SERVER_PID 2>/dev/null || true
    fi

    echo -e "${GREEN}Cleanup complete${NC}"
}

# Set up trap for cleanup
trap cleanup EXIT INT TERM

echo -e "${GREEN}pgEdge AI DBA Workbench CLI - Development Mode${NC}"
echo "=========================================="

# Check if CLI source directory exists
if [ ! -d "${CLI_DIR}" ]; then
    echo -e "${RED}Error: CLI source directory not found: ${CLI_DIR}${NC}"
    exit 1
fi

# Check if server source directory exists
if [ ! -d "${SERVER_SRC_DIR}" ]; then
    echo -e "${RED}Error: Server source directory not found: ${SERVER_SRC_DIR}${NC}"
    exit 1
fi

# Create bin directory if it doesn't exist
mkdir -p "${BIN_DIR}"

# Copy example CLI config if no config exists
if [ ! -f "${CLI_CONFIG}" ]; then
    if [ -f "${CLI_EXAMPLE_CONFIG}" ]; then
        echo -e "${YELLOW}No CLI config found, copying example config to ${CLI_CONFIG}${NC}"
        cp "${CLI_EXAMPLE_CONFIG}" "${CLI_CONFIG}"
        echo -e "${YELLOW}Please edit ${CLI_CONFIG} with your settings${NC}"
    else
        echo -e "${RED}Error: Example CLI config not found: ${CLI_EXAMPLE_CONFIG}${NC}"
        exit 1
    fi
fi

# Copy example server config if no config exists
if [ ! -f "${SERVER_CONFIG}" ]; then
    if [ -f "${SERVER_EXAMPLE_CONFIG}" ]; then
        echo -e "${YELLOW}No server config found, copying example config to ${SERVER_CONFIG}${NC}"
        cp "${SERVER_EXAMPLE_CONFIG}" "${SERVER_CONFIG}"
        echo -e "${YELLOW}Please edit ${SERVER_CONFIG} with your settings${NC}"
    else
        echo -e "${RED}Error: Example server config not found: ${SERVER_EXAMPLE_CONFIG}${NC}"
        exit 1
    fi
fi

# Display configuration
echo -e "${YELLOW}Configuration:${NC}"
echo "  CLI config: ${CLI_CONFIG}"
echo "  Server config: ${SERVER_CONFIG}"
echo "  Bin dir: ${BIN_DIR}"
echo ""

# Build or rebuild server binary if needed
if [ ! -f "${SERVER_BIN}" ]; then
    echo -e "${BLUE}Building MCP server binary...${NC}"
    cd "${SERVER_SRC_DIR}"
    go build -o "${SERVER_BIN}" ./cmd/mcp-server
    if [ $? -ne 0 ]; then
        echo -e "${RED}Error: Failed to build MCP server${NC}"
        exit 1
    fi
    cd "${SCRIPT_DIR}"
else
    echo -e "${BLUE}Checking if server binary needs rebuilding...${NC}"
    # Check if any Go source files are newer than the binary
    if [ -n "$(find "${SERVER_SRC_DIR}" -name "*.go" -newer "${SERVER_BIN}" 2>/dev/null)" ]; then
        echo -e "${BLUE}Source files changed, rebuilding MCP server...${NC}"
        cd "${SERVER_SRC_DIR}"
        go build -o "${SERVER_BIN}" ./cmd/mcp-server
        if [ $? -ne 0 ]; then
            echo -e "${RED}Error: Failed to build MCP server${NC}"
            exit 1
        fi
        cd "${SCRIPT_DIR}"
    else
        echo -e "${GREEN}Server binary is up to date${NC}"
    fi
fi

# Build the CLI
echo -e "${BLUE}Building CLI...${NC}"
cd "${CLI_DIR}"
go build -o "${CLI_BIN}" ./cmd/ai-cli
if [ $? -ne 0 ]; then
    echo -e "${RED}Error: Failed to build CLI${NC}"
    exit 1
fi
cd "${SCRIPT_DIR}"

echo ""
echo -e "${BLUE}+------------------------------------------------------------+${NC}"
echo -e "${BLUE}|     pgEdge AI DBA Workbench CLI Development Startup        |${NC}"
echo -e "${BLUE}+------------------------------------------------------------+${NC}"
echo ""

# Start MCP server
echo -e "${GREEN}[1/2] Starting MCP Server...${NC}"
cd "${BIN_DIR}"
"${SERVER_BIN}" --config "${SERVER_CONFIG}" > "${SERVER_LOG}" 2>&1 &
SERVER_PID=$!
cd "${SCRIPT_DIR}"

echo "      PID: $SERVER_PID"
echo "      Config: ${SERVER_CONFIG}"
echo "      Log: ${SERVER_LOG}"

# Wait a moment for process to stabilize
sleep 1

# Check if MCP server process is still running (catch immediate failures like port conflicts)
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo -e "${RED}Error: MCP Server process exited immediately${NC}"
    echo "This usually means the port is already in use or there's a configuration error."
    echo "Check the log file: ${SERVER_LOG}"
    tail -20 "${SERVER_LOG}"
    exit 1
fi

# Wait for MCP server to be ready
echo -e "${GREEN}Waiting for MCP Server to be ready...${NC}"
MAX_RETRIES=30
RETRY_COUNT=0
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if curl -s http://localhost:8080/health > /dev/null 2>&1; then
        echo "      MCP Server is ready!"
        break
    fi

    # Check if process is still running
    if ! kill -0 $SERVER_PID 2>/dev/null; then
        echo -e "${RED}Error: MCP Server process died during startup${NC}"
        echo "Check the log file: ${SERVER_LOG}"
        tail -20 "${SERVER_LOG}"
        exit 1
    fi

    RETRY_COUNT=$((RETRY_COUNT + 1))
    sleep 1
done

if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
    echo -e "${RED}Error: MCP Server failed to start within 30 seconds${NC}"
    echo "Check the log file: ${SERVER_LOG}"
    tail -20 "${SERVER_LOG}"
    exit 1
fi

# Start the CLI
echo -e "${GREEN}[2/2] Starting CLI...${NC}"
echo ""
echo -e "${GREEN}+------------------------------------------------------------+${NC}"
echo -e "${GREEN}|  AI DBA Workbench CLI is starting!                         |${NC}"
echo -e "${GREEN}+------------------------------------------------------------+${NC}"
echo ""
echo -e "${BLUE}Services:${NC}"
echo "  - MCP Server: http://localhost:8080"
echo ""
echo -e "${BLUE}Logs:${NC}"
echo "  - MCP Server: ${SERVER_LOG}"
echo ""
echo -e "${YELLOW}Type 'exit' or press Ctrl+D to quit${NC}"
echo ""

# Run the CLI (this will block until the CLI exits)
# Disable the EXIT trap temporarily so we can handle cleanup manually
trap - EXIT
"${CLI_BIN}" --config="${CLI_CONFIG}" "$@"
CLI_EXIT_CODE=$?

# Clean up the server
cleanup

exit $CLI_EXIT_CODE
