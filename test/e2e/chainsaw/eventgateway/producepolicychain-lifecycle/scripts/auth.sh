#!/bin/bash
# Restore the test-owned credential before cleanup, preserving the scenario's exit status.
restore_auth_secret() {
  local status="$?"
  trap - EXIT
  jq -n '
    {apiVersion: "v1", kind: "Secret",
     metadata: {name: env.AUTH_SECRET_NAME, namespace: env.NAMESPACE,
                labels: {"konghq.com/secret": "true", "konghq.com/credential": "konnect"}},
     type: "Opaque", stringData: {token: env.KONNECT_TOKEN}}
  ' | kubectl apply -f - >/dev/null || {
    echo "could not restore the test credential Secret" >&2
    exit 1
  }
  RESOURCE_TYPE=konnectapiauthconfigurations.konnect.konghq.com RESOURCE_NAME="${AUTH_NAME}" CONDITION_TYPE=APIAuthValid \
    JQ_CONDITION='.metadata.generation as $g | any(.status.conditions[]?;
      .type == "APIAuthValid" and .status == "True" and .observedGeneration == $g)' \
    bash "${COMMON}/wait_for_resource_reconciled.sh" >&2 || {
      echo "the test authentication did not recover" >&2
      exit 1
    }
  exit "${status}"
}
