package fixtures

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

const signingScript = "../../../../.github/actions/gwadmin-license/issue-license.sh"

func signingCommand(t *testing.T, env ...string) (*exec.Cmd, string) {
	t.Helper()
	dir := t.TempDir()
	mock := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"$MOCK_DIR/curl-args"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) config="$2"; shift ;;
    --data-binary) request="${2#@}"; shift ;;
    --output) output="$2"; shift ;;
  esac
  shift
done
cp "$config" "$MOCK_DIR/auth"
cp "$request" "$MOCK_DIR/request"
ls -ld "$(dirname "$config")" "$config" "$request" >"$MOCK_DIR/permissions"
if [[ "${MOCK_CURL_FAIL:-false}" == true ]]; then
  cat "$config" "$request" >&2
  printf '%s' "$MOCK_RESPONSE" >&2
  exit 1
fi
printf '%s' "$MOCK_RESPONSE" >"$output"
printf '%s' "${MOCK_STATUS:-200}"
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(mock), 0o700); err != nil { //nolint:gosec // Executable test stub in t.TempDir.
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "output"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-x", signingScript, "issue")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GW_ADMIN_LICENSE_") || strings.HasPrefix(key, "MOCK_") || key == "RUNNER_TEMP" || key == "GITHUB_OUTPUT" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env,
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"MOCK_DIR="+dir, "RUNNER_TEMP="+dir, "GITHUB_OUTPUT="+filepath.Join(dir, "output"),
		"GW_ADMIN_LICENSE_SIGNING_USERNAME=mock-user",
		"GW_ADMIN_LICENSE_SIGNING_PASSWORD=mock-\"password\\with\nnewline",
		"GW_ADMIN_LICENSE_CUSTOMER=mock-\"customer\\with\nnewline",
		"GW_ADMIN_LICENSE_KEY=mock-\"key\\with\nnewline",
		"GW_ADMIN_LICENSE_EXPIRATION_DATE=2099-12-31",
		"MOCK_RESPONSE="+testLicense+"\n",
	)
	cmd.Env = append(cmd.Env, env...)
	return cmd, dir
}

func assertSigningOutputSafe(t *testing.T, output []byte) {
	t.Helper()
	for _, value := range []string{
		testLicense, base64.StdEncoding.EncodeToString([]byte(testLicense + "\n")),
		"mock-user", "mock-\"password", "mock-\"customer", "mock-\"key",
		base64.StdEncoding.EncodeToString([]byte("mock-user:mock-\"password\\with\nnewline")),
	} {
		if strings.Contains(string(output), value) {
			t.Fatal("mock license or signing material leaked into logs/output")
		}
	}
}

func TestGWAdminLicenseSigningInputs(t *testing.T) {
	for _, tc := range []struct{ env, message string }{
		{"GW_ADMIN_LICENSE_SIGNING_USERNAME=", "missing required signing username"},
		{"GW_ADMIN_LICENSE_SIGNING_PASSWORD=", "missing required signing password"},
		{"GW_ADMIN_LICENSE_CUSTOMER= \n", "missing required customer"},
		{"GW_ADMIN_LICENSE_KEY=", "missing required license-key"},
		{"GW_ADMIN_LICENSE_EXPIRATION_DATE=", "missing required expiration-date"},
		{"GW_ADMIN_LICENSE_EXPIRATION_DATE=2099-02-30", "future UTC date"},
		{"GW_ADMIN_LICENSE_EXPIRATION_DATE=2000-01-01", "future UTC date"},
		{"GW_ADMIN_LICENSE_EXPIRATION_DATE=2099-12-31T00:00:00Z", "future UTC date"},
		{"GITHUB_OUTPUT=/nonexistent/action-output", "writable action output file"},
	} {
		t.Run(strings.Split(tc.env, "=")[0]+"-"+tc.message, func(t *testing.T) {
			cmd, dir := signingCommand(t, tc.env)
			output, err := cmd.CombinedOutput()
			assertSigningOutputSafe(t, output)
			if err == nil || !strings.Contains(string(output), tc.message) {
				t.Fatalf("expected explicit input error %q, got %v %s", tc.message, err, output)
			}
			if _, err := os.Stat(filepath.Join(dir, "curl-args")); !os.IsNotExist(err) {
				t.Fatal("invalid inputs must never invoke signing")
			}
			if _, err := os.Stat(filepath.Join(dir, "gwadmin-license")); !os.IsNotExist(err) {
				t.Fatal("invalid inputs must not create license files")
			}
		})
	}
}

