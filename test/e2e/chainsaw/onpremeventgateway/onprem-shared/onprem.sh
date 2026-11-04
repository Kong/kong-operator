#!/usr/bin/env bash
# Helpers of the on-prem Event Gateway e2e tests, run from a test directory:
# ../onprem-shared/onprem.sh <command> [args...]
#
# Required env:
#   NAMESPACE      Namespace of the data plane and of its OnPremEventGateway.
# Required env of roundtrip:
#   HEADER_KEY     Header the consume policy injects.
#   HEADER_VALUE   Its value.
set -euo pipefail

ns() { echo "${NAMESPACE:?NAMESPACE must be set}"; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Every command prints one JSON object: see json_output.sh.
[ -n "${JSON_OUTPUT_INNER:-}" ] || exec bash "$SCRIPT_DIR/../../common/scripts/json_output.sh" "$0" "$@"
# shellcheck source=../../common/scripts/json_output_lib.sh
source "$SCRIPT_DIR/../../common/scripts/json_output_lib.sh"
CLIENT_CA_ANNOTATION=konghq.com/gw-admin-cp-client-ca
GWADMIN_NAMESPACE_LABEL=gateway-operator.konghq.com/gwadmin-namespace
GWADMIN_NAME_LABEL=gateway-operator.konghq.com/gwadmin-name

# kill_tree pid: kills the process and all its descendants. The process is
# stopped first, so it can't start new children while they are killed.
kill_tree() {
  local child
  kill -STOP "$1" 2>/dev/null || return 0
  for child in $(pgrep -P "$1"); do kill_tree "$child"; done
  kill -KILL "$1" 2>/dev/null || true
}

# bounded seconds command...: runs the command, killing it and its descendants
# after seconds. kubectl run -i can stay attached forever to a pod that already
# finished, and the timeout of a chainsaw script doesn't help: chainsaw kills the
# shell of the script, then waits for the processes left behind, which still
# hold its output. Killing only the command would leave behind the kubectl of a
# script.
bounded() {
  local seconds=$1 pid watchdog status=0
  shift
  "$@" &
  pid=$!
  (
    sleep "$seconds"
    echo "timed out after ${seconds}s: $*" >&2
    kill_tree "$pid"
  ) >/dev/null &
  watchdog=$!
  # Without the notice of the shell about the killed command, the timeout is the
  # last line of stderr.
  wait "$pid" 2>/dev/null || status=$?
  kill_tree "$watchdog"
  return "$status"
}

# fingerprint ca_file: prints the SHA-256 fingerprint of the certificate, in
# hex, as the GWAdmin compares it with the client CA annotation of the data
# planes.
fingerprint() { openssl x509 -in "$1" -outform der | openssl dgst -sha256 -r | cut -d' ' -f1; }

# dp_client_ca dp: prints the cp client CA the Pod template of the Deployment
# of the data plane records its client certificate comes from.
dp_client_ca() {
  kubectl get deployment -n "$(ns)" "$1" -o json | jq -r --arg a "$CLIENT_CA_ANNOTATION" '.spec.template.metadata.annotations[$a] // ""'
}

dp_client_ca_is() { [ "$(dp_client_ca "$1")" = "$2" ]; }

# dp_labelled dp: the Deployment of the data plane is labelled for a GWAdmin.
dp_labelled() {
  kubectl get deployment -n "$(ns)" "$1" -o json |
    jq -e --arg n "$GWADMIN_NAMESPACE_LABEL" --arg m "$GWADMIN_NAME_LABEL" '.metadata.labels | has($n) or has($m)' >/dev/null
}

dp_unlabelled() { ! dp_labelled "$1"; }

TRAFFIC_IMAGE=deviceinsight/kafkactl:v5.21.0

# traffic_log pod: prints the log of the traffic pod.
traffic_log() { kubectl logs -n "$(ns)" "$1" 2>/dev/null; }

# traffic_acked_at_least pod count: the traffic pod got count records
# acknowledged.
traffic_acked_at_least() { [ "$(traffic_log "$1" | grep -c '^ok ')" -ge "$2" ]; }

# traffic_stopped pod: the traffic pod stopped producing.
traffic_stopped() { traffic_log "$1" | grep -q '^stopped '; }

PROBE_IMAGE=alpine/k8s:1.33.4

# gwadmin_replaced gwadmin_ns gwadmin old_pods...: none of the old gw-admin
# pods is left, and the Deployment has every replica Ready.
gwadmin_replaced() {
  local gw_ns=$1 gw=$2 d want ready
  shift 2
  for p in "$@"; do
    if kubectl get pod -n "$gw_ns" "$p" >/dev/null 2>&1; then return 1; fi
  done
  d=$(kubectl get deployment -n "$gw_ns" "$gw" -o json)
  want=$(printf '%s' "$d" | jq '.spec.replicas')
  ready=$(printf '%s' "$d" | jq '.status.readyReplicas // 0')
  [ "$ready" = "$want" ]
}

# cp_config_status cert_dir gateway: prints the HTTP status gw-admin answers
# the request of the configuration of the gateway with the client
# certificate in cert_dir (see save-dp-cert), through the cp Service of its
# GWAdmin, from a pod in the cluster, as keg 1.3.0 makes it: the signals of a
# node, which stream the configuration, so the request is cut short once answered.
# gw-admin answers 400 without the X-Node-Version header keg sends.
cp_config_status() {
  local gateway gw_ns gw uid bundle ca
  gateway=$(kubectl get onpremeventgateway -n "$(ns)" "$2" -o json)
  uid=$(printf '%s' "$gateway" | jq -r '.metadata.uid')
  gw=$(printf '%s' "$gateway" | jq -r '.spec.gwAdminRef.name')
  gw_ns=$(printf '%s' "$gateway" | jq -r --arg ns "$(ns)" '.spec.gwAdminRef.namespace // $ns')
  bundle=$(kubectl get gwadmin -n "$gw_ns" "$gw" -o json | jq -er '.status.tls.cpServerCABundleSecretName')
  ca=$(kubectl get secret -n "$gw_ns" "$bundle" -o json | jq -er '.data["ca.crt"]')
  bounded 60 env kubectl run -n "$(ns)" "cp-probe-$(date +%s)-$RANDOM" --image="$PROBE_IMAGE" --rm -i --restart=Never --quiet \
    --env="CA=$ca" --env="CRT=$(base64 <"$1/tls.crt" | tr -d '\n')" --env="KEY=$(base64 <"$1/tls.key" | tr -d '\n')" \
    --env="URL=https://$gw-cp.$gw_ns.svc:8099/v1/data-planes/event-gateway/$uid/nodes" \
    --command -- sh -c 'echo "$CA" | base64 -d >/tmp/ca.crt; echo "$CRT" | base64 -d >/tmp/c.crt; echo "$KEY" | base64 -d >/tmp/c.key
      NODE=$(cat /proc/sys/kernel/random/uuid)
      curl -s -o /dev/null -w "%{http_code}" --max-time 5 --cacert /tmp/ca.crt --cert /tmp/c.crt --key /tmp/c.key \
        -H "X-Node-Id: $NODE" -H "X-Node-Version: 1.3.0" \
        "$URL/$NODE/signals?capabilities%5Bconfig_sync%5D%5Bversion%5D=v1&capabilities%5Bconfig_sync%5D%5Boffset%5D=INIT"' 2>/dev/null
}

# cp_config_status_is cert_dir gateway status: gw-admin answers status, or,
# for !status, anything else but a failure to answer.
cp_config_status_is() {
  local got
  got=$(cp_config_status "$1" "$2") || true
  # kubectl run can print the output of the pod twice.
  got=${got: -3}
  case $3 in
  !*) [ -n "$got" ] && [ "$got" != 000 ] && [ "$got" != "${3#!}" ] && return 0 ;;
  *) [ "$got" = "$3" ] && return 0 ;;
  esac
  echo "gw-admin answers ${got:-nothing} for the configuration of $2, waiting for $3" >&2
  return 1
}

