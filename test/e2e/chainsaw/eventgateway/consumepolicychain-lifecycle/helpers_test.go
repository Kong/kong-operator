package consumepolicychain

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func helperCommand(t *testing.T, script string, env ...string) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"kubectl": `#!/bin/bash
set -euo pipefail
echo "$*" >> "$CALLS"
case "$1" in
  get)
    test "${GET_FAIL:-false}" != true || { echo "kubectl unavailable" >&2; exit 9; }
    if [[ "$2" == eventgatewayvirtualclusters.* ]]; then
      printf '%s' "${VC_JSON:-$RESOURCE_JSON}"
    else
      printf '%s' "$RESOURCE_JSON"
    fi ;;
  apply) cat > "$APPLIED_FILE"; test "${APPLY_FAIL:-false}" != true ;;
  delete|wait|patch) test "${MUTATION_FAIL:-false}" != true ;;
  *) exit 99 ;;
esac
`,
		"curl": `#!/bin/bash
set -euo pipefail
url=""
for arg in "$@"; do
  [[ "$arg" != *"$KONNECT_TOKEN"* ]] || exit 97
  url="$arg"
done
IFS= read -r header
[[ "$header" == "Authorization: Bearer $KONNECT_TOKEN" ]] || exit 98
echo "$*" >> "$CALLS"
test "${CURL_FAIL:-false}" != true || { echo "curl unavailable" >&2; exit 7; }
body="$HTTP_BODY"
if [[ "$url" == */parent-id && -n "${PARENT_BODY:-}" ]]; then body="$PARENT_BODY"; fi
while [[ "$#" -gt 0 ]]; do
  if [[ "$1" == -o ]]; then printf '%s' "$body" > "$2"; break; fi
  shift
done
printf '%s' "${HTTP_STATUS:-200}"
`,
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		require.NoError(t, os.Chmod(path, 0o700))
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"NAMESPACE=probe", "AUTH_SECRET_NAME=probe-auth-secret", "AUTH_NAME=probe-auth",
		"GATEWAY_NAME=probe-gateway", "TEST_NAME=probe", "VIRTUAL_CLUSTER_NAME=vc",
		"KONNECT_TOKEN=helper-test-token", "KONNECT_URL=invalid.example",
		"MAX_RETRIES=1", "RETRY_COUNT=1", "RETRY_DELAY=0",
		"CALLS="+filepath.Join(dir, "calls"), "APPLIED_FILE="+filepath.Join(dir, "applied.json"))
	cmd.Env = append(cmd.Env, env...)
	return cmd
}

func TestConsumeActualHTTPOrderAndFailures(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		env        []string
		ok         bool
	}{
		{name: "exact nested order", body: `{"policies":["parent","child","root"]}`, ok: true},
		{name: "wrong order", body: `{"policies":["root","parent","child"]}`},
		{name: "shortened set", body: `{"policies":["parent","root"]}`},
		{name: "duplicate", body: `{"policies":["parent","parent","root"]}`},
		{name: "null", body: `{"policies":null}`},
		{name: "missing", body: `{}`},
		{name: "invalid JSON", body: `not JSON`},
		{name: "HTTP error", body: `{"message":"backend unavailable"}`, env: []string{"HTTP_STATUS=503"}},
		{name: "command failure", body: `{}`, env: []string{"CURL_FAIL=true"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := append([]string{"HTTP_BODY=" + tt.body}, tt.env...)
			cmd := helperCommand(t, `source scripts/helpers.sh
remote_order '{"path":"/v1/probe/consume-policy-chain","order":["parent","child","root"]}'
`, env...)
			output, err := cmd.CombinedOutput()
			require.Equal(t, tt.ok, err == nil, "%s: %v", output, err)
			require.NotContains(t, string(output), "helper-test-token")
		})
	}
}

