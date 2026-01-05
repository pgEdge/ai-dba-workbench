#!/bin/bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench MCP Server - Development Server Startup Script
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

set -e

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
CONFIG_FILE="${SCRIPT_DIR}/configs/dev.conf"
SERVER_DIR="${SCRIPT_DIR}/server/src"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting pgEdge AI DBA Workbench MCP Server (Development)${NC}"
echo "=========================================="

# Check if config file exists
if [ ! -f "${CONFIG_FILE}" ]; then
    echo -e "${RED}Error: Configuration file not found: ${CONFIG_FILE}${NC}"
    exit 1
fi

# Check if server source directory exists
if [ ! -d "${SERVER_DIR}" ]; then
    echo -e "${RED}Error: Server source directory not found: ${SERVER_DIR}${NC}"
    exit 1
fi

# Display configuration
echo -e "${YELLOW}Configuration:${NC}"
echo "  Config file: ${CONFIG_FILE}"
echo "  Server dir: ${SERVER_DIR}"
echo ""

# Change to server directory
cd "${SERVER_DIR}"

# Build and run the server
echo -e "${GREEN}Building and starting MCP server...${NC}"
echo ""

# Run with the config file
exec go run . -v --config="${CONFIG_FILE}"
