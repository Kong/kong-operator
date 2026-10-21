#!/bin/sh
# The peers of a GWAdmin (see peers.yaml): provision every gw-admin pod and
# check both listeners over mTLS every second, logging ROUND, FAIL and GONE
# lines, and stand in for the controller of the data plane gwadmin-dp.
set -u
NS=$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)
api="$GWADMIN-api.$NS.svc"
cp="$GWADMIN-cp.$NS.svc"
d=/tmp/tls
mkdir -p "$d"
printf 'extendedKeyUsage=clientAuth\n' > "$d/dp.ext"
printf 'extendedKeyUsage=serverAuth\nsubjectAltName=DNS:%s\n' "$cp" > "$d/rogue.ext"
command -v openssl >/dev/null || apk add --no-cache openssl >/dev/null

# json_key key file: writes the decoded key of the Secret JSON read from
# stdin to file.
json_key() {
  jq -er --arg k "$1" '.data[$k] | @base64d' > "$2.new" && mv "$2.new" "$2"
}
secret_key() { # secret key file
  kubectl get secret -n "$NS" "$1" -o json | json_key "$2" "$3"
}
# reconcile_dp: what the controller managing the data plane gwadmin-dp
# does. It issues gwadmin-dp's client certificate from the published cp
# client CA first, then records on its Pod template the CA and the cp
# server bundle, which rolls it: its new pods mount at least what it
# records.
reconcile_dp() {
  if kubectl get configmap -n "$NS" gwadmin-dp-pause >/dev/null 2>&1; then
    # Acknowledges the pause once: gwadmin.sh dp-pause waits for it.
    [ "$paused" = 1 ] || { paused=1; echo "PAUSED $(date -u +%H:%M:%S)"; }
    return 0
  fi
  paused=0
  uid=$(kubectl get gwadmin -n "$NS" "$GWADMIN" -o 'jsonpath={.metadata.uid}') && [ -n "$uid" ] || return 1
  server=$(kubectl get gwadmin -n "$NS" "$GWADMIN" -o 'jsonpath={.status.tls.cpServerCABundleSecretName}') &&
    [ -n "$server" ] || return 1
  sec=$(kubectl get secret -n "$OPERATOR_NAMESPACE" "gwadmin-$uid-cp-client-ca" -o json) || return 1
  # The certificate authority is the first certificate of tls.crt, which
  # may carry its issuers after it.
  printf '%s' "$sec" | json_key tls.crt "$d/dp-ca-chain.crt" &&
    openssl x509 -in "$d/dp-ca-chain.crt" -out "$d/dp-ca.crt" 2>/dev/null || return 1
  ca_fp=$(openssl x509 -in "$d/dp-ca.crt" -outform DER | sha256sum | cut -d' ' -f1) || return 1
  bundle=$(kubectl get secret -n "$NS" "$server" -o json) || return 1
  bundle_sum=$(printf '%s' "$bundle" | jq -er '.data["ca.crt"]' | base64 -d | sha256sum | cut -d' ' -f1) || return 1
  dep=$(kubectl get deployment -n "$NS" gwadmin-dp -o json) || return 1
  have_ca=$(printf '%s' "$dep" | jq -r '.spec.template.metadata.annotations["konghq.com/gw-admin-cp-client-ca"] // ""')
  have_sum=$(printf '%s' "$dep" | jq -r '.spec.template.metadata.annotations["konghq.com/gw-admin-cp-server-bundle-checksum"] // ""')
  [ "$have_ca" = "$ca_fp" ] && [ "$have_sum" = "$bundle_sum" ] && return 0
  if [ "$have_ca" != "$ca_fp" ]; then
    printf '%s' "$sec" | json_key tls.key "$d/dp-ca.key" || return 1
    openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj /CN=data-plane \
      -keyout "$d/new.key" -out "$d/new.csr" 2>/dev/null &&
    openssl x509 -req -in "$d/new.csr" -CA "$d/dp-ca.crt" -CAkey "$d/dp-ca.key" -CAcreateserial \
      -days 1 -extfile "$d/dp.ext" -out "$d/new.crt" 2>/dev/null
    issued=$?
    # A server certificate for the cp host from the same certificate
    # authority: what anyone able to issue data plane certificates could
    # forge.
    if [ "$issued" -eq 0 ]; then
      openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$cp" \
        -keyout "$d/rogue.key" -out "$d/rogue.csr" 2>/dev/null &&
      openssl x509 -req -in "$d/rogue.csr" -CA "$d/dp-ca.crt" -CAkey "$d/dp-ca.key" -CAcreateserial \
        -days 1 -extfile "$d/rogue.ext" -out "$d/rogue.crt" 2>/dev/null
      issued=$?
    fi
    rm -f "$d/dp-ca.key"
    [ "$issued" -eq 0 ] || return 1
    kubectl create secret tls -n "$NS" gwadmin-dp-client --cert "$d/new.crt" --key "$d/new.key" \
      --dry-run=client -o json | kubectl apply -f - >/dev/null || return 1
    echo "DATAPLANE-CERT $(date -u +%H:%M:%S) issued from gwadmin-$uid-cp-client-ca"
  fi
  kubectl patch deployment -n "$NS" gwadmin-dp --type merge -p "$(jq -nc --arg ca "$ca_fp" --arg sum "$bundle_sum" \
    '{spec: {template: {metadata: {annotations: {
      "konghq.com/gw-admin-cp-client-ca": $ca, "konghq.com/gw-admin-cp-server-bundle-checksum": $sum}}}}}')" >/dev/null || return 1
  echo "DATAPLANE-ROLL $(date -u +%H:%M:%S) ca=$(echo "$ca_fp" | cut -c1-12) bundle=$(echo "$bundle_sum" | cut -c1-12)"
}
fetch() {
  secret_key "$GWADMIN-api-ca-bundle" ca.crt "$d/api-bundle.pem" || return 1
  server=$(kubectl get gwadmin -n "$NS" "$GWADMIN" -o 'jsonpath={.status.tls.cpServerCABundleSecretName}') &&
    [ -n "$server" ] || return 1
  secret_key "$server" ca.crt "$d/cp-ca.pem" || return 1
  # The client certificate and its key come from a single read of the
  # Secret, so that they always match.
  uid=$(kubectl get gwadmin -n "$NS" "$GWADMIN" -o 'jsonpath={.metadata.uid}') && [ -n "$uid" ] || return 1
  client=$(kubectl get secret -n "$OPERATOR_NAMESPACE" "gwadmin-$uid-operator-client-tls" -o json) || return 1
  printf '%s' "$client" | json_key tls.crt "$d/client.crt" &&
  printf '%s' "$client" | json_key tls.key "$d/client.key" || return 1
  # The data plane client certificate gwadmin-dp-client holds, once
  # issued, from a single read too.
  if dp=$(kubectl get secret -n "$NS" gwadmin-dp-client -o json 2>/dev/null); then
    printf '%s' "$dp" | json_key tls.crt "$d/dp.crt" &&
    printf '%s' "$dp" | json_key tls.key "$d/dp.key" || return 1
  fi
}
# alive: prints the addresses of the gw-admin pods that are not
# terminating; fails when they can't be listed.
alive() {
  slices=$(kubectl get endpointslices -n "$NS" -l "kubernetes.io/service-name=$GWADMIN-api" -o json) || return 1
  printf '%s' "$slices" |
    jq -r '.items[].endpoints[]? | select(.conditions.terminating != true) | .addresses[]'
}
# addr ip: the address as curl --resolve takes it, IPv6 in brackets.
addr() { case $1 in *:*) echo "[$1]" ;; *) echo "$1" ;; esac; }
# tls_error rc: whether a curl exit code is a TLS failure.
tls_error() {
  case $1 in 35|56|58|59|60|77|80|83|90|91) return 0 ;; *) return 1 ;; esac
}
# check ip: prints the failure and returns 1 on a TLS failure, or when a
# listener accepts the client certificate of the other one, 2 when the
# pod can't be reached, 0 otherwise.
check() {
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X PUT \
    -H 'Content-Type: application/json' -d '{"event_gateway_ids":[]}' \
    --cacert "$d/api-bundle.pem" --cert "$d/client.crt" --key "$d/client.key" \
    --resolve "$api:8083:$(addr "$1")" "https://$api:8083/v1/expected-event-gateways")
  rc=$?
  if [ $rc -ne 0 ]; then tls_error $rc && { echo "api rc=$rc"; return 1; }; return 2; fi
  [ "$code" = 204 ] || { echo "api http=$code"; return 1; }
  [ -s "$d/dp.crt" ] || return 2
  curl -s -o /dev/null --max-time 5 \
    --cacert "$d/cp-ca.pem" --cert "$d/dp.crt" --key "$d/dp.key" \
    --resolve "$cp:8099:$(addr "$1")" "https://$cp:8099/"
  rc=$?
  if [ $rc -ne 0 ]; then tls_error $rc && { echo "cp rc=$rc"; return 1; }; return 2; fi
  # Any outcome but a completed request is a rejection: only a listener
  # accepting the certificate is a failure.
  if curl -s -o /dev/null --max-time 5 \
    --cacert "$d/cp-ca.pem" --cert "$d/client.crt" --key "$d/client.key" \
    --resolve "$cp:8099:$(addr "$1")" "https://$cp:8099/"; then
    echo "cp accepted the operator client certificate"
    return 1
  fi
  if curl -s -o /dev/null --max-time 5 \
    --cacert "$d/api-bundle.pem" --cert "$d/dp.crt" --key "$d/dp.key" \
    --resolve "$api:8083:$(addr "$1")" "https://$api:8083/"; then
    echo "api accepted a data plane client certificate"
    return 1
  fi
  # The cp server trust bundle must not vouch for a certificate issued
  # from the published cp client CA.
  if [ -s "$d/rogue.crt" ] &&
    openssl verify -partial_chain -CAfile "$d/cp-ca.pem" -purpose sslserver "$d/rogue.crt" >/dev/null 2>&1; then
    echo "the cp server bundle trusts a certificate issued from the cp client CA"
    return 1
  fi
  return 0
}

round=0
paused=0
while true; do
  reconcile_dp
  if fetch; then
    ok=0
    for ip in $(alive); do
      out=$(check "$ip")
      rc=$?
      [ $rc -eq 0 ] && ok=$((ok + 1))
      [ $rc -eq 1 ] || continue
      # Only a pod known to be terminating now is excused.
      if now=$(alive) && ! printf '%s\n' "$now" | grep -qx "$ip"; then
        echo "GONE $(date -u +%H:%M:%S) $ip $out"
      else
        echo "FAIL $(date -u +%H:%M:%S) $ip $out"
      fi
    done
    if [ "$ok" -gt 0 ]; then
      round=$((round + 1))
      echo "ROUND $round $(date -u +%H:%M:%S) pods=$(alive | tr '\n' ' ')"
    fi
  fi
  sleep 1
done

