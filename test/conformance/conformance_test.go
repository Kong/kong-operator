package conformance

import (
	"path"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance"
	conformancev1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"

	"github.com/kong/gateway-operator/internal/metadata"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
)

var skippedTests = []string{
	// gateway
	tests.GatewayInvalidTLSConfiguration.ShortName,
	tests.GatewayModifyListeners.ShortName,
	// TODO: https://github.com/Kong/gateway-operator/issues/56
	tests.GatewayWithAttachedRoutes.ShortName,

	// httproute
	tests.HTTPRouteHeaderMatching.ShortName,
	tests.HTTPRouteHTTPSListener.ShortName,
	tests.HTTPRouteHostnameIntersection.ShortName,
	tests.HTTPRouteInvalidBackendRefUnknownKind.ShortName,
	tests.HTTPRouteListenerHostnameMatching.ShortName,
	tests.HTTPRouteObservedGenerationBump.ShortName,
}

func TestGatewayConformance(t *testing.T) {
	t.Parallel()

	t.Log("creating GatewayClass for gateway conformance tests")
	gwc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	require.NoError(t, clients.MgrClient.Create(ctx, gwc))
	defer func() {
		require.NoError(t, clients.MgrClient.Delete(ctx, gwc))
	}()

	// There are no explicit conformance tests for GatewayClass, but we can
	// still run the conformance test suite setup to ensure that the
	// GatewayClass gets accepted.
	t.Log("configuring the Gateway API conformance test suite")
	const reportFileName = "kong-gateway-operator.yaml" // TODO: https://github.com/Kong/gateway-operator/issues/268

	opts := conformance.DefaultOptions(t)
	opts.ReportOutputPath = "../../" + reportFileName
	opts.Client = clients.MgrClient
	opts.GatewayClassName = gwc.Name
	opts.BaseManifests = testutils.GatewayRawRepoURL + "/conformance/base/manifests.yaml"
	opts.SkipTests = skippedTests
	opts.ConformanceProfiles = sets.New(
		suite.GatewayHTTPConformanceProfileName,
	)
	opts.SupportedFeatures = sets.New(
		features.SupportHTTPRouteResponseHeaderModification,
	)
	opts.Implementation = conformancev1.Implementation{
		Organization: metadata.Organization,
		Project:      metadata.ProjectName,
		URL:          metadata.ProjectURL,
		Version:      metadata.Release,
		Contact: []string{
			path.Join(metadata.ProjectURL, "/issues/new/choose"),
		},
	}

	t.Log("running the Gateway API conformance test suite")
	conformance.RunConformanceWithOptions(t, opts)
}
