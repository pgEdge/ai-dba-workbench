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

# ── Check for existing stack ────────────────────────────────────────

EXISTING=$(docker compose ls --filter "name=workbench-walkthrough" --format json 2>/dev/null \
  | grep -c "workbench-walkthrough" 2>/dev/null || echo "0")

if [[ "$EXISTING" -gt 0 ]] || docker ps --filter "name=wt-" --format "{{.Names}}" 2>/dev/null | grep -q "wt-"; then
  echo ""
  warn "An existing walkthrough stack is running."
  echo ""
  explain "  1) Tear down and start fresh"
  explain "  2) Clean up and exit"
  explain "  3) Cancel (leave it running)"
  echo ""
  read -rp "  Choose [1/2/3]: " choice < /dev/tty 2>/dev/null || choice="3"

  case "$choice" in
    1)
      info "Tearing down existing stack..."
      (cd "$SCRIPT_DIR" && docker compose down -v 2>/dev/null) || true
      rm -f "$SCRIPT_DIR/secret/ai-dba.secret" \
            "$SCRIPT_DIR/secret/pg-password" \
            "$SCRIPT_DIR/secret/anthropic-api-key" \
            "$SCRIPT_DIR/secret/helper-token" 2>/dev/null
      info "Clean. Starting fresh..."
      echo ""
      ;;
    2)
      info "Cleaning up..."
      (cd "$SCRIPT_DIR" && docker compose down -v 2>/dev/null) || true
      rm -f "$SCRIPT_DIR/secret/ai-dba.secret" \
            "$SCRIPT_DIR/secret/pg-password" \
            "$SCRIPT_DIR/secret/anthropic-api-key" \
            "$SCRIPT_DIR/secret/helper-token" 2>/dev/null
      info "Cleaned up. Goodbye."
      exit 0
      ;;
    *)
      info "Leaving the stack running. Goodbye."
      exit 0
      ;;
  esac
fi

# ── Port detection ──────────────────────────────────────────────────

port_in_use() {
  if [[ "$OS" == "Darwin" ]]; then
    lsof -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
  else
    ss -tln 2>/dev/null | grep -q ":${1} "
  fi
}

find_free_port() {
  local start="$1"
  local p="$start"
  while port_in_use "$p"; do
    p=$((p + 1))
    if [[ $p -gt 65530 ]]; then
      echo "$start"
      return
    fi
  done
  echo "$p"
}

export WT_CLIENT_PORT=$(find_free_port 3000)
export WT_SERVER_PORT=$(find_free_port 8080)
export WT_DATASTORE_PORT=$(find_free_port 5432)
export WT_DEMO_PORT=$(find_free_port $((WT_DATASTORE_PORT + 1)))

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

info "Ports: client=${WT_CLIENT_PORT}, server=${WT_SERVER_PORT}, datastore=${WT_DATASTORE_PORT}, demo-db=${WT_DEMO_PORT}"
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
# Use the fixed secret that matches the pre-baked seed data.
# The seed contains encrypted passwords that were encrypted with
# this exact secret. Using a different secret would prevent the
# collector from decrypting connection passwords.
echo "q0xZ579yK4gFREb5LHlqhlyPgmKHt0S5j4O2deRKRhs=" > "$SCRIPT_DIR/secret/ai-dba.secret"
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

  if curl -sf http://localhost:${WT_SERVER_PORT}/health >/dev/null 2>&1; then
    SERVER_OK=true
  fi

  if curl -sf http://localhost:${WT_CLIENT_PORT} >/dev/null 2>&1; then
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

# ── Rebase timestamps ───────────────────────────────────────────────
# Shift all pre-baked metric timestamps so the data looks like it was
# collected in the last few hours, not whenever the seed was recorded.
# Stop the collector first so its fresh data doesn't mask the seed's
# old timestamps (MAX would find collector data, not seed data).

explain "Rebasing metric timestamps to current time..."

(cd "$SCRIPT_DIR" && docker compose stop collector alerter 2>/dev/null) || true

