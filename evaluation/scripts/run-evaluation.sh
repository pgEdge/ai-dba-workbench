#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - evaluation harness runner
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Starts a disposable PostgreSQL in Docker, builds and starts the
# server against it with the LLM providers whose keys are present in
# the environment, replays the incidents in evaluation/incidents with
# ai-dba-evaluate, writes results.json and report.md, then tears
# everything down. The container holds both the Workbench datastore
# and the databases staged for each incident.
#
# Environment:
#   EVALUATE_TARGETS     required; space- or comma-separated
#                        provider[:model] values, e.g.
#                        "anthropic:claude-sonnet-4-5 openai:gpt-5".
#                        The first is the server's default model.
#   ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, OLLAMA_URL
#                        credentials for the providers named in
#                        EVALUATE_TARGETS; they are written to files
#                        readable only by the current user.
#   EVALUATE_OUT         output directory (default evaluation/results)
#   EVALUATE_DB_PORT     host port for PostgreSQL (default 55433)
#   EVALUATE_SERVER_PORT port for the server (default 8090)
#   EVALUATE_KEEP_STACK  set to 1 to leave the stack running
#
# Any arguments are passed to "ai-dba-evaluate run", e.g. -runs 5 or
# -only lock-pileup.
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
E2E_SCRIPTS="${REPO_ROOT}/e2e/scripts"
# Each invocation gets its own runtime directory, so concurrent runs
# (with different ports) cannot remove each other's files.
mkdir -p "${REPO_ROOT}/evaluation/.runtime"
RUN_DIR="$(mktemp -d "${REPO_ROOT}/evaluation/.runtime/run.XXXXXX")"
OUT_DIR="${EVALUATE_OUT:-${REPO_ROOT}/evaluation/results}"
PG_CONTAINER="ai-dba-evaluate-postgres-$$"
PG_IMAGE="pgvector/pgvector:0.8.7-pg17"
HARNESS_BIN="${REPO_ROOT}/bin/ai-dba-evaluate"

: "${EVALUATE_TARGETS:?set EVALUATE_TARGETS to one or more provider:model values}"

# The e2e helper scripts read these.
export E2E_RUN_DIR="${RUN_DIR}"
export E2E_SERVER_BIN="${REPO_ROOT}/bin/ai-dba-server"
export E2E_COLLECTOR_BIN="${REPO_ROOT}/bin/ai-dba-collector"
export E2E_DB_HOST=127.0.0.1
export E2E_DB_PORT="${EVALUATE_DB_PORT:-55433}"
export E2E_DB_NAME=postgres
export E2E_DB_USER=postgres
E2E_DB_PASSWORD="$(openssl rand -hex 16)"
export E2E_DB_PASSWORD
SERVER_PORT="${EVALUATE_SERVER_PORT:-8090}"
export E2E_ADMIN_USERNAME=evaluate-admin
E2E_ADMIN_PASSWORD="$(openssl rand -hex 16)"
export E2E_ADMIN_PASSWORD

mkdir -p "${RUN_DIR}/logs" "${RUN_DIR}/data" "${OUT_DIR}"
chmod 700 "${RUN_DIR}"

cleanup() {
    if [ "${EVALUATE_KEEP_STACK:-0}" = "1" ]; then
        echo "EVALUATE_KEEP_STACK=1; leaving the stack running."
        echo "  Server log: ${RUN_DIR}/logs/server.log"
        echo "  Tear down with: E2E_RUN_DIR=${RUN_DIR} ${E2E_SCRIPTS}/stop-stack.sh && docker rm -f ${PG_CONTAINER}"
        return
    fi
    echo "==> Tearing down"
    "${E2E_SCRIPTS}/stop-stack.sh" >/dev/null || true
    docker rm -f "${PG_CONTAINER}" >/dev/null 2>&1 || true
    # The runtime directory holds the API key files; keep only the logs.
    rm -rf "${RUN_DIR}/keys" "${RUN_DIR}/data" "${RUN_DIR}/ai-dba-server.secret"
    echo "  Server log: ${RUN_DIR}/logs/server.log"
}
trap cleanup EXIT

# Work out the providers in use and the server's default model.
read -r -a TARGETS <<< "${EVALUATE_TARGETS//,/ }"
# The targets and OLLAMA_URL are written into the server's YAML
# configuration, so allow only characters that need no quoting there.
for t in "${TARGETS[@]}"; do
    if ! [[ "${t}" =~ ^(anthropic|openai|gemini|ollama)(:[A-Za-z0-9._/:@-]+)?$ ]]; then
        echo "ERROR: invalid target ${t}; use provider[:model]" >&2
        exit 1
    fi
