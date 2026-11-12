package fixtures

import (
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const testLicense = "dGVzdC1oZWFkZXI.dGVzdC1wYXlsb2Fk.dGVzdC1zaWduYXR1cmU"

func licenseCommand(t *testing.T, script, action, input string, env ...string) (*exec.Cmd, string) {
	t.Helper()
	dir := t.TempDir()
	mock := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >>"$MOCK_DIR/commands"
case "$1" in
  create)
    if [[ "${MOCK_FAIL:-}" == create ]]; then
      cat "$GW_ADMIN_LICENSE_FILE" >&2
      exit 1
    fi
    jq -n --arg ns "$NAMESPACE" --rawfile license "$GW_ADMIN_LICENSE_FILE" \
      '{apiVersion:"v1",kind:"Secret",metadata:{name:"gw-admin-license",namespace:$ns},data:{license:($license|@base64)}}'
    ;;
  apply)
    cat >"$MOCK_DIR/secret.json"
    if [[ "${MOCK_FAIL:-}" == apply ]]; then
      cat "$MOCK_DIR/secret.json" >&2
      exit 1
    fi
    ;;
  get)
    if [[ "$2" == secrets ]]; then
      jq -n --rawfile license "$GW_ADMIN_LICENSE_FILE" \
        '{items:[{metadata:{name:"gw-admin-license",annotations:{"kubectl.kubernetes.io/last-applied-configuration":$license}},data:{license:($license|@base64)},stringData:{license:$license}}]}'
    fi
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(mock), 0o700); err != nil { //nolint:gosec // Executable test stub in t.TempDir.
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-x", script, action)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GW_ADMIN_LICENSE_FILE", "GW_ADMIN_LICENSE_REQUIRED", "NAMESPACE", "KONG_LICENSE_DATA", "MOCK_FAIL", "DIRNAME":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "MOCK_DIR="+dir, "NAMESPACE=test-admin")
	if input != "" {
		file := filepath.Join(dir, "license file.jws")
		if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd.Env = append(cmd.Env, "GW_ADMIN_LICENSE_FILE="+file)
	}
	cmd.Env = append(cmd.Env, env...)
	return cmd, dir
}

func TestGWAdminLicenseInput(t *testing.T) {
	for _, tc := range []struct {
		name, action, input, message string
		env                          []string
	}{
		{name: "missing", action: "check", message: "GW_ADMIN_LICENSE_FILE is required"},
		{name: "required install", action: "install", message: "GW_ADMIN_LICENSE_FILE is required", env: []string{"GW_ADMIN_LICENSE_REQUIRED=true"}},
		{name: "legacy license is not reused", action: "check", message: "GW_ADMIN_LICENSE_FILE is required", env: []string{`KONG_LICENSE_DATA={"license":"legacy"}`}},
		{name: "legacy JSON", action: "check", input: `{"license":"legacy"}`, message: "compact JWS"},
		{name: "multiple licenses", action: "check", input: testLicense + "\n" + testLicense, message: "compact JWS"},
		{name: "leading whitespace", action: "check", input: " " + testLicense, message: "compact JWS"},
		{name: "invalid signature encoding", action: "check", input: "header.payload.@@@", message: "compact JWS"},
		{name: "missing file", action: "check", message: "readable, non-empty regular file", env: []string{"GW_ADMIN_LICENSE_FILE=/nonexistent/gwadmin-license.jws"}},
		{name: "empty file", action: "check", input: "\n", message: "compact JWS"},
		{name: "invalid required flag", action: "check", message: "must be 'true' or 'false'", env: []string{"GW_ADMIN_LICENSE_REQUIRED=yes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, dir := licenseCommand(t, "gwadmin-license.sh", tc.action, tc.input, tc.env...)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.message) {
				t.Fatalf("expected input failure %q, got err=%v output=%s", tc.message, err, output)
			}
			if _, err := os.Stat(filepath.Join(dir, "commands")); !os.IsNotExist(err) {
				t.Fatal("invalid input must fail before contacting Kubernetes")
			}
			if strings.TrimSpace(tc.input) != "" && strings.Contains(string(output), tc.input) {
				t.Fatal("license input leaked to output")
			}
		})
	}
	t.Run("preflight does not need a cluster", func(t *testing.T) {
		cmd, dir := licenseCommand(t, "gwadmin-license.sh", "check", testLicense+"\n")
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), `{"license_input":"checked"}`) {
			t.Fatalf("preflight failed: %v %s", err, output)
		}
		if strings.Contains(string(output), testLicense) {
			t.Fatal("license leaked under shell tracing")
		}
		if _, err := os.Stat(filepath.Join(dir, "commands")); !os.IsNotExist(err) {
			t.Fatal("preflight contacted Kubernetes")
		}
	})
	t.Run("optional local dev-image path does not require credentials", func(t *testing.T) {
		cmd, dir := licenseCommand(t, "gwadmin-license.sh", "install", "")
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), `{"license_input":"not-configured"}`) {
			t.Fatalf("optional install failed: %v %s", err, output)
		}
		if _, err := os.Stat(filepath.Join(dir, "commands")); !os.IsNotExist(err) {
			t.Fatal("optional install contacted Kubernetes")
		}
	})
}

