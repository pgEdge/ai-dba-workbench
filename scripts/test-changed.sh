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
# (its merge base with HEAD), plus staged, unstaged and untracked changes
# in the working tree. Sub-projects run serially, in the same order as the
# top-level `make test-all`, and the first failure stops the run.
#
# Environment:
#   BASE     Ref the branch is compared against (default: origin/main).
#   DRY_RUN  When set to anything but empty or 0, print the plan only.
#   MAKE     The make program to run (default: make).
#
# Dependency map (a change on the left tests everything on the right):
#   pkg/                               pkg, collector, server, alerter
#   collector/src/database/            collector, server, alerter (the
#                                      alerter's tests read schema.go)
#   collector/go.mod,
#   collector/src/coldfront/           collector, server (the server
#                                      builds them through the schema)
#   examples/, docker/config/          alerter (its tests parse the
#                                      alerter configs there)
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

# The branch's changes are diffed from the merge base. Without one (a
# shallow clone, or a BASE unrelated to HEAD) there is nothing to diff
# from, and testing nothing would look like success, so stop instead.
if ! merge_base="$(git merge-base "$BASE" HEAD)"; then
    echo "test-changed: no merge base between '${BASE}' and HEAD;" \
         "fetch more history (git fetch --unshallow) or pass BASE=<ref>." >&2
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

# Collect the changed paths NUL-delimited, so that names git would
# otherwise quote (non-ASCII, embedded newlines) still match below, into a
# temporary file rather than a process substitution, so that a failing git
# command stops the script under set -e. Duplicates are harmless: each
# reason is recorded once per sub-project.
changed="$(mktemp)"
trap 'rm -f "$changed"' EXIT
git diff -z --name-only --no-renames "${merge_base}" HEAD -- > "$changed"
git diff -z --name-only --no-renames HEAD -- >> "$changed"
git ls-files -z --others --exclude-standard >> "$changed"

while IFS= read -r -d '' file; do
    case "$file" in
        pkg/*)
            for project in pkg collector server alerter; do
                add_reason "$project" "pkg/ changed"
            done
            ;;
        collector/src/database/*)
            add_reason collector "collector/ changed"
            add_reason server "collector schema changed"
            add_reason alerter "collector schema changed"
            ;;
        collector/go.mod | collector/src/coldfront/*)
            add_reason collector "collector/ changed"
            add_reason server "collector go.mod or coldfront changed"
            ;;
        examples/* | docker/config/*)
            add_reason alerter "example or docker config changed"
            ;;
        collector/*) add_reason collector "collector/ changed" ;;
        server/*) add_reason server "server/ changed" ;;
        alerter/*) add_reason alerter "alerter/ changed" ;;
        client/*) add_reason client "client/ changed" ;;
        e2e/*) e2e_touched=1 ;;
    esac
done < "$changed"

selected=""
for project in $ORDER; do
    var="reason_${project}"
    if [ -n "${!var:-}" ]; then
        selected="${selected:+$selected }$project"
    fi
done

echo "test-changed: comparing against ${BASE} (merge base" \
     "${merge_base:0:12}) plus uncommitted changes"

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
