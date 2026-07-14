package envtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	egdataplane "github.com/kong/kong-operator/v2/controller/eventgateway/dataplane"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// kegCRDGroups are the CRD groups whose types the KEG DataPlane controller
// passes to ApplyIfChanged / ApplyStatusIfChanged, mirroring the set built in
// modules/manager/run.go.
var kegCRDGroups = map[string]struct{}{
	"eventgateway.konghq.com":  {},
	"configuration.konghq.com": {},
	"konnect.konghq.com":       {},
}

func TestKEGDataPlaneReconciler(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createKEGClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "keg-cluster-ca")

	// Builds the shared TypeConverter from the CRDs envtest already installed
	// (in-process, apiserver-style; no OpenAPI publication lag to wait out).
	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, kegCRDGroups)
	require.NoError(t, err)

	StartReconcilers(ctx, t, mgr, logs,
		&egdataplane.Reconciler{
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

	t.Run("KonnectEventGateway not found sets NotFound condition", func(t *testing.T) {
		t.Parallel()

		egdp := &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "egdp-kep-not-found",
				Namespace: ns.Name,
			},
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
						Name: "nonexistent-kep",
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, egdp))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), egdp)) {
				return
			}
			cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType))
			if !assert.NotNil(ct, cond) {
				return
			}
			assert.Equal(ct, metav1.ConditionFalse, cond.Status)
			assert.Equal(ct, string(eventgatewayv1alpha1.KonnectEventGatewayNotFoundReason), cond.Reason)
		}, waitTime, tickTime)

		// No Deployment should exist.
		var deployList appsv1.DeploymentList
		require.NoError(t, cl.List(ctx, &deployList,
			client.InNamespace(ns.Name),
			client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
		))
		require.Empty(t, deployList.Items)
	})

	t.Run("KonnectEventGateway not programmed sets NotProgrammed condition", func(t *testing.T) {
		t.Parallel()

		kep := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kep-not-programmed",
				Namespace: ns.Name,
			},
		}
		require.NoError(t, cl.Create(ctx, kep))

		egdp := &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "egdp-kep-not-programmed",
				Namespace: ns.Name,
			},
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
						Name: kep.Name,
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, egdp))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), egdp)) {
				return
			}
			cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType))
			if !assert.NotNil(ct, cond) {
				return
			}
			assert.Equal(ct, metav1.ConditionFalse, cond.Status)
			assert.Equal(ct, string(eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedReason), cond.Reason)
		}, waitTime, tickTime)

		var deployList appsv1.DeploymentList
		require.NoError(t, cl.List(ctx, &deployList,
			client.InNamespace(ns.Name),
			client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
		))
		require.Empty(t, deployList.Items)
	})

	t.Run("KEP programmed but cert not yet programmed: cert Secret + EventGatewayDataPlaneCertificate created, no Deployment", func(t *testing.T) {
		t.Parallel()

		kep := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kep-no-cert-programmed",
				Namespace: ns.Name,
			},
		}
		require.NoError(t, cl.Create(ctx, kep))
		updateKonnectEventGatewayStatusWithProgrammed(t, ctx, cl, kep, "konnect-id-no-cert")

		egdp := &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "egdp-no-cert-programmed",
				Namespace: ns.Name,
			},
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
						Name: kep.Name,
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, egdp))

		// The mTLS Secret should be provisioned.
		// Note: mTLS Secrets are labelled by their owner type but NOT by the
		// owner name; we identify them via ownerReference UID instead.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var secretList corev1.SecretList
			if !assert.NoError(ct, cl.List(ctx, &secretList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{consts.SecretKEGDataPlaneCertificateLabel: "true"},
			)) {
				return
			}
			owned := 0
			for _, s := range secretList.Items {
				for _, ref := range s.OwnerReferences {
					if ref.UID == egdp.UID {
						owned++
						break
					}
				}
			}
			assert.Equal(ct, 1, owned)
		}, waitTime, tickTime)

		// EventGatewayDataPlaneCertificate should be created.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			cert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: egdp.Name, Namespace: ns.Name}, cert))
		}, waitTime, tickTime)

		// KonnectCertificateRegistered should be False/NotProgrammed.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), egdp)) {
				return
			}
			cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.KonnectCertificateRegisteredType))
			if !assert.NotNil(ct, cond) {
				return
			}
			assert.Equal(ct, metav1.ConditionFalse, cond.Status)
			assert.Equal(ct, string(eventgatewayv1alpha1.KonnectCertificateNotProgrammedReason), cond.Reason)
		}, waitTime, tickTime)

		// No Deployment yet.
		var deployList appsv1.DeploymentList
		require.NoError(t, cl.List(ctx, &deployList,
			client.InNamespace(ns.Name),
			client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
		))
		require.Empty(t, deployList.Items)
	})

	t.Run("cert programmed: Deployment and Kafka Service are created, Ready=True", func(t *testing.T) {
		t.Parallel()

		kep := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kep-happy-path",
				Namespace: ns.Name,
			},
		}
		require.NoError(t, cl.Create(ctx, kep))
		updateKonnectEventGatewayStatusWithProgrammed(t, ctx, cl, kep, "konnect-id-happy")

		egdp := &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "egdp-happy-path",
				Namespace: ns.Name,
			},
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
						Name: kep.Name,
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, egdp))

		// Wait for EventGatewayDataPlaneCertificate to be created, then simulate Konnect programming it.
		konnectCert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: egdp.Name, Namespace: ns.Name}, konnectCert))
		}, waitTime, tickTime)

		updateEventGatewayDataPlaneCertificateStatusWithProgrammed(t, ctx, cl, konnectCert)

		// Deployment should appear.
		var deployList appsv1.DeploymentList
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.List(ctx, &deployList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
			))
			assert.Len(ct, deployList.Items, 1)
		}, waitTime, tickTime)
		require.Len(t, deployList.Items, 1)

		// Simulate pods becoming ready.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			deploy := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(&deployList.Items[0]), deploy)) {
				return
			}
			deploy.Status = appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}
			assert.NoError(ct, cl.Status().Update(ctx, deploy))
		}, waitTime, tickTime)

		// Kafka Service should appear.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			svc := &corev1.Service{}
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{
				Name:      fmt.Sprintf("%s-kafka", egdp.Name),
				Namespace: ns.Name,
			}, svc))
		}, waitTime, tickTime)

		// KegDataPlane should become Ready.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), egdp)) {
				return
			}
			cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.ReadyType))
			if !assert.NotNil(ct, cond) {
				return
			}
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		}, waitTime, tickTime)
	})

	t.Run("idempotency: re-reconcile does not duplicate Deployment or Service", func(t *testing.T) {
		t.Parallel()

		kep := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kep-idempotent",
				Namespace: ns.Name,
			},
		}
		require.NoError(t, cl.Create(ctx, kep))
		updateKonnectEventGatewayStatusWithProgrammed(t, ctx, cl, kep, "konnect-id-idempotent")

		egdp := &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "egdp-idempotent",
				Namespace: ns.Name,
			},
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
						Name: kep.Name,
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, egdp))

		konnectCert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: egdp.Name, Namespace: ns.Name}, konnectCert))
		}, waitTime, tickTime)
		updateEventGatewayDataPlaneCertificateStatusWithProgrammed(t, ctx, cl, konnectCert)

		// Wait for the happy state.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var deployList appsv1.DeploymentList
			assert.NoError(ct, cl.List(ctx, &deployList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
			))
			assert.Len(ct, deployList.Items, 1)
		}, waitTime, tickTime)

		// Trigger a re-reconcile by annotating the resource.
		triggerReconcile(t, ctx, cl, egdp)

		// After re-reconcile, still exactly one Deployment and one Kafka Service.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var deployList appsv1.DeploymentList
			assert.NoError(ct, cl.List(ctx, &deployList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
			))
			assert.Len(ct, deployList.Items, 1)

			// Kafka Service is named {egdp.Name}-kafka; no per-owner label is set
			// on the Service ObjectMeta, so we look it up by its deterministic name.
			svc := &corev1.Service{}
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{
				Name:      fmt.Sprintf("%s-kafka", egdp.Name),
				Namespace: ns.Name,
			}, svc))
		}, waitTime, tickTime)
	})

	t.Run("deletion: owned Deployment and EventGatewayDataPlaneCertificate are GC'd", func(t *testing.T) {
		t.Parallel()

		kep := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kep-deletion",
				Namespace: ns.Name,
			},
		}
		require.NoError(t, cl.Create(ctx, kep))
		updateKonnectEventGatewayStatusWithProgrammed(t, ctx, cl, kep, "konnect-id-deletion")

		egdp := &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "egdp-deletion",
				Namespace: ns.Name,
			},
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
						Name: kep.Name,
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, egdp))

		konnectCert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: egdp.Name, Namespace: ns.Name}, konnectCert))
		}, waitTime, tickTime)
		updateEventGatewayDataPlaneCertificateStatusWithProgrammed(t, ctx, cl, konnectCert)

		// Wait for Deployment to exist.
		var deployList appsv1.DeploymentList
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.List(ctx, &deployList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
			))
			assert.Len(ct, deployList.Items, 1)
		}, waitTime, tickTime)
		require.Len(t, deployList.Items, 1)

		// Delete the KegDataPlane.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, client.IgnoreNotFound(cl.Delete(ctx, egdp)))
		}, waitTime, tickTime)

		// KegDataPlane should be removed. Cascade GC of owned resources
		// (Deployment, EventGatewayDataPlaneCertificate) is a built-in
		// Kubernetes behaviour and is not exercised by envtest.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			err := cl.Get(ctx, client.ObjectKeyFromObject(egdp), egdp)
			assert.True(ct, apierrors.IsNotFound(err),
				"expected KegDataPlane to be deleted, got: %v", err)
		}, waitTime, tickTime)
	})

	// ── Deployment: customisation and merge/diff tests ────────────────────────

	t.Run("Deployment: spec.deployment.replicas is applied", func(t *testing.T) {
		t.Parallel()

		replicas := int32(3)
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-replicas", "konnect-id-replicas", "egdp-replicas",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Deployment: &eventgatewayv1alpha1.DeploymentOptions{Replicas: &replicas},
			},
		)

		deploy := waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)
		require.NotNil(t, deploy.Spec.Replicas)
		assert.EqualValues(t, 3, *deploy.Spec.Replicas)
	})

	t.Run("Deployment: PodTemplateSpec env var overlay merged into keg container; base env vars and security context preserved", func(t *testing.T) {
		t.Parallel()

		// This is the key SSA list-map-by-name merge test: overlaying a single
		// container by name must ADD the custom env var without removing any of
		// the operator-defaulted env vars, probes, or security context.
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-overlay-env", "konnect-id-overlay-env", "egdp-overlay-env",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Deployment: &eventgatewayv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: consts.KEGContainerName,
									Env: []corev1.EnvVar{
										{Name: "MY_CUSTOM_VAR", Value: "hello"},
									},
								},
							},
						},
					},
				},
			},
		)

		deploy := waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)
		keg := getKEGContainer(t, deploy)

		// Custom env var is present.
		assert.True(t, kegHasEnvVar(keg.Env, "MY_CUSTOM_VAR"))
		assert.Equal(t, "hello", kegGetEnvValue(t, keg.Env, "MY_CUSTOM_VAR"))

		// All operator-defaulted base env vars are preserved (schema-based
		// list-map merge by Name.
		for _, v := range []string{
			egdataplane.EnvKonnectRegion,
			egdataplane.EnvKonnectGatewayClusterID,
			egdataplane.EnvKonnectClientCertPath,
			egdataplane.EnvKonnectClientKeyPath,
			egdataplane.EnvKonnectDomain,
			egdataplane.EnvRuntimeHealthAddr,
		} {
			assert.True(t, kegHasEnvVar(keg.Env, v), "base env var %q must be preserved after overlay", v)
		}

		// Security context defaults preserved (not overridden by overlay).
		require.NotNil(t, keg.SecurityContext)
		assert.False(t, *keg.SecurityContext.AllowPrivilegeEscalation)
		assert.True(t, *keg.SecurityContext.ReadOnlyRootFilesystem)
		assert.EqualValues(t, 65532, *keg.SecurityContext.RunAsUser)

		// Probes preserved.
		assert.NotNil(t, keg.ReadinessProbe)
		assert.NotNil(t, keg.LivenessProbe)
	})

	t.Run("Deployment: PodTemplateSpec extra volume merged alongside base volumes", func(t *testing.T) {
		t.Parallel()

		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-overlay-vol", "konnect-id-overlay-vol", "egdp-overlay-vol",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Deployment: &eventgatewayv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							// containers is required by CRD validation when spec is set.
							Containers: []corev1.Container{
								{Name: consts.KEGContainerName},
							},
							Volumes: []corev1.Volume{
								{
									Name: "my-extra-vol",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
						},
					},
				},
			},
		)

		deploy := waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)

		volumeNames := lo.Associate(deploy.Spec.Template.Spec.Volumes, func(v corev1.Volume) (string, struct{}) {
			return v.Name, struct{}{}
		})
		// User volume added.
		assert.Contains(t, volumeNames, "my-extra-vol")
		// Base volumes preserved.
		assert.Contains(t, volumeNames, egdataplane.KonnectCertVolumeName)
		assert.Contains(t, volumeNames, "tmp")
	})

	t.Run("Deployment: PodTemplateSpec resource requests applied; base security context preserved", func(t *testing.T) {
		t.Parallel()

		cpu := resource.MustParse("250m")
		mem := resource.MustParse("128Mi")
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-resources", "konnect-id-resources", "egdp-resources",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Deployment: &eventgatewayv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: consts.KEGContainerName,
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    cpu,
											corev1.ResourceMemory: mem,
										},
									},
								},
							},
						},
					},
				},
			},
		)

		deploy := waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)
		keg := getKEGContainer(t, deploy)

		// Resource requests applied.
		assert.True(t, keg.Resources.Requests.Cpu().Equal(cpu))
		assert.True(t, keg.Resources.Requests.Memory().Equal(mem))

		// Base security context preserved.
		require.NotNil(t, keg.SecurityContext)
		assert.False(t, *keg.SecurityContext.AllowPrivilegeEscalation)
		assert.True(t, *keg.SecurityContext.ReadOnlyRootFilesystem)
		assert.EqualValues(t, 65532, *keg.SecurityContext.RunAsUser)
		assert.EqualValues(t, 65532, *keg.SecurityContext.RunAsGroup)
		require.NotNil(t, keg.SecurityContext.Capabilities)
		assert.Contains(t, keg.SecurityContext.Capabilities.Drop, corev1.Capability("NET_RAW"))

		// Pod-level security context preserved.
		require.NotNil(t, deploy.Spec.Template.Spec.SecurityContext)
		assert.True(t, *deploy.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
	})

	t.Run("Deployment: spec.config overrides reflected in env vars; other base env vars preserved", func(t *testing.T) {
		t.Parallel()

		domain := "custom.example.com"
		drain := int32(45)
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-config", "konnect-id-config", "egdp-config",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
					Konnect: &eventgatewayv1alpha1.KonnectConfig{
						Domain: &domain,
					},
					Runtime: &eventgatewayv1alpha1.RuntimeOptions{
						DrainDurationSeconds: &drain,
					},
				},
			},
		)

		deploy := waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)
		keg := getKEGContainer(t, deploy)

		// Config-driven overrides present.
		assert.Equal(t, "custom.example.com", kegGetEnvValue(t, keg.Env, egdataplane.EnvKonnectDomain))
		assert.Equal(t, "45s", kegGetEnvValue(t, keg.Env, egdataplane.EnvRuntimeDrainDuration))

		// Other base env vars preserved.
		assert.True(t, kegHasEnvVar(keg.Env, egdataplane.EnvKonnectRegion))
		assert.True(t, kegHasEnvVar(keg.Env, egdataplane.EnvKonnectGatewayClusterID))
		assert.True(t, kegHasEnvVar(keg.Env, egdataplane.EnvRuntimeHealthAddr))
	})

	t.Run("Deployment: spec.deployment.replicas change is propagated to Deployment", func(t *testing.T) {
		t.Parallel()

		one := int32(1)
		three := int32(3)
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-replicas-update", "konnect-id-replicas-update", "egdp-replicas-update",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Deployment: &eventgatewayv1alpha1.DeploymentOptions{Replicas: &one},
			},
		)

		// Initial state: replicas=1.
		deploy := waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)
		require.NotNil(t, deploy.Spec.Replicas)
		assert.EqualValues(t, 1, *deploy.Spec.Replicas)

		// Update KegDataPlane spec to replicas=3.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), egdp)) {
				return
			}
			egdp.Spec.Deployment.Replicas = &three
			assert.NoError(ct, cl.Update(ctx, egdp))
		}, waitTime, tickTime)

		// Deployment must be updated by the controller.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var deployList appsv1.DeploymentList
			assert.NoError(ct, cl.List(ctx, &deployList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
			))
			if !assert.Len(ct, deployList.Items, 1) {
				return
			}
			if !assert.NotNil(ct, deployList.Items[0].Spec.Replicas) {
				return
			}
			assert.EqualValues(ct, 3, *deployList.Items[0].Spec.Replicas)
		}, waitTime, tickTime)
	})

	t.Run("Deployment: no-op diff on re-reconcile leaves ResourceVersion unchanged", func(t *testing.T) {
		t.Parallel()

		// ApplyIfChanged must skip the SSA patch when the desired state equals
		// the existing state; the Deployment ResourceVersion must not change.
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-deploy-noop", "konnect-id-deploy-noop", "egdp-deploy-noop",
			eventgatewayv1alpha1.KegDataPlaneSpec{},
		)

		deploy := waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)
		rvBefore := deploy.ResourceVersion

		// Trigger a re-reconcile via a metadata-only annotation change.
		triggerReconcile(t, ctx, cl, egdp)

		// The ResourceVersion must never change: the controller should skip the
		// SSA patch when the desired state already matches the live state.
		assert.Never(t, func() bool {
			var deployList appsv1.DeploymentList
			if err := cl.List(ctx, &deployList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdp.Name},
			); err != nil {
				return false
			}
			return len(deployList.Items) == 1 && deployList.Items[0].ResourceVersion != rvBefore
		}, waitTime, tickTime, "Deployment was unexpectedly re-patched on a no-op reconcile")
	})

	// ── Kafka Service: customisation and merge/diff tests ─────────────────────

	t.Run("Kafka Service: type=LoadBalancer applied; base selector and default port 9092 preserved", func(t *testing.T) {
		t.Parallel()

		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-svc-lb", "konnect-id-svc-lb", "egdp-svc-lb",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Network: &eventgatewayv1alpha1.NetworkOptions{
					Services: &eventgatewayv1alpha1.Services{
						Kafka: &eventgatewayv1alpha1.ServiceOptions{
							Type: corev1.ServiceTypeLoadBalancer,
						},
					},
				},
			},
		)

		var svc corev1.Service
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{
				Name: fmt.Sprintf("%s-kafka", egdp.Name), Namespace: ns.Name,
			}, &svc))
		}, waitTime, tickTime)

		// Type overridden.
		assert.Equal(t, corev1.ServiceTypeLoadBalancer, svc.Spec.Type)

		// Default port 9092 preserved (operator base wins on port conflicts).
		ports := lo.Associate(svc.Spec.Ports, func(p corev1.ServicePort) (int32, struct{}) {
			return p.Port, struct{}{}
		})
		assert.Contains(t, ports, egdataplane.DefaultKafkaPort)

		// Operator-owned selector preserved.
		assert.Equal(t, string(consts.DataPlaneManagedByLabelValue), svc.Spec.Selector[consts.GatewayOperatorManagedByLabel])
		assert.Equal(t, egdp.Name, svc.Spec.Selector[consts.GatewayOperatorManagedByNameLabel])
	})

	t.Run("Kafka Service: user port merged alongside default port 9092 (SSA list-map merge by port number)", func(t *testing.T) {
		t.Parallel()

		// This is the key SSA list-map-by-port test for Services: the user
		// overlay adds port 9093 but the base port 9092 must not be removed.
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-svc-ports", "konnect-id-svc-ports", "egdp-svc-ports",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Network: &eventgatewayv1alpha1.NetworkOptions{
					Services: &eventgatewayv1alpha1.Services{
						Kafka: &eventgatewayv1alpha1.ServiceOptions{
							Ports: []eventgatewayv1alpha1.ServicePort{
								{Port: 9093, Name: new("extra-kafka")},
							},
						},
					},
				},
			},
		)

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var svc corev1.Service
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{
				Name: fmt.Sprintf("%s-kafka", egdp.Name), Namespace: ns.Name,
			}, &svc)) {
				return
			}
			ports := lo.Associate(svc.Spec.Ports, func(p corev1.ServicePort) (int32, struct{}) {
				return p.Port, struct{}{}
			})
			assert.Contains(ct, ports, egdataplane.DefaultKafkaPort, "base port 9092 must be preserved")
			assert.Contains(ct, ports, int32(9093), "user port 9093 must be present")
		}, waitTime, tickTime)
	})

	t.Run("Kafka Service: user port with same name as base port replaces it (no duplicate port names)", func(t *testing.T) {
		t.Parallel()

		// This tests the port-name clash: when the user provides a port named
		// "kafka" at a different port number than the base (9092), the base port
		// must be suppressed so the Service does not end up with two ports sharing
		// the name "kafka", which the Kubernetes API server rejects.
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-svc-name-clash", "konnect-id-svc-name-clash", "egdp-svc-name-clash",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Network: &eventgatewayv1alpha1.NetworkOptions{
					Services: &eventgatewayv1alpha1.Services{
						Kafka: &eventgatewayv1alpha1.ServiceOptions{
							Ports: []eventgatewayv1alpha1.ServicePort{
								{Name: new("kafka"), Port: 19092},
							},
						},
					},
				},
			},
		)

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var svc corev1.Service
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{
				Name: fmt.Sprintf("%s-kafka", egdp.Name), Namespace: ns.Name,
			}, &svc)) {
				return
			}
			var kafkaPorts []corev1.ServicePort
			for _, p := range svc.Spec.Ports {
				if p.Name == "kafka" {
					kafkaPorts = append(kafkaPorts, p)
				}
			}
			// Exactly one port named "kafka", no duplicate from the base.
			if !assert.Len(ct, kafkaPorts, 1, "expected exactly one port named 'kafka'") {
				return
			}
			// The user's port number (19092) wins; the base port (9092) is gone.
			assert.EqualValues(ct, 19092, kafkaPorts[0].Port)
		}, waitTime, tickTime)
	})

	t.Run("Kafka Service: annotations propagated; operator-owned selector and default port preserved", func(t *testing.T) {
		t.Parallel()

		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-svc-annot", "konnect-id-svc-annot", "egdp-svc-annot",
			eventgatewayv1alpha1.KegDataPlaneSpec{
				Network: &eventgatewayv1alpha1.NetworkOptions{
					Services: &eventgatewayv1alpha1.Services{
						Kafka: &eventgatewayv1alpha1.ServiceOptions{
							Annotations: map[string]string{
								"external-dns.alpha.kubernetes.io/hostname": "kafka.example.com",
							},
						},
					},
				},
			},
		)

		var svc corev1.Service
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{
				Name: fmt.Sprintf("%s-kafka", egdp.Name), Namespace: ns.Name,
			}, &svc))
		}, waitTime, tickTime)

		// Annotation propagated.
		assert.Equal(t, "kafka.example.com", svc.Annotations["external-dns.alpha.kubernetes.io/hostname"])

		// Selector and default port preserved.
		assert.Equal(t, string(consts.DataPlaneManagedByLabelValue), svc.Spec.Selector[consts.GatewayOperatorManagedByLabel])
		assert.Equal(t, egdp.Name, svc.Spec.Selector[consts.GatewayOperatorManagedByNameLabel])
		ports := lo.Associate(svc.Spec.Ports, func(p corev1.ServicePort) (int32, struct{}) {
			return p.Port, struct{}{}
		})
		assert.Contains(t, ports, egdataplane.DefaultKafkaPort)
	})

	t.Run("Kafka Service: no-op diff on re-reconcile leaves ResourceVersion unchanged", func(t *testing.T) {
		t.Parallel()

		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-svc-noop", "konnect-id-svc-noop", "egdp-svc-noop",
			eventgatewayv1alpha1.KegDataPlaneSpec{},
		)

		var svc corev1.Service
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{
				Name: fmt.Sprintf("%s-kafka", egdp.Name), Namespace: ns.Name,
			}, &svc))
		}, waitTime, tickTime)
		rvBefore := svc.ResourceVersion

		// Trigger a re-reconcile via a metadata-only annotation change.
		triggerReconcile(t, ctx, cl, egdp)

		// The ResourceVersion must never change: the controller should skip the
		// SSA patch when the desired state already matches the live state.
		assert.Never(t, func() bool {
			var svcAfter corev1.Service
			if err := cl.Get(ctx, client.ObjectKey{
				Name: fmt.Sprintf("%s-kafka", egdp.Name), Namespace: ns.Name,
			}, &svcAfter); err != nil {
				return false
			}
			return svcAfter.ResourceVersion != rvBefore
		}, waitTime, tickTime, "Kafka Service was unexpectedly re-patched on a no-op reconcile")
	})

	t.Run("CRD schema reconciler: live CRD update rebuilds the shared TypeConverter without breaking SSA applies", func(t *testing.T) {
		t.Parallel()

		// Bump the KegDataPlane CRD's resourceVersion. This is watched by
		// crdschema.Reconciler, which rebuilds and atomically swaps the shared
		// TypeConverter that this same test's DataPlane controller uses for
		// every ApplyIfChanged / ApplyStatusIfChanged call.
		crd := &apiextensionsv1.CustomResourceDefinition{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: "kegdataplanes.eventgateway.konghq.com"}, crd))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(crd), crd)) {
				return
			}
			if crd.Labels == nil {
				crd.Labels = map[string]string{}
			}
			crd.Labels["kong-operator-test/touch"] = "1"
			assert.NoError(ct, cl.Update(ctx, crd))
		}, waitTime, tickTime)

		// A normal reconcile started right after must still succeed: the live
		// Rebuild triggered above must not corrupt or race with concurrent
		// SSA applies against the shared TypeConverterProvider.
		egdp := setupProgrammedKEGDP(t, ctx, cl, ns.Name,
			"kep-crd-schema-reconciler", "konnect-id-crd-schema-reconciler", "egdp-crd-schema-reconciler",
			eventgatewayv1alpha1.KegDataPlaneSpec{},
		)
		waitForKEGDeployment(t, ctx, cl, ns.Name, egdp.Name)

		require.NoError(t, ssaProvider.Ready(nil))
	})
}

