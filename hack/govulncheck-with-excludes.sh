#!/usr/bin/env bash
set -Eeuo pipefail

# This script is a wrapper around "govulncheck" which allows for excluding vulnerabilities.
# It's highly inspired by https://github.com/tianon/gosu/blob/9dc5d8d7556e44d382b9f71734197b5071682c31/govulncheck-with-excludes.sh.
# When govulncheck supports excluding vulnerabilities, this script should be removed: https://github.com/golang/go/issues/59507

excludeVulns="$(jq -nc '[

  # Kubernetes kube-apiserver Vulnerable to Race Condition
  # It is not relevant to us as we do not run kube-apiserver itself or import code that is affected by this vulnerability.
  # https://github.com/kubernetes/kubernetes/issues/126587
  "GO-2025-3547",

  # Kubernetes GitRepo Volume Inadvertent Local Repository Access in k8s.io/kubernetes
  # We do not use the GitRepo volume type.
  # https://github.com/kubernetes/kubernetes/issues/130786
  "GO-2025-3521"

]')"
export excludeVulns

if out="$(${GOVULNCHECK} -scan "${SCAN}" -show color,verbose "$@")"; then
  printf '%s\n' "$out"
  exit 0
fi

json="$(${GOVULNCHECK} -json -scan "${SCAN}" "$@")"

# Depending on SCAN variable, we will filter vulns either by:
# - a 'function' key in the .finding.trace array first entry (SCAN=symbol)
# - a 'package' key in the .finding.trace array first entry (SCAN=package)
case "${SCAN}" in
  symbol)
    filter_by="function"
    ;;
  package)
    filter_by="package"
    ;;
  *)
    echo "Error: Unexpected SCAN value: ${SCAN}" >&2
    exit 1
    ;;
esac

vulns="$(jq --arg filter_by "$filter_by" <<<"$json" -cs '
  (
    map(
      .osv // empty
      | { key: .id, value: . }
    )
    | from_entries
  ) as $meta
  # https://github.com/tianon/gosu/issues/144
  | map(
    .finding // empty
    # https://github.com/golang/vuln/blob/3740f5cb12a3f93b18dbe200c4bcb6256f8586e2/internal/scan/template.go#L97-L104
    | select((.trace[0].[$filter_by] // "") != "")
    | .osv
  )
  | unique
  | map($meta[.])
')"
if [ "$(jq <<<"$vulns" -r 'length')" -le 0 ]; then
  printf '%s\n' "$out"
  exit 1
fi

filtered="$(jq <<<"$vulns" -c '
  (env.excludeVulns | fromjson) as $exclude
  | map(select(
    .id as $id
    | $exclude | index($id) | not
  ))
')"

text="$(jq <<<"$filtered" -r 'map("- \(.id) (aka \(.aliases | join(", ")))\n\n\t\(.details | gsub("\n"; "\n\t"))") | join("\n\n")')"

if [ -z "$text" ]; then
  printf 'No vulnerabilities found.\n'
  exit 0
else
  printf '%s\n' "$text"
  exit 1
fi
