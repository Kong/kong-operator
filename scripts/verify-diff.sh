#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

DIR="${1}"
shift
# The other arguments are directories only generators write to: a file there
# not committed yet is out of date too, and git diff doesn't show it. Other
# directories may hold untracked files of the build.
UNTRACKED=""
if [ "$#" -gt 0 ]; then
  UNTRACKED="$(git ls-files --others --exclude-standard -- "$@")"
fi

if git diff --quiet "${DIR}" && [ -z "${UNTRACKED}" ]
then
  echo "${DIR} up to date."
else
  echo "${DIR} appears to be out of date (make sure you've run 'make manifests' and 'make generate')"
  echo "Diff output:"
  git --no-pager diff "${DIR}"
  if [ -n "${UNTRACKED}" ]; then
    echo "Untracked files:"
    echo "${UNTRACKED}"
  fi
  exit 1
fi
