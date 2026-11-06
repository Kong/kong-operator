#!/usr/bin/env bash
# Fixture for the konnect-private chainsaw suite: a fake Konnect API answering
# on the private hostnames (us.svc.konghq.tech, global.svc.konghq.tech), with
# the cluster DNS pointed at it and the operator trusting its certificate.
#
# Deviation from the real thing, on purpose: the certificate here is signed by
# a CA generated for the test and injected into the operator pod, while the
# endpoint Kong serves over PrivateLink is publicly trusted and needs no such
# configuration.
#
# Usage: prereq.sh install | uninstall (through `make test.e2e.chainsaw.prereq`).
set -o errexit
set -o nounset
set -o pipefail

STUB_NAMESPACE=konnect-private-stub
OPERATOR_NAMESPACE=kong-system
OPERATOR_SELECTOR=app.kubernetes.io/name=kong-operator
# The two hostnames are the ones Konnect serves over PrivateLink, observed on
# Konnect dev on 2026-10-13; the rest of the fixture is placeholder values.
REGIONAL_HOST=us.svc.konghq.tech
GLOBAL_HOST=global.svc.konghq.tech
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# Holds the generated CA and certificate. install regenerates them on every run,
# so pass WORK_DIR=<dir> to keep one across runs of the same cluster.
WORK_DIR="${WORK_DIR:-$(mktemp -d)}"

info() { echo "INFO: $*"; }

# operator_deployment prints the operator Deployment holding kong-operator.
operator_deployment() {
  kubectl -n "${OPERATOR_NAMESPACE}" get deploy -l "${OPERATOR_SELECTOR}" -o name | head -1
}

# coredns_hosts_inserted reports whether the Corefile already carries the hosts
# block for the private hostnames.
coredns_hosts_inserted() {
  kubectl -n kube-system get configmap coredns -o jsonpath='{.data.Corefile}' | grep -q "${REGIONAL_HOST}"
}

# generate_certificates writes a CA and a server certificate for the two
# private hostnames into WORK_DIR.
generate_certificates() {
  info "generating the CA and the server certificate in ${WORK_DIR}"
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 365 \
    -subj "/CN=konnect-stub-ca" -addext "basicConstraints=critical,CA:TRUE" \
    -keyout "${WORK_DIR}/ca.key" -out "${WORK_DIR}/ca.crt" 2>/dev/null
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
    -subj "/CN=${REGIONAL_HOST}" -keyout "${WORK_DIR}/tls.key" -out "${WORK_DIR}/tls.csr" 2>/dev/null
  openssl x509 -req -in "${WORK_DIR}/tls.csr" -CA "${WORK_DIR}/ca.crt" -CAkey "${WORK_DIR}/ca.key" \
    -CAcreateserial -days 30 -out "${WORK_DIR}/tls.crt" -extfile <(printf '%s\n' \
      "basicConstraints=CA:FALSE" \
      "keyUsage=critical,digitalSignature" \
      "extendedKeyUsage=serverAuth" \
      "subjectAltName=DNS:${REGIONAL_HOST},DNS:${GLOBAL_HOST}") 2>/dev/null
}

# apply_stub applies the manifests and rolls the stub onto the current config.
apply_stub() {
  kubectl apply -f "${SCRIPT_DIR}/00-namespace.yaml"
  kubectl -n "${STUB_NAMESPACE}" create secret tls konnect-stub-tls \
    --cert="${WORK_DIR}/tls.crt" --key="${WORK_DIR}/tls.key" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl apply -f "${SCRIPT_DIR}/10-konnect-stub-config.yaml"
  kubectl apply -f "${SCRIPT_DIR}/20-konnect-stub.yaml"

  # Kong reads its declarative config at startup: stamp the pod template with
  # the config's checksum so a changed config rolls the pods.
  local checksum
  checksum="$(cksum <"${SCRIPT_DIR}/10-konnect-stub-config.yaml" | cut -d' ' -f1)"
  kubectl -n "${STUB_NAMESPACE}" patch deploy/konnect-stub --type merge \
    -p "{\"spec\":{\"template\":{\"metadata\":{\"annotations\":{\"konghq.com/konnect-stub-config-checksum\":\"${checksum}\"}}}}}"

  # Kong reads the certificate at startup, and every install regenerates it:
  # roll the pods so what they serve and what the operator trusts are the same
  # material.
  kubectl -n "${STUB_NAMESPACE}" rollout restart deploy/konnect-stub
  kubectl -n "${STUB_NAMESPACE}" rollout status deploy/konnect-stub --timeout=180s
}

# point_cluster_dns_at_stub adds a hosts block for the two private hostnames,
# ahead of the forward plugin, and restarts CoreDNS. The Corefile it replaces is
# kept in a ConfigMap so uninstall can put it back verbatim.
point_cluster_dns_at_stub() {
  if coredns_hosts_inserted; then
    info "CoreDNS already resolves the private hostnames"
    return
  fi

  local stub_ip corefile
  stub_ip="$(kubectl -n "${STUB_NAMESPACE}" get svc konnect-stub -o jsonpath='{.spec.clusterIP}')"
  [ -n "${stub_ip}" ] || {
    echo "the konnect-stub Service has no ClusterIP" >&2
    exit 1
  }
  corefile="$(kubectl -n kube-system get configmap coredns -o jsonpath='{.data.Corefile}')"
  printf '%s\n' "${corefile}" | grep -q 'forward \. /etc/resolv.conf' || {
    echo "no forward plugin in the Corefile: cannot place the hosts block" >&2
    exit 1
  }
  info "pointing ${REGIONAL_HOST} and ${GLOBAL_HOST} at ${stub_ip}"

  printf '%s' "${corefile}" | kubectl -n "${STUB_NAMESPACE}" create configmap konnect-coredns-backup \
    --from-file=Corefile=/dev/stdin --dry-run=client -o yaml | kubectl apply -f -

  # The Corefile is indented with four spaces; keep the block aligned with the
  # other plugins.
  kubectl -n kube-system get configmap coredns -o json |
    jq --arg block "    hosts {
        ${stub_ip} ${REGIONAL_HOST} ${GLOBAL_HOST}
        fallthrough
    }
