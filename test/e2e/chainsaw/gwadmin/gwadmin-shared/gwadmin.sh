#!/usr/bin/env bash
# Helpers for the GWAdmin certificate e2e tests. Usage: gwadmin.sh <command> [args...]
# They only cover what chainsaw can't do declaratively: generating CAs and
# certificates, verifying with openssl which CA signed a certificate,
# starting and stopping the peers (archiving their logs), counting and
# checking their handshakes, and restarting Kong Operator. Conditions, the
# trust bundle size, the Deployment and the Secrets are asserted with
# chainsaw (see the assert-*.yaml files next to this script).
# Every command acts on the namespace in $NAMESPACE. Files the tests keep
# across steps (generated CAs, archived peers logs) live in $WORK_DIR, which
# defaults to /tmp/gwadmin-e2e-$NAMESPACE and is removed by the tests' cleanup.
set -euo pipefail

ns() { echo "${NAMESPACE:?NAMESPACE must be set}"; }
work_dir() { echo "${WORK_DIR:-/tmp/gwadmin-e2e-$(ns)}"; }
operator_ns() { echo kong-system; }
OPERATOR_SELECTOR=app.kubernetes.io/name=kong-operator
PEERS_SELECTOR=app=gwadmin-peers
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# secret_data name key: prints the decoded key of the Secret.
secret_data() {
  kubectl get secret -n "$(ns)" "$1" -o json | jq -er --arg k "$2" '.data[$k] | @base64d'
}

# gwadmin_uid gwadmin: prints the UID of the GWAdmin.
gwadmin_uid() {
  local uid
  uid=$(kubectl get gwadmin -n "$(ns)" "$1" -o 'jsonpath={.metadata.uid}') && [ -n "$uid" ] && echo "$uid"
}

# private_secret_name gwadmin suffix: prints the name of a Secret Kong
# Operator keeps for the GWAdmin in the operator namespace, e.g. api-ca,
# cp-server-ca or operator-client-tls.
private_secret_name() {
  local uid
  uid=$(gwadmin_uid "$1") || return 1
  echo "gwadmin-$uid-$2"
}

# private_secret_data gwadmin suffix key: prints the decoded key of a Secret
# Kong Operator keeps for the GWAdmin in the operator namespace.
private_secret_data() {
  local name
  name=$(private_secret_name "$1" "$2") || return 1
  kubectl get secret -n "$(operator_ns)" "$name" -o json | jq -er --arg k "$3" '.data[$k] | @base64d'
}

# leaf_data gwadmin leaf: prints the tls.crt of a certificate of the
# GWAdmin: api and cp live in its namespace, operator-client in the operator
# namespace.
leaf_data() {
  case $2 in
  operator-client) private_secret_data "$1" operator-client-tls tls.crt ;;
  *) secret_data "$1-$2-tls" tls.crt ;;
  esac
}

# first_cert: prints the first certificate of the PEM read from stdin.
first_cert() { openssl x509; }

# leaves_signed_by gwadmin ca_file leaves...: whether every leaf certificate
# of the GWAdmin is signed by the certificate authority in ca_file.
leaves_signed_by() {
  local gw=$1 ca=$2
  shift 2
  local leaf tmp
  tmp=$(mktemp)
  for leaf in "$@"; do
    if ! leaf_data "$gw" "$leaf" 2>/dev/null | first_cert >"$tmp" 2>/dev/null ||
      ! openssl verify -CAfile "$ca" "$tmp" >/dev/null 2>&1; then
      rm -f "$tmp"
      return 1
    fi
  done
  rm -f "$tmp"
}

# fingerprint: prints the fingerprint of the first certificate of the PEM
# read from stdin.
fingerprint() { openssl x509 -noout -fingerprint -sha256; }

# publishes gwadmin ca_file: whether the GWAdmin publishes the cp client CA
# in ca_file, in gwadmin-<uid>-cp-client-ca in the operator namespace.
publishes() {
  local got want
  got=$(private_secret_data "$1" cp-client-ca tls.crt 2>/dev/null | fingerprint 2>/dev/null) || return 1
  want=$(fingerprint <"$2") || return 1
  [ "$got" = "$want" ]
}

# holds_another_ca secret old_ca_file: whether the Secret holds a certificate
# authority, other than the one in old_ca_file.
holds_another_ca() {
  local got
  got=$(secret_data "$1" tls.crt 2>/dev/null | fingerprint 2>/dev/null) && [ -n "$got" ] &&
    [ "$got" != "$(fingerprint <"$2")" ]
}

