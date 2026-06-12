#!/usr/bin/env bash
# Bridge for pgedge-builder-action, which hardcodes ./common/build.sh at
# the repo root. pgedge-builder-action is shared across many repos and
# changing it would affect all consumers — so we keep this 2-line shim
# at the contracted location and delegate to the real entry point
# inside packaging/scripts/.
exec "$(dirname "$0")/../packaging/scripts/build.sh" "$@"
