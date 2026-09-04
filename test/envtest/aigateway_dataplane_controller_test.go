package envtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	aigwdataplane "github.com/kong/kong-operator/v2/controller/aigateway/dataplane"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// aigwCRDGroups mirrors the CRD groups the AIGatewayDataPlane controller
// passes to ApplyIfChanged / MergeObjects, matching the set built in
// modules/manager/controller_setup.go.
var aigwCRDGroups = map[string]struct{}{
	"aigateway.konghq.com":       {},
	"configuration.konghq.com":   {},
	"aiconfiguration.konghq.com": {},
	"konnect.konghq.com":         {},
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

// TestAIGatewayDataPlaneReconciler_NoControlPlaneRef verifies that an
// AIGatewayDataPlane with no spec.controlPlaneRef gets a Deployment without
// any Konnect resolution or certificate registration taking place.
func TestAIGatewayDataPlaneReconciler_NoControlPlaneRef(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "aigw-cluster-ca-no-cp")

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

	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "aigwdp-no-cp", Namespace: ns.Name},
	}
	require.NoError(t, cl.Create(ctx, aigwdp))

	deploy := waitForAIGWDeployment(t, ctx, cl, ns.Name, aigwdp.Name)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	for _, e := range deploy.Spec.Template.Spec.Containers[0].Env {
		assert.NotEqual(t, "KONG_CLUSTER_CONTROL_PLANE", e.Name)
	}

	// No AIGatewayDataPlaneCertificate should ever be created: there's no
	// KonnectAIGateway to register a certificate against.
	assert.Never(t, func() bool {
		cert := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}
		return cl.Get(ctx, client.ObjectKeyFromObject(aigwdp), cert) == nil
	}, waitTime, tickTime)
}

// TestAIGatewayDataPlaneReconciler_ManualCertificateSecret verifies that an
// AIGatewayDataPlane referencing an existing, user-owned TLS Secret via
// spec.certificateSecret gets a Deployment that mounts that exact Secret,
// without the operator ever creating its own automatically-provisioned
// certificate Secret.
func TestAIGatewayDataPlaneReconciler_ManualCertificateSecret(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	StartReconcilers(ctx, t, mgr, logs,
		&aigwdataplane.Reconciler{
			Client:        mgr.GetClient(),
			CertTTL:       consts.DefaultCertTTL,
			TypeConverter: ssaProvider,
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()

	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("user-owned cert"))
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "user-owned-cert", Namespace: ns.Name},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"tls.crt": cert, "tls.key": key},
	}
	require.NoError(t, cl.Create(ctx, userSecret))

	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "aigwdp-manual-cert", Namespace: ns.Name},
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			CertificateSecret: &aigatewayv1alpha1.CertificateSecret{
				Provisioning: new(aigatewayv1alpha1.ManualCertificateProvisioning),
				SecretRef:    &aigatewayv1alpha1.SecretRef{Name: userSecret.Name},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, aigwdp))

	deploy := waitForAIGWDeployment(t, ctx, cl, ns.Name, aigwdp.Name)
	var certVolume *corev1.Volume
	for i := range deploy.Spec.Template.Spec.Volumes {
		if deploy.Spec.Template.Spec.Volumes[i].Name == aigwdataplane.KonnectCertVolumeName {
			certVolume = &deploy.Spec.Template.Spec.Volumes[i]
		}
	}
	require.NotNil(t, certVolume, "cert volume not found in Deployment")
	require.NotNil(t, certVolume.Secret)
	assert.Equal(t, userSecret.Name, certVolume.Secret.SecretName)

	// No automatically-provisioned certificate Secret should ever be created.
	assert.Never(t, func() bool {
		var secretList corev1.SecretList
		if err := cl.List(ctx, &secretList, client.InNamespace(ns.Name),
			client.MatchingLabels{consts.SecretAIGatewayDataPlaneCertificateLabel: "true"},
		); err != nil {
			return false
		}
		return len(secretList.Items) > 0
	}, waitTime, tickTime)
}

