#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SUBMODULE_STATUS="$(git submodule status)"

# Uninitialized submodules will have a '-' in the first column.
# If we find one, we exit with an error.
if [[ $SUBMODULE_STATUS =~ ^- ]]; then 
    exit 1; 
else 
    exit 0;
fi