func TestConsumeCheckAndIdentityFailuresPropagate(t *testing.T) {
	for _, script := range []string{
		`result="$(check '[]' ready)"; echo "unexpected success: $result"`,
		`result="$(snapshot '["child"]' false)"; echo "unexpected success: $result"`,
		`result="$(snapshot 'null' false)"; echo "unexpected success: $result"`,
	} {
		cmd := helperCommand(t, "source scripts/helpers.sh\n"+script,
			`RESOURCE_JSON={"metadata":{"generation":2},"status":{"conditions":[{"type":"Programmed","status":"False","observedGeneration":2}]}}`,
			`VC_JSON={"metadata":{"uid":"vc-uid"},"status":{"id":"vc-id","gatewayID":{"id":"gateway-id"}}}`)
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "%s", output)
		require.NotContains(t, string(output), "unexpected success")
	}
	cmd := helperCommand(t, `source scripts/helpers.sh
result="$(snapshot '[]' false)"
jq -e '.order == [] and .policies == {}' <<<"$result"
`, `VC_JSON={"metadata":{"uid":"vc-uid"},"status":{"id":"vc-id","gatewayID":{"id":"gateway-id"}}}`)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestConsumeSnapshotRejectsForeignScope(t *testing.T) {
	for _, status := range []string{
		`"virtualClusterID":{"id":"foreign-vc"},"gatewayID":{"id":"gateway-id"}`,
		`"virtualClusterID":{"id":"vc-id"},"gatewayID":{"id":"foreign-gateway"}`,
	} {
		cmd := helperCommand(t, `source scripts/helpers.sh
result="$(snapshot '["child"]' false)"
echo "unexpected success: $result"
`, `VC_JSON={"metadata":{"uid":"vc-uid"},"status":{"id":"vc-id","gatewayID":{"id":"gateway-id"}}}`,
			`RESOURCE_JSON={"metadata":{"uid":"child-uid"},"spec":{"apiSpec":{"type":"skipRecord"}},"status":{"id":"child-id",`+status+`}}`)
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "%s", output)
		require.Contains(t, string(output), "invalid policy identity")
		require.NotContains(t, string(output), "unexpected success")
	}
}

func TestConsumeActualParentIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, parent, root string
		ok                 bool
	}{
		{name: "exact parent", parent: "parent-id", root: `{"id":"parent-id","type":"schema_validation"}`, ok: true},
		{name: "wrong parent", parent: "foreign-id", root: `{}`},
		{name: "missing child parent", root: `{}`},
		{name: "malformed root parent", parent: "parent-id", root: `{"id":"parent-id","type":"schema_validation","parent_policy_id":false}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := helperCommand(t, `source scripts/helpers.sh
assert_identities '{
  "vc":{"id":"vc-id","uid":"vc-uid","collection_path":"/v1/event-gateways/gateway-id/virtual-clusters",
        "entity_path":"/v1/event-gateways/gateway-id/virtual-clusters/vc-id"},
  "policies":{
    "parent":{"id":"parent-id","uid":"parent-uid","parent":"","type":"schemaValidation","path":"/v1/event-gateways/gateway-id/virtual-clusters/vc-id/consume-policies/parent-id"},
    "child":{"id":"child-id","uid":"child-uid","parent":"parent","type":"decryptFields","path":"/v1/event-gateways/gateway-id/virtual-clusters/vc-id/consume-policies/child-id"}
  }}'
`, `RESOURCE_JSON={}`, "PARENT_BODY="+tt.root,
				"HTTP_BODY="+`{"id":"child-id","type":"decrypt_fields","parent_policy_id":"`+tt.parent+`"}`)
			// Supply both actual Kubernetes objects without replacing the identity predicate.
			shim := filepath.Join(t.TempDir(), "kubectl")
			require.NoError(t, os.WriteFile(shim, []byte(`#!/bin/bash
set -euo pipefail
case "$2/$3" in
  eventgatewayvirtualclusters.*/vc)
    echo '{"metadata":{"uid":"vc-uid"},"status":{"id":"vc-id","gatewayID":{"id":"gateway-id"}}}' ;;
  */child)
    echo '{"metadata":{"uid":"child-uid"},"spec":{"apiSpec":{"type":"decryptFields","parentPolicyRef":{"name":"parent"}}},"status":{"id":"child-id","virtualClusterID":{"id":"vc-id"},"gatewayID":{"id":"gateway-id"}}}' ;;
  */parent)
    echo '{"metadata":{"uid":"parent-uid"},"spec":{"apiSpec":{"type":"schemaValidation"}},"status":{"id":"parent-id","virtualClusterID":{"id":"vc-id"},"gatewayID":{"id":"gateway-id"}}}' ;;
  *) exit 99 ;;