# operator_idle: the Kong Operator has reconciled at least once since it started
# the data planes and the gateways, and nothing of those is queued or running.
# Its first reconciles are all queued before its first worker starts. Other
# tests running at once only delay this.
operator_idle() {
  local pod metrics controller
  pod=$(kubectl get pods -n kong-system -l app.kubernetes.io/name=kong-operator -o json |
    jq -er '[.items[] | select(.metadata.deletionTimestamp == null and .status.phase == "Running")][0].metadata.name')
  metrics=$(kubectl get --raw "/api/v1/namespaces/kong-system/pods/$pod:8080/proxy/metrics")
  for controller in kegdataplane onpremeventgateway; do
    printf '%s\n' "$metrics" | awk -v label="controller=\"$controller\"" '
      /^(workqueue_depth|controller_runtime_active_workers)\{/ && index($0, label) { busy += $NF }
      /^controller_runtime_reconcile_total\{/ && index($0, label) { done += $NF }
      END { exit !(busy == 0 && done > 0) }' || return 1
  done
}

# operator_settled: operator_idle holds three times in a row, a second apart:
# what a reconcile changes takes a moment to queue the next one.
operator_settled() {
  operator_idle && sleep 1 && operator_idle && sleep 1 && operator_idle
}

# long_traffic_file pod file: prints the file of the long-lived traffic pod.
long_traffic_file() { kubectl exec -n "$(ns)" "$1" -- cat "$2" 2>/dev/null; }

long_traffic_running() { [ -n "$(long_traffic_file "$1" /tmp/produced)" ] && [ "$(long_traffic_file "$1" /tmp/produced)" -ge "$2" ]; }

long_traffic_stopped() { [ -n "$(long_traffic_file "$1" /tmp/producer.exit)" ]; }

# long_traffic_consumed pod count: the consumer of the pod read the records 1
# to count.
long_traffic_consumed() {
  local consumed n
  consumed=$(long_traffic_file "$1" /tmp/consumed)
  for n in $(seq 1 "$2"); do
    printf '%s\n' "$consumed" | grep -E "(^|[[:space:]#])seq-$n\$" | grep -q "${HEADER_KEY:?}:${HEADER_VALUE:?}" ||
      { echo "record seq-$n not consumed with the header yet" >&2; return 1; }
  done
}

# dp_certificates dp: prints the names of the EventGatewayDataPlaneCertificates
# the data plane controls.
dp_certificates() {
  kubectl get eventgatewaydataplanecertificates.configuration.konghq.com -n "$(ns)" -o json |
    jq -r --arg dp "$1" '.items[] | select(any(.metadata.ownerReferences[]?; .kind == "KegDataPlane" and .name == $dp)) | .metadata.name'
}

# dp_cert_fingerprint dp: prints the fingerprint of the client certificate of the
# Secret the Deployment of the data plane mounts.
dp_cert_fingerprint() {
  local secret
  secret=$(kubectl get deployment -n "$(ns)" "$1" -o json | jq -er '.spec.template.spec.volumes[] | select(.name == "konnect-cert") | .secret.secretName')
  kubectl get secret -n "$(ns)" "$secret" -o json | jq -er '.data["tls.crt"] | @base64d' | openssl x509 -outform der | openssl dgst -sha256 -r | cut -d' ' -f1
}

dp_cert_is() { [ "$(dp_cert_fingerprint "$1")" = "$2" ]; }

# manual_copies_are dp secret cert_dir...: the data plane has one
# EventGatewayDataPlaneCertificate for each certificate of the cert_dirs, each
# registered from a Secret of its own: a copy of the certificate of the Manual
# Secret, with its key, marked as such, and controlled by the data plane.
manual_copies_are() {
  local dp=$1 secret=$2 cert copy got='' want=''
  shift 2
  for dir in "$@"; do want+="$(fingerprint "$dir/tls.crt")"$'\n'; done
  for cert in $(dp_certificates "$dp"); do
    ref=$(kubectl get eventgatewaydataplanecertificates.configuration.konghq.com -n "$(ns)" "$cert" -o json | jq -r '.spec.apiSpec.certificate.secretRef.name // ""')
    [ "$ref" = "$cert" ] || { echo "the certificate $cert is registered from the Secret '$ref', not from its copy" >&2; return 1; }
    copy=$(kubectl get secret -n "$(ns)" "$ref" -o json) || { echo "the copy $ref doesn't exist" >&2; return 1; }
    printf '%s' "$copy" | jq -e --arg dp "$dp" --arg from "$(ns)/$secret" '
      .metadata.labels["gateway-operator.konghq.com/secret-copy"] == "manual-certificate"
      and .metadata.annotations["gateway-operator.konghq.com/copied-from"] == $from
      and (.data | has("tls.crt") and has("tls.key"))
      and any(.metadata.ownerReferences[]?; .kind == "KegDataPlane" and .name == $dp and .controller == true)' >/dev/null ||
      { echo "the copy $ref isn't marked as a copy of $secret, lacks the certificate or the key, or isn't controlled by $dp" >&2; return 1; }
    got+="$(printf '%s' "$copy" | jq -er '.data["tls.crt"] | @base64d' | openssl x509 -outform der | openssl dgst -sha256 -r | cut -d' ' -f1)"$'\n'
  done
  [ "$(printf '%s' "$got" | sort)" = "$(printf '%s' "$want" | sort)" ] ||
    { echo "the certificates of $dp are registered from copies of [$got], not of [$want]" >&2; return 1; }
}

cmd=$1
shift
case $cmd in
bounded) # seconds command...: runs the command, killing it and its descendants after seconds, and reports its output
  output=$(bounded "$@") || { status=$?; printf '%s\n' "$output"; exit "$status"; }
  result output "$output"
  ;;
wait-dp-cert) # dp cert_dir timeout: waits for the Deployment of the data plane to mount the client certificate of cert_dir
  want=$(fingerprint "$2/tls.crt")
  until_true "$3" "$1 mounts the certificate of $2" dp_cert_is "$1" "$want"
  until_result data_plane "$1" certificate "$want"
  ;;
