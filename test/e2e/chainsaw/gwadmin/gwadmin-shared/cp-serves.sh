#!/bin/sh
# Runs in the peers pod (gwadmin.sh cp-serves): status.cp.serverURL and each
# url are served over mTLS with gwadmin-dp-client, verified with the cp server bundle of the GWAdmin,
# as a data plane connecting through the Service would be.
# Usage: cp-serves.sh gwadmin url...
set -eu
gw=$1
shift
NS=$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)
d=$(mktemp -d)
url=$(kubectl get gwadmin -n "$NS" "$gw" -o 'jsonpath={.status.cp.serverURL}')
[ -n "$url" ] || { echo "$gw publishes no status.cp.serverURL" >&2; exit 1; }
set -- "$url" "$@"
server=$(kubectl get gwadmin -n "$NS" "$gw" -o 'jsonpath={.status.tls.cpServerCABundleSecretName}')
kubectl get secret -n "$NS" "$server" -o json | jq -er '.data["ca.crt"] | @base64d' >"$d/ca.crt"
dp=$(kubectl get secret -n "$NS" gwadmin-dp-client -o json)
printf '%s' "$dp" | jq -er '.data["tls.crt"] | @base64d' >"$d/dp.crt"
printf '%s' "$dp" | jq -er '.data["tls.key"] | @base64d' >"$d/dp.key"
# Retried for 30s: a pod that just turned Ready may not be routed to yet.
for url in "$@"; do
  tries=0
  until curl -sS -o /dev/null --max-time 5 --cacert "$d/ca.crt" --cert "$d/dp.crt" --key "$d/dp.key" "$url"; do
    tries=$((tries + 1))
    [ "$tries" -lt 15 ] || { echo "$url is not served to the data plane" >&2; exit 1; }
    sleep 2
  done
  echo "ok: $url served"
done
