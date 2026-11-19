#!/usr/bin/env bash
# Check real gw-admin's complete flat list against Kubernetes policy identities.
set -euo pipefail

gateway=$1
shift
policies=$(kubectl get eventgatewayvirtualclusterconsumepolicies.configuration.konghq.com \
  -n "${NAMESPACE:?}" parse first second root -o json)
expected=$(jq -ce --args '
  INDEX(.items[]; .metadata.name) as $policies |
  [$ARGS.positional[] as $name | $policies[$name] |
    {id: .metadata.uid, name: .spec.apiSpec[.spec.apiSpec.type].name,
     type: ({schemaValidation: "schema_validation", modifyHeaders: "modify_headers",
             skipRecord: "skip_record"}[.spec.apiSpec.type] // error("unsupported policy type")),
     parent_policy_id: (if .spec.apiSpec.parentPolicyRef then
       $policies[.spec.apiSpec.parentPolicyRef.name].metadata.uid
       else null end)}]
' -- "$@" <<<"$policies")
vc_uid=$(kubectl get eventgatewayvirtualcluster.configuration.konghq.com \
  -n "$NAMESPACE" vcluster -o json | jq -e '.metadata.uid')

bash ../onprem-shared/onprem.sh wait-gwadmin-entities "$gateway" 150 "
  (.entities.virtual_clusters | length == 1) and
  (.entities.virtual_clusters[0].id == $vc_uid) and
  ([.entities.virtual_clusters[0].consume_policies[] |
    {id, name, type, parent_policy_id: (.parent_policy_id // null)}] == $expected)"
