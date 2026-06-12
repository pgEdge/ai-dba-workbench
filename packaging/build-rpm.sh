#!/bin/bash
set -euo pipefail

RHEL="$(rpm --eval %rhel)"
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
  ARCH="arm64"
elif [ "$ARCH" = "x86_64" ]; then
  ARCH="amd64"
fi

prepare() {
  setup_dnf_build_env

  echo "Copying packaging files..."
  cp ${COMPONENT_NAME}/rpm/ai-dba-workbench.spec ~/rpmbuild/SPECS/

  # Source tarballs.  The release.yml workflow stages the per-arch Go
  # tarballs + ai-dba-client.tar.gz under ./release-artifacts/ before
  # invoking pgedge-builder-action, so we always have them locally on the
  # CI path.  For standalone manual builds (no workflow), fall back to
  # the published GitHub release.
  STAGING="${LOCAL_STAGING_DIR:-release-artifacts}"
  if [ -d "$STAGING" ]; then
    echo "Using locally-staged tarballs from ${STAGING}/"
    cp "${STAGING}/ai-dba-server-linux-${ARCH}.tar.gz"    ~/rpmbuild/SOURCES/
    cp "${STAGING}/ai-dba-collector-linux-${ARCH}.tar.gz" ~/rpmbuild/SOURCES/
    cp "${STAGING}/ai-dba-alerter-linux-${ARCH}.tar.gz"   ~/rpmbuild/SOURCES/
    cp "${STAGING}/ai-dba-client.tar.gz"                  ~/rpmbuild/SOURCES/
  else
    echo "No local staging dir; downloading release tarballs from GitHub..."
    BASE_URL="https://github.com/pgEdge/ai-dba-workbench/releases/download/${AI_DBA_WORKBENCH_BRANCH}"
    wget -P ~/rpmbuild/SOURCES/ "${BASE_URL}/ai-dba-server-linux-${ARCH}.tar.gz"
    wget -P ~/rpmbuild/SOURCES/ "${BASE_URL}/ai-dba-collector-linux-${ARCH}.tar.gz"
    wget -P ~/rpmbuild/SOURCES/ "${BASE_URL}/ai-dba-alerter-linux-${ARCH}.tar.gz"
    wget -P ~/rpmbuild/SOURCES/ "${BASE_URL}/ai-dba-client.tar.gz"
  fi

  # YAML configs + LICENSE/README are tracked in the repo, so we always
  # source them from the local checkout (no network roundtrip needed).
  cp examples/ai-dba-server.yaml    ~/rpmbuild/SOURCES/
  cp examples/ai-dba-collector.yaml ~/rpmbuild/SOURCES/
  cp examples/ai-dba-alerter.yaml   ~/rpmbuild/SOURCES/
  cp LICENSE.md                     ~/rpmbuild/SOURCES/
  cp README.md                      ~/rpmbuild/SOURCES/

  # Patch server config to use port 8443
  sed -i 's/8080/8443/g' ~/rpmbuild/SOURCES/ai-dba-server.yaml

  cp ${COMPONENT_NAME}/common/pgedge-ai-dba-server.service ~/rpmbuild/SOURCES/
  cp ${COMPONENT_NAME}/common/pgedge-ai-dba-collector.service ~/rpmbuild/SOURCES/
  cp ${COMPONENT_NAME}/common/pgedge-ai-dba-alerter.service ~/rpmbuild/SOURCES/
  cp ${COMPONENT_NAME}/common/pgedge-ai-dba-client.nginx ~/rpmbuild/SOURCES/

  # This function is for debugging purpose if you have your own keys. GH workflow does not need it.
  #import_gpg_keys

  echo "Installing RPM build dependencies..."
  dnf builddep -y \
    --define "ai_dba_workbench_version ${AI_DBA_WORKBENCH_VERSION}" \
    --define "ai_dba_workbench_buildnum ${AI_DBA_WORKBENCH_BUILDNUM}" \
    --define "arch ${ARCH}" \
    ~/rpmbuild/SPECS/ai-dba-workbench.spec
}

build() {
  echo "Building RPM and SRPM..."
  QA_RPATHS=$(( 0xffff )) rpmbuild -ba ~/rpmbuild/SPECS/ai-dba-workbench.spec \
    --define "ai_dba_workbench_version ${AI_DBA_WORKBENCH_VERSION}" \
    --define "ai_dba_workbench_buildnum ${AI_DBA_WORKBENCH_BUILDNUM}" \
    --define "arch ${ARCH}"
}

post_build() {
  echo "Copying built RPMs to /output..."
  mkdir -p /output
  cp -v ~/rpmbuild/RPMS/*/*.rpm /output/ || echo "No binary RPMs found"
  cp -v ~/rpmbuild/SRPMS/*.src.rpm /output/ || echo "No SRPM found"

  sign_rpms /output/*.rpm
  validate_signatures /output/*.rpm
}
