#!/bin/sh
# The data plane gwadmin-dp of a GWAdmin (see peers.yaml): loads its trust
# material once at start and checks every gw-admin pod's cp listener with it
# every second, recording TLS failures in ConfigMap gwadmin-dp-failures.
set -u
NS=$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)
POD=$(hostname)
cp="$GWADMIN-cp.$NS.svc"
s=/tmp/tls
# Load the TLS material once, as a data plane that doesn't reload its
# certificates: what the pod mounts when it starts. The Secrets are
# optional for the first pod, created before they exist.
until [ -s /etc/dp/server/ca.crt ] && [ -s /etc/dp/client/tls.crt ] && [ -s /etc/dp/client/tls.key ]; do
  sleep 1
done
mkdir -p "$s"
cp /etc/dp/server/ca.crt /etc/dp/client/tls.crt /etc/dp/client/tls.key "$s/"
echo "LOADED $(date -u +%H:%M:%S) bundle=$(sha256sum < "$s/ca.crt" | cut -c1-12)"
alive() {
  slices=$(kubectl get endpointslices -n "$NS" -l "kubernetes.io/service-name=$GWADMIN-api" -o json) || return 1
  printf '%s' "$slices" |
    jq -r '.items[].endpoints[]? | select(.conditions.terminating != true) | .addresses[]'
}
addr() { case $1 in *:*) echo "[$1]" ;; *) echo "$1" ;; esac; }
tls_error() {
  case $1 in 35|56|58|59|60|77|80|83|90|91) return 0 ;; *) return 1 ;; esac
}
# record ip message: records a failure where a rolled pod can't lose it.
record() {
  key="$POD.$(date -u +%H%M%S).$(echo "$1" | tr '.:' '--')"
  echo "FAIL $(date -u +%H:%M:%S) $1 $2"
  kubectl patch configmap -n "$NS" gwadmin-dp-failures --type merge \
    -p "$(jq -nc --arg k "$key" --arg v "$2" '{data: {($k): $v}}')" >/dev/null 2>&1 ||
    kubectl create configmap -n "$NS" gwadmin-dp-failures --from-literal="$key=$2" >/dev/null 2>&1 || true
}
round=0
while true; do
  ok=0
  for ip in $(alive); do
    curl -s -o /dev/null --max-time 5 --cacert "$s/ca.crt" --cert "$s/tls.crt" --key "$s/tls.key" \
      --resolve "$cp:8099:$(addr "$ip")" "https://$cp:8099/"
    rc=$?
    [ $rc -eq 0 ] && ok=$((ok + 1))
    { [ $rc -ne 0 ] && tls_error $rc; } || continue
    # Only a pod known to be terminating now is excused.
    if now=$(alive) && ! printf '%s\n' "$now" | grep -qx "$ip"; then
      echo "GONE $(date -u +%H:%M:%S) $ip cp rc=$rc"
    else
      record "$ip" "cp rc=$rc"
    fi
  done
  if [ "$ok" -gt 0 ]; then
    round=$((round + 1))
    echo "DPROUND $round $(date -u +%H:%M:%S)"
  fi
  sleep 1
done

