#!/bin/bash
set -euo pipefail

# Script adapted from https://github.com/kubernetes/kubernetes/issues/79384#issuecomment-521493597.

VERSION=$(cat go.mod | grep "k8s.io/kubernetes v" | sed "s/^.*v\([0-9.]*\).*/\1/")
echo "Updating k8s.io go.mod replace directives for k8s.io/kubernetes@v$VERSION"

MODS=($(
    curl -sS https://raw.githubusercontent.com/kubernetes/kubernetes/v${VERSION}/go.mod |
    sed -n 's|.*k8s.io/\(.*\) => ./staging/src/k8s.io/.*|k8s.io/\1|p'
))

# Set concurrency level to the number of available CPU cores
CONCURRENCY=$(nproc)
export VERSION

# Create an empty array to store replace directives
declare -a REPLACE_COMMANDS

# Function to generate replace directive for a module
generate_replace_command() {
    local MOD=$1
    local V=$(
        go mod download -json "${MOD}@kubernetes-${VERSION}" |
        sed -n 's|.*"Version": "\(.*\)".*|\1|p'
    )
    echo "-replace=${MOD}=${MOD}@${V}"
}

# Export function for access in subshells run by xargs
export -f generate_replace_command

# Run in parallel to collect replace directives
echo "Collecting replace directives for ${#MODS[@]} modules concurrently (N=${CONCURRENCY})"
REPLACE_COMMANDS=($(printf "%s\n" "${MODS[@]}" | xargs -P "$CONCURRENCY" -n 1 -I {} bash -c 'generate_replace_command "$@"' _ {}))

# Apply each replace directive serially
for CMD in "${REPLACE_COMMANDS[@]}"; do
    echo "Applying go.mod $CMD"
    go mod edit "$CMD"
done

go get "k8s.io/kubernetes@v${VERSION}"