// triggerReconcile forces a reconcile of obj by adding a test annotation.
// The update is retried until it succeeds to handle transient conflicts.
func triggerReconcile(t *testing.T, ctx context.Context, cl client.Client, obj client.Object) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
			return
		}
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["test/trigger-reconcile"] = "1"
		obj.SetAnnotations(annotations)
		assert.NoError(ct, cl.Update(ctx, obj))
	}, waitTime, tickTime)
}

// setupProgrammedKEGDP creates a KonnectEventGateway and a KegDataPlane,
// programs the control plane and the resulting EventGatewayDataPlaneCertificate,
// and returns the KegDataPlane object. spec.ControlPlaneRef is populated by the
// helper, so callers only need to set the fields they care about.
func setupProgrammedKEGDP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	ns, kepName, konnectID, egdpName string,
	spec eventgatewayv1alpha1.KegDataPlaneSpec,
) *eventgatewayv1alpha1.KegDataPlane {
	t.Helper()

	kep := &konnectv1alpha1.KonnectEventGateway{
		ObjectMeta: metav1.ObjectMeta{Name: kepName, Namespace: ns},
	}
	require.NoError(t, cl.Create(ctx, kep))
	updateKonnectEventGatewayStatusWithProgrammed(t, ctx, cl, kep, konnectID)

	spec.ControlPlaneRef = eventgatewayv1alpha1.ControlPlaneRef{
		Type:                 eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
		KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{Name: kepName},
	}
	egdp := &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: egdpName, Namespace: ns},
		Spec:       spec,
	}
	require.NoError(t, cl.Create(ctx, egdp))

	konnectCert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Name: egdpName, Namespace: ns}, konnectCert))
	}, waitTime, tickTime)
	updateEventGatewayDataPlaneCertificateStatusWithProgrammed(t, ctx, cl, konnectCert)

	return egdp
}

