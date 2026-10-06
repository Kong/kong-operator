#!/bin/bash
# List every entity of a Konnect collection, following its pagination, and,
# optionally, retry until the list matches a jq condition. A collection
# returning a plain array is not paginated and is listed as is. Otherwise its
# pages are followed through meta.page.next, a next-page URI or the bare
# cursor depending on the API (cursor pagination), or
# meta.page.number, size and total (page-number pagination), and their items
# combined.
#
# Prints {"status": 200, "body": {"data": [<all items>]} (or [<items>] for a
# collection returning a plain array), "retry_attempt": <n>} on success, like
# konnect_api_request.sh, so the same jq expressions work on either output.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   KONNECT_TOKEN: Bearer token for the Konnect API.
#   KONNECT_URL: Konnect server host, e.g. us.api.konghq.tech.
#   API_PATH: Path of the entity collection, e.g. '/v1/event-gateways'.
#   EXPECTED_JQ: (optional) jq condition the combined list must satisfy. It
#     can reference environment variables via $ENV.
#   RETRY_COUNT: (optional) Number of attempts. Default: 120.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.
#   MAX_PAGES: (optional) Pages listed at most, to stop on a next page that
#     never ends. Default: 100.

KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_URL="${KONNECT_URL}"
API_PATH="${API_PATH}"
EXPECTED_JQ="${EXPECTED_JQ:-}"
RETRY_COUNT="${RETRY_COUNT:-120}"
RETRY_DELAY="${RETRY_DELAY:-1}"
MAX_PAGES="${MAX_PAGES:-100}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() {
  jq -n --arg error "$1" --arg path "${API_PATH}" '{error: $error, path: $path}'
  exit 1
}

# get PATH: prints the response body of a single GET, or fails.
get() {
  local response
  if ! response="$(METHOD=GET API_PATH="$1" RETRY_COUNT=1 BODY='' EXPECTED_STATUS='' EXPECTED_JQ='' \
    bash "${SCRIPT_DIR}/konnect_api_request.sh")"; then
    echo "${response}" >&2
    return 1
  fi
  jq -c '.body' <<<"${response}"
}

# with_page_query PATH QUERY: PATH, without its page parameters, with QUERY
# appended to its query string.
with_page_query() {
  local path
  path="$(sed -E 's/([?&])page(%5B|\[)[a-z]+(%5D|\])=[^&]*/\1/g; s/&&+/\&/g; s/[?&]+$//; s/\?&/?/' <<<"$1")"
  if [[ "${path}" == *"?"* ]]; then
    echo "${path}&$2"
  else
    echo "${path}?$2"
  fi
}

# page_items BODY: prints the items of a list page, or fails when BODY is not
# a list page, so that an unexpected response never passes for an empty list.
page_items() {
  if ! jq -ec 'if type == "object" and (.data | type) == "array" then .data else error end' <<<"$1" 2>/dev/null; then
    echo "unexpected list response: $1" >&2
    return 1
  fi
}

# list_all: prints the combined list of the collection, or fails. The first
# page is requested without page parameters, which a collection that is not
# paginated may reject; the next ones with those the first page reports.
list_all() {
  local path body items page next number size total pages=1
  # Paths of the pages requested, one per line, to detect a repeated page.
  local seen=""
  body="$(get "${API_PATH}")" || return 1
  if jq -e 'type == "array"' <<<"${body}" >/dev/null; then
    echo "${body}"
    return 0
  fi
  items="$(page_items "${body}")" || return 1
  while true; do
    next="$(jq -r '.meta.page.next // .meta.next // empty' <<<"${body}")"
    number="$(jq -r '.meta.page.number // empty' <<<"${body}")"
    if [[ -n "${next}" ]]; then
      # A next page URI has a query; a bare cursor may start with "/" (base64).
      if [[ "${next}" == *"?"* || "${next}" == *"://"* ]]; then
        # meta.page.next is a URI (e.g. Event Gateway APIs): keep its path and
        # query.
        if [[ "${next}" == *"://"* ]]; then
          next="/${next#*://*/}"
        fi
        path="${next}"
      else
        # meta.page.next is the bare cursor (e.g. AI Gateway sub-collections).
        size="$(jq -r '.meta.page.size // .meta.size // empty' <<<"${body}")"
        path="$(with_page_query "${API_PATH}" "page%5Bafter%5D=$(jq -rn --arg c "${next}" '$c | @uri')${size:+&page%5Bsize%5D=${size}}")"
      fi
    elif [[ -n "${number}" ]]; then
      size="$(jq -r '.meta.page.size // 0' <<<"${body}")"
      total="$(jq -r '.meta.page.total // 0' <<<"${body}")"
      if [[ "$(jq '.data // [] | length' <<<"${body}")" == 0 ]] ||
        ! jq -en --argjson n "${number}" --argjson s "${size}" --argjson t "${total}" '$s > 0 and $n * $s < $t' >/dev/null; then
        break
      fi
      path="$(with_page_query "${API_PATH}" "page%5Bsize%5D=${size}&page%5Bnumber%5D=$((number + 1))")"
    else
      break
    fi
    if grep -qxF -- "${path}" <<<"${seen}"; then
      echo "next page ${path} repeated" >&2
      return 1
    fi
    seen="${seen}${path}"$'\n'
    pages=$((pages + 1))
    if [[ "${pages}" -gt "${MAX_PAGES}" ]]; then
      echo "more than ${MAX_PAGES} pages" >&2
      return 1
    fi
    body="$(get "${path}")" || return 1
    page="$(page_items "${body}")" || return 1
    items="$(jq -c --argjson page "${page}" '. + $page' <<<"${items}")" || return 1
  done
  jq -c '{data: .}' <<<"${items}"
}

LAST_ERROR=""
for ATTEMPT in $(seq 1 "${RETRY_COUNT}"); do
  if ! LIST="$(list_all 2>&1)"; then
    LAST_ERROR="failed listing: ${LIST}"
  elif [[ -n "${EXPECTED_JQ}" ]] && ! jq -e "${EXPECTED_JQ}" <<<"${LIST}" >/dev/null 2>&1; then
    LAST_ERROR="list does not satisfy: ${EXPECTED_JQ}"
  else
    jq -n --argjson body "${LIST}" --argjson attempt "${ATTEMPT}" \
      '{status: 200, body: $body, retry_attempt: $attempt}'
    exit 0
  fi
  if [[ "${ATTEMPT}" -lt "${RETRY_COUNT}" ]]; then
    sleep "${RETRY_DELAY}"
  fi
done
fail "${LAST_ERROR}"
