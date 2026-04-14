#!/usr/bin/env bash
#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------
set -euo pipefail

# guide.sh -- Interactive guided walkthrough for the pgEdge AI DBA Workbench.
# Builds, configures, and launches the full walkthrough stack.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=runner.sh
source "$SCRIPT_DIR/runner.sh"

# Ensure echo is restored if the script exits unexpectedly
trap 'stty echo 2>/dev/null || true' EXIT

OS="$(uname -s)"

# ── LLM provider selection ─────────────────────────────────────────
# Shared function used by initial setup and the re-run menu.

select_llm_provider() {
  local llm_provider=""
  local llm_model=""
  local llm_key=""
  local llm_ollama_url=""

  echo ""
  explain "The AI DBA Workbench has many built-in AI features — like AI"
  explain "Overview, alert analysis, and Ask Ellie — but you don't need"
  explain "AI to find the workbench useful. You can add a key later by"
  explain "re-running this script."
  echo ""
  explain "Supported providers: Anthropic, OpenAI, Google Gemini, Ollama (local)"
  echo ""
  explain "  1) Anthropic (Claude)"
  explain "  2) OpenAI (GPT)"
  explain "  3) Google Gemini"
  explain "  4) Ollama (local, no key needed)"
  explain "  5) Skip — continue without AI features"
  echo ""
  read -rp "  Choose [1-5]: " llm_choice < /dev/tty 2>/dev/null || llm_choice="5"

  case "$llm_choice" in
    1)
      llm_provider="anthropic"
      llm_model="claude-sonnet-4-5"
      ;;
    2)
      llm_provider="openai"
      llm_model="gpt-4o"
      ;;
    3)
      llm_provider="gemini"
      llm_model="gemini-2.0-flash"
      ;;
    4)
      llm_provider="ollama"
      llm_model="qwen2.5:7b-instruct"
      echo ""
      read -rp "  Ollama URL [http://host.docker.internal:11434]: " llm_ollama_url < /dev/tty 2>/dev/null || llm_ollama_url=""
      llm_ollama_url="${llm_ollama_url:-http://host.docker.internal:11434}"
      ;;
    *)
      llm_provider=""
      llm_model=""
      ;;
  esac

  # Prompt for API key (skip for Ollama and Skip)
  if [[ -n "$llm_provider" && "$llm_provider" != "ollama" ]]; then
    echo ""
    printf "%s" "Enter your API key"
    echo ""
    printf "${DIM}(input is hidden, paste is OK)${RESET}: "

    printf '\e[?2004l' >/dev/tty 2>/dev/null || true
    stty -echo 2>/dev/null || true
    llm_key=""
    # Read char-by-char, printing * for each, to give visual
    # feedback that the paste/typing is being received.
    while IFS= read -r -s -n1 char </dev/tty; do
      if [[ -z "$char" ]]; then
        # Enter pressed (empty char = newline)
        break
      fi
      llm_key+="$char"
      printf '*'
    done
    stty echo 2>/dev/null || true
    printf '\e[?2004h' >/dev/tty 2>/dev/null || true
    echo ""

    if [[ -n "$llm_key" ]]; then
      if [[ ${#llm_key} -lt 20 ]]; then
        error "API key is too short (minimum 20 characters). Skipping AI."
        llm_provider=""
        llm_model=""
        llm_key=""
      elif ! printf '%s' "$llm_key" | grep -qE '^[a-zA-Z0-9._-]+$'; then
        error "API key contains invalid characters. Skipping AI."
        llm_provider=""
        llm_model=""
        llm_key=""
      else
        # Show masked preview so the user can confirm it's the right key
        local key_len=${#llm_key}
        local prefix="${llm_key:0:6}"
        local suffix="${llm_key: -4}"
        info "Key received: ${prefix}****...****${suffix} (${key_len} characters)"
      fi
    else
      warn "No API key provided. Skipping AI features."
      llm_provider=""
      llm_model=""
    fi
  fi

  # Export results via global variables
  SELECTED_LLM_PROVIDER="$llm_provider"
  SELECTED_LLM_MODEL="$llm_model"
  SELECTED_LLM_KEY="$llm_key"
  SELECTED_LLM_OLLAMA_URL="$llm_ollama_url"
}

# ── Write server config ────────────────────────────────────────────
# Generates config/ai-dba-server.yaml with the correct LLM section.

write_server_config() {
  local provider="$1"
  local model="$2"
  local ollama_url="$3"

  cat > "$SCRIPT_DIR/config/ai-dba-server.yaml" <<YAML
http:
    address: ":8080"
    tls:
        enabled: false
    auth:
        enabled: true

connection_security:
    allow_internal_networks: true

database:
    host: postgres
    port: 5432
    database: ai_workbench
    user: postgres
    password: postgres
    sslmode: disable

secret_file: /etc/pgedge/secret/ai-dba.secret
data_dir: /data
YAML

  case "$provider" in
    anthropic)
      cat >> "$SCRIPT_DIR/config/ai-dba-server.yaml" <<YAML

llm:
    provider: anthropic
    model: $model
    anthropic_api_key_file: /etc/pgedge/secret/llm-api-key
YAML
      ;;
    openai)
      cat >> "$SCRIPT_DIR/config/ai-dba-server.yaml" <<YAML

llm:
    provider: openai
    model: $model
    openai_api_key_file: /etc/pgedge/secret/llm-api-key
YAML
      ;;
    gemini)
      cat >> "$SCRIPT_DIR/config/ai-dba-server.yaml" <<YAML

llm:
    provider: gemini
    model: $model
    gemini_api_key_file: /etc/pgedge/secret/llm-api-key
YAML
      ;;
    ollama)
      cat >> "$SCRIPT_DIR/config/ai-dba-server.yaml" <<YAML

