#!/usr/bin/env bash
#
# One-command setup and start for E2E tests
#
# Usage:
#   bash client/tests/e2e/start.sh [postgres_version]
#
# Examples:
#   bash client/tests/e2e/start.sh          # PG 17 (default)
#   bash client/tests/e2e/start.sh 16       # PG 16
#   bash client/tests/e2e/start.sh 18       # PG 18
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
POSTGRES_VERSION="${1:-17}"
COMPOSE_FILE="${SCRIPT_DIR}/docker/docker-compose.yml"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}E2E Test Suite - Setup & Start${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Step 1: Generate secrets
echo -e "${BLUE}Step 1: Generating secrets...${NC}"
bash "${SCRIPT_DIR}/scripts/setup-secrets.sh"
echo -e "${GREEN}✓ Secrets generated${NC}"
echo ""

# Step 2: Set PostgreSQL image based on version
echo -e "${BLUE}Step 2: Setting up PostgreSQL ${POSTGRES_VERSION}...${NC}"
case "${POSTGRES_VERSION}" in
  16)
    export POSTGRES_IMAGE="postgres:16-alpine"
    export PGDATA_DIR="/var/lib/postgresql/data"
    ;;
  17)
    export POSTGRES_IMAGE="postgres:17-alpine"
    export PGDATA_DIR="/var/lib/postgresql/data"
    ;;
  18)
    export POSTGRES_IMAGE="ghcr.io/pgedge/pgedge-postgres:18-spock5-standard"
    export PGDATA_DIR="/var/lib/pgsql/18/data"
    ;;
  *)
    echo -e "${RED}✗ Unsupported PostgreSQL version: ${POSTGRES_VERSION}${NC}"
    echo "Supported versions: 16, 17, 18"
    exit 1
    ;;
esac
export POSTGRES_PASSWORD="postgres"
echo -e "${GREEN}✓ PostgreSQL ${POSTGRES_VERSION} configured${NC}"
echo ""

# Step 3: Check if stack is already running
echo -e "${BLUE}Step 3: Checking for existing stack...${NC}"
if docker compose -f "${COMPOSE_FILE}" ps 2>/dev/null | grep -q "healthy\|running"; then
  echo -e "${RED}✗ Stack is already running!${NC}"
  echo "To restart, run:"
  echo "  docker compose -f ${COMPOSE_FILE} down -v"
  echo "Then run this script again."
  exit 1
fi
echo -e "${GREEN}✓ No existing stack found${NC}"
echo ""

# Step 4: Build Docker images
echo -e "${BLUE}Step 4: Building Docker images (this may take a few minutes)...${NC}"
docker compose -f "${COMPOSE_FILE}" build --parallel
echo -e "${GREEN}✓ Docker images built${NC}"
echo ""

# Step 5: Start services
echo -e "${BLUE}Step 5: Starting services...${NC}"
docker compose -f "${COMPOSE_FILE}" up -d
echo -e "${GREEN}✓ Services started${NC}"
echo ""

# Step 6: Wait for PostgreSQL
echo -e "${BLUE}Step 6: Waiting for PostgreSQL to be healthy...${NC}"
attempts=0
max_attempts=30
while [ $attempts -lt $max_attempts ]; do
  if docker compose -f "${COMPOSE_FILE}" ps postgres | grep -q "healthy"; then
    echo -e "${GREEN}✓ PostgreSQL is healthy${NC}"
    break
  fi
  echo "  Attempt $((attempts + 1))/${max_attempts}..."
  sleep 3
  attempts=$((attempts + 1))
done
if [ $attempts -ge $max_attempts ]; then
  echo -e "${RED}✗ PostgreSQL failed to start${NC}"
  docker compose -f "${COMPOSE_FILE}" logs postgres
  exit 1
fi
echo ""

# Step 7: Wait for Server
echo -e "${BLUE}Step 7: Waiting for Server API...${NC}"
attempts=0
max_attempts=30
while [ $attempts -lt $max_attempts ]; do
  if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Server is running${NC}"
    break
  fi
  echo "  Attempt $((attempts + 1))/${max_attempts}..."
  sleep 3
  attempts=$((attempts + 1))
done
if [ $attempts -ge $max_attempts ]; then
  echo -e "${RED}✗ Server failed to start${NC}"
  docker compose -f "${COMPOSE_FILE}" logs server
  exit 1
fi
echo ""

# Step 8: Create admin user
echo -e "${BLUE}Step 8: Creating admin user...${NC}"
docker compose -f "${COMPOSE_FILE}" exec -T server \
  /usr/local/bin/ai-dba-server \
  -config /etc/pgedge/ai-dba-server.yaml \
  -data-dir /data \
  -add-user -username admin -password "E2ETestPass123!" \
  -full-name "E2E Admin" -email "admin@e2e.test" 2>&1 | grep -i "user\|error" || true

docker compose -f "${COMPOSE_FILE}" exec -T server \
  /usr/local/bin/ai-dba-server \
  -config /etc/pgedge/ai-dba-server.yaml \
  -data-dir /data \
  -set-superuser -username admin 2>&1 | grep -i "superuser\|error" || true

echo -e "${GREEN}✓ Admin user created${NC}"
echo ""

# Step 9: Install NPM dependencies
echo -e "${BLUE}Step 9: Installing NPM dependencies...${NC}"
cd "${SCRIPT_DIR}"
npm ci > /dev/null 2>&1
echo -e "${GREEN}✓ NPM dependencies installed${NC}"
echo ""

# Step 10: Install Playwright browsers
echo -e "${BLUE}Step 10: Installing Playwright browsers...${NC}"
npx playwright install --with-deps chromium > /dev/null 2>&1
echo -e "${GREEN}✓ Playwright browsers installed${NC}"
echo ""

# Success!
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ Setup Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

echo "You can now run tests:"
echo ""
echo -e "${BLUE}  cd ${SCRIPT_DIR}${NC}"
echo -e "${BLUE}  npm test${NC}"
echo ""

echo "Other useful commands:"
echo ""
echo -e "${BLUE}  npm run test:headed    # Run with browser visible${NC}"
echo -e "${BLUE}  npm run test:debug     # Debug mode${NC}"
echo -e "${BLUE}  npm run test:ui        # Interactive UI${NC}"
echo -e "${BLUE}  npm run report         # View HTML report${NC}"
echo ""

echo "To stop the stack:"
echo ""
echo -e "${BLUE}  docker compose -f ${COMPOSE_FILE} down -v${NC}"
echo ""

echo "Stack status:"
docker compose -f "${COMPOSE_FILE}" ps
