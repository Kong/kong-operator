package gatewayapi

import (
	"context"
	"maps"
	"sync"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/dataplane"
	kogateway "github.com/kong/kong-operator/v2/controller/gateway"
	"github.com/kong/kong-operator/v2/controller/konnect"
	secretcert "github.com/kong/kong-operator/v2/controller/secret_cert"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/ipfamily"
	"github.com/kong/kong-operator/v2/pkg/metadata"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/pkg/vars"
	"github.com/kong/kong-operator/v2/test/envtest"
	envtestconsts "github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// konnectCPLabelsRecorder records the labels sent to Konnect by the latest
// create or update ControlPlane call made through the mocked SDK.
type konnectCPLabelsRecorder struct {
	mu     sync.Mutex
	labels map[string]string
}

func (r *konnectCPLabelsRecorder) record(labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.labels = maps.Clone(labels)
}

func (r *konnectCPLabelsRecorder) get() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.labels)
}

// TestGatewayKonnectLabels verifies that the Konnect labels set in the
// konnect-cp-labels and konnect-dp-labels annotations of a Gateway and its
// GatewayClass are merged (the Gateway taking precedence) and propagated to
// the Konnect control plane (through the mocked Konnect SDK) and to the
// Deployment of the DataPlane (through the KONG_CLUSTER_DP_LABELS env var),
// both on creation and when the annotations change.
func TestGatewayKonnectLabels(t *testing.T) {
	t.Parallel()

	const (
		konnectControlPlaneID = "6a1d5c58-2e0b-4f7e-9c1a-4c2b8f0e7d31"
		dpCertID              = "dp-cert-konnect-labels"
	)

	scheme := managerscheme.Get()
	ctx := t.Context()

	cfg, ns := envtest.Setup(t, ctx, scheme, envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme)
	c := mgr.GetClient()

	t.Log("creating the cluster CA Secret")
	caCert, caKey := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("kong-operator-cluster-ca"),
		certificate.WithCATrue(),
	)
	caSecret := &corev1.Secret{
		Namespace: ns.Name,
		Name:      "cluster-ca",
		Labels: map[string]string{
			"konghq.com/secret": "internal",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": caCert,
			"tls.key": caKey,
		},
	}
	// Uncached client: this Secret is seeded before StartReconcilers starts
	// the manager, and going through the manager's client that early
	// deadlocks under read-your-own-writes consistency.
	require.NoError(t, envtest.NewControllerClient(t, scheme, cfg).Create(ctx, caSecret))

	t.Log("setting up the mocked Konnect SDK")
	factory := sdkmocks.NewMockSDKFactory(t)
	cpSDK := factory.SDK.ControlPlaneSDK
	var cpLabels konnectCPLabelsRecorder
	cpSDK.EXPECT().
		CreateControlPlane(mock.Anything, mock.Anything).
		Run(func(_ context.Context, req sdkkonnectcomp.CreateControlPlaneRequest, _ ...sdkkonnectops.Option) {
			cpLabels.record(req.Labels)
		}).
		Return(&sdkkonnectops.CreateControlPlaneResponse{
			ControlPlane: &sdkkonnectcomp.ControlPlane{
				ID: konnectControlPlaneID,
				Config: sdkkonnectcomp.ControlPlaneConfig{
					ClusterType: sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane,
				},
			},
		}, nil)
	cpSDK.EXPECT().
		UpdateControlPlane(mock.Anything, konnectControlPlaneID, mock.Anything).
		Run(func(_ context.Context, _ string, req sdkkonnectcomp.UpdateControlPlaneRequest, _ ...sdkkonnectops.Option) {
			cpLabels.record(req.Labels)
		}).
		Return(&sdkkonnectops.UpdateControlPlaneResponse{
			ControlPlane: &sdkkonnectcomp.ControlPlane{
				ID: konnectControlPlaneID,
			},
		}, nil).
		Maybe()
	cpSDK.EXPECT().
		ListControlPlanes(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.ListControlPlanesResponse{
			ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
				Data: []sdkkonnectcomp.ControlPlane{
					{
						ID: konnectControlPlaneID,
						Config: sdkkonnectcomp.ControlPlaneConfig{
							ClusterType:          sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane,
							ControlPlaneEndpoint: "https://cp.endpoint",
							TelemetryEndpoint:    "https://tp.endpoint",
						},
					},
				},
			},
		}, nil).
		Maybe()
	cpSDK.EXPECT().
		DeleteControlPlane(mock.Anything, konnectControlPlaneID).
		Return(&sdkkonnectops.DeleteControlPlaneResponse{}, nil).
		Maybe()
	dpCertSDK := factory.SDK.DataPlaneCertificatesSDK
	dpCertSDK.EXPECT().
		CreateDataplaneCertificate(mock.Anything, konnectControlPlaneID, mock.Anything).
		Return(&sdkkonnectops.CreateDataplaneCertificateResponse{
			DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
				Item: &sdkkonnectcomp.DataPlaneClientCertificate{
					ID:   new(dpCertID),
					Cert: new(deploy.TestValidCACertPEM),
				},
			},
		}, nil)
	dpCertSDK.EXPECT().
		DeleteDataplaneCertificate(mock.Anything, konnectControlPlaneID, dpCertID).
		Return(&sdkkonnectops.DeleteDataplaneCertificateResponse{}, nil).
		Maybe()

	envtest.StartReconcilers(ctx, t, mgr, logs,
		&kogateway.Reconciler{
			Client:                mgr.GetClient(),
			Scheme:                scheme,
			Namespace:             ns.Name,
			DataPlaneIPFamily:     ipfamily.IPv4,
			DefaultDataPlaneImage: consts.DefaultDataPlaneImage,
			KonnectEnabled:        true,
		},
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha2.KonnectGatewayControlPlane](envtestconsts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha2.KonnectGatewayControlPlane](&metricsmocks.MockRecorder{}),
		),
		&konnect.KonnectExtensionReconciler{
			Client:                   mgr.GetClient(),
			SyncPeriod:               envtestconsts.KonnectInfiniteSyncTime,
			ClusterCASecretName:      caSecret.Name,
			ClusterCASecretNamespace: caSecret.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
		},
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongDataPlaneClientCertificate](envtestconsts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongDataPlaneClientCertificate](&metricsmocks.MockRecorder{}),
		),
		&dataplane.Reconciler{
			Client:                   mgr.GetClient(),
			DataPlaneIPFamily:        ipfamily.IPv4,
			ClusterCASecretName:      caSecret.Name,
			ClusterCASecretNamespace: caSecret.Namespace,
			DefaultImage:             consts.DefaultDataPlaneImage,
			CertTTL:                  consts.DefaultCertTTL,
			KonnectEnabled:           true,
			EnforceConfig:            true,
		},
		&secretcert.Reconciler{
			Client:               mgr.GetClient(),
			CertExpirationMargin: consts.DefaultCertExpirationMargin,
		},
	)

	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, client.NewNamespacedClient(c, ns.Name))

	gwConfig := deploy.GatewayConfiguration(t, ctx, c,
		func(obj client.Object) { obj.SetName("konnect-labels-gwconfig"); obj.SetNamespace(ns.Name) },
		deploy.WithGatewayConfigKonnectAuthRef(auth.Name, ns.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gwConfig) })

	gc := deploy.GatewayClass(t, ctx, c,
		func(obj client.Object) {
			obj.SetName("konnect-labels-gc")
			obj.SetAnnotations(map[string]string{
				metadata.AnnotationKeyKonnectCPLabels: "env=prod,team=platform",
				metadata.AnnotationKeyKonnectDPLabels: "region=eu,team=platform",
			})
		},
		deploy.WithGatewayClassControllerName(vars.ControllerName()),
		deploy.WithGatewayClassParametersRef("gateway-operator.konghq.com", "GatewayConfiguration", gwConfig.Name, ns.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gc) })

	t.Log("patching GatewayClass status to Accepted=True")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), envtestconsts.WaitTime, envtestconsts.TickTime)

	gw := deploy.Gateway(t, ctx, c,
		func(obj client.Object) {
			obj.SetName("konnect-labels-gw")
			obj.SetNamespace(ns.Name)
			obj.SetAnnotations(map[string]string{
				metadata.AnnotationKeyKonnectCPLabels: "team=payments",
				metadata.AnnotationKeyKonnectDPLabels: "team=payments,tier=gold",
			})
		},
		deploy.WithGatewayClassName(gc.Name),
		deploy.WithGatewayListeners(gatewayv1.Listener{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gw) })

	// requireLabelsPropagated waits until the labels sent to Konnect contain
	// expectedCPLabels and the DataPlane's Deployment has expectedDPLabelsEnv
	// set as KONG_CLUSTER_DP_LABELS.
	requireLabelsPropagated := func(t *testing.T, expectedCPLabels map[string]string, expectedDPLabelsEnv string) {
		t.Helper()

		t.Log("waiting for the labels to be sent to Konnect")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			got := cpLabels.get()
			for k, v := range expectedCPLabels {
				assert.Equal(t, v, got[k], "label %q sent to Konnect", k)
			}
		}, envtestconsts.WaitTime, envtestconsts.TickTime)

		t.Log("waiting for the labels to be set in the DataPlane's Deployment")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			assert.Equal(t, expectedDPLabelsEnv, dataPlaneLabelsEnv(t, ctx, c, ns.Name))
		}, envtestconsts.WaitTime, envtestconsts.TickTime)
	}

	t.Run("labels are merged and propagated on creation", func(t *testing.T) {
		requireLabelsPropagated(t,
			map[string]string{"env": "prod", "team": "payments"},
			"region:eu,team:payments,tier:gold",
		)
	})

	t.Run("labels are updated when the Gateway annotations change", func(t *testing.T) {
		setKonnectLabelsAnnotations(t, ctx, c, &gatewayv1.Gateway{}, client.ObjectKeyFromObject(gw),
			"team=checkout", "team=checkout,tier=gold",
		)
		requireLabelsPropagated(t,
			map[string]string{"env": "prod", "team": "checkout"},
			"region:eu,team:checkout,tier:gold",
		)
	})

	t.Run("labels are updated when the GatewayClass annotations change", func(t *testing.T) {
		setKonnectLabelsAnnotations(t, ctx, c, &gatewayv1.GatewayClass{}, client.ObjectKeyFromObject(gc),
			"env=staging,team=platform", "region=us,team=platform",
		)
		requireLabelsPropagated(t,
			map[string]string{"env": "staging", "team": "checkout"},
			"region:us,team:checkout,tier:gold",
		)
	})
}