assert-dp-cert) # dp cert_dir: the Deployment of the data plane mounts the client certificate of cert_dir right now
  want=$(fingerprint "$2/tls.crt")
  got=$(dp_cert_fingerprint "$1")
  [ "$got" = "$want" ] || { echo "$1 mounts the certificate $got, not the one of $2 ($want)" >&2; exit 1; }
  result data_plane "$1" certificate "$got"
  ;;
replace-dp-pods) # dp timeout: deletes the pods of the data plane, as a restart or a drain does, and waits for new ones to be Ready, which takes the configuration of gw-admin
  selector=$(kubectl get deployment -n "$(ns)" "$1" -o json | jq -er '.spec.selector.matchLabels | to_entries | map("\(.key)=\(.value)") | join(",")')
  old=$(kubectl get pods -n "$(ns)" -l "$selector" -o json | jq -r '.items[].metadata.name' | tr '\n' ' ')
  kubectl delete pods -n "$(ns)" -l "$selector" --wait=false >/dev/null
  # shellcheck disable=SC2086 # one word per pod name
  until_true "$2" "the pods of $1 are replaced and Ready" gwadmin_replaced "$(ns)" "$1" $old
  until_result data_plane "$1" replaced_pods "$old"
  ;;
wait-manual-copies) # dp secret timeout cert_dir...: waits for the data plane to register exactly the certificates of the cert_dirs, each from its copy of the Manual Secret (see manual_copies_are)
  dp=$1 secret=$2 timeout=$3
  shift 3
  until_true "$timeout" "$dp registers exactly the certificates of $*, from copies of $secret" manual_copies_are "$dp" "$secret" "$@"
  until_result data_plane "$dp" secret "$secret" certificates "$#"
  ;;