esac
`), 0o600))
			require.NoError(t, os.Chmod(shim, 0o700))
			for i, value := range cmd.Env {
				if len(value) >= 5 && value[:5] == "PATH=" {
					cmd.Env[i] = "PATH=" + filepath.Dir(shim) + string(os.PathListSeparator) + value[5:]
				}
			}
			output, err := cmd.CombinedOutput()
			if tt.ok {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err, "%s", output)
				require.Contains(t, string(output), "does not satisfy")
			}
		})
	}
}

func TestConsumeRestorationAndTermination(t *testing.T) {
	for _, tt := range []struct {
		name, extra, status string
		code                int
	}{
		{name: "success", status: "True"},
		{name: "preserve failure", status: "True", extra: "exit 7", code: 7},
		{name: "handled termination", status: "True", extra: "trap 'exit 1' TERM; kill -TERM $$", code: 1},
		{name: "restore apply fails", status: "True", extra: "export APPLY_FAIL=true", code: 1},
		{name: "auth remains invalid", status: "False", code: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := helperCommand(t, "source scripts/helpers.sh\ntrap restore_auth_secret EXIT\n"+tt.extra,
				`RESOURCE_JSON={"metadata":{"generation":2},"status":{"conditions":[{"type":"APIAuthValid","status":"`+tt.status+`","observedGeneration":2}]}}`)
			output, err := cmd.CombinedOutput()
			if tt.code == 0 {
				require.NoError(t, err, "%s", output)
			} else {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, "%s", output)
				require.Equal(t, tt.code, exit.ExitCode(), "%s", output)
			}
			require.NotContains(t, string(output), "helper-test-token")
		})
	}
}

func TestConsumeObservationAndCommandFailures(t *testing.T) {
	for _, script := range []string{
		`WINDOW_SECONDS=60 source scripts/helpers.sh`,
		`WINDOW_SECONDS=invalid source scripts/helpers.sh`,
		`source scripts/helpers.sh; create_policy child modifyHeaders`,
		`source scripts/helpers.sh; delete_resource "$POLICY" child`,
		`source scripts/helpers.sh; wait_policy child`,
	} {
		cmd := helperCommand(t, script, "APPLY_FAIL=true", "MUTATION_FAIL=true", "GET_FAIL=true", "RESOURCE_JSON={}")
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "%s", output)
	}
}

func TestConsumeCleanupRejectsMalformedInventory(t *testing.T) {
	cmd := helperCommand(t, `bash scripts/cleanup.sh`, `HTTP_BODY={"data":null}`)
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "%s", output)
	require.Contains(t, string(output), "unexpected list response")
	require.NotContains(t, string(output), "helper-test-token")
}

func TestConsumeHoldNeverRetriesAwayOrderMismatch(t *testing.T) {
	cmd := helperCommand(t, `source scripts/helpers.sh
if hold '{"path":"/v1/probe/consume-policy-chain","order":["parent","child"]}' ready; then
  echo "unexpected success"
  exit 1
