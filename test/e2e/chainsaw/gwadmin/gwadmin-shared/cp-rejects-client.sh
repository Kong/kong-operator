#!/bin/sh
# Runs in the peers pod (gwadmin.sh cp-rejects-client): the gw-admin pods are
# only reachable in the cluster. Every gw-admin pod's cp listener accepts
# gwadmin-dp-client and rejects the rogue client certificate.
# Usage: cp-rejects-client.sh gwadmin rogue_crt_base64 rogue_key_base64
set -eu
gw=$1
NS=$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)
addr() { case $1 in *:*) echo "[$1]" ;; *) echo "$1" ;; esac; }
cp="$gw-cp.$NS.svc"
d=$(mktemp -d)
echo "$2" | base64 -d >"$d/rogue.crt"
echo "$3" | base64 -d >"$d/rogue.key"
kubectl get secret -n "$NS" "$gw-cp-server-ca-bundle" -o json | jq -er '.data["ca.crt"] | @base64d' >"$d/ca.crt"
dp=$(kubectl get secret -n "$NS" gwadmin-dp-client -o json)
printf '%s' "$dp" | jq -er '.data["tls.crt"] | @base64d' >"$d/dp.crt"
printf '%s' "$dp" | jq -er '.data["tls.key"] | @base64d' >"$d/dp.key"
slices=$(kubectl get endpointslices -n "$NS" -l "kubernetes.io/service-name=$gw-api" -o json)
ips=$(printf '%s' "$slices" | jq -r '.items[].endpoints[]? | select(.conditions.terminating != true) | .addresses[]')
[ -n "$ips" ] || { echo "no gw-admin pod" >&2; exit 1; }
for ip in $ips; do
  curl -s -o /dev/null --max-time 5 --cacert "$d/ca.crt" --cert "$d/dp.crt" --key "$d/dp.key" \
    --resolve "$cp:8099:$(addr "$ip")" "https://$cp:8099/" || { echo "$ip rejects the data plane certificate" >&2; exit 1; }
  if curl -s -o /dev/null --max-time 5 --cacert "$d/ca.crt" --cert "$d/rogue.crt" --key "$d/rogue.key" \
    --resolve "$cp:8099:$(addr "$ip")" "https://$cp:8099/"; then
    echo "$ip accepts a client certificate from the rogue certificate authority" >&2
    exit 1
  fi
done
echo "ok: the cp listener of $(echo "$ips" | wc -w | tr -d ' ') pod(s) rejects the rogue client certificate"