llm:
    provider: ollama
    model: $model
    ollama_url: $ollama_url
YAML
      ;;
    *)
      # No LLM section — AI features disabled
      ;;
  esac
}

# ── Check for existing stack ────────────────────────────────────────

if docker ps --filter "name=wt-" --format "{{.Names}}" 2>/dev/null | grep -q "wt-"; then
  echo ""
  warn "An existing walkthrough stack is running."
  echo ""
  explain "  1) Tear down and start fresh"
  explain "  2) Clean up and exit"
  explain "  3) Cancel (leave it running)"
  explain "  4) Change LLM configuration"
  echo ""
  read -rp "  Choose [1/2/3/4]: " choice < /dev/tty 2>/dev/null || choice="3"

  case "$choice" in
    1)
      info "Tearing down existing stack..."
      (cd "$SCRIPT_DIR" && docker compose down -v 2>/dev/null) || true
      rm -f "$SCRIPT_DIR/secret/ai-dba.secret" \
            "$SCRIPT_DIR/secret/pg-password" \
            "$SCRIPT_DIR/secret/llm-api-key" 2>/dev/null
      info "Clean. Starting fresh..."
      echo ""
      ;;
    2)
      info "Cleaning up..."
      (cd "$SCRIPT_DIR" && docker compose down -v 2>/dev/null) || true
      rm -f "$SCRIPT_DIR/secret/ai-dba.secret" \
            "$SCRIPT_DIR/secret/pg-password" \
            "$SCRIPT_DIR/secret/llm-api-key" 2>/dev/null
      info "Cleaned up. Goodbye."
      exit 0
      ;;
    4)
      info "Changing LLM configuration..."
      select_llm_provider

      write_server_config "$SELECTED_LLM_PROVIDER" "$SELECTED_LLM_MODEL" "$SELECTED_LLM_OLLAMA_URL"
      info "Server config updated."

      if [[ -n "$SELECTED_LLM_KEY" ]]; then
        echo "$SELECTED_LLM_KEY" > "$SCRIPT_DIR/secret/llm-api-key"
        chmod 600 "$SCRIPT_DIR/secret/llm-api-key"
        info "API key written."
      else
        rm -f "$SCRIPT_DIR/secret/llm-api-key"
        touch "$SCRIPT_DIR/secret/llm-api-key"
        chmod 600 "$SCRIPT_DIR/secret/llm-api-key"
      fi

      info "Restarting server..."
      (cd "$SCRIPT_DIR" && docker compose restart server 2>/dev/null) || true
      echo ""
      info "LLM configuration updated. The server is restarting."

      if [[ -n "$SELECTED_LLM_PROVIDER" ]]; then
        info "Provider: $SELECTED_LLM_PROVIDER ($SELECTED_LLM_MODEL)"
      else
        warn "AI features are disabled."
      fi
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
  elif [[ "$OS" == MINGW* || "$OS" == MSYS* ]]; then
    netstat -an 2>/dev/null | grep -q "[:.]${1} .*LISTEN"
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
explain "  1. Prompt for an optional LLM provider and API key"
explain "  2. Generate secrets and start the walkthrough stack"
explain "  3. Create an admin user and register a demo database"
explain "  4. Open the workbench in your browser"
echo ""

info "Ports: client=${WT_CLIENT_PORT}, server=${WT_SERVER_PORT}, datastore=${WT_DATASTORE_PORT}, demo-db=${WT_DEMO_PORT}"
echo ""

# ── Prompt for LLM provider ─────────────────────────────────────────

explain "${BOLD}LLM Provider${RESET}"

