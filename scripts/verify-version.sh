#!/bin/bash

set -o nounset

get_tag() {
  tag="$(git describe --tags --exact-match HEAD 2>/dev/null)"
  if [[ $? -eq 0 ]]; then
    echo $(git describe --tags --exact-match HEAD)
  else
    echo ""
  fi
}

get_commit() {
  git describe --always --abbrev=40
}

get_branch() {
  git rev-parse --abbrev-ref HEAD
}

get_release() {
  tag="$(get_tag)"
  if [[ -n "${tag}" ]]; then
    echo "${tag}"
  else
    echo "$(get_branch)"
  fi
}

if [[ "${#}" != 1 ]]; then
  echo "Usage:"
  echo "verify-version.sh <GITHUB_REPO>"
  echo "    * the version json is read from stdin"
  echo
  echo "Example:"
  echo "echo '{\"release\":\"v10.0.2\",\"repo\":\"https://github.com/Kong/gateway-operator.git\",\"commit\":\"033624266c256a486effa169558c7ec834254c95\"}' | verify-version.sh Kong/gateway-operator"
  exit 1
fi

REPO="https://github.com/${1}.git"
COMMIT="$(get_commit)"
RELEASE="$(get_release)"

EXPECTED_VERSION="{\"release\":\"${RELEASE}\",\"repo\":\"${REPO}\",\"commit\":\"${COMMIT}\"}"
EXPECTED_VERSION="$(jq --sort-keys <<< ${EXPECTED_VERSION})"
GOT_VERSION="$(jq --sort-keys <&0)"

echo "received version info: ${GOT_VERSION}"
echo "${GOT_VERSION}" | jq -e ".commit | test(\"${COMMIT}\")" || (echo "commit doesn't match: ${GOT_VERSION}" && exit 1)
echo "${GOT_VERSION}" | jq -e ".repo | test(\"${REPO}\")" || (echo "repo doesn't match: ${GOT_VERSION}" && exit 1)
# We don't check the the .release field because this script will be run with
# many pseudo tags that are pushed to container registry like v0.8.0-arm64 or sha-1a9b278-amd64.
echo "${GOT_VERSION}" | jq -e ".release" || (echo "release doesn't exist in provided version info: ${GOT_VERSION}" && exit 1)

echo "Version information match"