done
if [ -n "${OLLAMA_URL:-}" ] && ! [[ "${OLLAMA_URL}" =~ ^https?://[A-Za-z0-9._:/-]+$ ]]; then
    echo "ERROR: OLLAMA_URL must be an http or https URL" >&2
    exit 1
fi
DEFAULT_PROVIDER="${TARGETS[0]%%:*}"
DEFAULT_MODEL=""
if [[ "${TARGETS[0]}" == *:* ]]; then
    DEFAULT_MODEL="${TARGETS[0]#*:}"
fi

LLM_KEYS=""
mkdir -p "${RUN_DIR}/keys"
chmod 700 "${RUN_DIR}/keys"
write_key() {
    local provider="$1" value="$2"
    if [ -z "${value}" ]; then
        echo "ERROR: a target uses ${provider}, but its API key is not set" >&2
        exit 1
    fi
    (umask 077; printf '%s' "${value}" > "${RUN_DIR}/keys/${provider}.key")
    LLM_KEYS+="  ${provider}_api_key_file: \"${RUN_DIR}/keys/${provider}.key\""$'\n'
}
declare -A SEEN=()
for t in "${TARGETS[@]}"; do
    p="${t%%:*}"
    [ -n "${SEEN[${p}]:-}" ] && continue
    SEEN[${p}]=1
    case "${p}" in
        anthropic) write_key anthropic "${ANTHROPIC_API_KEY:-}" ;;
        openai) write_key openai "${OPENAI_API_KEY:-}" ;;
        gemini) write_key gemini "${GEMINI_API_KEY:-}" ;;
        ollama) LLM_KEYS+="  ollama_url: \"${OLLAMA_URL:-http://127.0.0.1:11434}\""$'\n' ;;
    esac
done

echo "==> Starting PostgreSQL (${PG_IMAGE}) on port ${E2E_DB_PORT}"
docker run --rm -d --name "${PG_CONTAINER}" \
    -e POSTGRES_PASSWORD="${E2E_DB_PASSWORD}" \
    -p "127.0.0.1:${E2E_DB_PORT}:5432" \
    "${PG_IMAGE}" >/dev/null

echo "==> Building the server, collector and harness"
(cd "${REPO_ROOT}/server/src" && go build -o "${E2E_SERVER_BIN}" ./cmd/mcp-server \
    && go build -o "${HARNESS_BIN}" ./cmd/ai-dba-evaluate)
(cd "${REPO_ROOT}/collector/src" && go build -o "${E2E_COLLECTOR_BIN}" .)

"${E2E_SCRIPTS}/wait-for-postgres.sh" "${E2E_DB_HOST}" "${E2E_DB_PORT}" "${E2E_DB_USER}"

echo "==> Rendering the server configuration"
(umask 077; openssl rand -hex 32 > "${RUN_DIR}/ai-dba-server.secret")
(umask 077; cat > "${RUN_DIR}/ai-dba-server.yaml" <<YAML
# Generated by run-evaluation.sh for one evaluation run.
http:
  address: "127.0.0.1:${SERVER_PORT}"
  tls:
    enabled: false
  auth:
    enabled: true

# The staged incident databases are on 127.0.0.1.
connection_security:
  allow_internal_networks: true

database:
  host: "${E2E_DB_HOST}"
  port: ${E2E_DB_PORT}
  database: "${E2E_DB_NAME}"
  user: "${E2E_DB_USER}"
  password: "${E2E_DB_PASSWORD}"
  sslmode: "disable"

embedding:
  enabled: false

llm:
  provider: "${DEFAULT_PROVIDER}"
  model: "${DEFAULT_MODEL}"
  timeout_seconds: 300
${LLM_KEYS}
builtins:
  tools:
    similarity_search: false
    generate_embedding: false
    search_knowledgebase: false

secret_file: "${RUN_DIR}/ai-dba-server.secret"
data_dir: "${RUN_DIR}/data"
YAML
)

echo "==> Applying the collector datastore schema"
"${E2E_SCRIPTS}/apply-collector-schema.sh"

echo "==> Starting the server"
"${E2E_SERVER_BIN}" --config="${RUN_DIR}/ai-dba-server.yaml" > "${RUN_DIR}/logs/server.log" 2>&1 &
echo $! > "${RUN_DIR}/server.pid"
"${E2E_SCRIPTS}/wait-for-http.sh" "http://127.0.0.1:${SERVER_PORT}/health" 30

echo "==> Creating the harness user and token"
"${E2E_SCRIPTS}/bootstrap-admin.sh" >/dev/null
AI_DBA_EVALUATE_TOKEN="$("${E2E_SERVER_BIN}" --config="${RUN_DIR}/ai-dba-server.yaml" -add-token \
    -user "${E2E_ADMIN_USERNAME}" -token-note "evaluation harness" -token-expiry 1d \
    | awk '/^Token: / {print $2}')"
if [ -z "${AI_DBA_EVALUATE_TOKEN}" ]; then
    echo "ERROR: could not create a service token" >&2
    exit 1
fi
export AI_DBA_EVALUATE_TOKEN
DSN="postgres://${E2E_DB_USER}:${E2E_DB_PASSWORD}@${E2E_DB_HOST}:${E2E_DB_PORT}/${E2E_DB_NAME}?sslmode=disable"
export AI_DBA_EVALUATE_ADMIN_DSN="${DSN}"
export AI_DBA_EVALUATE_DATASTORE_DSN="${DSN}"

TARGET_FLAGS=()
for t in "${TARGETS[@]}"; do
    TARGET_FLAGS+=(-target "${t}")
done

echo "==> Replaying incidents"
cd "${REPO_ROOT}"
"${HARNESS_BIN}" run -server-url "http://127.0.0.1:${SERVER_PORT}" \
    -out "${OUT_DIR}" "${TARGET_FLAGS[@]}" "$@"
