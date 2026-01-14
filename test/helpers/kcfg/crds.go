package kcfg

import (
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/samber/lo"
	"golang.org/x/mod/modfile"
)

const gatewayAPIModule = "sigs.k8s.io/gateway-api"

var (
	cfgPath = filepath.Join(projectRootPath(), "config")
	crdPath = filepath.Join(cfgPath, "crd")

	rbacBase           = filepath.Join(cfgPath, "rbac", "base")
	rbacRole           = filepath.Join(cfgPath, "rbac", "role")
	validatingPolicies = filepath.Join(cfgPath, "default", "validating_policies")

	chartPath = path.Join(projectRootPath(), "charts/kong-operator")

	gatewayAPIPackageVersion = lo.Must(extractModuleVersion(gatewayAPIModule))
	gatewayAPIModulePath     = constructModulePath(gatewayAPIModule, gatewayAPIPackageVersion)
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

func GatewayAPIConformanceTestsFilesystemsWithManifests() []fs.FS {
	return []fs.FS{os.DirFS(filepath.Join(gatewayAPIModulePath, "conformance"))}
}

// extractModuleVersion extracts version of an imported module in go.mod.
// If the module is not found, or the module version can't be parsed, it returns an error.
func extractModuleVersion(moduleName string) (string, error) {
	const moduleFile = "go.mod"
	content, err := os.ReadFile(filepath.Join(projectRootPath(), moduleFile))
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

// projectRootPath returns the root directory of this project.
func projectRootPath() string {
	_, b, _, _ := runtime.Caller(0) //nolint:dogsled

	// Returns root directory of this project.
	// NOTE: it depends on the path of this file itself. When the file is moved, the second param may need updating.
	return filepath.Join(filepath.Dir(b), "../../..")
}
