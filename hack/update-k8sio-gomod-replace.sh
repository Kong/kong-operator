#!/bin/bash
set -euo pipefail

# Script adapted from https://github.com/kubernetes/kubernetes/issues/79384#issuecomment-521493597.

VERSION=$(cat go.mod | grep "k8s.io/kubernetes v" | sed "s/^.*v\([0-9.]*\).*/\1/")
echo "Updating k8s.io go.mod replace directives for k8s.io/kubernetes@v$VERSION"

MODS=($(
    curl -sS https://raw.githubusercontent.com/kubernetes/kubernetes/v${VERSION}/go.mod |
    sed -n 's|.*k8s.io/\(.*\) => ./staging/src/k8s.io/.*|k8s.io/\1|p'
))

# Export variables for access in subshells run by xargs
export VERSION

# Function to update go.mod replace directive for a module
update_mod() {
    local MOD=$1
    local V=$(
        go mod download -json "${MOD}@kubernetes-${VERSION}" |
        sed -n 's|.*"Version": "\(.*\)".*|\1|p'
    )
    echo "Updating go.mod replace directive for ${MOD}"
    go mod edit "-replace=${MOD}=${MOD}@${V}"
}

# Export function for access in subshells run by xargs
export -f update_mod

# Run the updates concurrently
CONCURRENCY=$(nproc)
echo "[DEBUG] Updating modules concurrently with N=${CONCURRENCY}"
printf "%s\n" "${MODS[@]}" | xargs -P "${CONCURRENCY}" -n 1 -I {} bash -c 'update_mod "$@"' _ {}

go get "k8s.io/kubernetes@v${VERSION}"
