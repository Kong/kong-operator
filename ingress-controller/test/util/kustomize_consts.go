package util

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"github.com/samber/lo"

	"github.com/kong/kong-operator/ingress-controller/test/consts"
)

const (
	// kubernetesConfigurationModulePath is the kubernetes-configuration path used by the kustomize.
	// It's different than the path used by Go mod related functions as these do change
	// based on the major version of the module used whereas this one doesn't.
	kubernetesConfigurationModulePath = "github.com/kong/kubernetes-configuration"
	ingressControllerLocalPath        = "./ingress-controller/"
)

var (
	kongRBACsKustomize        = initKongRBACsKustomizePath()
	kongGatewayRBACsKustomize = initKongGatewayRBACsKustomizePath()
	kongCRDsRBACsKustomize    = initKongCRDsRBACsKustomizePath()

	kubernetesConfigurationModuleVersion = lo.Must(DependencyModuleVersionGit(consts.KubernetesConfigurationModulePath))
	kongCRDsKustomize                    = initKongConfigurationCRDs()
	kongIncubatorCRDsKustomize           = initKongIncubatorCRDsKustomizePath()
)

func initKongIncubatorCRDsKustomizePath() string {
	return fmt.Sprintf("%s/config/crd/ingress-controller-incubator?ref=%s", kubernetesConfigurationModulePath, kubernetesConfigurationModuleVersion)
}

func initKongRBACsKustomizePath() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), ingressControllerLocalPath+"config/rbac/")
	ensureDirExists(dir)
	return dir
}

func initKongGatewayRBACsKustomizePath() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), ingressControllerLocalPath+"config/rbac/gateway")
	ensureDirExists(dir)
	return dir
}

func initKongCRDsRBACsKustomizePath() string {
	dir := filepath.Join(lo.Must(getRepoRoot()), ingressControllerLocalPath+"config/rbac/crds")
	ensureDirExists(dir)
	return dir
}

func initKongConfigurationCRDs() string {
	return fmt.Sprintf("%s/config/crd/ingress-controller?ref=%s", kubernetesConfigurationModulePath, kubernetesConfigurationModuleVersion)
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
