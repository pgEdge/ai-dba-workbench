#!/usr/bin/env bash
set -euo pipefail

BUILD_DIR="/tmp/pg_deb_build"
SRC_DIR="${BUILD_DIR}/src"
CWD="$(pwd)"
export DEBIAN_FRONTEND=noninteractive

ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
  ARCH="arm64"
elif [ "$ARCH" = "x86_64" ]; then
  ARCH="amd64"
fi

prepare() {
  setup_apt_build_env

  # This function is for debugging purpose if you have your own keys. GH workflow does not need it.
  #import_gpg_keys

  rm -rf "$SRC_DIR"
  mkdir -p "$SRC_DIR"/{server,collector,alerter,client}

  # Source tarballs.  The release.yml workflow stages the per-arch Go
  # tarballs + ai-dba-client.tar.gz under ./release-artifacts/ before
  # invoking pgedge-builder-action, so we always have them locally on the
  # CI path.  For standalone manual builds (no workflow), fall back to
  # the published GitHub release.
  STAGING="${LOCAL_STAGING_DIR:-${CWD}/release-artifacts}"
  if [ -d "$STAGING" ]; then
    echo "Using locally-staged tarballs from ${STAGING}/"
    tar -xzf "${STAGING}/ai-dba-server-linux-${ARCH}.tar.gz"    -C "$SRC_DIR/server"
    tar -xzf "${STAGING}/ai-dba-collector-linux-${ARCH}.tar.gz" -C "$SRC_DIR/collector"
    tar -xzf "${STAGING}/ai-dba-alerter-linux-${ARCH}.tar.gz"   -C "$SRC_DIR/alerter"
    tar -xzf "${STAGING}/ai-dba-client.tar.gz"                  -C "$SRC_DIR/client"
  else
    echo "No local staging dir; downloading release tarballs from GitHub..."
    BASE_URL="https://github.com/pgEdge/ai-dba-workbench/releases/download/${AI_DBA_WORKBENCH_BRANCH}"
    wget -P "${SRC_DIR}/.tmp" "${BASE_URL}/ai-dba-server-linux-${ARCH}.tar.gz"
    wget -P "${SRC_DIR}/.tmp" "${BASE_URL}/ai-dba-collector-linux-${ARCH}.tar.gz"
    wget -P "${SRC_DIR}/.tmp" "${BASE_URL}/ai-dba-alerter-linux-${ARCH}.tar.gz"
    wget -P "${SRC_DIR}/.tmp" "${BASE_URL}/ai-dba-client.tar.gz"
    tar -xzf "${SRC_DIR}/.tmp/ai-dba-server-linux-${ARCH}.tar.gz"    -C "$SRC_DIR/server"
    tar -xzf "${SRC_DIR}/.tmp/ai-dba-collector-linux-${ARCH}.tar.gz" -C "$SRC_DIR/collector"
    tar -xzf "${SRC_DIR}/.tmp/ai-dba-alerter-linux-${ARCH}.tar.gz"   -C "$SRC_DIR/alerter"
    tar -xzf "${SRC_DIR}/.tmp/ai-dba-client.tar.gz"                  -C "$SRC_DIR/client"
    rm -rf "${SRC_DIR}/.tmp"
  fi

  # YAML configs + LICENSE/README are tracked in the repo, so we always
  # source them from the local checkout.
  cp "${CWD}/examples/ai-dba-server.yaml"    "$SRC_DIR/"
  cp "${CWD}/examples/ai-dba-collector.yaml" "$SRC_DIR/"
  cp "${CWD}/examples/ai-dba-alerter.yaml"   "$SRC_DIR/"
  cp "${CWD}/LICENSE.md"                     "$SRC_DIR/"
  cp "${CWD}/README.md"                      "$SRC_DIR/"

  # Patch server config to use port 8443
  sed -i 's/8080/8443/g' "$SRC_DIR/ai-dba-server.yaml"

  echo "Copying Debian packaging files..."
  cp -r "${CWD}/${COMPONENT_NAME}/deb/debian" "$SRC_DIR/"

  # Copy service files into debian/
  cp "${CWD}/${COMPONENT_NAME}/common/pgedge-ai-dba-server.service" "$SRC_DIR/debian/"
  cp "${CWD}/${COMPONENT_NAME}/common/pgedge-ai-dba-collector.service" "$SRC_DIR/debian/"
  cp "${CWD}/${COMPONENT_NAME}/common/pgedge-ai-dba-alerter.service" "$SRC_DIR/debian/"
  cp "${CWD}/${COMPONENT_NAME}/common/pgedge-ai-dba-client.nginx" "$SRC_DIR/debian/pgedge-ai-dba-client.nginx"

  echo "Installing build dependencies..."
  cd "$SRC_DIR"
  sudo apt-get update
  sudo apt-get build-dep -y .
}

build() {
  cd "$SRC_DIR"

  echo "Building Debian packages..."
  DISTRO=$(lsb_release -cs)

  cat > debian/changelog <<EOF
pgedge-ai-dba-workbench (${AI_DBA_WORKBENCH_VERSION}-${AI_DBA_WORKBENCH_BUILDNUM}.${DISTRO}) stable; urgency=medium

  * pgEdge AI DBA Workbench ${AI_DBA_WORKBENCH_VERSION}-${AI_DBA_WORKBENCH_BUILDNUM}

 -- pgEdge Build Team <support@pgedge.com>  $(date -R)
EOF

  dpkg-buildpackage -us -uc -b
}

post_build() {
  echo "Copying .deb packages to output..."
  sudo mkdir -p "/output"
  rename_ddeb_packages $BUILD_DIR
  sudo cp "$BUILD_DIR"/*.deb "/output" || echo "No .deb packages found."
}
