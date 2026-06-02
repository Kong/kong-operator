#!/usr/bin/env bash
# Generate a self-signed CA and wildcard TLS certificate for SNI routing,
# then create or replace the Kubernetes TLS Secret and a ConfigMap holding
# the CA certificate (base64-encoded) for use by test pods.
#
# The wildcard cert covers *.{LB-IP-dashes}.sslip.io, which sslip.io resolves
# to the LoadBalancer IP. This means clients can reach
#   bootstrap-{dns_label}.{LB-IP-dashes}.sslip.io:19092
# and the SNI hostname is used by keg to route to the correct virtual cluster.
#
# Required env:
#   LB_IP         LoadBalancer external IP address.
#   NAMESPACE     Kubernetes namespace for the Secret and ConfigMap.
#   SECRET_NAME   Name of the Kubernetes TLS Secret to create/replace.
# Optional env:
#   RETRY_DELAY   Not used; script fails fast on any error.
set -o errexit
set -o nounset
set -o pipefail

LB_IP="${LB_IP}"
NAMESPACE="${NAMESPACE}"
SECRET_NAME="${SECRET_NAME}"

# Derive the sslip.io-based SNI suffix from the LB IP.
# e.g. 10.96.1.5 -> .10-96-1-5.sslip.io
IP_DASHES="${LB_IP//./-}"
SNI_SUFFIX=".${IP_DASHES}.sslip.io"
WILDCARD_CN="*.${IP_DASHES}.sslip.io"

WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' EXIT
pushd "${WORKDIR}" >/dev/null

# Root CA.
openssl genrsa -out rootCA.key 4096 2>/dev/null
openssl req -x509 -new -nodes -key rootCA.key \
  -sha256 -days 3650 \
  -subj "/C=US/ST=Local/L=Local/O=Dev CA/CN=Dev Root CA" \
  -out rootCA.crt 2>/dev/null

# Gateway TLS key + CSR.
openssl genrsa -out tls.key 2048 2>/dev/null
openssl req -new -key tls.key \
  -subj "/C=US/ST=Local/L=Local/O=Dev/CN=${WILDCARD_CN}" \
  -out tls.csr 2>/dev/null

# SAN extension (required for modern TLS clients).
cat > tls.ext <<EOF
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
authorityKeyIdentifier = keyid,issuer

[alt_names]
DNS.1 = ${WILDCARD_CN}
EOF

# Sign with the CA.
openssl x509 -req -in tls.csr \
  -CA rootCA.crt -CAkey rootCA.key -CAcreateserial \
  -out tls.crt -days 825 -sha256 \
  -extfile tls.ext 2>/dev/null

# Create/replace TLS Secret.
# The label konghq.com/secret=true is required so the operator's Secret
# informer cache (filtered by --secret-label-selector) picks up this Secret.
kubectl create secret tls "${SECRET_NAME}" \
  --cert=tls.crt --key=tls.key \
  -n "${NAMESPACE}" \
  --dry-run=client -o yaml | \
kubectl label -f - "konghq.com/secret=true" --local -o yaml | \
kubectl apply -f - >/dev/null 2>&1

# Create/replace ConfigMap holding the CA cert for test pods.
CA_CM_NAME="${SECRET_NAME}-ca"
CA_CERT=$(cat rootCA.crt)
kubectl create configmap "${CA_CM_NAME}" \
  --from-literal=ca.crt="${CA_CERT}" \
  -n "${NAMESPACE}" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1

CA_CERT_B64=$(base64 < rootCA.crt | tr -d '\n')
popd >/dev/null

cat <<EOF
{
  "success": true,
  "sni_suffix": "${SNI_SUFFIX}",
  "wildcard_cn": "${WILDCARD_CN}",
  "ca_cert_b64": "${CA_CERT_B64}",
  "secret_name": "${SECRET_NAME}",
  "ca_cm_name": "${CA_CM_NAME}"
}
EOF
