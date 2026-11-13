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
# Run `make test-all` in each sub-project touched by the current branch.
#
# The changed files are those on the branch since it forked from BASE
# (git diff BASE...HEAD), plus staged, unstaged and untracked changes in
# the working tree. Sub-projects run serially, in the same order as the
# top-level `make test-all`, and the first failure stops the run.
#
# Environment:
#   BASE     Ref the branch is compared against (default: origin/main).
#   DRY_RUN  When set to anything but empty or 0, print the plan only.
#   MAKE     The make program to run (default: make).
#
# Dependency map (a change on the left tests everything on the right):
#   pkg/                               pkg, collector, server, alerter
#   collector/go.mod,
#   collector/src/database/            collector, server
#   collector/, server/, alerter/,
#   client/                            that sub-project only
#   e2e/                               nothing; prints a reminder that
#                                      `make test-e2e` is run separately

set -euo pipefail

BASE="${BASE:-origin/main}"
DRY_RUN="${DRY_RUN:-}"
MAKE="${MAKE:-make}"

# Execution order, matching the top-level test-all target.
ORDER="pkg collector server alerter client"

root="$(git rev-parse --show-toplevel)"
cd "$root"

if ! git rev-parse --verify --quiet "${BASE}^{commit}" > /dev/null; then
    echo "test-changed: base ref '${BASE}' not found;" \
         "run 'git fetch origin' or pass BASE=<ref>." >&2
    exit 2
fi

# Record why a sub-project was selected; each reason is listed once.
# Reasons live in variables named reason_<sub-project>, which avoids
# associative arrays so that the script also runs on bash 3.2 (macOS).
add_reason() {
    local var="reason_$1" current
    current="${!var:-}"
    case "; ${current}; " in
        *"; $2; "*) return ;;
    esac
    if [ -n "$current" ]; then
        printf -v "$var" '%s; %s' "$current" "$2"
    else
        printf -v "$var" '%s' "$2"
    fi
}

e2e_touched=""

changed_files() {
    git diff --name-only --no-renames "${BASE}...HEAD"
    git diff --name-only --no-renames HEAD
    git ls-files --others --exclude-standard
}

while IFS= read -r file; do
    case "$file" in
        pkg/*)
            for project in pkg collector server alerter; do
                add_reason "$project" "pkg/ changed"
            done
            ;;
        collector/go.mod | collector/src/database/*)
            add_reason collector "collector/ changed"
            add_reason server "collector schema or go.mod changed"
            ;;
        collector/*) add_reason collector "collector/ changed" ;;
        server/*) add_reason server "server/ changed" ;;
        alerter/*) add_reason alerter "alerter/ changed" ;;
        client/*) add_reason client "client/ changed" ;;
        e2e/*) e2e_touched=1 ;;
    esac
done < <(changed_files | sort -u)

selected=""
for project in $ORDER; do
    var="reason_${project}"
    if [ -n "${!var:-}" ]; then
        selected="${selected:+$selected }$project"
    fi
done

echo "test-changed: comparing against ${BASE} (merge base" \
     "$(git merge-base "$BASE" HEAD | cut -c1-12)) plus uncommitted changes"

if [ -n "$e2e_touched" ]; then
    echo "test-changed: e2e/ changed; run 'make test-e2e' separately" \
         "(it needs Docker and is not part of test-all)."
fi

if [ -z "$selected" ]; then
    echo "test-changed: no sub-project changes found; nothing to test."
    exit 0
fi

echo "test-changed: selected sub-projects:"
for project in $selected; do
    var="reason_${project}"
    printf '  %-10s %s\n' "$project" "${!var}"
done

case "$DRY_RUN" in
    "" | 0) ;;
    *)
        echo "test-changed: DRY_RUN set; not running any tests."
        exit 0
        ;;
esac

for project in $selected; do
    echo "test-changed: running test-all in ${project}..."
    "$MAKE" -C "$root/$project" test-all
done

echo "test-changed: all selected sub-projects passed ($selected)."
