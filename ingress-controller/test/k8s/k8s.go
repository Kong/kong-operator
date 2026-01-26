package k8s

import (
	"k8s.io/client-go/rest"

	internal "github.com/kong/kong-operator/ingress-controller/internal/k8s"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
)

func GetKubeconfig(c managercfg.Config) (*rest.Config, error) {
	return internal.GetKubeconfig(c)
}
