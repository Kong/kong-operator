package util

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"github.com/samber/lo"

	"github.com/kong/kong-operator/v2/ingress-controller/test/consts"
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

	kubernetesConfigurationModuleVersion = lo.Must(DependencyModuleVersionGit(consts.KubernetesConfigurationModulePath))
	kongCRDsKustomize                    = initKongConfigurationCRDs()
	kongIncubatorCRDsKustomize           = initKongIncubatorCRDsKustomizePath()
	kongKOCRDsKustomize                  = initKongOperatorConfigurationCRDs()
)

func initKongIncubatorCRDsKustomizePath() string {
	return fmt.Sprintf("%s/config/crd/ingress-controller-incubator?ref=%s", kubernetesConfigurationModulePath, kubernetesConfigurationModuleVersion)
}

func initKongRBACsKustomizePath() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), ingressControllerLocalPath+"config/rbac/")
	ensureDirExists(dir)
	return dir
}

func initKongConfigurationCRDs() string {
	return fmt.Sprintf("%s/config/crd/ingress-controller?ref=%s", kubernetesConfigurationModulePath, kubernetesConfigurationModuleVersion)
}

func initKongOperatorConfigurationCRDs() string {
	return fmt.Sprintf("%s/config/crd/gateway-operator?ref=%s", kubernetesConfigurationModulePath, kubernetesConfigurationModuleVersion)
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
