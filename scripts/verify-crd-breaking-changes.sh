#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly SCRIPT_ROOT

RELATIVE_CRD_DIR="config/crd/kong-operator"
readonly RELATIVE_CRD_DIR

CURRENT_CRD_DIR="${SCRIPT_ROOT}/${RELATIVE_CRD_DIR}"
readonly CURRENT_CRD_DIR

# Optional markdown report with only the breaking changes, used to build the pull-request comment.
CRD_BREAKING_CHANGES_REPORT="${CRD_BREAKING_CHANGES_REPORT:-}"
readonly CRD_BREAKING_CHANGES_REPORT

# Keeps only the breaking changes of crdify output. It drops WARNING findings, which are not
# breaking changes. The console keeps the full output. A finding line looks like:
#   - v1alpha1 - ^.spec.foo - type - ERROR - type changed : "string" -> "integer"
# crdify cannot classify a change to a CEL validation rule, so it reports the finding as
# `unhandled` and prints the struct diff on the indented lines that follow. Those lines show the
# actual change, so they are kept with their finding. Without them the comment only says that
# unhandled changes exist, which does not tell the reviewer what changed.
breaking_changes_only() {
	awk '
		/^- [^[:space:]]+ - / {
			keep = ($0 ~ / - ERROR - /)
			if (keep) {
				print
			}
			next
		}
		keep && $0 != "" {
			print
			next
		}
		{ keep = 0 }
	'
}

report_breaking_change() {
	local basename="${1}"
	local details="${2}"

	if [[ -z "${CRD_BREAKING_CHANGES_REPORT}" ]]; then
		return 0
	fi

	if [[ -z "${details}" ]]; then
		details="The check failed without details."
	fi

	{
		printf '### `%s`\n\n' "${basename}"
		printf '```\n%s\n```\n\n' "${details}"
	} >> "${CRD_BREAKING_CHANGES_REPORT}"
}

CONFIG_FILE="${SCRIPT_ROOT}/.crdify.yaml"
readonly CONFIG_FILE

is_crd_basename() {
	local basename="${1}"

	case "${basename}" in
		kustomization.yaml)
			return 1
			;;
		*.yaml)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

collect_crd_basenames() {
	local basename

	while IFS= read -r basename; do
		[[ -n "${basename}" ]] || continue
		if is_crd_basename "${basename}"; then
			printf '%s\n' "${basename}"
		fi
	done | LC_ALL=C sort
}

CRDIFY_BIN="${CRDIFY_BIN:-}"
if [[ -z "${CRDIFY_BIN}" ]]; then
	echo "CRDIFY_BIN must be set"
	exit 1
fi

ACK_CRD_BREAKING_CHANGE="${ACK_CRD_BREAKING_CHANGE:-false}"
readonly ACK_CRD_BREAKING_CHANGE

ACK_CRD_BREAKING_CHANGE_LABEL="${ACK_CRD_BREAKING_CHANGE_LABEL:-ack_crd_breaking_change}"
readonly ACK_CRD_BREAKING_CHANGE_LABEL

case "${ACK_CRD_BREAKING_CHANGE}" in
	true|false)
		;;
	*)
		echo "ACK_CRD_BREAKING_CHANGE must be true or false"
		exit 1
		;;
esac

resolve_base_revision() {
	local candidate

	if [[ -n "${CRD_BREAKING_CHANGES_BASE_SHA:-}" ]]; then
		candidate="${CRD_BREAKING_CHANGES_BASE_SHA}"
		if git rev-parse --verify "${candidate}^{commit}" >/dev/null 2>&1; then
			printf '%s\n' "${candidate}"
			return 0
		fi
		echo "Failed to resolve CRD_BREAKING_CHANGES_BASE_SHA=${candidate}" >&2
		return 1
	fi

	for candidate in \
		"${CRD_BREAKING_CHANGES_BASE_REF:-}" \
		"${GITHUB_BASE_REF:+origin/${GITHUB_BASE_REF}}" \
		origin/main \
		main \
		HEAD^
	do
		if [[ -z "${candidate}" ]]; then
			continue
		fi
		if git rev-parse --verify "${candidate}^{commit}" >/dev/null 2>&1; then
			printf '%s\n' "${candidate}"
			return 0
		fi
	done

	echo "Failed to resolve a base revision for CRD compatibility checks." >&2
	echo "Set CRD_BREAKING_CHANGES_BASE_SHA or CRD_BREAKING_CHANGES_BASE_REF to override the default." >&2
	return 1
}

