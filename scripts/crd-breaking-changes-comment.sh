#!/bin/bash
# Prints the pull-request comment for the CRD breaking-change check.
#
# Usage: scripts/crd-breaking-changes-comment.sh [REPORT_FILE]
#
# REPORT_FILE is the report that verify-crd-breaking-changes.sh writes when
# CRD_BREAKING_CHANGES_REPORT is set. A missing or empty report means that the check found no
# breaking changes. To see an example, write a report by hand:
#
#   printf '### `example.yaml`\n\n```\n- v1alpha1 - ^.spec.foo - type - ERROR - type changed : "string" -> "integer"\n```\n\n' > /tmp/report.md
#   scripts/crd-breaking-changes-comment.sh /tmp/report.md

set -o errexit
set -o nounset
set -o pipefail

REPORT_FILE="${1:-${CRD_BREAKING_CHANGES_REPORT:-}}"
readonly REPORT_FILE

ACK_CRD_BREAKING_CHANGE="${ACK_CRD_BREAKING_CHANGE:-false}"
readonly ACK_CRD_BREAKING_CHANGE

ACK_CRD_BREAKING_CHANGE_LABEL="${ACK_CRD_BREAKING_CHANGE_LABEL:-ack_crd_breaking_change}"
readonly ACK_CRD_BREAKING_CHANGE_LABEL

BASE_REF="${CRD_BREAKING_CHANGES_BASE_REF:-main}"
readonly BASE_REF

# Full SHA of the commit under test, so that an edited comment says which push it describes.
COMMIT_SHA="${CRD_BREAKING_CHANGES_COMMIT_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
readonly COMMIT_SHA

# Link to the workflow run, which holds the full output of the check. Empty outside GitHub Actions.
RUN_URL=""
if [[ -n "${GITHUB_RUN_ID:-}" ]]; then
	RUN_URL="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}"
fi
readonly RUN_URL

# First line of the body. The workflow uses it to find the comment that this job owns, so the
# job edits that comment instead of adding a new one on every run.
MARKER="<!-- crd-breaking-changes -->"
readonly MARKER

echo "${MARKER}"

if [[ -z "${REPORT_FILE}" || ! -s "${REPORT_FILE}" ]]; then
	echo "## No breaking CRD changes"
	echo
	echo "This pull request has no breaking CRD changes against \`${BASE_REF}\` at commit ${COMMIT_SHA}."
	exit 0
fi

echo "## Breaking CRD changes"
echo
if [[ "${ACK_CRD_BREAKING_CHANGE}" == "true" ]]; then
	echo "The label \`${ACK_CRD_BREAKING_CHANGE_LABEL}\` acknowledges these changes (SHA ${COMMIT_SHA}), so they do not block this pull request."
else
	echo "Fix these changes (SHA ${COMMIT_SHA}), or add the \`${ACK_CRD_BREAKING_CHANGE_LABEL}\` label to acknowledge them."
fi
if [[ -n "${RUN_URL}" ]]; then
	echo
	echo "Full output: ${RUN_URL}"
fi
echo

# GitHub rejects a comment body over 65536 characters, so a large report is cut at a line
# boundary. The cut list has whole lines only, and an unclosed code fence would break rendering.
MAX_REPORT_BYTES=60000
readonly MAX_REPORT_BYTES

if [[ "$(wc -c < "${REPORT_FILE}")" -le "${MAX_REPORT_BYTES}" ]]; then
	cat "${REPORT_FILE}"
else
	list="$(awk -v max="${MAX_REPORT_BYTES}" '{ n += length($0) + 1; if (n > max) exit; print }' "${REPORT_FILE}")"
	printf '%s\n' "${list}"
	fences="$(printf '%s\n' "${list}" | grep -c '^```' || true)"
	if [[ $(( fences % 2 )) -eq 1 ]]; then
		echo '```'
	fi
	echo
	echo "_The list is cut short to fit the comment length limit._"
fi
