#!/usr/bin/env bash
set -euo pipefail

# guide.sh -- Interactive guided walkthrough for the pgEdge AI DBA Workbench.
# Builds, configures, and launches the full walkthrough stack.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=runner.sh
source "$SCRIPT_DIR/runner.sh"

# Ensure echo is restored if the script exits unexpectedly
trap 'stty echo 2>/dev/null || true' EXIT

OS="$(uname -s)"

# ── Prerequisites ────────────────────────────────────────────────────

bash "$SCRIPT_DIR/setup.sh" || exit 1

# ── Branded header ───────────────────────────────────────────────────

header "pgEdge AI DBA Workbench — Guided Walkthrough"

explain "This script will:"
explain "  1. Prompt for an optional Anthropic API key"
explain "  2. Generate secrets and start the walkthrough stack"
explain "  3. Create an admin user and register a demo database"
explain "  4. Open the workbench in your browser"
echo ""

# ── Prompt for Anthropic API key ─────────────────────────────────────

explain "${BOLD}Anthropic API Key${RESET}"
explain ""
explain "AI features (Ask Ellie, alert analysis, AI overview) need an"
explain "Anthropic API key. You can add one later during the tour."
echo ""

printf "%s" "Enter your Anthropic API key (optional, press Enter to skip)"
echo ""
printf "${DIM}(input is hidden, paste is OK)${RESET}: "

# Disable bracketed paste mode and echo for secure input
printf '\e[?2004l' >/dev/tty 2>/dev/null || true
stty -echo 2>/dev/null || true

API_KEY=""
read -r API_KEY </dev/tty || true

# Re-enable echo and bracketed paste mode
stty echo 2>/dev/null || true
printf '\e[?2004h' >/dev/tty 2>/dev/null || true
echo ""