gen-intermediate-ca) # root_dir common-name dir: writes to dir/tls.key the key of an intermediate CA signed by the CA in root_dir, and to dir/tls.crt the chain: the intermediate, then the root
  mkdir -p "$3"
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$2" \
    -keyout "$3/tls.key" -out "$3/tls.csr" 2>/dev/null
  openssl x509 -req -in "$3/tls.csr" -CA "$1/tls.crt" -CAkey "$1/tls.key" -CAcreateserial -days 30 \
    -extfile <(printf 'basicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,cRLSign\n') \
    -out "$3/intermediate.crt" 2>/dev/null
  cat "$3/intermediate.crt" "$1/tls.crt" >"$3/tls.crt"
  result chain_file "$3/tls.crt" key_file "$3/tls.key"
  ;;
assert-cert-issuer) # cert_dir common-name: the certificate in cert_dir is signed by the certificate authority of that common name
  issuer=$(openssl x509 -in "$1/tls.crt" -noout -issuer -nameopt multiline | sed -n 's/^ *commonName *= //p')
  [ "$issuer" = "$2" ] || { echo "the certificate of $1 is issued by '$issuer', not by '$2'" >&2; exit 1; }
  result cert_file "$1/tls.crt" issuer "$issuer"
  ;;
gwadmin-replace-pods) # gwadmin_ns gwadmin one|all timeout: deletes one, or every, gw-admin pod of the GWAdmin, and waits for their replacements to be Ready, which takes Kong Operator provisioning them, as gw-admin keeps nothing across a restart
  sel=$(kubectl get deployment -n "$1" "$2" -o json | jq -r '.spec.selector.matchLabels | to_entries | map("\(.key)=\(.value)") | join(",")')
  pods=()
  while IFS= read -r p; do pods+=("$p"); done < <(kubectl get pods -n "$1" -l "$sel" -o json | jq -r '.items[] | select(.metadata.deletionTimestamp == null) | .metadata.name')
  [ "${#pods[@]}" -gt 0 ] || { echo "no gw-admin pod" >&2; exit 1; }
  [ "$3" = all ] || pods=("${pods[0]}")
  kubectl delete pod -n "$1" "${pods[@]}" --wait=false >/dev/null
  until_true "$4" "the gw-admin pods ${pods[*]} are replaced by Ready ones" gwadmin_replaced "$1" "$2" "${pods[@]}"
  until_result gwadmin "$2" replaced_pods "${pods[*]}"
  ;;
