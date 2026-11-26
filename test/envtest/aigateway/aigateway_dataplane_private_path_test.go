/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aigateway

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	aigwdataplane "github.com/kong/kong-operator/v2/controller/aigateway/dataplane"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	"github.com/kong/kong-operator/v2/controller/konnect"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest"
	testconsts "github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/envtest/create"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
)

// konnectPrivateAIGatewayToken is the token the fake expects. It must start
// with kpat_ to pass the CRD's own validation.
const konnectPrivateAIGatewayToken = "kpat_aigatewayprivatepathfake"

// TestAIGatewayDataPlaneOnThePrivateKonnectPath drives the operator's real
// Konnect client against the fake Konnect API served on the private hostnames,
// for an AI Gateway: the KonnectAIGateway is created over the private path, its
// endpoints come back in the private form, and the AIGatewayDataPlane rendered
// from them carries the addresses Kong accepts. Before this, the port was
// appended to the whole value, so it landed inside the path.
func TestAIGatewayDataPlaneOnThePrivateKonnectPath(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())

	fake := envtest.StartKonnectPrivateServer(t, konnectPrivateAIGatewayToken)
	factory := sdkops.NewSDKFactory(sdkops.WithHTTPClient(fake.HTTPClient()))

	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "aigw-cluster-ca-private")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	t.Log("Setting up the manager with the real SDK factory")
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectAPIAuthConfigurationReconciler(controller.Options{}, factory, logging.DevelopmentMode, mgr.GetClient()),
		konnect.NewKonnectEntityReconciler[konnectv1alpha1.KonnectAIGateway](factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectAIGateway](testconsts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.KonnectAIGateway](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler[aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate](factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate](testconsts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate](&metricsmocks.MockRecorder{}),
		),
		&aigwdataplane.Reconciler{
			Client:                   mgr.GetClient(),
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
			TypeConverter:            ssaProvider,
			LicenseGetter:            shareddataplane.NewKongLicenseCacheGetter(mgr.GetClient()),
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()
	clientNamespaced := client.NewNamespacedClient(cl, ns.Name)

	t.Log("Creating the authentication pointing at the private serverURL")
	apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced,
		deploy.KonnectAPIAuthConfigurationWithTestToken(konnectPrivateAIGatewayToken, envtest.KonnectPrivateServerURL))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(apiAuth), apiAuth)) {
			return
		}
		assert.True(ct, k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, apiAuth))
		assert.Equal(ct, envtest.KonnectPrivateOrgID, apiAuth.Status.OrganizationID)
	}, waitTime, tickTime, "the authentication did not resolve the organization over the private host")

	t.Log("Creating the AI Gateway control plane over the private host")
	aigwcp := deploy.KonnectAIGateway(t, ctx, clientNamespaced, apiAuth)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(aigwcp), aigwcp)) {
			return
		}
		assert.True(ct, envtest.ConditionsContainProgrammed(aigwcp.GetConditions(), metav1.ConditionTrue))
		assert.Equal(ct, envtest.KonnectPrivateAIGatewayID, aigwcp.GetKonnectID())
		if assert.NotNil(ct, aigwcp.Status.Endpoints) {
			assert.Equal(ct, strings.TrimPrefix(envtest.KonnectPrivateAIGatewayCPEndpoint, "https://"),
				aigwcp.Status.Endpoints.Configuration)
			assert.Equal(ct, strings.TrimPrefix(envtest.KonnectPrivateAIGatewayTPEndpoint, "https://"),
				aigwcp.Status.Endpoints.Telemetry)
		}
	}, waitTime, tickTime, "the AI Gateway did not get programmed with the private endpoints")

	t.Log("Creating the data plane referencing it")
	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		Name: "aigwdp-private", Namespace: ns.Name,
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			ControlPlaneRef: &aigatewayv1alpha1.ControlPlaneRef{
				Type:                 aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &aigatewayv1alpha1.NamespacedRef{Name: aigwcp.Name},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, aigwdp))

	t.Log("Waiting for the Deployment to carry the private addresses")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		deploy := waitForAIGWDeployment(t, ctx, cl, ns.Name, aigwdp.Name)
		envs := map[string]string{}
		for _, e := range deploy.Spec.Template.Spec.Containers[0].Env {
			envs[e.Name] = e.Value
		}
		// The port must not land inside the path: Kong opens the address whole.
		assert.Equal(ct, "us.svc.konghq.tech/cp/aigw123", envs[aigwdataplane.EnvKongClusterControlPlane])
		assert.Equal(ct, "us.svc.konghq.tech", envs[aigwdataplane.EnvKongClusterServerName])
		assert.Equal(ct, "us.svc.konghq.tech:443/tp/aigw123", envs[aigwdataplane.EnvKongClusterTelemetryEndpoint])
		assert.Equal(ct, "us.svc.konghq.tech", envs[aigwdataplane.EnvKongClusterTelemetryServerName])
	}, waitTime, tickTime, "the Deployment did not carry the private addresses")

	t.Log("Checking the requests the fake received")
	requests := fake.Requests()
	require.NotEmpty(t, requests)

	assert.Contains(t, requests, envtest.KonnectRequest{
		Method:        http.MethodPost,
		Host:          envtest.KonnectPrivateRegionalHost,
		ServerName:    envtest.KonnectPrivateRegionalHost,
		Path:          "/api/v1/ai-gateways",
		Authorization: "Bearer " + konnectPrivateAIGatewayToken,
	}, "the AI Gateway create must go to the private host's path, authenticated")

	for _, r := range requests {
		assert.Equal(t, envtest.KonnectPrivateRegionalHost, r.Host, "every request must go to the private host")
		assert.Equal(t, r.Host, r.ServerName, "the TLS handshake must present the host being called")
		assert.Contains(t, r.Path, "/api/", "the configured path prefix must be preserved")
	}
}