func TestGWAdminLicenseSigningRequest(t *testing.T) {
	cmd, dir := signingCommand(t)
	output, err := cmd.CombinedOutput()
	assertSigningOutputSafe(t, output)
	if err != nil {
		t.Fatalf("mock signing failed: %v %s", err, output)
	}
	request, err := os.ReadFile(filepath.Join(dir, "request"))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Customer       string   `json:"customer"`
		LicenseKey     string   `json:"license_key"`
		ExpirationDate string   `json:"license_expiration_date"`
		Entitlements   []string `json:"entitlements"`
	}
	if err := json.Unmarshal(request, &body); err != nil {
		t.Fatal(err)
	}
	if body.Customer != "mock-\"customer\\with\nnewline" || body.LicenseKey != "mock-\"key\\with\nnewline" ||
		body.ExpirationDate != "2099-12-31" || len(body.Entitlements) != 1 || body.Entitlements[0] != "event_gateway" {
		t.Fatal("signing request must preserve explicit JSON inputs and request only event_gateway")
	}
	auth, err := os.ReadFile(filepath.Join(dir, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	header, err := strconv.Unquote(strings.TrimSpace(strings.TrimPrefix(string(auth), "header = ")))
	if err != nil {
		t.Fatal(err)
	}
	if header != "Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("mock-user:mock-\"password\\with\nnewline")) {
		t.Fatal("signing request must use Basic auth with the supplied username/password")
	}
	args, err := os.ReadFile(filepath.Join(dir, "curl-args"))
	if err != nil {
		t.Fatal(err)
	}
	assertSigningOutputSafe(t, args)
	if !strings.HasPrefix(string(args), "--disable\n") ||
		!strings.Contains(string(args), "--request\nPOST\n") ||
		!strings.Contains(string(args), "--proto\n=https\n") ||
		strings.Contains(string(args), "--location") ||
		strings.Contains(string(args), "--insecure") ||
		!strings.Contains(string(args), "--max-time\n60\n") ||
		!strings.Contains(string(args), "https://license.api.konghq.tech/v1/license/enveloped") {
		t.Fatal("signing must use a bounded HTTPS POST to the confirmed endpoint, without default curl config, redirects or insecure TLS")
	}
	permissions, err := os.ReadFile(filepath.Join(dir, "permissions"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(permissions)), "\n") {
		if !strings.HasPrefix(line, "drwx------") && !strings.HasPrefix(line, "-rw-------") {
			t.Fatal("signing directory/request/auth files must be owner-only")
		}
	}
	path := filepath.Join(dir, "gwadmin-license", "license.jws")
	license, err := os.ReadFile(path)
	if err != nil || string(license) != testLicense+"\n" {
		t.Fatalf("issued file must preserve original JWS bytes: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("issued file must be 0600: %v", err)
	}
	actionOutput, err := os.ReadFile(filepath.Join(dir, "output"))
	if err != nil || string(actionOutput) != "license-file="+path+"\n" {
		t.Fatalf("action output must contain only the private file path: %v", err)
	}
	for _, file := range []string{"curl.conf", "request.json", "response"} {
		if _, err := os.Stat(filepath.Join(dir, "gwadmin-license", file)); !os.IsNotExist(err) {
			t.Fatal("temporary signing material must be removed after success")
		}
	}
	cleanup := exec.Command("bash", "-x", signingScript, "cleanup")
	cleanup.Env = cmd.Env
	output, err = cleanup.CombinedOutput()
	assertSigningOutputSafe(t, output)
	if err != nil {
		t.Fatalf("cleanup failed: %v %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(dir, "gwadmin-license")); !os.IsNotExist(err) {
		t.Fatal("workflow cleanup must remove the license and its directory")
	}
}

func TestGWAdminLicenseSigningFailures(t *testing.T) {
	for _, tc := range []struct{ env, message string }{
		{"MOCK_CURL_FAIL=true", "transport failed"},
		{"MOCK_STATUS=401", "HTTP 401"},
		{"MOCK_STATUS=403", "HTTP 403"},
		{"MOCK_STATUS=429", "HTTP 429"},
		{"MOCK_STATUS=500", "HTTP 500"},
		{"MOCK_STATUS=" + testLicense, "invalid HTTP status"},
		{"MOCK_RESPONSE=", "did not return a compact JWS"},
		{`MOCK_RESPONSE={"license":"legacy"}`, "did not return a compact JWS"},
		{"MOCK_RESPONSE=" + testLicense + "\n" + testLicense, "did not return a compact JWS"},
	} {
		t.Run(tc.message, func(t *testing.T) {
			cmd, dir := signingCommand(t, tc.env)
			output, err := cmd.CombinedOutput()
			assertSigningOutputSafe(t, output)
			if err == nil || !strings.Contains(string(output), tc.message) {
				t.Fatalf("expected signing failure %q, got %v %s", tc.message, err, output)
			}
			actionOutput, err := os.ReadFile(filepath.Join(dir, "output"))
			if err != nil || len(actionOutput) != 0 {
				t.Fatal("failed signing must not publish a success output")
			}
			if _, err := os.Stat(filepath.Join(dir, "gwadmin-license")); !os.IsNotExist(err) {
				t.Fatal("failed signing must remove credentials/request/response/license")
			}
		})
	}
}

func TestGWAdminLicenseSigningHTTP(t *testing.T) {
	realCurl, err := exec.LookPath("curl")
	if err != nil {
		t.Fatal(err)
	}
	type receivedRequest struct {
		method, path, username, password string
		body                             map[string]any
		err                              error
	}
	requests := make(chan receivedRequest, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, _ := r.BasicAuth()
		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		requests <- receivedRequest{r.Method, r.URL.Path, username, password, body, err}
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte(testLicense)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	cmd, dir := signingCommand(t)
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	// Only the test shim replaces the production endpoint with the local TLS
	// server. The action itself never accepts a credentials-bearing URL override.
	shim := `#!/usr/bin/env bash
set -euo pipefail
args=()
translated=false
for arg in "$@"; do
  if [[ "$arg" == https://license.api.konghq.tech/v1/license/enveloped ]]; then
    args+=("$MOCK_SERVER_URL/v1/license/enveloped")
    translated=true
  else
    args+=("$arg")
  fi
done
[[ "$translated" == true ]] || exit 1
exec "$REAL_CURL" "${args[@]}" --cacert "$MOCK_CA"
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(shim), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(cmd.Env, "REAL_CURL="+realCurl, "MOCK_SERVER_URL="+server.URL, "MOCK_CA="+ca,
		"HTTPS_PROXY=", "https_proxy=", "ALL_PROXY=", "all_proxy=", "NO_PROXY=127.0.0.1,localhost")
	output, err := cmd.CombinedOutput()
	assertSigningOutputSafe(t, output)
	if err != nil {
		t.Fatalf("local TLS signing request failed: %v %s", err, output)
	}
	select {
	case request := <-requests:
		if request.err != nil || request.method != http.MethodPost || request.path != "/v1/license/enveloped" ||
			request.username != "mock-user" || request.password != "mock-\"password\\with\nnewline" ||
			request.body["customer"] != "mock-\"customer\\with\nnewline" ||
			request.body["license_key"] != "mock-\"key\\with\nnewline" ||
			request.body["license_expiration_date"] != "2099-12-31" {
			t.Fatalf("unexpected local signing request: decode err=%v", request.err)
		}
		entitlements, ok := request.body["entitlements"].([]any)
		if !ok || len(entitlements) != 1 || entitlements[0] != "event_gateway" {
			t.Fatal("HTTP request must carry the event_gateway entitlement")
		}
	default:
		t.Fatal("real curl did not reach the local TLS mock")
	}
}

func TestGWAdminLicenseSigningWorkflow(t *testing.T) {
	data, err := os.ReadFile("../../../../.github/workflows/__e2e_chainsaw_tests.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Env   map[string]string
			Steps []struct {
				ID, Name, Uses, If, Run string
				Env, With               map[string]string
				ContinueOnError         bool `json:"continue-on-error"`
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	job := workflow.Jobs["e2e-tests-chainsaw"]
	required := job.Env["GW_ADMIN_LICENSE_REQUIRED"]
	if !strings.Contains(required, "matrix.suite == 'gwadmin' || matrix.suite == 'onpremeventgateway'") ||
		!strings.Contains(required, "&& 'true' || 'false'") {
		t.Fatal("CI must require the license only for GWAdmin-producing suites")
	}
	expiration, issuer, prerequisites, tests, cleanup, download := -1, -1, -1, -1, -1, -1
	var expirationDate string
	for i, step := range job.Steps {
		switch {
		case step.ID == "gwadmin-license-expiration":
			expiration = i
			if !strings.Contains(step.If, "matrix.suite == 'gwadmin' || matrix.suite == 'onpremeventgateway'") || step.ContinueOnError {
				t.Fatal("calculate the required expiration only for GWAdmin-producing suites")
			}
			for _, timezone := range []string{"Pacific/Kiritimati", "Etc/GMT+12"} {
				t.Run("UTC-date-in-"+timezone, func(t *testing.T) {
					outputFile := filepath.Join(t.TempDir(), "date-output")
					if err := os.WriteFile(outputFile, nil, 0o600); err != nil {
						t.Fatal(err)
					}
					before := time.Now().UTC().AddDate(0, 0, 7).Format(time.DateOnly)
					cmd := exec.Command("bash", "-s")
					cmd.Stdin = strings.NewReader(step.Run)
					cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TZ=" + timezone, "GITHUB_OUTPUT=" + outputFile}
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("CI date computation failed: %v %s", err, output)
					}
					after := time.Now().UTC().AddDate(0, 0, 7).Format(time.DateOnly)
					data, err := os.ReadFile(outputFile)
					if err != nil {
						t.Fatal(err)
					}
					date := strings.TrimSuffix(strings.TrimPrefix(string(data), "date="), "\n")
					if string(data) != "date="+date+"\n" || (date != before && date != after) {
						t.Fatal("CI expiration must be exactly seven days after the UTC date, independent of runner timezone")
					}
					expirationDate = date
				})
			}
		case step.ID == "gwadmin-license":
			issuer = i
			if step.Uses != "./.github/actions/gwadmin-license" || !strings.Contains(step.If, "matrix.suite == 'gwadmin' || matrix.suite == 'onpremeventgateway'") ||
				strings.Contains(step.If, "fork") || step.ContinueOnError || len(step.With) != 5 {
				t.Fatal("issuer must run for both GWAdmin suites, with all explicit inputs and no fork-specific skip")
			}
			if step.With["expiration-date"] != "${{ steps.gwadmin-license-expiration.outputs.date }}" {
				t.Fatal("signing must receive the automatically computed date, not a repository variable")
			}
		case step.Name == "Apply chainsaw suite prerequisites":
			prerequisites = i
			if step.Env["GW_ADMIN_LICENSE_FILE"] != "${{ steps.gwadmin-license.outputs.license-file }}" {
				t.Fatal("suite preflight must receive the issued file")
			}
		case step.ID == "chainsaw-tests":
			tests = i
			if step.Env["GW_ADMIN_LICENSE_FILE"] != "${{ steps.gwadmin-license.outputs.license-file }}" {
				t.Fatal("per-namespace fixtures must receive the issued file")
			}
		case step.Name == "Remove GWAdmin license and signing files":
			cleanup = i
			if !strings.Contains(step.If, "always()") || step.Run != "bash .github/actions/gwadmin-license/issue-license.sh cleanup" {
				t.Fatal("license/signing material must be cleaned up on failure and cancellation too")
			}
		case strings.HasPrefix(step.Uses, "actions/download-artifact@"):
			download = i
		}
	}
	if expiration < 0 || issuer <= expiration || download <= issuer || prerequisites <= issuer || tests <= prerequisites || cleanup <= tests {
		t.Fatal("license signing must fail before expensive setup, reach both test paths, and always be cleaned up afterward")
	}
	if expirationDate == "" {
		t.Fatal("CI must produce an expiration date")
	}
	cmd, dir := signingCommand(t, "GW_ADMIN_LICENSE_EXPIRATION_DATE="+expirationDate)
	output, err := cmd.CombinedOutput()
	assertSigningOutputSafe(t, output)
	if err != nil {
		t.Fatalf("signing with CI date failed: %v %s", err, output)
	}
	request, err := os.ReadFile(filepath.Join(dir, "request"))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		ExpirationDate string `json:"license_expiration_date"`
	}
	if err := json.Unmarshal(request, &body); err != nil {
		t.Fatal(err)
	}
	if body.ExpirationDate != expirationDate {
		t.Fatal("license_expiration_date must preserve the CI-calculated seven-day date")
	}
}