save-dp-cert) # dp dir: saves the client certificate the Deployment of the data plane mounts to dir/tls.crt and dir/tls.key, and the keg version it runs, which keg sends along, to dir/version
  d=$(kubectl get deployment -n "$(ns)" "$1" -o json)
  secret=$(printf '%s' "$d" | jq -er '.spec.template.spec.volumes[] | select(.name == "konnect-cert") | .secret.secretName')
  mkdir -p "$2"
  kubectl get secret -n "$(ns)" "$secret" -o json | jq -er '.data["tls.crt"] | @base64d' >"$2/tls.crt"
  kubectl get secret -n "$(ns)" "$secret" -o json | jq -er '.data["tls.key"] | @base64d' >"$2/tls.key"
  printf '%s' "$d" | jq -er '.spec.template.spec.containers[] | select(.name == "keg") | .image | split(":") | last' >"$2/version"
  result cert_file "$2/tls.crt" key_file "$2/tls.key" keg_version "$(cat "$2/version")"
  ;;
expect-cp-config) # cert_dir gateway status timeout: waits for gw-admin to answer status to the request of the configuration of the gateway with the client certificate in cert_dir
  until_true "$4" "gw-admin answers $3 for the configuration of $2 with $1" cp_config_status_is "$1" "$2" "$3"
  until_result gateway "$2" expected_status "$3"
  ;;
expect-blocked) # dp topic timeout: waits for producing through the data plane to fail ten times in a row, e.g. once it no longer forwards its listener
  inner='cat >/tmp/k.yml <<EOF
contexts:
  vc:
    brokers: ["$GATEWAY_ADDR"]
    requestTimeout: 1s
EOF
end=$(($(date +%s) + DEADLINE)); failures=0
while [ "$(date +%s)" -lt "$end" ]; do
  # Kafka creates no topic on first use: a missing topic must not count as blocked.
  kafkactl -C /tmp/k.yml --context vc create topic "$TOPIC" >/dev/null 2>&1
  if kafkactl -C /tmp/k.yml --context vc produce "$TOPIC" --value blocked >/dev/null 2>&1; then failures=0; else failures=$((failures + 1)); fi
  [ "$failures" -ge 10 ] && { echo "producing fails"; exit 0; }
  sleep 1
done
echo "producing still succeeds" >&2; exit 1'
  output=$(bounded $(($3 + 120)) env kubectl run -n "$(ns)" "blocked-$(date +%s)" --image="$TRAFFIC_IMAGE" --rm -i --restart=Never --quiet \
    --env="GATEWAY_ADDR=$1-kafka.$(ns).svc:19092" --env="TOPIC=$2" --env="DEADLINE=$3" \
    --command -- sh -c "$inner")
  result data_plane "$1" topic "$2" kubectl_output "$output"
  ;;
acl-blocked) # dp topic: creates the topic through the data plane, and waits for the ACL policy to deny producing to it
  inner='cat >/tmp/k.yml <<EOF
contexts:
  vc:
    brokers: ["$GATEWAY_ADDR"]
    requestTimeout: 1s
EOF
for i in $(seq 1 180); do
  kafkactl -C /tmp/k.yml --context vc create topic "$TOPIC" >/dev/null 2>&1
  out=$(kafkactl -C /tmp/k.yml --context vc produce "$TOPIC" --value blocked 2>&1)
  echo "$out" | grep -qi "not authorized" && { echo "ok: denied: $out"; exit 0; }
  sleep 1
done
echo "producing is not denied: $out" >&2; exit 1'
  output=$(bounded 300 env kubectl run -n "$(ns)" "acl-$(date +%s)" --image="$TRAFFIC_IMAGE" --rm -i --restart=Never --quiet \
    --env="GATEWAY_ADDR=$1-kafka.$(ns).svc:19092" --env="TOPIC=$2" \
    --command -- sh -c "$inner")
  result data_plane "$1" topic "$2" kubectl_output "$output"
  ;;
