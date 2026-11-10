package producepolicychain

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func helperCommand(t *testing.T, script string, env ...string) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(`#!/bin/bash
set -euo pipefail
case "$1" in
  get) printf '%s' "$RESOURCE_JSON";;
  apply)
    test "${APPLY_FAIL:-false}" != true
    cat > "$APPLIED_FILE"
    ;;
  *) exit 99;;
esac
`), 0o600))
	require.NoError(t, os.Chmod(filepath.Join(dir, "kubectl"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(`#!/bin/bash
set -euo pipefail
for arg in "$@"; do
  [[ "$arg" != *"$KONNECT_TOKEN"* ]] || exit 97
done
IFS= read -r header
[[ "$header" == "Authorization: Bearer $KONNECT_TOKEN" ]] || exit 98
while [[ "$#" -gt 0 ]]; do
  if [[ "$1" == -o ]]; then
    printf '%s' "$HTTP_BODY" > "$2"
    break
  fi
  shift
done
printf '%s' "${HTTP_STATUS:-200}"
`), 0o600))
	require.NoError(t, os.Chmod(filepath.Join(dir, "curl"), 0o700))
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"NAMESPACE=probe", "AUTH_SECRET_NAME=probe-auth-secret", "AUTH_NAME=probe-auth",
		"KONNECT_TOKEN=helper-test-token", "KONNECT_URL=invalid.example",
		"MAX_RETRIES=1", "RETRY_COUNT=1", "RETRY_DELAY=0",
		"APPLIED_FILE="+filepath.Join(dir, "applied.json"))
	cmd.Env = append(cmd.Env, env...)
	return cmd
}

func TestAuthenticationRestoration(t *testing.T) {
	for _, tt := range []struct {
		name, status, extra string
		exitCode            int
	}{
		{name: "success preserves success", status: "True"},
		{name: "failure preserves original code", status: "True", extra: "exit 7", exitCode: 7},
		{name: "termination restores auth", status: "True", extra: "trap 'exit 1' TERM; kill -TERM $$", exitCode: 1},
		{name: "failed restoration fails the run", status: "True", extra: "export APPLY_FAIL=true", exitCode: 1},
		{name: "unrecovered auth fails the run", status: "False", exitCode: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := helperCommand(t, `
source scripts/chain.sh
trap restore_auth_secret EXIT
`+tt.extra, `RESOURCE_JSON={"metadata":{"generation":2},"status":{"conditions":[{"type":"APIAuthValid","status":"`+tt.status+`","observedGeneration":2}]}}`)
			output, err := cmd.CombinedOutput()
			if tt.exitCode == 0 {
				require.NoError(t, err, "%s", output)
			} else {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, "%s", output)
				require.Equal(t, tt.exitCode, exit.ExitCode(), "%s", output)
			}
			require.NotContains(t, string(output), "helper-test-token")
		})
	}
}

func TestActualWaiterAndChainConditions(t *testing.T) {
	for _, tt := range []struct {
		name, conditionType, status string
		generation                  int
		ok                          bool
	}{
		{name: "auth without Programmed", conditionType: "APIAuthValid", status: "True", generation: 2, ok: true},
		{name: "false auth", conditionType: "APIAuthValid", status: "False", generation: 2},
		{name: "stale auth", conditionType: "APIAuthValid", status: "True", generation: 1},
		{name: "wrong condition", conditionType: "Programmed", status: "True", generation: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obj := map[string]any{
				"metadata": map[string]any{"generation": 2},
				"status": map[string]any{"conditions": []any{map[string]any{
					"type": tt.conditionType, "status": tt.status, "observedGeneration": tt.generation,
				}}},
			}
			raw, err := json.Marshal(obj)
			require.NoError(t, err)
			cmd := helperCommand(t, `source scripts/chain.sh
CONDITION_TYPE=APIAuthValid wait_resource konnectapiauthconfigurations.konnect.konghq.com probe-auth \
  '.metadata.generation as $g | any(.status.conditions[]?; .type == "APIAuthValid" and .status == "True" and .observedGeneration == $g)'
`, "RESOURCE_JSON="+string(raw))
			output, err := cmd.CombinedOutput()
			require.Equal(t, tt.ok, err == nil, "%s: %v", output, err)
		})
	}
	for _, state := range []string{"ready", "reference", "mismatch", "conflict"} {
		t.Run(state+" requires current Programmed", func(t *testing.T) {
			cmd := helperCommand(t, "source scripts/chain.sh; assert_state "+state+" ReferenceInvalid",
				`RESOURCE_JSON={"metadata":{"generation":2},"status":{"conditions":[{"type":"Programmed","status":"True","observedGeneration":1}]}}`)
			require.Error(t, cmd.Run())
		})
	}
}

func TestActualHTTPHelperCompleteOrder(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		ok         bool
	}{
		{name: "exact complete sequence", body: `{"policies":["parent","child","root"]}`, ok: true},
		{name: "shortened chain", body: `{"policies":["parent","root"]}`},
		{name: "wrong order", body: `{"policies":["root","parent","child"]}`},
		{name: "null", body: `{"policies":null}`},
		{name: "missing", body: `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := helperCommand(t, `source scripts/chain.sh
remote_order '{"path":"/v1/probe/produce-policy-chain","order":["parent","child","root"]}' 1
`, "HTTP_BODY="+tt.body)
			output, err := cmd.CombinedOutput()
			require.Equal(t, tt.ok, err == nil, "%s: %v", output, err)
			require.NotContains(t, string(output), "helper-test-token")
		})
	}
}

func TestObservationMustExceedSyncPeriod(t *testing.T) {
	cmd := helperCommand(t, "bash scripts/chain.sh", "KONNECT_SYNC_PERIOD_SECONDS=60", "WINDOW_SECONDS=60")
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "must exceed the actual operator sync period")
}

func TestCheckPropagatesFailureInsideSnapshotSubshell(t *testing.T) {
	cmd := helperCommand(t, `source scripts/chain.sh
result="$(check '[]' ready)"
echo "unexpected success: $result"
`, `RESOURCE_JSON={"metadata":{"generation":2},"status":{"conditions":[{"type":"Programmed","status":"False","observedGeneration":2}]}}`)
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "does not satisfy")
	require.NotContains(t, string(output), "unexpected success")
}
