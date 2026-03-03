#!/usr/bin/env bash

# ------------------------------------------------------------------------------
# This script is used predominantly by CI in order to deploy a testing environment
# for running the chart tests in this repository. The testing environment includes
# a fully functional Kubernetes cluster, usually based on a local Kubernetes
# distribution like Kubernetes in Docker (KIND).
#
# Note: Callers are responsible for cleaning up after themselves, the testing env
#       created here can be torn down with `ktf environments delete --name <NAME>`.
# ------------------------------------------------------------------------------

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
cd "${SCRIPT_DIR}/.."

[[ -z ${KUBERNETES_VERSION} ]] && echo "ERROR: KUBERNETES_VERSION is not set" && exit 1

# ------------------------------------------------------------------------------
# Setup Tools - KTF
# ------------------------------------------------------------------------------

# ensure ktf command is accessible
if ! command -v ktf 1>/dev/null
then
	echo "ERROR: ktf command not found"
    exit 1
fi

# ------------------------------------------------------------------------------
# Configuration
# ------------------------------------------------------------------------------

TEST_ENV_NAME="${TEST_ENV_NAME:-kong-charts-tests}"
if [[ "$#" -eq 1 ]]
then
    if [[ "$1" == "cleanup" ]]
    then
        ktf environments delete --name "${TEST_ENV_NAME}"
        exit $?
    fi
fi

# ------------------------------------------------------------------------------
# Setup Tools - Docker
# ------------------------------------------------------------------------------

# ensure docker command is accessible
if ! command -v docker &> /dev/null
then
    echo "ERROR: docker command not found"
    exit 10
fi

# ensure docker is functional
docker info 1>/dev/null

# ------------------------------------------------------------------------------
# Setup Tools - Kind
# ------------------------------------------------------------------------------

# ensure kind command is accessible
if ! command -v kind &> /dev/null
then
    echo "ERROR: kind command not found"
    exit 1
fi

# ensure kind is functional
kind version 1>/dev/null

# ------------------------------------------------------------------------------
# Create Testing Environment
# ------------------------------------------------------------------------------
ktf environments create --name "${TEST_ENV_NAME}" --addon metallb --kubernetes-version "${KUBERNETES_VERSION}"
