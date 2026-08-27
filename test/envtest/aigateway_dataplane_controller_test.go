package envtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	aigwdataplane "github.com/kong/kong-operator/v2/controller/aigateway/dataplane"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// aigwCRDGroups mirrors the CRD groups the AIGatewayDataPlane controller
// passes to ApplyIfChanged / MergeObjects, matching the set built in
// modules/manager/controller_setup.go.
var aigwCRDGroups = map[string]struct{}{
	"aigateway.konghq.com":     {},
	"configuration.konghq.com": {},
	"konnect.konghq.com":       {},
}

// TestAIGatewayDataPlaneReconciler_SelectorStability is a regression test for
// the bug fixed in "fix: fix label selector when patching AIGatewayDataPlane":
// the Deployment build path taken when spec.deployment.podTemplateSpec is
// unset produced a different spec.selector.matchLabels than the path taken
// once it is set, so switching between the two on the same Deployment hit
// Kubernetes' "spec.selector: field is immutable" rejection.
//
// This must be an envtest: the fake client used by unit tests neither
// enforces Deployment selector immutability (that lives in the real
// kube-apiserver's Deployment strategy validation) nor merges
// LabelSelector/matchLabels the way the real OpenAPI schema does (the schema
// treats LabelSelector as atomic, so a divergent overlay selector replaces
// the base's rather than unioning with it, as the unit tests' deduced
// TypeConverter would do).
func TestAIGatewayDataPlaneReconciler_SelectorStability(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "aigw-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	StartReconcilers(ctx, t, mgr, logs,
		&aigwdataplane.Reconciler{
			Client:                   mgr.GetClient(),
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
			TypeConverter:            ssaProvider,
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()

	t.Run("switching to a PodTemplateSpec overlay after a plain create does not trip immutable selector", func(t *testing.T) {
		t.Parallel()

		aigwdp := setupProgrammedAIGWDP(t, ctx, cl, ns.Name,
			"aigwcp-selector", "konnect-id-selector", "aigwdp-selector",
			aigatewayv1alpha1.AIGatewayDataPlaneSpec{},
		)

		// Step 1: Deployment created via the non-overlay path.
		deploy := waitForAIGWDeployment(t, ctx, cl, ns.Name, aigwdp.Name)
		selectorBefore := deploy.Spec.Selector.MatchLabels
		assert.Equal(t, map[string]string{
			consts.GatewayOperatorManagedByLabel:          consts.AIGatewayDataPlaneManagedByLabelValue,
			consts.GatewayOperatorManagedByNameLabel:      aigwdp.Name,
			consts.GatewayOperatorManagedByNamespaceLabel: ns.Name,
		}, selectorBefore)

		// Step 2: switch to the PodTemplateSpec overlay path by setting
		// spec.deployment.podTemplateSpec. This forces buildDeployment onto
		// the MergeObjects branch against the very same stored Deployment.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(aigwdp), aigwdp)) {
				return
			}
			aigwdp.Spec.Deployment = &aigatewayv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: consts.AIGatewayDataPlaneContainerName, Image: "custom/aigw:overlay"},
						},
					},
				},
			}
			assert.NoError(ct, cl.Update(ctx, aigwdp))
		}, waitTime, tickTime)

		// Step 3: the overlay must actually land. Pre-fix, the SSA apply is
		// rejected with "spec.selector: field is immutable", the container
		// image never changes, and a DeploymentFailed event is recorded.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var deployAfter appsv1.Deployment
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: aigwdp.Name, Namespace: ns.Name}, &deployAfter)) {
				return
			}
			var found bool
			for _, c := range deployAfter.Spec.Template.Spec.Containers {
				if c.Name == consts.AIGatewayDataPlaneContainerName {
					found = assert.Equal(ct, "custom/aigw:overlay", c.Image)
				}
			}
			assert.True(ct, found, "aigw container not found in Deployment")
			// Step 4: selector must be unchanged across the transition.
			assert.Equal(ct, selectorBefore, deployAfter.Spec.Selector.MatchLabels)
		}, waitTime, tickTime)
	})
}