traffic-start) # pod dp topic: starts producing a record every half second through the data plane, from the pod, and waits for the first records to be acknowledged
  kubectl run -n "$(ns)" "$1" --image="$TRAFFIC_IMAGE" --restart=Never \
    --env="GATEWAY_ADDR=$2-kafka.$(ns).svc:19092" --env="TOPIC=$3" --env="TRAFFIC_INTERVAL=0.5" \
    --command -- sh -c "$(cat "$SCRIPT_DIR/traffic_inner.sh")" >/dev/null
  until_true 180 "the traffic pod $1 gets records acknowledged" traffic_acked_at_least "$1" 5
  until_result pod "$1" data_plane "$2" topic "$3"
  ;;
long-traffic-start) # pod dp topic: starts a long-lived producer session, producing a record every half second through the data plane, and a long-lived consumer session, from the pod, and waits for the first records
  kubectl run -n "$(ns)" "$1" --image="$TRAFFIC_IMAGE" --restart=Never \
    --env="GATEWAY_ADDR=$2-kafka.$(ns).svc:19092" --env="TOPIC=$3" --env="TRAFFIC_INTERVAL=0.5" \
    --command -- sh -c "$(cat "$SCRIPT_DIR/long_traffic_inner.sh")" >/dev/null
  until_true 180 "the long-lived traffic pod $1 produces" long_traffic_running "$1" 5
  until_true 120 "the long-lived consumer of $1 reads the first records" long_traffic_consumed "$1" 3
  until_result pod "$1" data_plane "$2" topic "$3"
  ;;
long-traffic-check) # pod: stops the long-lived producer of the pod, fails when it didn't get every record acknowledged, and checks the consumer group read every record, with the header the consume policy injects
  kubectl exec -n "$(ns)" "$1" -- touch /tmp/stop
  until_true 90 "the long-lived producer of $1 stops" long_traffic_stopped "$1"
  code=$(long_traffic_file "$1" /tmp/producer.exit)
  produced=$(long_traffic_file "$1" /tmp/produced)
  if [ "$code" != 0 ]; then
    echo "the long-lived producer failed after $produced records (exit $code):" >&2
    long_traffic_file "$1" /tmp/producer.log | tail -20 >&2
    exit 1
  fi
  if ! until_true 120 "the long-lived consumer of $1 reads the $produced records" long_traffic_consumed "$1" "$produced"; then
    echo "consumer sessions restarted: $(long_traffic_file "$1" /tmp/consumer.restarts || true), last consumer errors:" >&2
    long_traffic_file "$1" /tmp/consumer.log | tail -20 >&2 || true
    echo "the long-lived consumer of $1 did not read the $produced records" >&2
    exit 1
  fi
  restarts=$(long_traffic_file "$1" /tmp/consumer.restarts)
  jq -n --arg pod "$1" --argjson records "$produced" --argjson restarts "${restarts:-0}" \
    '{pod: $pod, records_acknowledged: $records, records_consumed: $records, consumer_restarts: $restarts}'
  ;;
traffic-check) # pod: stops the traffic of the pod, fails when a record wasn't acknowledged, and checks every record acknowledged is consumed, with the header the consume policy injects
  kubectl exec -n "$(ns)" "$1" -- touch /tmp/stop
  until_true 60 "the traffic pod $1 stops" traffic_stopped "$1"
  log=$(traffic_log "$1")
  acked=$(printf '%s\n' "$log" | grep -c '^ok ' || true)
  failed=$(printf '%s\n' "$log" | grep '^fail ' || true)
  if [ -n "$failed" ]; then
    echo "records not acknowledged through the data plane, out of $(printf '%s\n' "$log" | grep '^stopped ' | cut -d' ' -f2):" >&2
    printf '%s\n' "$failed" >&2
    exit 1
  fi
  topic=$(kubectl get pod -n "$(ns)" "$1" -o json | jq -r '.spec.containers[0].env[] | select(.name == "TOPIC") | .value')
  consumed=$(bounded 120 env kubectl exec -n "$(ns)" "$1" -- kafkactl -C /tmp/kafkactl.yml --context vc consume "$topic" --from-beginning --exit --print-headers)
  missing=""
  for n in $(printf '%s\n' "$log" | sed -n 's/^ok //p'); do
    printf '%s\n' "$consumed" | grep -E "(^|[[:space:]#])seq-$n\$" | grep -q "${HEADER_KEY:?}:${HEADER_VALUE:?}" || missing="$missing $n"
  done
  [ -z "$missing" ] || { echo "records acknowledged but not consumed with the header:$missing" >&2; exit 1; }
  jq -n --arg pod "$1" --argjson records "$acked" '{pod: $pod, records_acknowledged: $records, records_consumed: $records}'
  ;;
