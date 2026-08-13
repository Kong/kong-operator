#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly SCRIPT_ROOT

WORKFLOWS_DIR="${SCRIPT_ROOT}/.github/workflows"
readonly WORKFLOWS_DIR

YQ_BIN="${YQ_BIN:-yq}"
readonly YQ_BIN

# Pulls every step's `run:` script body out of a workflow file, each one
# preceded by a marker line identifying its job and step name. This
# deliberately excludes `env:`, `with:`, `if:`, `name:`, cache `key:`/`path:`,
# etc. so only the actual shell script text run by the runner is checked.
extract_run_bodies() {
	local file="${1}"

	"${YQ_BIN}" eval '
		(.jobs // {}) as $jobs
		| ($jobs | keys)[] as $job_id
		| ($jobs[$job_id].steps // [])[]
		| select(has("run"))
		| "===STEP:job=" + $job_id + " step=\"" + (.name // "<unnamed>") + "\"===\n" + .run
	' "${file}" 2>/dev/null || true
}

status=0

for file in "${WORKFLOWS_DIR}"/*; do
	[[ -f "${file}" ]] || continue

	current_step=""
	while IFS= read -r line; do
		if [[ "${line}" == ===STEP:*=== ]]; then
			current_step="${line#===STEP:}"
			current_step="${current_step%===}"
			continue
		fi

		if [[ "${line}" == *'${{'* ]]; then
			echo "Script-injection risk: a '\${{ }}' expression is used directly in a run: script."
			echo "  File: ${file#"${SCRIPT_ROOT}"/}"
			echo "  ${current_step}"
			echo "  Line: ${line}"
			echo
			status=1
		fi
	done < <(extract_run_bodies "${file}")
done

if [[ "${status}" -ne 0 ]]; then
	echo "One or more 'run:' steps interpolate a '\${{ ... }}' expression directly into"
	echo "the shell script text. GitHub Actions splices the expression's value into the"
	echo "script before the shell parses it, so values with shell metacharacters can"
	echo "inject arbitrary commands. Fix: move the value into a step- or job-level"
	echo "'env:' block and reference it as a shell variable (e.g. \"\$VAR\") instead."
	echo "See: https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#understanding-the-risk-of-script-injections"
fi

exit "${status}"