select_llm_provider

echo ""

# ── Generate secrets ─────────────────────────────────────────────────

explain "Generating secrets..."

mkdir -p "$SCRIPT_DIR/secret"
# DEMO ONLY: This is a fixed secret that matches the pre-baked seed data.
# The seed contains encrypted passwords that were encrypted with
# this exact secret. Using a different secret would prevent the
# collector from decrypting connection passwords.
# Do NOT reuse this secret in production deployments.
echo "q0xZ579yK4gFREb5LHlqhlyPgmKHt0S5j4O2deRKRhs=" > "$SCRIPT_DIR/secret/ai-dba.secret"
echo "postgres" > "$SCRIPT_DIR/secret/pg-password"
echo "$SELECTED_LLM_KEY" > "$SCRIPT_DIR/secret/llm-api-key"
chmod 600 "$SCRIPT_DIR/secret/"*

info "Secrets written to $SCRIPT_DIR/secret/"
echo ""

# ── Write server config ─────────────────────────────────────────────

explain "Writing server configuration..."

write_server_config "$SELECTED_LLM_PROVIDER" "$SELECTED_LLM_MODEL" "$SELECTED_LLM_OLLAMA_URL"

info "Server config written to $SCRIPT_DIR/config/ai-dba-server.yaml"
echo ""

# ── Build and start the stack ────────────────────────────────────────

start_spinner "Building and starting AI DBA Workbench..."
# Pull pre-built images by default. Use --build to build from source
# if developing locally with the full repository.
(cd "$SCRIPT_DIR" && docker compose up -d 2>"$SCRIPT_DIR/build.log")
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
  warn "Check build.log for details: cat $SCRIPT_DIR/build.log"
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

# ── Connection and cluster ───────────────────────────────────────────
# The demo connection, cluster group, and cluster are pre-baked in the
# datastore seed. However, the encrypted password was created by the
# recording session's server instance. Re-encrypt it with the current
# server so the collector can decrypt it and connect to pg-demo.

explain "Re-encrypting demo connection password..."

SESSION=$(curl -sf -D - -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"DemoPass2026"}' \
  "http://localhost:${WT_SERVER_PORT}/api/v1/auth/login" 2>/dev/null \
  | grep session_token | sed 's/.*session_token=//;s/;.*//' | tr -d '\r\n')

if [[ -z "$SESSION" ]]; then
  warn "Could not authenticate for password re-encryption (continuing)."
else
  curl -sf -X PUT \
    -H "Cookie: session_token=$SESSION" \
    -H "Content-Type: application/json" \
    -d '{"password":"postgres"}' \
    "http://localhost:${WT_SERVER_PORT}/api/v1/connections/1" \
    >/dev/null 2>&1 \
    || warn "Could not re-encrypt connection password (continuing)."

  info "Demo connection password re-encrypted."
fi

# Restart collector to pick up the re-encrypted password
(cd "$SCRIPT_DIR" && docker compose restart collector 2>/dev/null) || true

info "Demo connection and cluster loaded from seed data."
echo ""

# ── Open browser ─────────────────────────────────────────────────────

explain "Opening the AI DBA Workbench in your browser..."

OPEN_URL="http://localhost:${WT_CLIENT_PORT}"
if [[ "$OS" == "Darwin" ]]; then
  open "$OPEN_URL" 2>/dev/null || warn "Could not open browser automatically."
elif [[ "$OS" == MINGW* || "$OS" == MSYS* ]]; then
  start "$OPEN_URL" 2>/dev/null || warn "Could not open browser automatically."
elif command -v xdg-open &>/dev/null; then
  xdg-open "$OPEN_URL" 2>/dev/null || warn "Could not open browser automatically."
else
  warn "Could not open browser automatically."
  explain "Open ${OPEN_URL} in your browser."
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

if [[ -n "$SELECTED_LLM_PROVIDER" ]]; then
  info "AI features are enabled (${SELECTED_LLM_PROVIDER}, ${SELECTED_LLM_MODEL})."
else
  warn "AI features are disabled. Run guide.sh again to add an LLM provider."
fi

echo ""
explain "${DIM}Note: This uses a demo-only encryption secret.${RESET}"
explain "${DIM}Do not reuse this installation for production.${RESET}"
echo ""
explain "${DIM}To reconfigure or${RESET}"
explain "${DIM}  uninstall:${RESET}       cd $SCRIPT_DIR && bash guide.sh"
explain "${DIM}To stop the stack:${RESET}  cd $SCRIPT_DIR && docker compose down -v"
explain "${DIM}To view logs:${RESET}       cd $SCRIPT_DIR && docker compose logs -f"
echo ""