roundtrip) # dp topic record: produces the record through the data plane and consumes it back, with the header the consume policy injects
  output=$(
    cd "$SCRIPT_DIR/../../eventgateway/proxy-roundtrip"
    GATEWAY_ADDR="$1-kafka.$(ns).svc:19092" TOPIC=$2 RECORD_VALUE=$3 bounded 270 bash scripts/kafkactl_roundtrip.sh
  )
  result data_plane "$1" topic "$2" record "$3" kafkactl_output "$output"
  ;;
gen-client-cert) # ca_dir common-name dir: writes a client certificate signed by the CA in ca_dir to dir/tls.crt and dir/tls.key, as a user provides one for a Manual data plane certificate
  mkdir -p "$3"
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$2" \
    -keyout "$3/tls.key" -out "$3/tls.csr" 2>/dev/null
  openssl x509 -req -in "$3/tls.csr" -CA "$1/tls.crt" -CAkey "$1/tls.key" -CAcreateserial -days 30 \
    -extfile <(printf 'basicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=clientAuth\n') \
    -out "$3/tls.crt" 2>/dev/null
  result cert_file "$3/tls.crt" key_file "$3/tls.key"
  ;;
encryption-check) # dp topic: produces a record through the data plane, and checks keg hands it back as it was, while Kafka holds it encrypted
  inner='cat >/tmp/k.yml <<EOF
contexts:
  keg:
    brokers: ["$GATEWAY_ADDR"]
    requestTimeout: 1s
  kafka:
    brokers: ["$KAFKA_DIRECT"]
EOF
value="plaintext-$(date +%s)"
i=0
until kafkactl -C /tmp/k.yml --context keg create topic "$TOPIC" >/dev/null 2>&1 ||
  kafkactl -C /tmp/k.yml --context keg describe topic "$TOPIC" >/dev/null 2>&1; do
  i=$((i + 1)); [ "$i" -ge 180 ] && { echo "creating the topic through keg fails" >&2; exit 1; }; sleep 1
done
i=0
until kafkactl -C /tmp/k.yml --context keg produce "$TOPIC" --value "$value" >/dev/null 2>&1; do
  i=$((i + 1)); [ "$i" -ge 180 ] && { echo "producing through keg fails" >&2; exit 1; }; sleep 1
done
# Consuming right after the first produce to a new topic can find it empty.
i=0
until through_keg=$(kafkactl -C /tmp/k.yml --context keg consume "$TOPIC" --from-beginning --exit 2>&1); echo "$through_keg" | grep -qx "$value"; do
  i=$((i + 1)); [ "$i" -ge 180 ] && { echo "keg hands back: $through_keg" >&2; exit 1; }; sleep 1
done
in_kafka=$(kafkactl -C /tmp/k.yml --context kafka consume "$KAFKA_TOPIC" --from-beginning --exit 2>&1)
[ -n "$in_kafka" ] || { echo "Kafka holds nothing" >&2; exit 1; }
echo "$in_kafka" | grep -q "$value" && { echo "Kafka holds the record in plaintext: $in_kafka" >&2; exit 1; }
echo "ok: keg hands back $value, which Kafka holds encrypted"'
  # The virtual cluster prefixes the topic with the namespace on the shared
  # Kafka cluster (onprem-shared/entities.yaml).
  output=$(bounded 300 env kubectl run -n "$(ns)" "encryption-$(date +%s)" --image="$TRAFFIC_IMAGE" --rm -i --restart=Never --quiet \
    --env="GATEWAY_ADDR=$1-kafka.$(ns).svc:19092" --env="KAFKA_DIRECT=kafka-bootstrap.kafka-shared.svc:9092" \
    --env="TOPIC=$2" --env="KAFKA_TOPIC=$(ns)_$2" \
    --command -- sh -c "$inner")
  result data_plane "$1" topic "$2" kubectl_output "$output"
  ;;
mtls-check) # dp server_ca_dir client_dir|none ok|rejected: produces through the TLS listener of the data plane, verifying it with the CA in server_ca_dir, with the client certificate in client_dir, or none, and checks it is accepted, or rejected every time
  if [ "$3" = none ]; then crt=""; key=""; else crt=$(base64 <"$3/tls.crt" | tr -d '\n'); key=$(base64 <"$3/tls.key" | tr -d '\n'); fi
  inner='echo "$CA" | base64 -d >/tmp/ca.crt
tls="    tls:
      enabled: true
      ca: /tmp/ca.crt"
if [ -n "$CRT" ]; then
  echo "$CRT" | base64 -d >/tmp/c.crt; echo "$KEY" | base64 -d >/tmp/c.key
  tls="$tls
      cert: /tmp/c.crt
      certKey: /tmp/c.key"
