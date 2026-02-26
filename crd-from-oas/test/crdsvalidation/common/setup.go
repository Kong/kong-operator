package common

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/kong/kong-operator/v2/test/envtest"
)

func Setup(
	t *testing.T,
	scheme *runtime.Scheme,
) (*rest.Config, *corev1.Namespace) {
	return envtest.Setup(t, t.Context(), scheme,
		envtest.WithInstallGatewayCRDs(false),
		envtest.WithInstallKongCRDs(false),
		// TODO: make this not relative
		envtest.WithAdditionalCRDPaths([]string{"../../../config/crd/"}),
	)
}
