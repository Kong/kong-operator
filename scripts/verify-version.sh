#!/bin/bash

set -o nounset
set -o pipefail
# set -x

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
DIFF="$(diff <(echo ${EXPECTED_VERSION}) <(echo ${GOT_VERSION}) )"

if [[ "${DIFF}" != "" ]]; then
  echo "Versions do not match!"
  echo "Expected:"
  echo "${EXPECTED_VERSION}"
  echo "Got:"
  echo "${GOT_VERSION}"
  exit 1
fi

echo "Versions match"
