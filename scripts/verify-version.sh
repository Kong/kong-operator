#!/bin/bash

set -o nounset
set -e

get_tag() {
  tag="$(git describe --tags --exact-match HEAD 2>/dev/null)"
  if [[ $? -eq 0 ]]; then
    echo $(git describe --tags --exact-match HEAD)
  else
    echo ""
  fi
}

get_commit() {
  git rev-parse --short HEAD
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

is_set() {
  local FIELD="${1}"
  local GOT_VERSION="${2}"

  if jq -e ".${FIELD} // \"empty\" | test(\"empty\")" <<<"${GOT_VERSION}" >/dev/null 2>/dev/null; then
    printf "${FIELD} is unset\n"
    exit 1
  fi
  if jq -e ".${FIELD} | test(\"NOT_SET\")" <<<"${GOT_VERSION}" >/dev/null; then
    printf "${FIELD} is 'NOT_SET' but shouldn't be\n"
    exit 1
  fi
}

field_matches() {
  local FIELD="${1}"
  local EXPECTED="${2}"
  local GOT_VERSION="${3}"

  if jq -e ".${FIELD} | test(\"${EXPECTED}\")" <<<"${GOT_VERSION}" >/dev/null; then
    printf "${FIELD} matches expected %s\n" "${EXPECTED}"
  else
    printf "${FIELD} value '%s' does not match, expected %s\n" "$(jq -e \".${FIELD}\")" "${EXPECTED}"
    exit 1
  fi
}

if [[ "${#}" != 1 ]]; then
  echo "Usage:"
  echo "verify-version.sh <GITHUB_REPO>"
  echo "    * the version json is read from stdin"
  echo
  echo "Example:"
  echo "echo '{\"release\":\"v10.0.2\",\"repo\":\"https://github.com/kong/kong-operator.git\",\"commit\":\"033624266c256a486effa169558c7ec834254c95\"}' | verify-version.sh kong/kong-operator"
  exit 1
fi

REPO="https://github.com/${1}"
COMMIT="$(get_commit)"
RELEASE="$(get_release)"

EXPECTED_VERSION="{\"release\":\"${RELEASE}\",\"repo\":\"${REPO}\",\"commit\":\"${COMMIT}\"}"
EXPECTED_VERSION="$(jq --sort-keys <<< ${EXPECTED_VERSION})"
GOT_VERSION="$(jq --sort-keys <&0)"

echo "received version info: ${GOT_VERSION}"

is_set "commit" "${GOT_VERSION}"
is_set "repo" "${GOT_VERSION}"
is_set "release" "${GOT_VERSION}"

field_matches "commit" "${COMMIT}" "${GOT_VERSION}"
field_matches "repo" "${REPO}" "${GOT_VERSION}"
# We don't check the the .release field because this script will be run with
# many pseudo tags that are pushed to container registry like v0.8.0-arm64 or sha-1a9b278-amd64.

echo "Version information match"
