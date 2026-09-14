#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# SessionStart hook: report plugin skills and binaries that the standing
# instructions in CLAUDE.md depend on but that are missing from this
# developer environment. Tooling is installed centrally, so this script
# only reports; it never installs anything and always exits 0. Output on
# stdout is injected into the session context, where CLAUDE.md tells the
# agent to stop and report any line starting "TOOLING MISSING:".

set -u

CACHE="${HOME}/.claude/plugins/cache"

missing() {
    printf 'TOOLING MISSING: %s (%s)\n' "$1" "$2"
}

# Plugin skills, located via the plugin cache so that no CLI call is needed.
check_skill() {
    local plugin_path="$1" skill="$2" hint="$3"
    local found=""
    for f in "${CACHE}"/${plugin_path}/*/skills/"${skill}"/SKILL.md; do
        if [ -f "$f" ]; then
            found="$f"
            break
        fi
    done
    [ -n "$found" ] || missing "skill ${skill}" "$hint"
}

check_skill "pgedge-skills/pgedge-skills" pgedge-docs "pgedge-skills plugin"
check_skill "pgedge-skills/pgedge-skills" pgedge-psql "pgedge-skills plugin"
check_skill "claude-plugins-official/superpowers" using-superpowers \
    "superpowers plugin"

# Binaries that CLAUDE.md assumes are on PATH.
check_bin() {
    command -v "$1" >/dev/null 2>&1 || missing "$1" "$2"
}

check_bin playwright-cli "playwright-cli skill needs @playwright/cli"
check_bin gofmt "Go toolchain"
check_bin golangci-lint "Go linting"
check_bin npm "Node toolchain for client and e2e"
check_bin gh "GitHub CLI for PR workflow"

exit 0