func TestGWAdminLicenseSecret(t *testing.T) {
	for _, failure := range []string{"", "create", "apply"} {
		t.Run("kubectl-"+failure, func(t *testing.T) {
			cmd, dir := licenseCommand(t, "gwadmin-license.sh", "install", testLicense+"\n", "MOCK_FAIL="+failure)
			output, err := cmd.CombinedOutput()
			if strings.Contains(string(output), testLicense) || strings.Contains(string(output), base64.StdEncoding.EncodeToString([]byte(testLicense+"\n"))) {
				t.Fatal("raw or encoded license leaked from kubectl output")
			}
			if failure != "" {
				if err == nil || !strings.Contains(string(output), "could not provision") {
					t.Fatalf("expected explicit kubectl failure, got %v %s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("install failed: %v %s", err, output)
			}
			data, err := os.ReadFile(filepath.Join(dir, "secret.json"))
			if err != nil {
				t.Fatal(err)
			}
			var secret struct {
				Metadata struct {
					Name, Namespace string
					Annotations     map[string]string
					Labels          map[string]string
				}
				Data map[string]string
			}
			if err := json.Unmarshal(data, &secret); err != nil {
				t.Fatal(err)
			}
			if secret.Metadata.Name != "gw-admin-license" || secret.Metadata.Namespace != "test-admin" ||
				len(secret.Data) != 1 || secret.Data["license"] != base64.StdEncoding.EncodeToString([]byte(testLicense+"\n")) ||
				secret.Metadata.Labels["konghq.com/secret"] != "true" ||
				len(secret.Metadata.Annotations) != 0 {
				t.Fatal("Secret must preserve original bytes under the fixture's name/key, in the GWAdmin namespace, without payload-bearing annotations")
			}
			commands, err := os.ReadFile(filepath.Join(dir, "commands"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(commands), "--server-side\n") || !strings.Contains(string(commands), "--from-file=license=") {
				t.Fatal("Secret must use file input and server-side apply")
			}
		})
	}
}

func TestGWAdminLicenseDispatcher(t *testing.T) {
	for _, tc := range []struct {
		suite    string
		required bool
	}{
		{suite: "gwadmin", required: true},
		{suite: "onpremeventgateway", required: true},
		{suite: "eventgateway-cross-namespace", required: true},
		{suite: "eventgateway-cross-namespace"},
		{suite: "gateway", required: true},
	} {
		t.Run(tc.suite+"-required-"+strconv.FormatBool(tc.required), func(t *testing.T) {
			cmd, dir := licenseCommand(t, "run-prereq.sh", "install", "", "DIRNAME="+tc.suite, "GW_ADMIN_LICENSE_REQUIRED="+strconv.FormatBool(tc.required))
			output, err := cmd.CombinedOutput()
			if tc.suite == "gateway" || !tc.required {
				if err != nil || !strings.Contains(string(output), "no suite-specific prerequisites") {
					t.Fatalf("unrelated suite must not require GWAdmin license: %v %s", err, output)
				}
			} else if err == nil || !strings.Contains(string(output), "GW_ADMIN_LICENSE_FILE is required") {
				t.Fatalf("GWAdmin suite must fail early: %v %s", err, output)
			}
			if _, err := os.Stat(filepath.Join(dir, "commands")); !os.IsNotExist(err) {
				t.Fatal("dispatcher must preflight before cluster writes")
			}
		})
	}
}

func TestGWAdminLicenseCrossNamespaceTemplate(t *testing.T) {
	const root = "../eventgateway-cross-namespace"
	data, err := os.ReadFile(filepath.Join(root, "_step_templates", "apply-onpremEventGateway.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var template struct {
		Spec struct {
			Try []struct {
				Script struct {
					Content string
					Env     []struct{ Name, Value string }
				}
				Apply struct {
					Resource struct {
						Kind     string
						Metadata struct{ Namespace string }
						Spec     struct {
							License struct {
								SecretRef struct{ Name, Key string }
							}
						}
					}
				}
			}
		}
	}
	if err := yaml.Unmarshal(data, &template); err != nil {
		t.Fatal(err)
	}
	if len(template.Spec.Try) < 2 {
		t.Fatal("shared onprem template must install the license before creating the GWAdmin")
	}
	script := template.Spec.Try[0].Script
	resource := template.Spec.Try[1].Apply.Resource
	if script.Content != "bash ../../../fixtures/gwadmin-license.sh install" ||
		len(script.Env) != 1 || script.Env[0].Name != "NAMESPACE" || script.Env[0].Value != "($namespace)" ||
		resource.Kind != "GWAdmin" || resource.Metadata.Namespace != "($namespace)" ||
		resource.Spec.License.SecretRef.Name != "gw-admin-license" || resource.Spec.License.SecretRef.Key != "license" {
		t.Fatal("shared template must provision the referenced Secret in the GWAdmin namespace, not a referenced object's namespace")
	}
	for _, gateway := range []string{"onprem", "konnect"} {
		files, err := filepath.Glob(filepath.Join(root, "*", gateway, "chainsaw-test.yaml"))
		if err != nil || len(files) == 0 {
			t.Fatalf("find nested cross-namespace scenarios: %v", err)
		}
		for _, file := range files {
			t.Run(file, func(t *testing.T) {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var test struct {
					Metadata struct{ Labels map[string]string }
					Spec     struct {
						Steps []struct {
							Use struct{ Template string }
						}
					}
				}
				if err := yaml.Unmarshal(data, &test); err != nil {
					t.Fatal(err)
				}
				if test.Metadata.Labels["gateway"] != gateway {
					t.Fatal("scenario must preserve its CI gateway selector")
				}
				usesOnprem := false
				for _, step := range test.Spec.Steps {
					usesOnprem = usesOnprem || step.Use.Template == "../../_step_templates/apply-onpremEventGateway.yaml"
				}
				if usesOnprem != (gateway == "onprem") {
					t.Fatal("only onprem scenarios must reach the shared GWAdmin/license template")
				}
				if gateway != "onprem" {
					return
				}
				cmd, dir := licenseCommand(t, "", "", testLicense, "GW_ADMIN_LICENSE_REQUIRED=true")
				cmd.Args = []string{"bash", "-s"}
				cmd.Stdin = strings.NewReader(script.Content)
				cmd.Dir = filepath.Dir(file)
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("license hook failed from the nested test directory: %v %s", err, output)
				}
				if strings.Contains(string(output), testLicense) {
					t.Fatal("nested template must not render the license")
				}
				data, err = os.ReadFile(filepath.Join(dir, "secret.json"))
				if err != nil {
					t.Fatal(err)
				}
				var secret struct{ Metadata struct{ Namespace string } }
				if err := json.Unmarshal(data, &secret); err != nil {
					t.Fatal(err)
				}
				if secret.Metadata.Namespace != "test-admin" {
					t.Fatal("nested script must preserve the GWAdmin namespace")
				}
			})
		}
	}
}

func TestGWAdminLicenseDebugSnapshot(t *testing.T) {
	cmd, dir := licenseCommand(t, "../common/scripts/debug_snapshot.sh", "", testLicense+"\n", "SKIP_RESOURCE_COUNTS=1", "TEST_NAME=license-redaction")
	outputDir := filepath.Join(dir, "snapshots")
	cmd.Env = append(cmd.Env, "OUTPUT_DIR="+outputDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("snapshot failed: %v %s", err, output)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(testLicense + "\n"))
	assertRedacted := func(data []byte) {
		t.Helper()
		if strings.Contains(string(data), testLicense) || strings.Contains(string(data), encoded) {
			t.Fatal("license payload leaked into snapshot output or artifact")
		}
	}
	assertRedacted(output)
	root, err := os.OpenRoot(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := root.ReadFile(path)
		if err != nil {
			return err
		}
		assertRedacted(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := root.ReadFile(filepath.Join("license-redaction", "resources.yaml"))
	if err != nil || !strings.Contains(string(data), "gw-admin-license") {
		t.Fatalf("snapshot must include redacted Secret metadata: %v", err)
	}
}

func TestGWAdminLicenseFixtureNamespaces(t *testing.T) {
	for _, suite := range []string{"gwadmin", "onpremeventgateway"} {
		files, err := filepath.Glob(filepath.Join("..", suite, "*", "chainsaw-test.yaml"))
		if err != nil || len(files) == 0 {
			t.Fatalf("find suite tests: %v", err)
		}
		for _, file := range files {
			if filepath.Base(filepath.Dir(file)) == "onprem-in-use-orphan" {
				continue // This scenario never creates a GWAdmin.
			}
			t.Run(file, func(t *testing.T) {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var test struct {
					Spec struct {
						Steps []struct {
							Use struct {
								Template string
								With     struct {
									Bindings []struct{ Name, Value string }
								}
							}
						}
					}
				}
				if err := yaml.Unmarshal(data, &test); err != nil {
					t.Fatal(err)
				}
				if len(test.Spec.Steps) == 0 {
					t.Fatal("scenario has no steps")
				}
				use := test.Spec.Steps[0].Use
				if use.Template != "../../common/_step_templates/apply-gwadmin-license.yaml" {
					t.Fatal("GWAdmin fixtures must provision the license before creating a GWAdmin")
				}
				switch filepath.Base(filepath.Dir(file)) {
				case "onprem-cross-namespace", "onprem-grant-revoked":
					if len(use.With.Bindings) != 1 || use.With.Bindings[0].Name != "gwadmin_license_namespace" || use.With.Bindings[0].Value != "($admin_namespace)" {
						t.Fatal("cross-namespace license must go in the admin namespace, not the data-plane namespace")
					}
				default:
					if len(use.With.Bindings) != 0 {
						t.Fatal("same-namespace scenario must use the test namespace")
					}
				}
			})
		}
	}
}