fi
cat >/tmp/k.yml <<EOF
contexts:
  keg:
    brokers: ["$GATEWAY_ADDR"]
    requestTimeout: 1s
$tls
EOF
ok=0; failed=0
for i in $(seq 1 180); do
  # Kafka creates no topic on first use: a missing topic must not count as rejected.
  kafkactl -C /tmp/k.yml --context keg create topic "$TOPIC" >/dev/null 2>&1
  if kafkactl -C /tmp/k.yml --context keg produce "$TOPIC" --value "mtls-$i" >/tmp/out 2>&1; then ok=$((ok + 1)); else failed=$((failed + 1)); fi
  case $EXPECT in
  ok) [ "$ok" -ge 3 ] && { echo "ok: accepted"; exit 0; } ;;
  rejected) [ "$failed" -ge 10 ] && [ "$ok" -eq 0 ] && { echo "ok: rejected: $(cat /tmp/out)"; exit 0; } ;;
  esac
  [ "$EXPECT" = rejected ] && [ "$ok" -gt 0 ] && { echo "accepted, although expected to be rejected" >&2; exit 1; }
  sleep 1
done
echo "expected $EXPECT, got $ok accepted and $failed rejected: $(cat /tmp/out)" >&2; exit 1'
  output=$(bounded 300 env kubectl run -n "$(ns)" "mtls-$(date +%s)" --image="$TRAFFIC_IMAGE" --rm -i --restart=Never --quiet \
    --env="GATEWAY_ADDR=$1-kafka.$(ns).svc:19092" --env="TOPIC=mtls" --env="EXPECT=$4" \
    --env="CA=$(base64 <"$2/tls.crt" | tr -d '\n')" --env="CRT=$crt" --env="KEY=$key" \
    --command -- sh -c "$inner")
  result data_plane "$1" expected "$4" kubectl_output "$output"
  ;;
wait-operator-idle) # timeout: waits for the Kong Operator to finish what it has to do for data planes and gateways, e.g. after a change or a restart
  until_true "$1" "the Kong Operator is idle" operator_settled
  until_result
  ;;
wait-rolled-out) # dp timeout: waits for the Deployment of the data plane to roll out
  kubectl rollout status deployment -n "$(ns)" "$1" --timeout="$2s" >/dev/null
  result data_plane "$1"
  ;;
wait-dp-client-ca) # dp ca_file timeout: waits for the data plane to run a client certificate from the CA in ca_file, rolled out
  want=$(fingerprint "$2")
  until_true "$3" "$1 records a client certificate from $2" dp_client_ca_is "$1" "$want"
  kubectl rollout status deployment -n "$(ns)" "$1" --timeout="$3s" >/dev/null
  until_result data_plane "$1" client_ca "$want"
  ;;
assert-dp-client-ca) # dp ca_file: the data plane runs a client certificate from the CA in ca_file right now
  want=$(fingerprint "$2")
  got=$(dp_client_ca "$1")
  [ "$got" = "$want" ] || { echo "$1 records the client CA $got, not $want ($2)" >&2; exit 1; }
  result data_plane "$1" client_ca "$got"
  ;;
wait-dp-gateway-id) # dp gateway timeout: waits for the data plane to run keg with the UID of the OnPremEventGateway, the id gw-admin serves it under, rolled out
  dp_gateway_id_is() {
    local uid got
    uid=$(kubectl get onpremeventgateway -n "$(ns)" "$2" -o jsonpath='{.metadata.uid}')
    got=$(kubectl get deployment -n "$(ns)" "$1" -o json |
      jq -r '.spec.template.spec.containers[] | select(.name == "keg") | .env[]? | select(.name == "KONG_KONNECT_GATEWAY_CLUSTER_ID") | .value')
    [ -n "$uid" ] && [ "$got" = "$uid" ]
  }
  until_true "$3" "$1 runs keg with the UID of $2" dp_gateway_id_is "$1" "$2"
  kubectl rollout status deployment -n "$(ns)" "$1" --timeout="$3s" >/dev/null
  until_result data_plane "$1" gateway "$2"
  ;;
wait-dp-unlabelled) # dp timeout: waits for the Deployment of the data plane not to be labelled for a GWAdmin
  until_true "$2" "the Deployment $1 is no longer labelled for its GWAdmin" dp_unlabelled "$1"
  until_result data_plane "$1"
  ;;
*)
  echo "unknown command $cmd" >&2
  exit 1
  ;;
esac