" '.data.Corefile = (.data.Corefile | sub("forward \\. /etc/resolv\\.conf"; $block + "forward . /etc/resolv.conf"))' |
    kubectl apply -f -

  coredns_hosts_inserted || {
    echo "the Corefile rewrite did not apply" >&2
    exit 1
  }
  kubectl -n kube-system rollout restart deployment coredns
  kubectl -n kube-system rollout status deployment coredns --timeout=60s
}

# trust_stub_in_operator gives the operator the test CA as its only trust
# store: with SSL_CERT_DIR set, Go stops reading the system directories, so a
# call to any host but the stub fails its handshake.
trust_stub_in_operator() {
  local deploy
  deploy="$(operator_deployment)"
  [ -n "${deploy}" ] || {
    echo "no operator Deployment found in ${OPERATOR_NAMESPACE}" >&2
    exit 1
  }

  info "sharing the test CA with the operator (${deploy})"
  kubectl -n "${OPERATOR_NAMESPACE}" create configmap konnect-stub-ca \
    --from-file=ca.crt="${WORK_DIR}/ca.crt" --dry-run=client -o yaml | kubectl apply -f -

  kubectl -n "${OPERATOR_NAMESPACE}" patch "${deploy}" --type=strategic -p '{
    "spec": { "template": { "spec": {
      "volumes": [ { "name": "konnect-stub-ca", "configMap": { "name": "konnect-stub-ca" } } ],
      "containers": [ {
        "name": "manager",
        "volumeMounts": [ { "name": "konnect-stub-ca", "mountPath": "/etc/konnect-stub-ca", "readOnly": true } ],
        "env": [
          { "name": "SSL_CERT_FILE", "value": "/etc/konnect-stub-ca/ca.crt" },
          { "name": "SSL_CERT_DIR", "value": "/etc/konnect-stub-ca" }
        ]
      } ]
    } } } }'

  # Every install writes a new CA into the ConfigMap, and Go reads the trust
  # store once per process: restart so the running operator is the one that
  # trusts the certificate the stub now serves.
  kubectl -n "${OPERATOR_NAMESPACE}" rollout restart "${deploy}"
  kubectl -n "${OPERATOR_NAMESPACE}" rollout status "${deploy}" --timeout=120s
}

# drop_operator_trust undoes trust_stub_in_operator.
drop_operator_trust() {
  local deploy
  deploy="$(operator_deployment)" || return 0
  [ -n "${deploy}" ] || return 0

  info "taking the test CA back out of the operator (${deploy})"
  # Remove only what install added: the operator's own env (the CONTROLLER_*
  # flags the chart sets) has to survive.
  kubectl -n "${OPERATOR_NAMESPACE}" patch "${deploy}" --type=strategic -p '{
    "spec": { "template": { "spec": {
      "volumes": [ { "name": "konnect-stub-ca", "$patch": "delete" } ],
      "containers": [ {
        "name": "manager",
        "volumeMounts": [ { "mountPath": "/etc/konnect-stub-ca", "$patch": "delete" } ],
        "env": [
          { "name": "SSL_CERT_FILE", "$patch": "delete" },
          { "name": "SSL_CERT_DIR", "$patch": "delete" }
        ]
      } ]
    } } } }' || true
  kubectl -n "${OPERATOR_NAMESPACE}" delete configmap konnect-stub-ca --ignore-not-found
  kubectl -n "${OPERATOR_NAMESPACE}" rollout status "${deploy}" --timeout=120s || true
}

# unpatch_cluster_dns puts the Corefile saved at install back.
unpatch_cluster_dns() {
  local corefile
  if ! corefile="$(kubectl -n "${STUB_NAMESPACE}" get configmap konnect-coredns-backup -o jsonpath='{.data.Corefile}' 2>/dev/null)" ||
    [ -z "${corefile}" ]; then
    info "no Corefile backup: leaving the cluster DNS as it is"
    return 0
  fi

  info "restoring the Corefile from the backup"
  kubectl -n kube-system get configmap coredns -o json |
    jq --arg corefile "${corefile}" '.data.Corefile = $corefile' |
    kubectl apply -f -
  kubectl -n kube-system rollout restart deployment coredns
  kubectl -n kube-system rollout status deployment coredns --timeout=60s
  kubectl -n "${STUB_NAMESPACE}" delete configmap konnect-coredns-backup --ignore-not-found
}

case "${1:-}" in
install)
  generate_certificates
  apply_stub
  point_cluster_dns_at_stub
  trust_stub_in_operator
  info "konnect-private stub ready"
  ;;
uninstall)
  drop_operator_trust
  unpatch_cluster_dns
  kubectl delete --ignore-not-found -f "${SCRIPT_DIR}/20-konnect-stub.yaml"
  kubectl delete --ignore-not-found -f "${SCRIPT_DIR}/10-konnect-stub-config.yaml"
  kubectl delete --ignore-not-found -f "${SCRIPT_DIR}/00-namespace.yaml"
  info "konnect-private stub removed"
  ;;
*)
  echo "usage: $0 install|uninstall" >&2
  exit 2
  ;;
esac