// setupProgrammedAIGWDP creates a KonnectAIGateway and an AIGatewayDataPlane,
// programs the control plane and the resulting AIGatewayDataPlaneCertificate,
// and returns the AIGatewayDataPlane. spec.ControlPlaneRef is populated by the
// helper, so callers only need to set the fields they care about.
func setupProgrammedAIGWDP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	ns, aigwcpName, konnectID, aigwdpName string,
	spec aigatewayv1alpha1.AIGatewayDataPlaneSpec,
) *aigatewayv1alpha1.AIGatewayDataPlane {
	t.Helper()

	aigwcp := &konnectv1alpha1.KonnectAIGateway{
		ObjectMeta: metav1.ObjectMeta{Name: aigwcpName, Namespace: ns},
		Spec: konnectv1alpha1.KonnectAIGatewaySpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{Name: "unused-auth"},
			},
			APISpec: &konnectv1alpha1.KonnectAIGatewayAPISpec{
				Name:        aigwcpName,
				DisplayName: aigwcpName,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, aigwcp))
	updateKonnectAIGatewayStatusWithProgrammed(t, ctx, cl, aigwcp, konnectID)

	spec.ControlPlaneRef = aigatewayv1alpha1.ControlPlaneRef{
		Type:                 aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
		KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{Name: aigwcpName},
	}
	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: aigwdpName, Namespace: ns},
		Spec:       spec,
	}
	require.NoError(t, cl.Create(ctx, aigwdp))

	konnectCert := &configurationv1alpha1.AIGatewayDataPlaneCertificate{}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: aigwdpName, Namespace: ns}, konnectCert))
	}, waitTime, tickTime)
	updateAIGatewayDataPlaneCertificateStatusWithProgrammed(t, ctx, cl, konnectCert)

	return aigwdp
}

// waitForAIGWDeployment waits for exactly one Deployment owned by the given
// AIGatewayDataPlane name and returns it.
func waitForAIGWDeployment(t *testing.T, ctx context.Context, cl client.Client, ns, aigwdpName string) appsv1.Deployment {
	t.Helper()
	var deployList appsv1.DeploymentList
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, cl.List(ctx, &deployList,
			client.InNamespace(ns),
			client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: aigwdpName},
		))
		assert.Len(ct, deployList.Items, 1)
	}, waitTime, tickTime)
	require.Len(t, deployList.Items, 1)
	return deployList.Items[0]
}

// updateKonnectAIGatewayStatusWithProgrammed sets aigwcp's status to
// Programmed=True with endpoints, as the real Konnect controller would once
// the AI Gateway control plane exists on Konnect.
func updateKonnectAIGatewayStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	aigwcp *konnectv1alpha1.KonnectAIGateway,
	id string,
) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(aigwcp), aigwcp)) {
			return
		}
		aigwcp.Status.Endpoints = &konnectv1alpha1.KonnectAIGatewayEndpoints{
			Configuration: "cp." + id + ".example.com",
			Telemetry:     "tp." + id + ".example.com",
		}
		aigwcp.Status.Conditions = []metav1.Condition{
			ProgrammedCondition(aigwcp.GetGeneration()),
		}
		assert.NoError(ct, cl.Status().Update(ctx, aigwcp))
	}, waitTime, tickTime)
}

// waitForAIGWHPA waits for exactly one HPA owned by the given AIGatewayDataPlane and returns it.
func waitForAIGWHPA(t *testing.T, ctx context.Context, cl client.Client, ns, aigwdpName string) autoscalingv2.HorizontalPodAutoscaler {
	t.Helper()
	var hpaList autoscalingv2.HorizontalPodAutoscalerList
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, cl.List(ctx, &hpaList, client.InNamespace(ns), client.MatchingLabels{"app": aigwdpName}))
		assert.Len(ct, hpaList.Items, 1)
	}, waitTime, tickTime)
	require.Len(t, hpaList.Items, 1)
	return hpaList.Items[0]
}

// waitForNoAIGWHPA asserts that no HPA owned by the given AIGatewayDataPlane exists.
func waitForNoAIGWHPA(t *testing.T, ctx context.Context, cl client.Client, ns, aigwdpName string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var hpaList autoscalingv2.HorizontalPodAutoscalerList
		assert.NoError(ct, cl.List(ctx, &hpaList, client.InNamespace(ns), client.MatchingLabels{"app": aigwdpName}))
		assert.Empty(ct, hpaList.Items)
	}, waitTime, tickTime)
}