// TestAIGatewayDataPlaneReconciler_KonnectCertificateBlueGreenRotation
// verifies the full certificate-rotation lifecycle against a real apiserver:
// when the referenced certificate Secret's content changes, a new Konnect
// certificate entity is registered alongside the old one; the old entity is
// kept until the Deployment rollout to the new certificate is confirmed
// complete (guarded by the apiserver's real metadata.generation bump on the
// Deployment spec update, which a fake client cannot simulate), and only then
// removed.
func TestAIGatewayDataPlaneReconciler_KonnectCertificateBlueGreenRotation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	StartReconcilers(ctx, t, mgr, logs,
		&aigwdataplane.Reconciler{
			Client:        mgr.GetClient(),
			CertTTL:       consts.DefaultCertTTL,
			TypeConverter: ssaProvider,
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()

	certV1, keyV1 := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("rotation cert v1"))
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rotating-cert", Namespace: ns.Name},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"tls.crt": certV1, "tls.key": keyV1},
	}
	require.NoError(t, cl.Create(ctx, userSecret))

	aigwdp := setupProgrammedAIGWDP(t, ctx, cl, ns.Name,
		"aigwcp-rotation", "konnect-id-rotation", "aigwdp-rotation",
		aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			CertificateSecret: &aigatewayv1alpha1.CertificateSecret{
				Provisioning: new(aigatewayv1alpha1.ManualCertificateProvisioning),
				SecretRef:    &aigatewayv1alpha1.SecretRef{Name: userSecret.Name},
			},
		},
	)

	// setupProgrammedAIGWDP already waited for and Programmed the first
	// certificate entity (A); fetch a handle to it.
	certA := waitForAIGWCertificate(t, ctx, cl, ns.Name, aigwdp.Name)
	deploy := waitForAIGWDeployment(t, ctx, cl, ns.Name, aigwdp.Name)
	checksumV1 := deploy.Spec.Template.Annotations[consts.AIGatewayDataPlaneCertificateChecksumAnnotation]
	require.NotEmpty(t, checksumV1)
	setDeploymentRolloutStatus(t, ctx, cl, ns.Name, deploy.Name, true)

	// Rotate the Secret's content in place -- the cert-manager-renewal scenario.
	certV2, keyV2 := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("rotation cert v2"))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(userSecret), userSecret)) {
			return
		}
		userSecret.Data = map[string][]byte{"tls.crt": certV2, "tls.key": keyV2}
		assert.NoError(ct, cl.Update(ctx, userSecret))
	}, waitTime, tickTime)

	// A new Konnect certificate entity (B) appears alongside A; A must not be removed yet.
	var certB *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var certList aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateList
		if !assert.NoError(ct, cl.List(ctx, &certList, client.InNamespace(ns.Name),
			client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: aigwdp.Name})) {
			return
		}
		for i := range certList.Items {
			if certList.Items[i].Name != certA.Name {
				certB = &certList.Items[i]
			}
		}
		assert.NotNil(ct, certB)
	}, waitTime, tickTime)
	require.NotNil(t, certB)

	assert.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(certA), &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}),
		"old certificate must still exist once the new one is merely registered")

	updateAIGatewayDataPlaneCertificateStatusWithProgrammed(t, ctx, cl, certB)

	// Once B is Programmed, the Deployment rolls to it -- a real spec write,
	// so the apiserver bumps metadata.generation. Its status still reflects
	// the OLD generation/rollout (set above), so cleanup must not fire yet.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var d appsv1.Deployment
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(&deploy), &d)) {
			return
		}
		cs := d.Spec.Template.Annotations[consts.AIGatewayDataPlaneCertificateChecksumAnnotation]
		assert.NotEmpty(ct, cs)
		assert.NotEqual(ct, checksumV1, cs)
	}, waitTime, tickTime)

	assert.Never(t, func() bool {
		return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(certA), &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}))
	}, waitTime, tickTime, "old certificate must not be removed before the rollout to the new one is confirmed complete")

	// Confirm the rollout to the new generation.
	setDeploymentRolloutStatus(t, ctx, cl, ns.Name, deploy.Name, true)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(
			cl.Get(ctx, client.ObjectKeyFromObject(certA), &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{})))
	}, waitTime, tickTime)
	assert.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(certB), &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}))
}

// setDeploymentRolloutStatus sets deployment status fields to either report a
// fully-complete rollout (matching k8sutils.DeploymentRolloutComplete) or a
// zeroed, clearly-incomplete one. Envtest doesn't run the real Deployment
// controller, so nothing populates these fields on its own.
func setDeploymentRolloutStatus(t *testing.T, ctx context.Context, cl client.Client, ns, name string, complete bool) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		deploy := &appsv1.Deployment{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, deploy)) {
			return
		}
		if complete {
			replicas := int32(1)
			if deploy.Spec.Replicas != nil {
				replicas = *deploy.Spec.Replicas
			}
			deploy.Status = appsv1.DeploymentStatus{
				ObservedGeneration: deploy.Generation,
				Replicas:           replicas,
				UpdatedReplicas:    replicas,
				ReadyReplicas:      replicas,
				AvailableReplicas:  replicas,
			}
		} else {
			deploy.Status = appsv1.DeploymentStatus{}
		}
		assert.NoError(ct, cl.Status().Update(ctx, deploy))
	}, waitTime, tickTime)
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

	spec.ControlPlaneRef = &aigatewayv1alpha1.ControlPlaneRef{
		Type:                 aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
		KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{Name: aigwcpName},
	}
	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: aigwdpName, Namespace: ns},
		Spec:       spec,
	}
	require.NoError(t, cl.Create(ctx, aigwdp))

	konnectCert := waitForAIGWCertificate(t, ctx, cl, ns, aigwdpName)
	updateAIGatewayDataPlaneCertificateStatusWithProgrammed(t, ctx, cl, konnectCert)

	return aigwdp
}

// waitForAIGWCertificate waits for exactly one AIGatewayDataPlaneCertificate
// owned by the given AIGatewayDataPlane name and returns it. The CR's name is
// derived from the mTLS certificate Secret's content checksum (see
// certEntityName in controller/aigateway/dataplane), not from aigwdpName, so
// it can't be looked up by a fixed name.
func waitForAIGWCertificate(t *testing.T, ctx context.Context, cl client.Client, ns, aigwdpName string) *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
	t.Helper()
	var certList aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateList
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, cl.List(ctx, &certList,
			client.InNamespace(ns),
			client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: aigwdpName},
		))
		assert.Len(ct, certList.Items, 1)
	}, waitTime, tickTime)
	require.Len(t, certList.Items, 1)
	return &certList.Items[0]
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
	obj *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
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
