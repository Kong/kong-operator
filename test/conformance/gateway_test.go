//go:build conformance_tests
// +build conformance_tests

package conformance

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
)

const (
	showDebug                  = true
	shouldCleanup              = true
	enableAllSupportedFeatures = true
)

func TestGatewayConformance(t *testing.T) {
	t.Parallel()

	t.Log("creating GatewayClass for gateway conformance tests")
	gwc := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
		},
	}
	require.NoError(t, clients.MgrClient.Create(ctx, gwc))
	defer func() {
		require.NoError(t, clients.MgrClient.Delete(ctx, gwc))
	}()

	// There are no explicit conformance tests for GatewayClass, but we can
	// still run the conformance test suite setup to ensure that the
	// GatewayClass gets accepted.
	t.Log("starting the gateway conformance test suite")
	conformanceTestsBaseManifests := fmt.Sprintf("%s/conformance/base/manifests.yaml", testutils.GatewayRawRepoURL)

	cSuite := suite.New(suite.Options{
		Client:                     clients.MgrClient,
		GatewayClassName:           gwc.Name,
		Debug:                      showDebug,
		CleanupBaseResources:       shouldCleanup,
		BaseManifests:              conformanceTestsBaseManifests,
		EnableAllSupportedFeatures: enableAllSupportedFeatures,
	})
	cSuite.Setup(t)
}
