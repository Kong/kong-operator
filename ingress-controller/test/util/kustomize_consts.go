package util

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"github.com/samber/lo"
)

const (
	// kubernetesConfigurationModulePath is the kubernetes-configuration path used by the kustomize.
	// It's different than the path used by Go mod related functions as these do change
	// based on the major version of the module used whereas this one doesn't.
	kubernetesConfigurationModulePath = "github.com/kong/kubernetes-configuration"
	// ingressControllerModulePath points to Kubernetes configuration for KGO, since KIC part
	// only needs a subset of it, so a dedicated one is not needed.
	ingressControllerLocalPath = "./"
)

var (
	kongRBACsKustomize = initKongRBACsKustomizePath()

	kubernetesConfigurationModuleVersion = "" // unused; local paths are used instead of remote module refs
	kongCRDsKustomize                    = initKongConfigurationCRDs()
	kongIncubatorCRDsKustomize           = initKongIncubatorCRDsKustomizePath()
	kongKOCRDsKustomize                  = initKongOperatorConfigurationCRDs()
)

func initKongIncubatorCRDsKustomizePath() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), ingressControllerLocalPath+"config/crd/ingress-controller-incubator")
	ensureDirExists(dir)
	return dir
}

func initKongRBACsKustomizePath() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), ingressControllerLocalPath+"config/rbac/")
	ensureDirExists(dir)
	return dir
}

func initKongConfigurationCRDs() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), ingressControllerLocalPath+"config/crd/ingress-controller")
	ensureDirExists(dir)
	return dir
}

func initKongOperatorConfigurationCRDs() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), "config/crd/gateway-operator")
	ensureDirExists(dir)
	return dir
}

func ensureDirExists(dir string) {
	fi, err := os.Stat(dir)
	if err != nil {
		panic(err)
	}
	if !fi.IsDir() {
		panic(fmt.Errorf("%s is not a directory", dir))
	}
}

func getRepoRoot() (string, error) {
	_, b, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get repo root: runtime.Caller(0) failed")
	}
	d := filepath.Dir(path.Join(path.Dir(b), "../..")) // Number of ../ depends on the path of this file.
	return filepath.Abs(d)
}
