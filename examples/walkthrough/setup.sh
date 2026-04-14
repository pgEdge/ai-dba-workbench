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

# setup.sh -- Check prerequisites for the AI DBA Workbench walkthrough.
# Run this before guide.sh to verify your environment is ready.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=runner.sh
source "$SCRIPT_DIR/runner.sh"
OS="$(uname -s)"

# ── Prerequisite checks ─────────────────────────────────────────────

header "AI DBA Workbench Walkthrough -- Prerequisites Check"

explain "Checking that required tools are installed..."
echo ""

REQUIRED_CMDS=(docker curl)
MISSING=()

for cmd in "${REQUIRED_CMDS[@]}"; do
  if command -v "$cmd" &>/dev/null; then
    info "$cmd  found ($(command -v "$cmd"))"
  else
    error "$cmd  not found"
    MISSING+=("$cmd")
  fi
done

echo ""

if [[ ${#MISSING[@]} -gt 0 ]]; then
  warn "Missing tools: ${MISSING[*]}"
  echo ""
  explain "Install hints:"

  for cmd in "${MISSING[@]}"; do
    case "$cmd" in
      docker)
        explain "  docker  -- https://docs.docker.com/get-docker/"
        ;;
      curl)
        explain "  curl    -- https://curl.se/download.html"
        ;;
    esac
  done

  echo ""
  error "Install the missing tools, then re-run:"
  explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
  echo ""
  exit 1
fi

# ── Verify Docker Compose v2 ────────────────────────────────────────

explain "Checking Docker Compose v2..."

if docker compose version &>/dev/null; then
  info "Docker Compose v2 is available ($(docker compose version --short 2>/dev/null || echo 'ok'))"
else
  echo ""
  error "Docker Compose v2 is not available."
  explain ""
  explain "The walkthrough requires 'docker compose' (v2). Install hints:"
  explain ""
  explain "  ${DIM}https://docs.docker.com/compose/install/${RESET}"
  explain ""
  explain "Then re-run:"
  explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
  exit 1
fi

echo ""

# ── Verify Docker daemon is accessible ────────────────────────────────

explain "Verifying Docker daemon is accessible..."

if ! docker info &>/dev/null; then
  echo ""
  if [[ "$OS" == "Darwin" || "$OS" == MINGW* || "$OS" == MSYS* ]]; then
    error "Docker does not appear to be running."
    explain ""
    explain "Open Docker Desktop and wait for it to start, then re-run:"
    explain ""
    explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
  elif command -v systemctl &>/dev/null; then
    # Distinguish "not running" from "no permission" without sudo prompts
    if systemctl is-active docker &>/dev/null 2>&1; then
      error "Docker is running but your user cannot access it."
      explain ""
      explain "Run these commands to fix permissions, then re-run the guide:"
      explain ""
      explain "  ${DIM}sudo usermod -aG docker \$USER${RESET}"
      explain "  ${DIM}newgrp docker${RESET}"
      explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
    else
      error "Docker is installed but the daemon is not running."
      explain ""
      explain "Run these commands to start Docker, then re-run the guide:"
      explain ""
      explain "  ${DIM}sudo systemctl daemon-reload${RESET}"
      explain "  ${DIM}sudo systemctl start docker${RESET}"
      explain "  ${DIM}sudo systemctl enable docker${RESET}"
      explain "  ${DIM}sudo usermod -aG docker \$USER${RESET}"
      explain "  ${DIM}newgrp docker${RESET}"
      explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
    fi
  else
    error "Cannot connect to the Docker daemon."
    explain ""
    explain "Make sure Docker is installed and running, then re-run:"
    explain ""
    explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
  fi
  exit 1
fi

info "Docker daemon is running."
echo ""

# ── Check port availability ──────────────────────────────────────────

explain "Checking default ports (guide.sh will find alternatives if busy)..."

PORTS_NEEDED=(3000 8080)
PORTS_BUSY=()

for port in "${PORTS_NEEDED[@]}"; do
  busy=false
  if [[ "$OS" == "Darwin" ]]; then
    if lsof -iTCP:"$port" -sTCP:LISTEN &>/dev/null; then
      busy=true
    fi
  elif [[ "$OS" == MINGW* || "$OS" == MSYS* ]]; then
    if netstat -an 2>/dev/null | grep -q "[:.]${port} .*LISTEN"; then
      busy=true
    fi
  else
    if ss -tln 2>/dev/null | grep -q ":${port} "; then
      busy=true
    fi
  fi

  if [[ "$busy" == "true" ]]; then
    warn "Port $port is in use (guide.sh will find an alternative)."
    PORTS_BUSY+=("$port")
  else
    info "Port $port is available"
  fi
done

echo ""

if [[ ${#PORTS_BUSY[@]} -gt 0 ]]; then
  warn "Default ports in use: ${PORTS_BUSY[*]}. guide.sh will select alternatives."
fi

# ── Done ─────────────────────────────────────────────────────────────

info "All prerequisites satisfied. You are ready to run guide.sh."
