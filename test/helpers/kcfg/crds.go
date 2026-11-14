package kcfg

import (
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"golang.org/x/mod/modfile"

	"github.com/kong/kong-operator/v2/test/helpers"
)

const (
	gatewayAPIModule            = "sigs.k8s.io/gateway-api"
	gatewayAPIConformanceModule = "sigs.k8s.io/gateway-api/conformance"
)

var (
	cfgPath = filepath.Join(helpers.ProjectRootPath(), "config")
	crdPath = filepath.Join(cfgPath, "crd")

	rbacBase           = filepath.Join(cfgPath, "rbac", "base")
	rbacRole           = filepath.Join(cfgPath, "rbac", "role")
	validatingPolicies = filepath.Join(cfgPath, "default", "validating_policies")

	chartPath = path.Join(helpers.ProjectRootPath(), "charts/kong-operator")

	gatewayAPIPackageVersion = lo.Must(gatewayAPIVersion())
	gatewayAPIModulePath     = constructModulePath(gatewayAPIModule, gatewayAPIPackageVersion)

	gatewayAPIConformancePackageVersion = lo.Must(extractModuleVersion(gatewayAPIConformanceModule))
	gatewayAPIConformanceModulePath     = constructModulePath(gatewayAPIConformanceModule, gatewayAPIConformancePackageVersion)
)

func ChartPath() string {
	return chartPath
}

func KongOperatorCRDsPath() string {
	return filepath.Join(crdPath, "kong-operator")
}

func ValidatingWebhookPath() string {
	return filepath.Join(cfgPath, "default", "validating_webhook")
}

func ValidatingPoliciesPath() string {
	return validatingPolicies
}

func IngressControllerIncubatorCRDsPath() string {
	return filepath.Join(crdPath, "ingress-controller-incubator")
}

func GatewayAPIExperimentalCRDsPath() string {
	return filepath.Join(gatewayAPIModulePath, "config", "crd", "experimental")
}

func GatewayAPIStandardCRDsPath() string {
	return filepath.Join(gatewayAPIModulePath, "config", "crd", "standard")
}

// envGatewayAPIVersionOverride allows tests to run against Gateway API CRDs of
// a version other than the one pinned in go.mod (compatibility testing).
// The module of that version must be
// present in the module cache (e.g. `go mod download module@version` or the
// `ensure.go.pkg.downloaded.gateway-api` make target with GATEWAY_API_VERSION
// set to the same version).
const envGatewayAPIVersionOverride = "GATEWAY_API_VERSION"

// gatewayAPIVersion returns the Gateway API version the test cluster CRDs are
// installed from: the GATEWAY_API_VERSION env if set, otherwise the version
// pinned in go.mod.
func gatewayAPIVersion() (string, error) {
	if v := os.Getenv(envGatewayAPIVersionOverride); v != "" {
		if !strings.HasPrefix(v, "v") {
			return "", fmt.Errorf("%s must start with 'v', got %q", envGatewayAPIVersionOverride, v)
		}
		return v, nil
	}
	return extractModuleVersion(gatewayAPIModule)
}

// envGatewayAPICRDsChannel selects the Gateway API CRD channel tests install
// on the cluster: "experimental" (default) or "standard". The standard
// channel has no TLSRoute/TCPRoute/UDPRoute CRDs.
const envGatewayAPICRDsChannel = "TEST_GATEWAY_API_CRDS_CHANNEL"

// GatewayAPICRDsPath returns the path of the Gateway API CRDs to install in
// test clusters, based on TEST_GATEWAY_API_CRDS_CHANNEL (default: experimental).
func GatewayAPICRDsPath() string {
	switch os.Getenv(envGatewayAPICRDsChannel) {
	case "standard":
		return GatewayAPIStandardCRDsPath()
	default:
		return GatewayAPIExperimentalCRDsPath()
	}
}

func GatewayAPIConformanceTestsFilesystemsWithManifests() []fs.FS {
	return []fs.FS{os.DirFS(gatewayAPIConformanceModulePath)}
}

// extractModuleVersion extracts version of an imported module in go.mod.
// If the module is not found, or the module version can't be parsed, it returns an error.
func extractModuleVersion(moduleName string) (string, error) {
	const moduleFile = "go.mod"
	content, err := os.ReadFile(filepath.Join(helpers.ProjectRootPath(), moduleFile))
	if err != nil {
		return "", err
	}
	f, err := modfile.Parse(moduleFile, content, nil)
	if err != nil {
		return "", err
	}
	module, found := lo.Find(f.Require, func(r *modfile.Require) bool {
		return r.Mod.Path == moduleName
	})
	if !found {
		return "", fmt.Errorf("module %s not found", moduleName)
	}
	return module.Mod.Version, nil
}

// constructModulePath constructs the module path for the given module name and version.
// It accounts for v1+ modules which are stored in separate directories in the GOPATH.
func constructModulePath(moduleName, version string) string {
	modulePath := filepath.Join(build.Default.GOPATH, "pkg", "mod")
	modulePath = filepath.Join(append([]string{modulePath}, strings.Split(moduleName, "/")...)...)
	modulePath += "@" + version
	return modulePath
}