if [[ -n "$API_KEY" ]]; then
  # Validate: alphanumeric, dots, hyphens, underscores; min 20 chars
  if [[ ${#API_KEY} -lt 20 ]]; then
    error "API key is too short (minimum 20 characters). Skipping."
    API_KEY=""
  elif ! printf '%s' "$API_KEY" | grep -qE '^[a-zA-Z0-9._-]+$'; then
    error "API key contains invalid characters. Skipping."
    API_KEY=""
  else
    info "API key accepted."
  fi
else
  warn "No API key provided. AI features will be unavailable until configured."
fi
echo ""

# ── Generate secrets ─────────────────────────────────────────────────

explain "Generating secrets..."

mkdir -p "$SCRIPT_DIR/secret"
openssl rand -base64 32 > "$SCRIPT_DIR/secret/ai-dba.secret"
echo "postgres" > "$SCRIPT_DIR/secret/pg-password"
echo "$API_KEY" > "$SCRIPT_DIR/secret/anthropic-api-key"
chmod 600 "$SCRIPT_DIR/secret/"*

info "Secrets written to $SCRIPT_DIR/secret/"
echo ""

# ── Build and start the stack ────────────────────────────────────────

start_spinner "Building and starting AI DBA Workbench..."
(cd "$SCRIPT_DIR" && docker compose up -d --build 2>/dev/null)
stop_spinner

info "Docker containers started."
echo ""

# ── Wait for health ──────────────────────────────────────────────────

start_spinner "Waiting for services to become healthy..."

TIMEOUT=120
ELAPSED=0

while [[ $ELAPSED -lt $TIMEOUT ]]; do
  SERVER_OK=false
  CLIENT_OK=false

  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    SERVER_OK=true
  fi

  if curl -sf http://localhost:3000 >/dev/null 2>&1; then
    CLIENT_OK=true
  fi

  if [[ "$SERVER_OK" == "true" && "$CLIENT_OK" == "true" ]]; then
    break
  fi

  sleep 2
  ELAPSED=$((ELAPSED + 2))
done

stop_spinner

if [[ $ELAPSED -ge $TIMEOUT ]]; then
  warn "Timed out waiting for services after ${TIMEOUT}s."
  warn "Check status with: cd $SCRIPT_DIR && docker compose ps"
  warn "View logs with:    cd $SCRIPT_DIR && docker compose logs"
  echo ""
else
  info "Server and client are healthy."
  echo ""
fi

# ── Create admin user ────────────────────────────────────────────────

explain "Creating admin user..."

docker exec wt-server sh -c \
  'echo "Demo2026!" > /tmp/pw && ai-dba-server -add-user -username admin -password-file /tmp/pw -full-name "Demo Admin" -email "admin@demo.local" -user-note "Walkthrough admin" -config /etc/pgedge/ai-dba-server.yaml && rm /tmp/pw' \
  2>/dev/null || warn "Admin user may already exist (continuing)."

docker exec wt-server \
  ai-dba-server -set-superuser -username admin -config /etc/pgedge/ai-dba-server.yaml \
  2>/dev/null || warn "Could not set superuser flag (continuing)."

info "Admin user created."
echo ""

# ── Create service token ─────────────────────────────────────────────

explain "Creating service token for walkthrough helper..."

TOKEN_OUTPUT=$(docker exec wt-server \
  ai-dba-server -add-token -user admin -token-note "walkthrough-helper" -token-expiry never -config /etc/pgedge/ai-dba-server.yaml \
  2>&1) || true

TOKEN=$(echo "$TOKEN_OUTPUT" | grep -oE 'aidba_[a-zA-Z0-9_]+' || true)

if [[ -n "$TOKEN" ]]; then
  echo "$TOKEN" > "$SCRIPT_DIR/secret/helper-token"
  chmod 600 "$SCRIPT_DIR/secret/helper-token"
  info "Service token created."
else
  warn "Could not extract service token. Helper sidecar may not function."
  warn "Output: $TOKEN_OUTPUT"
fi
echo ""

# ── Register demo connection ─────────────────────────────────────────

explain "Registering demo database connection..."

if [[ -n "$TOKEN" ]]; then
  CONN_RESPONSE=$(curl -sf -X POST http://localhost:8080/api/v1/connections \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
      "name": "demo-ecommerce",
      "host": "pg-demo",
      "port": 5432,
      "database_name": "ecommerce",
      "username": "postgres",
      "password": "postgres",
      "ssl_mode": "disable",
      "is_shared": true,
      "is_monitored": true
    }' 2>&1) || warn "Could not register demo connection (continuing)."

  if [[ -n "${CONN_RESPONSE:-}" ]]; then
    info "Demo connection registered."
  fi
else
  warn "Skipping connection registration (no service token)."
fi
echo ""

# ── Create cluster group and cluster ─────────────────────────────────

explain "Creating demo cluster group and cluster..."

if [[ -n "$TOKEN" ]]; then
  # Create cluster group
  GROUP_RESPONSE=$(curl -sf -X POST http://localhost:8080/api/v1/cluster-groups \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
      "name": "demo-group",
      "description": "Walkthrough demo cluster group"
    }' 2>&1) || warn "Could not create cluster group (continuing)."

  GROUP_ID=""
  if [[ -n "${GROUP_RESPONSE:-}" ]] && command -v python3 &>/dev/null; then
    GROUP_ID=$(python3 -c "import json,sys; print(json.loads(sys.stdin.read()).get('id',''))" <<< "$GROUP_RESPONSE" 2>/dev/null) || true
  fi

  if [[ -n "$GROUP_ID" ]]; then
    # Create cluster
    CLUSTER_RESPONSE=$(curl -sf -X POST http://localhost:8080/api/v1/clusters \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "{
        \"name\": \"demo-cluster\",
        \"cluster_group_id\": \"$GROUP_ID\",
        \"description\": \"Walkthrough demo cluster\"
      }" 2>&1) || warn "Could not create cluster (continuing)."

    CLUSTER_ID=""
    if [[ -n "${CLUSTER_RESPONSE:-}" ]]; then
      CLUSTER_ID=$(python3 -c "import json,sys; print(json.loads(sys.stdin.read()).get('id',''))" <<< "$CLUSTER_RESPONSE" 2>/dev/null) || true
    fi

    # Get the connection ID for the demo connection
    CONN_ID=""
    if [[ -n "${CONN_RESPONSE:-}" ]]; then
      CONN_ID=$(python3 -c "import json,sys; print(json.loads(sys.stdin.read()).get('id',''))" <<< "$CONN_RESPONSE" 2>/dev/null) || true
    fi

    # Assign server to cluster
    if [[ -n "$CLUSTER_ID" && -n "$CONN_ID" ]]; then
      curl -sf -X PUT "http://localhost:8080/api/v1/clusters/${CLUSTER_ID}/servers/${CONN_ID}" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        2>/dev/null || warn "Could not assign server to cluster (continuing)."
      info "Cluster group, cluster, and server assignment created."
    else
      warn "Could not complete cluster setup (missing IDs)."
    fi
  else
    warn "Could not extract cluster group ID (continuing)."
  fi
else
  warn "Skipping cluster setup (no service token)."
fi
echo ""

# ── Restart helper sidecar ───────────────────────────────────────────

explain "Restarting walkthrough helper..."

(cd "$SCRIPT_DIR" && docker compose restart walkthrough-helper 2>/dev/null) \
  || warn "Could not restart walkthrough helper (continuing)."

info "Helper sidecar restarted."
echo ""

# ── Open browser ─────────────────────────────────────────────────────

explain "Opening the AI DBA Workbench in your browser..."

OPEN_URL="http://localhost:3000"
if [[ "$OS" == "Darwin" ]]; then
  open "$OPEN_URL" 2>/dev/null || warn "Could not open browser automatically."
elif command -v xdg-open &>/dev/null; then
  xdg-open "$OPEN_URL" 2>/dev/null || warn "Could not open browser automatically."
else
  warn "Open this URL in your browser: $OPEN_URL"
fi

# ── Done ─────────────────────────────────────────────────────────────

echo ""
header "Walkthrough Ready"

explain "The pgEdge AI DBA Workbench is running and ready to explore."
echo ""
explain "${BOLD}Web Interface:${RESET}  http://localhost:3000"
explain "${BOLD}Login:${RESET}          admin / Demo2026!"
echo ""
explain "${BOLD}API Server:${RESET}     http://localhost:8080"
echo ""

if [[ -n "$API_KEY" ]]; then
  info "AI features are enabled (API key configured)."
else
  warn "AI features are disabled. Add an API key via the in-browser tour"
  warn "or write it to: $SCRIPT_DIR/secret/anthropic-api-key"
fi

echo ""
explain "${DIM}To stop the stack:${RESET}  cd $SCRIPT_DIR && docker compose down -v"
explain "${DIM}To view logs:${RESET}       cd $SCRIPT_DIR && docker compose logs -f"
echo ""