# until_true timeout description command...: retries command every 2s.
until_true() {
  local timeout=$1 what=$2
  shift 2
  local deadline=$((SECONDS + timeout))
  until "$@"; do
    if ((SECONDS >= deadline)); then
      echo "timed out after ${timeout}s waiting for: $what" >&2
      return 1
    fi
    sleep 2
  done
  echo "ok: $what"
}

# peers_logs: prints the logs of the peers, those of the pods stopped by
# peers-stop (archived) first, then those of the running pods.
peers_logs() {
  local archive
  archive="$(work_dir)/peers-logs"
  if [ -d "$archive" ]; then
    cat "$archive"/*.log 2>/dev/null || true
  fi
  # Not reading the logs of the running pods must fail the check.
  kubectl logs -n "$(ns)" -l "$PEERS_SELECTOR" --tail=-1 --max-log-requests=10
}

# live_count prefix: prints how many lines starting with prefix the running
# peers pods logged.
live_count() {
  kubectl logs -n "$(ns)" -l "$PEERS_SELECTOR" --tail=-1 --max-log-requests=10 2>/dev/null |
    grep -c "^$1" || true
}

live_rounds() { live_count ROUND; }

# no_pods namespace selector: no pod matches the selector.
no_pods() {
  local out
  out=$(kubectl get pods -n "$1" -l "$2" -o name) && [ -z "$out" ]
}

# pods_gone namespace names: none of the pods (space separated) exists.
pods_gone() {
  local pod out
  for pod in $2; do
    out=$(kubectl get pod -n "$1" "$pod" -o name --ignore-not-found) && [ -z "$out" ] || return 1
  done
}

rounds_at_least() { [ "$(live_rounds)" -ge "$1" ]; }

# paused_above count: the running peers logged more than count PAUSED lines.
paused_above() { [ "$(live_count PAUSED)" -gt "$1" ]; }

# no_private_secrets uid: no Secret of the GWAdmin with that UID is left in
# the operator namespace.
no_private_secrets() {
  local out
  out=$(kubectl get secrets -n "$(operator_ns)" -l "gateway-operator.konghq.com/gwadmin-uid=$1" -o name) && [ -z "$out" ]
}

# server_bundle_count gwadmin: prints how many certificate authorities the cp
# server trust bundle of the GWAdmin holds.
server_bundle_count() {
  secret_data "$1-cp-server-ca-bundle" ca.crt | grep -c -- '-----BEGIN CERTIFICATE-----'
}

# dp_current gwadmin: the data plane gwadmin-dp rolled out recording the cp
# client CA the GWAdmin publishes and its current cp server bundle.
dp_current() {
  local ca sum dep
  ca=$(private_secret_data "$1" cp-client-ca tls.crt | openssl x509 -outform DER | sha256sum | cut -d' ' -f1) || return 1
  sum=$(kubectl get secret -n "$(ns)" "$1-cp-server-ca-bundle" -o json | jq -er '.data["ca.crt"]' | base64 -d | sha256sum | cut -d' ' -f1) || return 1
  dep=$(kubectl get deployment -n "$(ns)" gwadmin-dp -o json) || return 1
  printf '%s' "$dep" | jq -e --arg ca "$ca" --arg sum "$sum" '
    .spec.template.metadata.annotations["konghq.com/gw-admin-cp-client-ca"] == $ca and
    .spec.template.metadata.annotations["konghq.com/gw-admin-cp-server-bundle-checksum"] == $sum and
    .status.observedGeneration >= .metadata.generation and
    .status.updatedReplicas == .spec.replicas and .status.replicas == .spec.replicas and
    .status.readyReplicas == .spec.replicas' >/dev/null
}

# dp_records_bundle gwadmin: gwadmin-dp's Pod template records the current cp
# server bundle.
dp_records_bundle() {
  local sum
  sum=$(kubectl get secret -n "$(ns)" "$1-cp-server-ca-bundle" -o json | jq -er '.data["ca.crt"]' | base64 -d | sha256sum | cut -d' ' -f1) || return 1
  kubectl get deployment -n "$(ns)" gwadmin-dp -o json | jq -e --arg sum "$sum" \
    '.spec.template.metadata.annotations["konghq.com/gw-admin-cp-server-bundle-checksum"] == $sum' >/dev/null
}

# dp_runs_rounds: a running pod of the data plane completed a round.
dp_runs_rounds() {
  local pod
  for pod in $(kubectl get pods -n "$(ns)" -l app=gwadmin-dp --field-selector=status.phase=Running -o name 2>/dev/null); do
    # Not grep -q: exiting on the first match would kill kubectl with
    # SIGPIPE on long logs, and pipefail would turn the match into a failure.
    kubectl logs -n "$(ns)" "$pod" --tail=-1 2>/dev/null | grep '^DPROUND' >/dev/null && return 0
  done
  return 1
}

# pem_body file: prints the base64 body of the PEM certificates in file, on
# one line.
pem_body() { grep -v -- '-----' "$1" | tr -d '\n'; }

# bundle_lacks secret ca_file: the ca.crt of the bundle Secret doesn't hold
# the certificate in ca_file.
bundle_lacks() {
  local data
  data=$(secret_data "$1" ca.crt 2>/dev/null) || return 1
  [ -n "$data" ] || return 1
  ! printf '%s\n' "$data" | grep -v -- '-----' | tr -d '\n' | grep -qF "$(pem_body "$2")"
}

# new_operator_ready old_pods: a Kong Operator pod that is not one of
# old_pods (space separated) is Ready and not being deleted.
new_operator_ready() {
  kubectl get pods -n "$(operator_ns)" -l "$OPERATOR_SELECTOR" -o json | jq -e --arg old "$1" '
    ($old | split(" ")) as $old | any(.items[];
      .metadata.deletionTimestamp == null and
      (.metadata.name as $n | $old | index($n) | not) and
      any(.status.conditions[]?; .type == "Ready" and .status == "True"))' >/dev/null
}

# apply_manifest manifest: applies the manifest.
apply_manifest() { kubectl apply -f - >/dev/null <<<"$1"; }

# private_ca_changed name old_ca_file: whether the Secret name in the operator
# namespace holds a certificate authority other than the one in old_ca_file.
private_ca_changed() {
  local got
  got=$(kubectl get secret -n "$(operator_ns)" "$1" -o json | jq -er '.data["tls.crt"] | @base64d' |
    openssl x509 -noout -fingerprint -sha256 2>/dev/null) && [ -n "$got" ] &&
    [ "$got" != "$(openssl x509 -noout -fingerprint -sha256 <"$2")" ]
}

# asn1_time epoch: prints the time as openssl ca's -startdate and -enddate
# take it, with GNU or BSD date.
asn1_time() { date -u -d "@$1" +%Y%m%d%H%M%SZ 2>/dev/null || date -u -r "$1" +%Y%m%d%H%M%SZ; }

# sign_dated csr out age remaining extensions key [ca_crt]: signs csr with key
# (self-signed without ca_crt) into a certificate issued age seconds ago and
# expiring in remaining seconds. extensions is printf'd into its extensions.
# openssl ca backdates on any openssl 3, unlike openssl x509.
sign_dated() {
  local d now
  d=$(mktemp -d)
  now=$(date +%s)
  : >"$d/index.txt"
  echo 01 >"$d/serial"
  # shellcheck disable=SC2059 # extensions holds the escapes printf expands.
  printf "$5" >"$d/ext"
  cat >"$d/ca.cnf" <<EOF
[ca]
default_ca = dated
[dated]
database = $d/index.txt
new_certs_dir = $d
serial = $d/serial
default_md = sha256
policy = any
unique_subject = no
[any]
commonName = supplied
EOF
  local issuer=(-selfsign)
  [ $# -lt 7 ] || issuer=(-cert "$7")
  openssl ca -batch -notext -config "$d/ca.cnf" -keyfile "$6" "${issuer[@]}" -in "$1" -out "$2" -extfile "$d/ext" \
    -startdate "$(asn1_time $((now - $3)))" -enddate "$(asn1_time $((now + $4)))" 2>"$d/err" ||
    { cat "$d/err" >&2; rm -rf "$d"; return 1; }
  rm -rf "$d"
}

cmd=$1
shift
case $cmd in
gen-ca) # common-name dir [age remaining]: writes a CA keypair to dir/tls.crt and dir/tls.key, issued age seconds ago and expiring in remaining seconds when given
  mkdir -p "$2"
  if [ $# -ge 4 ]; then
    openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$1" \
      -keyout "$2/tls.key" -out "$2/tls.csr" 2>/dev/null
    sign_dated "$2/tls.csr" "$2/tls.crt" "$3" "$4" \
      'basicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,cRLSign\n' "$2/tls.key"
    exit
  fi
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 365 -subj "/CN=$1" \
    -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -keyout "$2/tls.key" -out "$2/tls.crt" 2>/dev/null
  ;;
gen-leaf) # ca_dir common-name dir: writes a leaf certificate signed by the CA in ca_dir to dir
  mkdir -p "$3"
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$2" \
    -keyout "$3/tls.key" -out "$3/tls.csr" 2>/dev/null
  openssl x509 -req -in "$3/tls.csr" -CA "$1/tls.crt" -CAkey "$1/tls.key" -CAcreateserial -days 30 \
    -extfile <(printf 'basicConstraints=CA:FALSE\nsubjectAltName=DNS:%s\n' "$2") -out "$3/tls.crt" 2>/dev/null
  ;;
put-server-cert) # name ca_dir host dir [age remaining]: writes a server certificate for host, signed by the CA in ca_dir (issued age seconds ago, expiring in remaining seconds when given), to dir, and to a TLS Secret visible to the operator, with the CA in ca.crt as cert-manager does
  mkdir -p "$4"
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$3" \
    -keyout "$4/tls.key" -out "$4/tls.csr" 2>/dev/null
  ext="basicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=serverAuth\nsubjectAltName=DNS:$3\n"
  if [ $# -ge 6 ]; then
    sign_dated "$4/tls.csr" "$4/tls.crt" "$5" "$6" "$ext" "$2/tls.key" "$2/tls.crt"
  else
    # shellcheck disable=SC2059 # ext holds the escapes printf expands.
    openssl x509 -req -in "$4/tls.csr" -CA "$2/tls.crt" -CAkey "$2/tls.key" -CAcreateserial -days 30 \
      -extfile <(printf "$ext") -out "$4/tls.crt" 2>/dev/null
  fi
  manifest=$(kubectl create secret generic -n "$(ns)" "$1" --type=kubernetes.io/tls \
    --from-file=tls.crt="$4/tls.crt" --from-file=tls.key="$4/tls.key" --from-file=ca.crt="$2/tls.crt" \
    --dry-run=client -o json | jq '.metadata.labels["konghq.com/secret"] = "true"')
  until_true 60 "Secret $1 applied" apply_manifest "$manifest"
  ;;
wait-new-ca) # secret old_ca_file dir timeout: waits for the Secret to hold another CA, saved to dir
  until_true "$4" "$1 holds a new certificate authority" holds_another_ca "$1" "$2"
  mkdir -p "$3"
  secret_data "$1" tls.crt | first_cert >"$3/tls.crt"
  ;;
put-tls-secret) # name dir: creates, or updates in place, a TLS Secret visible to the operator
  # Retried: the operator's Secret validating webhook is briefly unreachable
  # while the operator restarts.
  manifest=$(kubectl create secret tls -n "$(ns)" "$1" --cert "$2/tls.crt" --key "$2/tls.key" --dry-run=client -o json |
    jq '.metadata.labels["konghq.com/secret"] = "true"')
  until_true 60 "Secret $1 applied" apply_manifest "$manifest"
  ;;
save-ca) # secret dir: saves the certificate authority of a Secret's tls.crt to dir/tls.crt
  mkdir -p "$2"
  secret_data "$1" tls.crt | first_cert >"$2/tls.crt"
  ;;
save-private-ca) # gwadmin suffix dir: saves a certificate authority Kong Operator keeps in the operator namespace (api-ca, cp-server-ca) to dir/tls.crt
  mkdir -p "$3"
  private_secret_data "$1" "$2" tls.crt | first_cert >"$3/tls.crt"
  ;;
wait-new-private-ca) # gwadmin suffix old_ca_file dir timeout: waits for a certificate authority in the operator namespace to change, saved to dir
  name=$(private_secret_name "$1" "$2")
  until_true "$5" "$name holds a new certificate authority" private_ca_changed "$name" "$3"
  mkdir -p "$4"
  private_secret_data "$1" "$2" tls.crt | first_cert >"$4/tls.crt"
  ;;
wait-private-secrets-gone) # uid timeout: waits for the Secrets of a deleted GWAdmin to leave the operator namespace
  until_true "$2" "no Secret of GWAdmin $1 left in $(operator_ns)" no_private_secrets "$1"
  ;;
assert-server-bundle-count) # gwadmin count: the cp server trust bundle holds count certificate authorities right now
  got=$(server_bundle_count "$1")
  [ "$got" -eq "$2" ] || { echo "the cp server bundle of $1 holds $got certificate authorities, not $2" >&2; exit 1; }
  echo "ok: the cp server bundle of $1 holds $2 certificate authorities"
  ;;
wait-leaves-on) # gwadmin ca_file timeout leaves...
  gw=$1 ca=$2 timeout=$3
  shift 3
  until_true "$timeout" "certificates of $gw ($*) signed by $ca" leaves_signed_by "$gw" "$ca" "$@"
  ;;
wait-published) # gwadmin ca_file timeout: waits for the GWAdmin to publish the certificate authority in ca_file
  until_true "$3" "$1 publishes $2" publishes "$1" "$2"
  ;;
assert-published) # gwadmin ca_file: the GWAdmin publishes the certificate authority in ca_file right now
  publishes "$1" "$2" || { echo "$1 does not publish $2" >&2; exit 1; }
  ;;
assert-leaves-on) # gwadmin ca_file leaves...: the certificates are signed by ca_file right now
  gw=$1 ca=$2
  shift 2
  leaves_signed_by "$gw" "$ca" "$@" || { echo "certificates of $gw ($*) are not signed by $ca" >&2; exit 1; }
  echo "ok: certificates of $gw ($*) signed by $ca"
  ;;
expect-leaves-stay-on) # gwadmin ca_file seconds leaves...: the certificates don't move for that long
  gw=$1 ca=$2 duration=$3
  shift 3
  deadline=$((SECONDS + duration))
  while ((SECONDS < deadline)); do
    leaves_signed_by "$gw" "$ca" "$@" || { echo "certificates of $gw moved off $ca too early" >&2; exit 1; }
    sleep 3
  done
  echo "ok: certificates of $gw stayed on $ca for ${duration}s"
  ;;
tamper-bundle) # secret ca_file: adds the certificate authority in ca_file to the ca.crt of the bundle Secret, as someone who can write the Secrets of the namespace could
  data=$({
    secret_data "$1" ca.crt
    cat "$2"
  } | base64 | tr -d '\n')
  kubectl patch secret -n "$(ns)" "$1" --type merge -p "{\"data\":{\"ca.crt\":\"$data\"}}" >/dev/null
  echo "ok: $1 tampered with"
  ;;
wait-bundle-lacks) # secret ca_file timeout: waits for the bundle Secret not to hold the certificate in ca_file
  until_true "$3" "$1 doesn't trust $2" bundle_lacks "$1" "$2"
  ;;
cp-rejects-client) # gwadmin ca_dir: every gw-admin pod's cp listener rejects a client certificate issued from the CA in ca_dir, and accepts gwadmin-dp-client
  dir=$(mktemp -d)
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj /CN=rogue-data-plane \
    -keyout "$dir/tls.key" -out "$dir/tls.csr" 2>/dev/null
  openssl x509 -req -in "$dir/tls.csr" -CA "$2/tls.crt" -CAkey "$2/tls.key" -CAcreateserial -days 1 \
    -extfile <(printf 'extendedKeyUsage=clientAuth\n') -out "$dir/tls.crt" 2>/dev/null
  crt=$(base64 <"$dir/tls.crt" | tr -d '\n')
  key=$(base64 <"$dir/tls.key" | tr -d '\n')
  rm -rf "$dir"
  # From the peers pod: the gw-admin pods are only reachable in the cluster.
  kubectl exec -n "$(ns)" deploy/gwadmin-peers -- sh /scripts/cp-rejects-client.sh "$1" "$crt" "$key"
  ;;
cp-serves) # gwadmin url...: from the peers pod, status.cp.serverURL and each url are served over mTLS with gwadmin-dp-client, verified with the cp server bundle: what a data plane connecting through the Service gets
  kubectl exec -n "$(ns)" deploy/gwadmin-peers -- sh /scripts/cp-serves.sh "$@"
  ;;
restart-stale-pods) # gwadmin: deletes the gw-admin pods that don't run the trust checksum of the current Pod template, failing if there is none: their replacements mount the current Secrets
  want=$(kubectl get deployment -n "$(ns)" "$1" -o json | jq -er '.spec.template.metadata.annotations["konghq.com/gw-admin-trust-checksum"]')
  pods=$(kubectl get pods -n "$(ns)" -l "gateway-operator.konghq.com/managed-by=gw-admin,gateway-operator.konghq.com/managed-by-name=$1" -o json |
    jq -r --arg want "$want" '.items[] | select(.metadata.deletionTimestamp == null and .metadata.annotations["konghq.com/gw-admin-trust-checksum"] != $want) | .metadata.name')
  [ -n "$pods" ] || { echo "no gw-admin pod of $1 runs stale trust material" >&2; exit 1; }
  for pod in $pods; do
    kubectl delete pod -n "$(ns)" "$pod" --wait=false >/dev/null
    echo "restarted $pod"
  done
  ;;
dp-pause) # pauses the stand-in controller of the data plane gwadmin-dp: it is no longer rolled
  before=$(live_count PAUSED)
  kubectl create configmap -n "$(ns)" gwadmin-dp-pause --dry-run=client -o json | kubectl apply -f - >/dev/null
  # A round in progress may still roll it once: wait for the peers to see
  # the pause. Stopped peers roll nothing, and check it before anything once
  # started.
  if ! no_pods "$(ns)" "$PEERS_SELECTOR"; then
    until_true 60 "the peers acknowledged the pause" paused_above "$before"
  fi
  echo "ok: gwadmin-dp paused"
  ;;
dp-resume) # resumes the stand-in controller of the data plane gwadmin-dp
  kubectl delete configmap -n "$(ns)" gwadmin-dp-pause --ignore-not-found >/dev/null
  echo "ok: gwadmin-dp resumed"
  ;;
wait-dp-current) # gwadmin timeout: waits for gwadmin-dp to roll out with the published cp client CA and the current cp server bundle
  until_true "$2" "gwadmin-dp runs the current trust material of $1" dp_current "$1"
  ;;
assert-dp-behind) # gwadmin: gwadmin-dp doesn't record the current cp server bundle right now
  if dp_records_bundle "$1"; then echo "gwadmin-dp already records the current cp server bundle" >&2; exit 1; fi
  echo "ok: gwadmin-dp runs a previous cp server bundle"
  ;;
wait-rounds) # count timeout: the running peers complete count more rounds
  # Every round checks every gw-admin pod, so count >= 2 guarantees at least
  # one full round started after this command.
  target=$(($(live_rounds) + $1))
  until_true "$2" "peers completed $1 more round(s)" rounds_at_least "$target"
  ;;
peers-stop) # timeout: stops the peers so that new gw-admin pods can't turn Ready
  # The logs of the peers pods are archived first: assert-no-failures reads
  # them, and they are lost once the pods are deleted. The pods must be gone
  # (not just terminating, when they still run) before anything is changed.
  archive="$(work_dir)/peers-logs"
  mkdir -p "$archive"
  pids=()
  for pod in $(kubectl get pods -n "$(ns)" -l "$PEERS_SELECTOR" -o jsonpath='{.items[*].metadata.name}'); do
    kubectl logs -f -n "$(ns)" "$pod" --tail=-1 >"$archive/$pod.log" 2>/dev/null &
    pids+=("$!")
  done
  kubectl scale deployment -n "$(ns)" gwadmin-peers --replicas=0
  until_true "$1" "the peers pods are gone" no_pods "$(ns)" "$PEERS_SELECTOR"
  # The log streams end with the containers; don't hang if one doesn't.
  for pid in ${pids[@]+"${pids[@]}"}; do
    for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
    kill "$pid" 2>/dev/null || true
    wait "$pid" || true
  done
  echo "ok: peers stopped"
  ;;
peers-start) # timeout: starts the peers again
  kubectl scale deployment -n "$(ns)" gwadmin-peers --replicas=1
  kubectl rollout status deployment -n "$(ns)" gwadmin-peers --timeout="${1}s"
  ;;
restart-operator) # timeout: deletes the Kong Operator pod(s) and waits for a new one to be Ready
  old=$(kubectl get pods -n "$(operator_ns)" -l "$OPERATOR_SELECTOR" -o jsonpath='{.items[*].metadata.name}')
  [ -n "$old" ] || { echo "no Kong Operator pod found in $(operator_ns)" >&2; exit 1; }
  echo "deleting Kong Operator pod(s): $old"
  # shellcheck disable=SC2086 # One argument per pod name.
  kubectl delete pods -n "$(operator_ns)" $old --wait=false
  until_true "$1" "a new Kong Operator pod is Ready" new_operator_ready "$old"
  until_true "$1" "the previous Kong Operator pod(s) are gone" pods_gone "$(operator_ns)" "$old"
  ;;
assert-no-failures) # every handshake of the peers succeeded
  logs=$(peers_logs)
  echo "$logs" | grep -E '^(ROUND|FAIL|RECOVERED|GONE)' | tail -20 || true
  # Zero tolerance: a TLS failure on a pod that is not terminating is a
  # failure, even if a retry would have succeeded. The peers only skip (and
  # log as GONE) failures of a pod that turned terminating meanwhile: its
  # connections may be cut mid-handshake.
  # The logs are matched from a here-string, not piped from echo: grep -q
  # exits on the first match, echo of long logs then dies with SIGPIPE, and
  # pipefail turns the match into a miss.
  if grep -qE '^(FAIL|RECOVERED)' <<<"$logs"; then
    echo "the peers saw TLS failures" >&2
    exit 1
  fi
  grep -q '^ROUND' <<<"$logs" || { echo "the peers never ran a round" >&2; exit 1; }
  # A restarted peers container lost the logs of its previous run, failures
  # included.
  restarts=$(kubectl get pods -n "$(ns)" -l "$PEERS_SELECTOR" -o json |
    jq '[.items[].status.containerStatuses[]?.restartCount] | add // 0')
  [ "$restarts" -eq 0 ] || { echo "the peers restarted $restarts time(s), losing their logs" >&2; exit 1; }
  # The failures of the data plane, whose rolled pods' logs are gone.
  failures=$(kubectl get configmap -n "$(ns)" gwadmin-dp-failures -o json --ignore-not-found | jq -r '.data // {} | to_entries[] | "\(.key) \(.value)"')
  if [ -n "$failures" ]; then
    echo "$failures"
    echo "the data plane saw TLS failures" >&2
    exit 1
  fi
  # A pod of the data plane may have just been rolled.
  until_true 90 "the data plane runs rounds" dp_runs_rounds
  echo "ok: the peers and the data plane saw no TLS failure"
  ;;
peers-scripts) # creates the ConfigMaps holding the scripts the peers and the data plane gwadmin-dp run (peers.yaml mounts them)
  for cm in "gwadmin-peers peers.sh cp-rejects-client.sh cp-serves.sh" "gwadmin-dp dp.sh"; do
    # shellcheck disable=SC2086 # One word per ConfigMap name and file.
    set -- $cm
    name=$1
    shift
    files=()
    for f in "$@"; do files+=(--from-file="$f=$SCRIPT_DIR/$f"); done
    kubectl create configmap -n "$(ns)" "$name" "${files[@]}" --dry-run=client -o json | kubectl apply -f - >/dev/null
  done
  echo "ok: peers scripts loaded"
  ;;
clean-work-dir) # removes the files the test kept across steps: generated CA keys, archived peers logs
  rm -rf "$(work_dir)"
  ;;
gwadmin-uid) # gwadmin: prints the UID of the GWAdmin, without a newline: chainsaw outputs keep it
  printf '%s' "$(gwadmin_uid "$1")"
  ;;
secret-json) # name: prints the Secret as JSON
  kubectl get secret -n "$(ns)" "$1" -o json
  ;;
template-annotations) # gwadmin: prints the annotations of the Pod template of the GWAdmin's Deployment, as JSON
  kubectl get deployment -n "$(ns)" "$1" -o json | jq -ec '.spec.template.metadata.annotations'
  ;;
delete-secret) # name...: deletes Secrets of the namespace, without waiting for their name to be free (Kong Operator recreates some at once), and prints the UID of the first
  kubectl get secret -n "$(ns)" "$1" -o 'jsonpath={.metadata.uid}'
  kubectl delete secret -n "$(ns)" "$@" >&2
  ;;
delete-private-secrets) # gwadmin suffix...: deletes Secrets Kong Operator keeps for the GWAdmin in the operator namespace (cp-client-ca, api-ca, cp-server-ca, operator-client-tls)
  gw=$1
  shift
  names=()
  for suffix in "$@"; do names+=("$(private_secret_name "$gw" "$suffix")"); done
  kubectl delete secret -n "$(operator_ns)" "${names[@]}"
  ;;
assert-not-published) # gwadmin: no cp client CA is published: nothing the pods don't trust yet
  name=$(private_secret_name "$1" cp-client-ca)
  if kubectl get secret -n "$(operator_ns)" "$name" >/dev/null 2>&1; then
    echo "a certificate authority the pods don't trust is published in $name" >&2
    exit 1
  fi
  echo "ok: nothing published in $name"
  ;;
delete-bundles) # gwadmin: deletes the three trust bundle Secrets of the GWAdmin
  kubectl delete secret -n "$(ns)" "$1-cp-client-ca-bundle" "$1-cp-server-ca-bundle" "$1-api-ca-bundle"
  ;;
tamper-bundles) # gwadmin ca_file timeout: adds the certificate authority in ca_file to each trust bundle of the GWAdmin, and waits for Kong Operator to rewrite it without
  for bundle in "$1-cp-client-ca-bundle" "$1-api-ca-bundle" "$1-cp-server-ca-bundle"; do
    "$0" tamper-bundle "$bundle" "$2"
    until_true "$3" "$bundle doesn't trust $2" bundle_lacks "$bundle" "$2"
  done
  ;;
assert-ca-key-not-in-namespace) # gwadmin: the key of the cp client CA, kept in the operator namespace, is in no Secret of the GWAdmin namespace, under any name
  key=$(private_secret_data "$1" cp-client-ca tls.key | base64 | tr -d '\n')
  [ -n "$key" ]
  leaks=$(kubectl get secrets -n "$(ns)" -o json |
    jq -r --arg k "$key" '.items[] | select(.data["tls.key"] == $k) | .metadata.name')
  [ -z "$leaks" ] || { echo "the cp client CA key is in Secret(s): $leaks" >&2; exit 1; }
  echo "ok: the cp client CA key is not in $(ns)"
  ;;
verify-certificate-chains) # gwadmin: every certificate chains to the bundle of its trust domain and no other, with the expected subjects; data plane certificates issued from the published cp client CA can't impersonate gw-admin
  gw=$1
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' EXIT
  fail() { echo "$*" >&2; exit 1; }
  server=$(kubectl get gwadmin -n "$(ns)" "$gw" -o 'jsonpath={.status.tls.cpServerCABundleSecretName}')
  secret_data "$server" ca.crt >"$dir/cp-server-bundle.pem"
  secret_data "$gw-cp-client-ca-bundle" ca.crt >"$dir/cp-client-bundle.pem"
  secret_data "$gw-api-ca-bundle" ca.crt >"$dir/api-bundle.pem"
  secret_data "$gw-api-tls" tls.crt >"$dir/api.pem"
  secret_data "$gw-cp-tls" tls.crt >"$dir/cp.pem"
  private_secret_data "$gw" operator-client-tls tls.crt >"$dir/client.pem"
  private_secret_data "$gw" cp-client-ca tls.crt >"$dir/ca.pem"
  private_secret_data "$gw" cp-client-ca tls.key >"$dir/ca.key"
  openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj /CN=data-plane \
    -keyout "$dir/dp.key" -out "$dir/dp.csr" 2>/dev/null
  openssl x509 -req -in "$dir/dp.csr" -CA "$dir/ca.pem" -CAkey "$dir/ca.key" -CAcreateserial -days 1 \
    -extfile <(printf 'extendedKeyUsage=clientAuth\n') -out "$dir/dp.pem" 2>/dev/null
  # What anyone able to issue data plane certificates could forge.
  openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$gw-cp" \
    -keyout "$dir/rogue.key" -out "$dir/rogue.csr" 2>/dev/null
  openssl x509 -req -in "$dir/rogue.csr" -CA "$dir/ca.pem" -CAkey "$dir/ca.key" -CAcreateserial -days 1 \
    -extfile <(printf 'extendedKeyUsage=serverAuth\nsubjectAltName=DNS:%s\n' "$gw-cp.$(ns).svc") -out "$dir/rogue.pem" 2>/dev/null
  openssl verify -CAfile "$dir/api-bundle.pem" -purpose sslserver "$dir/api.pem"
  openssl verify -CAfile "$dir/api-bundle.pem" -purpose sslclient "$dir/client.pem"
  openssl verify -CAfile "$dir/cp-server-bundle.pem" -purpose sslserver "$dir/cp.pem"
  openssl verify -CAfile "$dir/cp-client-bundle.pem" -purpose sslclient "$dir/dp.pem"
  ! openssl verify -CAfile "$dir/api-bundle.pem" -purpose sslclient "$dir/dp.pem" >/dev/null 2>&1 ||
    fail "a data plane client certificate chains to the API bundle"
  ! openssl verify -CAfile "$dir/cp-client-bundle.pem" -purpose sslclient "$dir/client.pem" >/dev/null 2>&1 ||
    fail "the operator client certificate chains to the cp client bundle"
  ! openssl verify -CAfile "$dir/cp-server-bundle.pem" -purpose sslserver "$dir/rogue.pem" >/dev/null 2>&1 ||
    fail "a server certificate issued from the cp client CA chains to the cp server bundle"
  ! openssl verify -CAfile "$dir/cp-client-bundle.pem" -purpose sslserver "$dir/cp.pem" >/dev/null 2>&1 ||
    fail "the cp server certificate chains to the cp client CA"
  cn() { openssl x509 -in "$dir/$1" -noout -subject -nameopt multiline | sed -n 's/^ *commonName *= //p'; }
  sans() { openssl x509 -in "$dir/$1" -noout -ext subjectAltName 2>/dev/null | grep -o 'DNS:[^,]*' || true; }
  [ "$(cn api.pem)" = "$gw-api" ] || fail "api certificate CN is '$(cn api.pem)'"
  [ "$(cn cp.pem)" = "$gw-cp" ] || fail "cp certificate CN is '$(cn cp.pem)'"
  [ "$(cn client.pem)" = kong-operator ] || fail "operator client certificate CN is '$(cn client.pem)'"
  sans api.pem | grep -qx "DNS:$gw-api.$(ns).svc" || fail "api certificate SANs: $(sans api.pem)"
  sans cp.pem | grep -qx "DNS:$gw-cp.$(ns).svc" || fail "cp certificate SANs: $(sans cp.pem)"
  [ -z "$(sans client.pem)" ] || fail "operator client certificate has DNS names: $(sans client.pem)"
  echo "ok: certificate chains of $gw verified"
  ;;
*)
  echo "unknown command $cmd" >&2
  exit 2
  ;;
esac