// waitForKEGDeployment waits for exactly one Deployment owned by the given
// KegDataPlane name and returns it.
func waitForKEGDeployment(t *testing.T, ctx context.Context, cl client.Client, ns, egdpName string) appsv1.Deployment {
	t.Helper()
	var deployList appsv1.DeploymentList
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, cl.List(ctx, &deployList,
			client.InNamespace(ns),
			client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: egdpName},
		))
		assert.Len(ct, deployList.Items, 1)
	}, waitTime, tickTime)
	require.Len(t, deployList.Items, 1)
	return deployList.Items[0]
}

// getKEGContainer returns the container named consts.KEGContainerName from a Deployment.
func getKEGContainer(t *testing.T, deploy appsv1.Deployment) corev1.Container {
	t.Helper()
	for _, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name == consts.KEGContainerName {
			return c
		}
	}
	t.Fatalf("container %q not found in Deployment %s", consts.KEGContainerName, deploy.Name)
	return corev1.Container{}
}

// kegHasEnvVar returns true if name exists in the env var list.
func kegHasEnvVar(envVars []corev1.EnvVar, name string) bool {
	for _, e := range envVars {
		if e.Name == name {
			return true
		}
	}
	return false
}

// kegGetEnvValue returns the value of the named env var, failing the test if absent.
func kegGetEnvValue(t *testing.T, envVars []corev1.EnvVar, name string) string {
	t.Helper()
	for _, e := range envVars {
		if e.Name == name {
			return e.Value
		}
	}
	t.Fatalf("env var %q not found", name)
	return ""
}

// createKEGClusterCASecret creates a self-signed CA Secret for use as the cluster CA
// in KEG DataPlane controller tests.
func createKEGClusterCASecret(t *testing.T, ctx context.Context, cl client.Client, namespace, name string) *corev1.Secret {
	t.Helper()

	cert, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName(fmt.Sprintf("%s-ca", name)),
		certificate.WithCATrue(),
	)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"konghq.com/secret": "internal",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": cert,
			"tls.key": key,
		},
	}
	require.NoError(t, cl.Create(ctx, secret))

	return secret
}
