#!/usr/bin/env bash
# common.sh - Common environment variables

export AI_DBA_WORKBENCH_BRANCH=${COMPONENT_BRANCH:-v1.0.0}
export AI_DBA_WORKBENCH_VERSION=${COMPONENT_VERSION:-1.0.0}
export AI_DBA_WORKBENCH_BUILDNUM=${COMPONENT_BUILDNUM:-1}
export REPO_TYPE="${REPO_TYPE:-daily}"

# For DEB builds, when BUILDNUM follows the pgEdge pre-release convention
# '<pretag>_<revision>' (e.g., 'beta3_1', 'rc1_1'), move the pretag from
# the debian revision into the upstream VERSION with a leading '~'. This
# follows Debian Policy 5.6.12: stable releases will then sort ABOVE
# pre-releases because '~' sorts before "end of part" in dpkg comparison.
#
# Without this, a beta revision like 'beta3~1.bullseye' would sort ABOVE
# the stable revision '1.bullseye' (since "" sorts before letters in
# non-digit prefix comparison), and reprepro would keep the beta and
# reject incoming stable DEBs on rebuild.
#
# Examples:
#   BUILDNUM=1        → VERSION unchanged,            BUILDNUM=1
#   BUILDNUM=beta3_1  → VERSION='1.0.0~beta3',        BUILDNUM=1
# Composed in build-deb.sh as ${VERSION}-${BUILDNUM}.${DISTRO}:
#   stable: '1.0.0-1.bullseye'
#   beta:   '1.0.0~beta3-1.bullseye'  (dpkg: this < '1.0.0-1.bullseye' ✓)
if command -v apt-get &>/dev/null; then
    if [[ "$AI_DBA_WORKBENCH_BUILDNUM" == *_* ]]; then
        AI_DBA_WORKBENCH_PRETAG="${AI_DBA_WORKBENCH_BUILDNUM%%_*}"
        export AI_DBA_WORKBENCH_VERSION="${AI_DBA_WORKBENCH_VERSION}~${AI_DBA_WORKBENCH_PRETAG}"
        AI_DBA_WORKBENCH_BUILDNUM="${AI_DBA_WORKBENCH_BUILDNUM##*_}"
    fi
fi