// setKonnectLabelsAnnotations sets the Konnect CP and DP labels annotations
// on the object identified by key, retrying on conflicts.
func setKonnectLabelsAnnotations(
	t *testing.T,
	ctx context.Context,
	c client.Client,
	obj client.Object,
	key client.ObjectKey,
	cpLabels, dpLabels string,
) {
	t.Helper()

	require.Eventually(t, func() bool {
		if err := c.Get(ctx, key, obj); err != nil {
			t.Logf("failed getting %s: %v", key, err)
			return false
		}
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[metadata.AnnotationKeyKonnectCPLabels] = cpLabels
		annotations[metadata.AnnotationKeyKonnectDPLabels] = dpLabels
		obj.SetAnnotations(annotations)
		if err := c.Update(ctx, obj); err != nil {
			t.Logf("failed updating %s: %v", key, err)
			return false
		}
		return true
	}, envtestconsts.WaitTime, envtestconsts.TickTime, "failed setting Konnect labels annotations on %s", key)
}

// dataPlaneLabelsEnv returns the value of KONG_CLUSTER_DP_LABELS in the proxy
// container of the Deployment of the only DataPlane in the namespace.
func dataPlaneLabelsEnv(t *assert.CollectT, ctx context.Context, c client.Client, namespace string) string {
	var dataPlanes operatorv1beta1.DataPlaneList
	require.NoError(t, c.List(ctx, &dataPlanes, client.InNamespace(namespace)))
	require.Len(t, dataPlanes.Items, 1)

	var deployments appsv1.DeploymentList
	require.NoError(t, c.List(ctx, &deployments,
		client.InNamespace(namespace),
		client.MatchingLabels{"app": dataPlanes.Items[0].Name},
	))
	require.Len(t, deployments.Items, 1)

	container := k8sutils.GetPodContainerByName(&deployments.Items[0].Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)
	for _, env := range container.Env {
		if env.Name == "KONG_CLUSTER_DP_LABELS" {
			return env.Value
		}
	}
	return ""
}
