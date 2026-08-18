#!/usr/bin/env bash
# Drive a chat-completions request through an AIGatewayDataPlane and verify the
# gateway processed it: HTTP 200 and the model header present.
#
# The request is issued from a short-lived curl Pod inside the cluster (the
# AIGatewayDataPlane ingress is only reachable in-cluster), targeting the ingress
# Service by DNS. Exits non-zero (failing the chainsaw step) unless a 200 with the
# expected header is observed within the retry budget.
#
# Required env:
#   NAMESPACE     Namespace the AIGatewayDataPlane / ingress Service live in.
#   DP_SVC        Ingress Service name (typically <aigw-dp-name>-ingress).
#   ROUTE_PATH    Route path configured on the model (e.g. /aigw11/chat).
#   MODEL_ALIAS   Model alias to send in the request body.
# Optional env:
#   EXPECT_HEADER Response header proving AI Gateway processed the request.
#                 Default: X-Kong-LLM-Model.
#   EXPECT_SUCCESS
#                 "true" (default): succeed once a 200 with EXPECT_HEADER is seen.
#                 "false": the alias is expected to NOT resolve to a model
#                 (e.g. a value absent from the selector's `values`). Succeeds
#                 once REJECT_CONFIRMATIONS consecutive attempts fail to
#                 produce a 200-with-header, guarding against a transient
#                 blip while the route is still propagating.
#   REJECT_CONFIRMATIONS
#                 Consecutive non-matches required when EXPECT_SUCCESS=false.
#                 Default: 3.
#   PORT          Ingress Service port. Default: 443.
#   CURL_IMAGE    Image for the in-cluster curl Pod. Default: curlimages/curl:latest.
#   MAX_RETRIES   Retry attempts. Default: 60.
#   RETRY_DELAY   Seconds between retries. Default: 5.
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE="${NAMESPACE}"
DP_SVC="${DP_SVC}"
ROUTE_PATH="${ROUTE_PATH}"
MODEL_ALIAS="${MODEL_ALIAS}"
EXPECT_HEADER="${EXPECT_HEADER:-X-Kong-LLM-Model}"
EXPECT_SUCCESS="${EXPECT_SUCCESS:-true}"
REJECT_CONFIRMATIONS="${REJECT_CONFIRMATIONS:-3}"
PORT="${PORT:-443}"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:latest}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-5}"

POD="aigw-traffic-${RANDOM}"
URL="https://${DP_SVC}.${NAMESPACE}.svc:${PORT}${ROUTE_PATH}/chat/completions"
BODY="{\"model\":\"${MODEL_ALIAS}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: Kong AI Gateway works.\"}]}"

cleanup() {
  kubectl -n "${NAMESPACE}" delete pod "${POD}" --ignore-not-found --grace-period=0 --force >/dev/null 2>&1 || true
}
trap cleanup EXIT

kubectl -n "${NAMESPACE}" run "${POD}" \
  --image="${CURL_IMAGE}" --image-pull-policy=IfNotPresent --restart=Never \
  --command -- sleep 3600 >/dev/null
kubectl -n "${NAMESPACE}" wait --for=condition=Ready "pod/${POD}" --timeout=120s >/dev/null

REJECTIONS=0
for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  OUT="$(kubectl -n "${NAMESPACE}" exec "${POD}" -- sh -c \
    "curl -sk -m 60 -o /tmp/b -D /tmp/h -w '%{http_code}' -X POST '${URL}' \
       -H 'X-Kong-LLM-Model: ${MODEL_ALIAS}' \
       -H 'Content-Type: application/json' \
       -d '${BODY}'; echo; \
     grep -i '^${EXPECT_HEADER}:' /tmp/h | head -1 | cut -d' ' -f2- | tr -d '\r'; echo; \
     head -c 500 /tmp/b | tr '\n' ' '" \
    2>/dev/null || true)"
  CODE="$(printf '%s\n' "${OUT}" | sed -n '1p')"
  HDR="$(printf '%s\n' "${OUT}" | sed -n '2p')"
  MATCHED=false
  [ "${CODE}" = "200" ] && [ -n "${HDR}" ] && MATCHED=true

  if [ "${EXPECT_SUCCESS}" = "false" ]; then
    if [ "${MATCHED}" = "false" ]; then
      REJECTIONS=$((REJECTIONS + 1))
      echo "attempt ${ATTEMPT}/${MAX_RETRIES}: rejection ${REJECTIONS}/${REJECT_CONFIRMATIONS} (status='${CODE}')" >&2
      if [ "${REJECTIONS}" -ge "${REJECT_CONFIRMATIONS}" ]; then
        echo "ok: alias consistently unmatched (status='${CODE}')"
        exit 0
      fi
    else
      echo "attempt ${ATTEMPT}/${MAX_RETRIES}: alias unexpectedly matched (status='${CODE}' ${EXPECT_HEADER}=${HDR})" >&2
      REJECTIONS=0
    fi
  else
    if [ "${MATCHED}" = "true" ]; then
      echo "ok: status=${CODE} ${EXPECT_HEADER}=${HDR}"
      exit 0
    fi
    echo "attempt ${ATTEMPT}/${MAX_RETRIES}: status='${CODE}' header='${HDR}'" >&2
  fi
  sleep "${RETRY_DELAY}"
done

if [ "${EXPECT_SUCCESS}" = "false" ]; then
  echo "FAILED: alias matched a model after ${MAX_RETRIES} attempts, expected it to stay unmatched" >&2
else
  echo "FAILED: no 200 with ${EXPECT_HEADER} after ${MAX_RETRIES} attempts (last status='${CODE}')" >&2
fi
exit 1