base_revision="$(resolve_base_revision)"
echo "Comparing CRDs in ${RELATIVE_CRD_DIR} against ${base_revision}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

current_basenames="${tmpdir}/current_basenames.txt"
base_basenames="${tmpdir}/base_basenames.txt"
union_basenames="${tmpdir}/union_basenames.txt"

find "${CURRENT_CRD_DIR}" -maxdepth 1 -type f -name '*.yaml' -print \
	| sed 's#.*/##' \
	| collect_crd_basenames > "${current_basenames}"

(
	git ls-tree -r --name-only "${base_revision}" -- "${RELATIVE_CRD_DIR}" \
		| sed 's#.*/##' \
		| collect_crd_basenames
) > "${base_basenames}" || true

cat "${current_basenames}" "${base_basenames}" | sed '/^$/d' | LC_ALL=C sort -u > "${union_basenames}"

if [[ ! -s "${union_basenames}" ]]; then
	echo "No CRDs were found in ${RELATIVE_CRD_DIR} to compare."
	exit 1
fi

# The path can hold a report from an earlier run, so empty it here: report_breaking_change only
# appends, and a second run with the same path would add to the first run's findings.
if [[ -n "${CRD_BREAKING_CHANGES_REPORT}" ]]; then
	: > "${CRD_BREAKING_CHANGES_REPORT}"
fi

status=0
while IFS= read -r basename; do
	[[ -n "${basename}" ]] || continue

	current_file="${CURRENT_CRD_DIR}/${basename}"
	base_file="${RELATIVE_CRD_DIR}/${basename}"

	if ! git cat-file -e "${base_revision}:${base_file}" 2>/dev/null; then
		echo "Skipping new CRD ${basename}"
		continue
	fi

	if [[ ! -f "${current_file}" ]]; then
		echo "Breaking change detected: ${basename} was removed from ${RELATIVE_CRD_DIR}."
		report_breaking_change "${basename}" "CRD was removed from ${RELATIVE_CRD_DIR}."
		status=1
		continue
	fi

	old_file="${tmpdir}/${basename}"
	git show "${base_revision}:${base_file}" > "${old_file}"

	echo "Checking ${basename}"
	crdify_output=""
	if ! crdify_output="$("${CRDIFY_BIN}" --config "${CONFIG_FILE}" "file://${old_file}" "file://${current_file}")"; then
		status=1
		report_breaking_change "${basename}" "$(printf '%s\n' "${crdify_output}" | breaking_changes_only)"
	fi
	if [[ -n "${crdify_output}" ]]; then
		printf '%s\n' "${crdify_output}"
	fi
done < "${union_basenames}"

if [[ "${status}" -ne 0 && "${ACK_CRD_BREAKING_CHANGE}" == "true" ]]; then
	echo "::warning title=CRD breaking changes acknowledged::Breaking CRD changes were detected, but label '${ACK_CRD_BREAKING_CHANGE_LABEL}' acknowledges them, so this job will not block merging."

	if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
		{
			echo "### CRD breaking changes acknowledged"
			echo
			echo "Breaking CRD changes were detected, but label \`${ACK_CRD_BREAKING_CHANGE_LABEL}\` acknowledges them, so this job will not block merging."
		} >> "${GITHUB_STEP_SUMMARY}"
	fi

	exit 0
fi

exit "${status}"