fi
test "$(grep -c 'https://invalid.example' "$CALLS")" == 1
`, "WINDOW_SECONDS=2", "KONNECT_SYNC_PERIOD_SECONDS=1", "CHECK_INTERVAL=1",
		`HTTP_BODY={"policies":["child","parent"]}`,
		`RESOURCE_JSON={"metadata":{"generation":2},"status":{"conditions":[
{"type":"EventGatewayVirtualClusterRefValid","status":"True","observedGeneration":2},
{"type":"APIAuthResolvedRef","status":"True","observedGeneration":2},
{"type":"APIAuthValid","status":"True","observedGeneration":2},
{"type":"KonnectReferencesResolved","status":"True","observedGeneration":2},
{"type":"PolicyChainOwnerValid","status":"True","observedGeneration":2},
{"type":"Programmed","status":"True","observedGeneration":2}]}}`)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "does not satisfy")
	require.NotContains(t, string(output), "unexpected success")
}

func TestConsumeCleanupScopeAndChildFirstOrder(t *testing.T) {
	for _, mode := range []string{"success", "remote-delete-fails"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			curl := filepath.Join(dir, "curl")
			require.NoError(t, os.WriteFile(curl, []byte(`#!/bin/bash
set -euo pipefail
IFS= read -r header
[[ "$header" == "Authorization: Bearer $KONNECT_TOKEN" ]]
method=GET
out=""
url=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -X) method="$2"; shift ;;
    -o) out="$2"; shift ;;
    https://*) url="$1" ;;
  esac
  shift
done
echo "$method $url" >> "$CALLS"
body='{"data":[]}'
status=200
case "$method $url" in
  "GET https://invalid.example/v1/event-gateways")
    if [[ -f "$REMOTE_ABSENT" ]]; then
      body='{"data":[{"id":"other","name":"probe-gateway","labels":{"test":"another-test"}}]}'
    else
      body='{"data":[{"id":"owned","name":"probe-gateway","labels":{"test":"probe"}},{"id":"other","name":"probe-gateway","labels":{"test":"another-test"}},{"id":"near","name":"other-gateway","labels":{"test":"probe"}}]}'
    fi ;;
  "GET https://invalid.example/v1/event-gateways/owned/virtual-clusters")
    body='{"data":[{"id":"vc"}]}' ;;
  "GET https://invalid.example/v1/event-gateways/owned/virtual-clusters/vc/consume-policies")
    body='{"data":[{"id":"parent","parent_policy_id":null},{"id":"child","parent_policy_id":"parent"}]}' ;;
  "DELETE https://invalid.example/v1/event-gateways/owned")
    if [[ "$CLEANUP_MODE" == remote-delete-fails ]]; then
      status=503
      body='{"message":"cannot delete dedicated gateway"}'
    else
      touch "$REMOTE_ABSENT"
      status=204
    fi ;;
  "GET https://invalid.example/v1/event-gateways/owned") status=404 ;;
  "DELETE "*) status=204 ;;
esac
printf '%s' "$body" > "$out"
printf '%s' "$status"
`), 0o600))
			require.NoError(t, os.Chmod(curl, 0o700))
			cmd := helperCommand(t, `bash scripts/cleanup.sh`,
				"CLEANUP_MODE="+mode, "REMOTE_ABSENT="+filepath.Join(dir, "absent"), "RESOURCE_JSON=")
			for i, value := range cmd.Env {
				if len(value) >= 5 && value[:5] == "PATH=" {
					cmd.Env[i] = "PATH=" + dir + string(os.PathListSeparator) + value[5:]
				}
			}
			output, err := cmd.CombinedOutput()
			if mode == "success" {
				require.NoError(t, err, "%s", output)
				require.Contains(t, string(output), "gateway absent remotely")
			} else {
				require.Error(t, err, "%s", output)
				require.Contains(t, string(output), "HTTP status 503")
			}
			var callsPath string
			for _, value := range cmd.Env {
				if len(value) >= 6 && value[:6] == "CALLS=" {
					callsPath = value[6:]
				}
			}
			calls, err := os.ReadFile(callsPath)
			require.NoError(t, err)
			require.NotContains(t, string(calls), "/event-gateways/other")
			require.NotContains(t, string(calls), "/event-gateways/near")
			require.Regexp(t, `(?s)DELETE [^\n]+/consume-policies/child\nDELETE [^\n]+/consume-policies/parent`, string(calls))
			require.NotContains(t, string(calls), "finalizers", "no finalizer release is allowed before verified remote absence")
			require.NotContains(t, string(output), "helper-test-token")
		})
	}
}
