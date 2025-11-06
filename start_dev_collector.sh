#!/bin/bash
#-------------------------------------------------------------------------
#
# pgEdge AI Workbench Collector - Development Server Startup Script
#
# Copyright (c) 2025, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------

set -e

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
CONFIG_FILE="${SCRIPT_DIR}/configs/dev.conf"
COLLECTOR_DIR="${SCRIPT_DIR}/collector/src"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting pgEdge AI Workbench Collector (Development)${NC}"
echo "=========================================="

# Check if config file exists
if [ ! -f "${CONFIG_FILE}" ]; then
    echo -e "${RED}Error: Configuration file not found: ${CONFIG_FILE}${NC}"
    exit 1
fi

# Check if collector source directory exists
if [ ! -d "${COLLECTOR_DIR}" ]; then
    echo -e "${RED}Error: Collector source directory not found: ${COLLECTOR_DIR}${NC}"
    exit 1
fi

# Display configuration
echo -e "${YELLOW}Configuration:${NC}"
echo "  Config file: ${CONFIG_FILE}"
echo "  Collector dir: ${COLLECTOR_DIR}"
echo ""

# Change to collector directory
cd "${COLLECTOR_DIR}"

# Build and run the collector
echo -e "${GREEN}Building and starting collector...${NC}"
echo ""

# Run with the config file
exec go run . -v --config="${CONFIG_FILE}"