// TestAIGatewayDataPlaneReconciler_HPA verifies the full HPA lifecycle:
// create when scaling is configured, update on spec change, delete when scaling is removed.
func TestAIGatewayDataPlaneReconciler_HPA(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "aigw-cluster-ca-hpa")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	StartReconcilers(ctx, t, mgr, logs,
		&aigwdataplane.Reconciler{
			Client:                   mgr.GetClient(),
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
			TypeConverter:            ssaProvider,
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()
	maxReplicas := int32(5)

	t.Run("HPA is created when horizontal scaling is configured", func(t *testing.T) {
		t.Parallel()

		aigwdp := setupProgrammedAIGWDP(t, ctx, cl, ns.Name,
			"aigwcp-hpa-create", "konnect-id-hpa-create", "aigwdp-hpa-create",
			aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				Deployment: &aigatewayv1alpha1.DeploymentOptions{
					Scaling: &aigatewayv1alpha1.Scaling{
						HorizontalScaling: &aigatewayv1alpha1.HorizontalScaling{
							MaxReplicas: maxReplicas,
						},
					},
				},
			},
		)

		hpa := waitForAIGWHPA(t, ctx, cl, ns.Name, aigwdp.Name)
		assert.Equal(t, maxReplicas, hpa.Spec.MaxReplicas)
		assert.Equal(t, aigwdp.Name, hpa.Spec.ScaleTargetRef.Name)

		// Verify the Deployment was created. The replica guard (omitting spec.replicas from
		// the SSA patch when HPA is active) is covered by TestGenerateBaseDeployment_ReplicaGuard;
		// the API server defaults spec.replicas to 1 so it is never nil in the stored object.
		waitForAIGWDeployment(t, ctx, cl, ns.Name, aigwdp.Name)
	})

	t.Run("HPA is deleted when scaling is removed", func(t *testing.T) {
		t.Parallel()

		aigwdp := setupProgrammedAIGWDP(t, ctx, cl, ns.Name,
			"aigwcp-hpa-delete", "konnect-id-hpa-delete", "aigwdp-hpa-delete",
			aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				Deployment: &aigatewayv1alpha1.DeploymentOptions{
					Scaling: &aigatewayv1alpha1.Scaling{
						HorizontalScaling: &aigatewayv1alpha1.HorizontalScaling{
							MaxReplicas: maxReplicas,
						},
					},
				},
			},
		)

		// Wait for HPA to be created.
		_ = waitForAIGWHPA(t, ctx, cl, ns.Name, aigwdp.Name)

		// Remove scaling from the spec.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(aigwdp), aigwdp)) {
				return
			}
			aigwdp.Spec.Deployment = nil
			assert.NoError(ct, cl.Update(ctx, aigwdp))
		}, waitTime, tickTime)

		waitForNoAIGWHPA(t, ctx, cl, ns.Name, aigwdp.Name)
	})

	t.Run("HPA spec is updated when maxReplicas changes", func(t *testing.T) {
		t.Parallel()

		aigwdp := setupProgrammedAIGWDP(t, ctx, cl, ns.Name,
			"aigwcp-hpa-update", "konnect-id-hpa-update", "aigwdp-hpa-update",
			aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				Deployment: &aigatewayv1alpha1.DeploymentOptions{
					Scaling: &aigatewayv1alpha1.Scaling{
						HorizontalScaling: &aigatewayv1alpha1.HorizontalScaling{
							MaxReplicas: maxReplicas,
						},
					},
				},
			},
		)

		_ = waitForAIGWHPA(t, ctx, cl, ns.Name, aigwdp.Name)

		// Update maxReplicas to 10.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(aigwdp), aigwdp)) {
				return
			}
			aigwdp.Spec.Deployment.Scaling.HorizontalScaling.MaxReplicas = 10
			assert.NoError(ct, cl.Update(ctx, aigwdp))
		}, waitTime, tickTime)

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var hpa autoscalingv2.HorizontalPodAutoscaler
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: aigwdp.Name, Namespace: ns.Name}, &hpa)) {
				return
			}
			assert.Equal(ct, int32(10), hpa.Spec.MaxReplicas)
		}, waitTime, tickTime)
	})
}

// updateAIGatewayDataPlaneCertificateStatusWithProgrammed flips the owned
// AIGatewayDataPlaneCertificate to Programmed=True, as the real Konnect
// controller would once the certificate is registered on Konnect.
func updateAIGatewayDataPlaneCertificateStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.AIGatewayDataPlaneCertificate,
) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
			return
		}
		obj.Status.Conditions = []metav1.Condition{
			ProgrammedCondition(obj.GetGeneration()),
		}
		assert.NoError(ct, cl.Status().Update(ctx, obj))
	}, waitTime, tickTime)
}
