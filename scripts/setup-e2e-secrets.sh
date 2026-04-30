#!/usr/bin/env bash
#
# Generate secrets for the E2E Docker Compose stack.
#
# Usage:
#   bash scripts/setup-e2e-secrets.sh
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SECRET_DIR="${PROJECT_ROOT}/docker/secret"

mkdir -p "${SECRET_DIR}"

echo "Generating secrets in ${SECRET_DIR}..."

openssl rand -base64 32 > "${SECRET_DIR}/ai-dba.secret"
echo "postgres" > "${SECRET_DIR}/pg-password"

echo "Secrets generated:"
echo "  ${SECRET_DIR}/ai-dba.secret"
echo "  ${SECRET_DIR}/pg-password"
