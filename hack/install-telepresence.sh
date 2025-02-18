#!/usr/bin/env bash
set -euo pipefail

# Note: this is just a quick workaround script to install telepresence to be used in conformance tests.
# Ideally we should use a more robust solution (e.g. mise).

# Determine OS and exit if unsupported
case "$OSTYPE" in
  darwin*) OS="darwin" ;;
  linux*)  OS="linux" ;;
  *) echo "Error: Unsupported OS $OSTYPE. This script only supports darwin and linux." >&2; exit 1 ;;
esac

# Determine architecture and map to suffix
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)     ARCH_SUFFIX="amd64" ;;
  arm64|aarch64) ARCH_SUFFIX="arm64" ;;
  *) echo "Error: Unsupported architecture $ARCH." >&2; exit 1 ;;
esac

# Construct artifact name
ARTIFACT_URL="https://github.com/telepresenceio/telepresence/releases/download/v2.21.3/telepresence-${OS}-${ARCH_SUFFIX}"

# Download and install telepresence
echo "Downloading telepresence from ${ARTIFACT_URL}"
curl -L "${ARTIFACT_URL}" -o ./bin/telepresence
chmod +x ./bin/telepresence
