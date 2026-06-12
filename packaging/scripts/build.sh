#!/usr/bin/env bash
set -euo pipefail

COMPONENT_NAME=$1

# This script lives at <component>/scripts/build.sh; the component dir is
# the parent of the scripts dir. We resolve both with cd+pwd so the path
# lookups don't depend on how the caller spelled $0 — the bridge wrapper
# at ./common/build.sh exec's us with a relative path containing "..".
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPONENT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

source "${COMPONENT_DIR}/common.sh"

COMMON_FILE="${SCRIPT_DIR}/common-functions.sh"

if [ -f "$COMMON_FILE" ]; then
  source "$COMMON_FILE"
else
  echo "Error: $COMMON_FILE not found!" >&2
  exit 1
fi

###########
# Main
###########
detect_os_type
prepare
build
post_build