if [[ -x "$SCRIPT_DIR/seed/rebase-timestamps.sh" ]]; then
  "$SCRIPT_DIR/seed/rebase-timestamps.sh" wt-datastore ai_workbench \
    || warn "Timestamp rebase had issues (continuing)."
  info "Timestamps rebased."
else
  warn "rebase-timestamps.sh not found (skipping)."
fi

(cd "$SCRIPT_DIR" && docker compose start collector alerter 2>/dev/null) || true
echo ""

# ── Create admin user ────────────────────────────────────────────────

explain "Creating admin user..."

docker exec wt-server sh -c \
  'echo "DemoPass2026" > /tmp/pw && ai-dba-server -add-user -username admin -password-file /tmp/pw -full-name "Demo Admin" -email "admin@demo.local" -user-note "Walkthrough admin" -data-dir /data -config /etc/pgedge/ai-dba-server.yaml && rm /tmp/pw' \
  2>/dev/null || warn "Admin user may already exist (continuing)."

docker exec wt-server \
  ai-dba-server -set-superuser -username admin -data-dir /data -config /etc/pgedge/ai-dba-server.yaml \
  2>/dev/null || warn "Could not set superuser flag (continuing)."

info "Admin user created."
echo ""

# ── Create service token ─────────────────────────────────────────────

explain "Creating service token for walkthrough helper..."

TOKEN_OUTPUT=$(docker exec wt-server \
  ai-dba-server -add-token -user admin -token-note "walkthrough-helper" -token-expiry never -data-dir /data -config /etc/pgedge/ai-dba-server.yaml \
  2>&1) || true

TOKEN=$(echo "$TOKEN_OUTPUT" | grep "^Token:" | awk '{print $2}' || true)

if [[ -n "$TOKEN" ]]; then
  echo "$TOKEN" > "$SCRIPT_DIR/secret/helper-token"
  chmod 600 "$SCRIPT_DIR/secret/helper-token"
  info "Service token created."
else
  warn "Could not extract service token. Helper sidecar may not function."
  warn "Output: $TOKEN_OUTPUT"
fi
echo ""

# ── Connection and cluster ───────────────────────────────────────────
# The demo connection, cluster group, and cluster are pre-baked in the
# datastore seed. However, the encrypted password was created by the
# recording session's server instance. Re-encrypt it with the current
# server so the collector can decrypt it and connect to pg-demo.

explain "Re-encrypting demo connection password..."

if [[ -n "$TOKEN" ]]; then
  SESSION=$(curl -sf -D - -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"DemoPass2026"}' \
    "http://localhost:${WT_SERVER_PORT}/api/v1/auth/login" 2>&1 \
    | grep session_token | sed 's/.*session_token=//;s/;.*//' | tr -d '\r\n')

  curl -sf -X PUT \
    -H "Cookie: session_token=$SESSION" \
    -H "Content-Type: application/json" \
    -d '{"password":"postgres"}' \
    "http://localhost:${WT_SERVER_PORT}/api/v1/connections/1" \
    >/dev/null 2>&1 \
    || warn "Could not re-encrypt connection password (continuing)."

  info "Demo connection password re-encrypted."
else
  warn "Skipping password re-encryption (no token)."
fi

# Restart collector to pick up the re-encrypted password
(cd "$SCRIPT_DIR" && docker compose restart collector 2>/dev/null) || true

info "Demo connection and cluster loaded from seed data."
echo ""


# ── Restart helper sidecar ───────────────────────────────────────────

explain "Restarting walkthrough helper..."

(cd "$SCRIPT_DIR" && docker compose restart walkthrough-helper 2>/dev/null) \
  || warn "Could not restart walkthrough helper (continuing)."

info "Helper sidecar restarted."
echo ""

# ── Open browser ─────────────────────────────────────────────────────

explain "Opening the AI DBA Workbench in your browser..."

OPEN_URL="http://localhost:${WT_CLIENT_PORT}"
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
explain "${BOLD}Web Interface:${RESET}  http://localhost:${WT_CLIENT_PORT}"
explain "${BOLD}Login:${RESET}          admin / DemoPass2026"
echo ""
explain "${BOLD}API Server:${RESET}     http://localhost:${WT_SERVER_PORT}"
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
