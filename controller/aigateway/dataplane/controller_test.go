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

package dataplane

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	pkgconsts "github.com/kong/kong-operator/v2/pkg/consts"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

const (
	reconcileTestNS         = testCASecretNamespace
	reconcileTestDPName     = testDPName
	reconcileTestAIGWCPName = "my-aigwcp"
)

// newReconcileAIGWDP builds the standard AIGatewayDataPlane used across Reconcile tests.
func newReconcileAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: reconcileTestNS,
			Name:      reconcileTestDPName,
			UID:       types.UID("aigwdp-uid"),
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "aigateway.konghq.com/v1alpha1",
			Kind:       "AIGatewayDataPlane",
		},
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			ControlPlaneRef: &aigatewayv1alpha1.ControlPlaneRef{
				Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
					Name: reconcileTestAIGWCPName,
				},
			},
		},
	}
}

// newReconcileAIGWDPNoControlPlaneRef builds an AIGatewayDataPlane with no
// ControlPlaneRef configured, for the manual/unmanaged control plane path.
func newReconcileAIGWDPNoControlPlaneRef() *aigatewayv1alpha1.AIGatewayDataPlane {
	aigwdp := newReconcileAIGWDP()
	aigwdp.Spec.ControlPlaneRef = nil
	return aigwdp
}

// newReconcileAIGWDPManualCert builds an AIGatewayDataPlane with no
// ControlPlaneRef and a Manual certificateSecret referencing manualCertSecretName.
func newReconcileAIGWDPManualCert() *aigatewayv1alpha1.AIGatewayDataPlane {
	aigwdp := newReconcileAIGWDPNoControlPlaneRef()
	aigwdp.Spec.CertificateSecret = &aigatewayv1alpha1.CertificateSecret{
		Provisioning: new(aigatewayv1alpha1.ManualCertificateProvisioning),
		SecretRef:    &aigatewayv1alpha1.SecretRef{Name: manualCertSecretName},
	}
	return aigwdp
}

// newReconcileAIGWDPAutomaticCertNoControlPlaneRef builds an AIGatewayDataPlane
// with no ControlPlaneRef but an explicit Automatic certificateSecret, to
// exercise the mismatch-surfacing path for the Automatic (not just Manual) case.
func newReconcileAIGWDPAutomaticCertNoControlPlaneRef() *aigatewayv1alpha1.AIGatewayDataPlane {
	aigwdp := newReconcileAIGWDPNoControlPlaneRef()
	aigwdp.Spec.CertificateSecret = &aigatewayv1alpha1.CertificateSecret{
		Provisioning: new(aigatewayv1alpha1.AutomaticCertificateProvisioning),
	}
	return aigwdp
}

// newReconcileAIGWDPManualCertWithControlPlane is like newReconcileAIGWDPManualCert
// but keeps ControlPlaneRef set, so Konnect certificate registration runs.
func newReconcileAIGWDPManualCertWithControlPlane() *aigatewayv1alpha1.AIGatewayDataPlane {
	aigwdp := newReconcileAIGWDP()
	aigwdp.Spec.CertificateSecret = &aigatewayv1alpha1.CertificateSecret{
		Provisioning: new(aigatewayv1alpha1.ManualCertificateProvisioning),
		SecretRef:    &aigatewayv1alpha1.SecretRef{Name: manualCertSecretName},
	}
	return aigwdp
}

// newProgrammedKonnectAIGateway builds a KonnectAIGateway (controlplane)
// with Programmed=True and endpoints set.
func newProgrammedKonnectAIGateway() *konnectv1alpha1.KonnectAIGateway {
	return &konnectv1alpha1.KonnectAIGateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: reconcileTestNS,
			Name:      reconcileTestAIGWCPName,
		},
		Status: konnectv1alpha1.KonnectAIGatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: metav1.ConditionTrue,
					Reason: "Programmed",
				},
			},
			Endpoints: &konnectv1alpha1.KonnectAIGatewayEndpoints{
				Configuration: "cp.example.com",
				Telemetry:     "tp.example.com",
			},
		},
	}
}

// newNotProgrammedKonnectAIGateway builds an KonnectAIGateway with Programmed=False.
func newNotProgrammedKonnectAIGateway() *konnectv1alpha1.KonnectAIGateway {
	aigwcp := newProgrammedKonnectAIGateway()
	aigwcp.Status.Conditions[0].Status = metav1.ConditionFalse
	return aigwcp
}

// newTestReconciler builds a Reconciler wired to cl and recorder.
// The fake client is wrapped with an interceptor that populates TypeMeta on
// AIGatewayDataPlane objects after Get, because the fake client does not set it.
func newTestReconciler(cl client.WithWatch, recorder *events.FakeRecorder) *Reconciler {
	wrapped := interceptor.NewClient(cl, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := c.Get(ctx, key, obj, opts...); err != nil {
				return err
			}
			if aigwdp, ok := obj.(*aigatewayv1alpha1.AIGatewayDataPlane); ok {
				gvks, _, _ := c.Scheme().ObjectKinds(aigwdp)
				if len(gvks) > 0 {
					aigwdp.TypeMeta = metav1.TypeMeta{
						APIVersion: gvks[0].GroupVersion().String(),
						Kind:       gvks[0].Kind,
					}
				}
			}
			return nil
		},
	})
	return &Reconciler{
		Client:                   wrapped,
		TypeConverter:            managedfields.NewDeducedTypeConverter(),
		eventRecorder:            recorder,
		ClusterCASecretName:      testCASecretName,
		ClusterCASecretNamespace: testCASecretNamespace,
		CertTTL:                  pkgconsts.DefaultCertTTL,
	}
}

// getAIGWDP fetches the fresh AIGatewayDataPlane from the fake client.
func getAIGWDP(t *testing.T, cl client.Client) *aigatewayv1alpha1.AIGatewayDataPlane {
	t.Helper()
	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{}
	err := cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, aigwdp)
	require.NoError(t, err)
	return aigwdp
}

// assertCondition checks a named status condition on aigwdp.
func assertCondition(t *testing.T, aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, condType consts.ConditionType, wantStatus metav1.ConditionStatus, wantReason consts.ConditionReason) {
	t.Helper()
	cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(condType))
	require.NotNilf(t, cond, "condition %q must be present", condType)
	assert.Equalf(t, wantStatus, cond.Status, "condition %q status", condType)
	assert.Equalf(t, string(wantReason), cond.Reason, "condition %q reason", condType)
}

// drainEvents returns all events currently buffered in the recorder.
func drainEvents(recorder *events.FakeRecorder) []string {
	var collected []string
	for {
		select {
		case e := <-recorder.Events:
			collected = append(collected, e)
		default:
			return collected
		}
	}
}

// markGeneratedCertProgrammed finds the automatically-generated mTLS
// certificate Secret and creates a Programmed=True AIGatewayDataPlaneCertificate
// for it, named exactly as the real reconciler would name it (certEntityName
// is checksum-derived, so the name can't be known statically ahead of the
// Secret actually being generated). Safe to call more than once: it's a
// no-op once the CR already exists.
func markGeneratedCertProgrammed(t *testing.T, cl client.Client) {
	t.Helper()

	var secrets corev1.SecretList
	require.NoError(t, cl.List(t.Context(), &secrets,
		client.InNamespace(reconcileTestNS),
		client.MatchingLabels{pkgconsts.SecretAIGatewayDataPlaneCertificateLabel: "true"},
	))
	require.Len(t, secrets.Items, 1, "expected exactly one automatically-generated certificate Secret")
	secret := &secrets.Items[0]

	certName := certEntityName(newReconcileAIGWDP(), certificateChecksum(secret))
	existing := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: certName}, existing); err == nil {
		return
	}

	cert := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
		ObjectMeta: metav1.ObjectMeta{Namespace: reconcileTestNS, Name: certName},
		Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: reconcileTestAIGWCPName},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
				Cert: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: secret.Name, Key: corev1.TLSCertKey},
				},
				Title: reconcileTestDPName,
			},
		},
	}
	require.NoError(t, cl.Create(t.Context(), cert))
	cert.Status.Conditions = []metav1.Condition{
		{
			Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status: metav1.ConditionTrue,
			Reason: "Programmed",
		},
	}
	require.NoError(t, cl.Status().Update(t.Context(), cert))
}

// markManualCertProgrammed creates (or, if the reconciler already created it,
// marks) a Programmed=True AIGatewayDataPlaneCertificate for the
// manually-referenced certificate Secret (manualCertSecret(true)), named
// exactly as the real reconciler would name it. Safe to call more than once.
func markManualCertProgrammed(t *testing.T, cl client.Client) {
	t.Helper()

	secret := &corev1.Secret{}
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
		Namespace: reconcileTestNS, Name: manualCertSecretName,
	}, secret))

	certName := certEntityName(newReconcileAIGWDP(), certificateChecksum(secret))
	cert := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: certName}, cert); err != nil {
		require.True(t, apierrors.IsNotFound(err))
		cert = &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
			ObjectMeta: metav1.ObjectMeta{Namespace: reconcileTestNS, Name: certName},
			Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
				AIGatewayRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: reconcileTestAIGWCPName},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
					Cert: aiconfigurationv1alpha1.SensitiveDataSource{
						Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
						SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: secret.Name, Key: corev1.TLSCertKey},
					},
					Title: reconcileTestDPName,
				},
			},
		}
		require.NoError(t, cl.Create(t.Context(), cert))
	}

	if apimeta.IsStatusConditionTrue(cert.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType) {
		return
	}
	cert.Status.Conditions = []metav1.Condition{
		{
			Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status: metav1.ConditionTrue,
			Reason: "Programmed",
		},
	}
	require.NoError(t, cl.Status().Update(t.Context(), cert))
}

// -----------------------------------------------------------------
// TestReconciler_Reconcile
// -----------------------------------------------------------------

func TestReconciler_Reconcile(t *testing.T) {
	scheme := managerscheme.Get()

	tests := []struct {
		name string
		// Seed objects in the fake client before any reconcile call.
		objects []client.Object
		// reconcileCount is the number of times Reconcile is called.
		// Only the result of the final call is checked. Defaults to 1.
		reconcileCount int
		wantResult     ctrl.Result
		wantErr        bool
		// betweenReconciles runs after each intermediate reconcile (i.e. every
		// call except the last), before the next one. Used to seed state that
		// depends on what a previous reconcile actually produced (e.g. marking
		// the real, checksum-named AIGatewayDataPlaneCertificate as Programmed
		// once the automatically-generated cert Secret exists).
		betweenReconciles func(t *testing.T, cl client.Client)
		// assertFn runs after all reconcile calls to check cluster state.
		assertFn func(t *testing.T, cl client.Client, recorder *events.FakeRecorder)
	}{
		{
			name:       "AIGatewayDataPlane not found: no-op",
			objects:    nil,
			wantResult: ctrl.Result{},
		},
		{
			name: "KonnectAIGateway not found: error returned (runtime handles backoff), KonnectAIGatewayResolved=False",
			objects: []client.Object{
				newReconcileAIGWDP(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			wantErr:    true,
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.KonnectAIGatewayResolvedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.KonnectAIGatewayNotFoundReason,
				)
			},
		},
		{
			name: "KonnectAIGateway not yet programmed: error returned (runtime handles backoff), KonnectAIGatewayResolved=False",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newNotProgrammedKonnectAIGateway(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			wantErr:    true,
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.KonnectAIGatewayResolvedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.KonnectAIGatewayNotProgrammedReason,
				)
			},
		},
		{
			name: "CA secret missing: error returned, CertificateProvisioned=False",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newProgrammedKonnectAIGateway(),
			},
			wantErr: true,
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.UnableToProvisionReason,
				)
			},
		},
		{
			name: "certificate secret just created: first reconcile returns early, CertificateProvisioned=True",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newProgrammedKonnectAIGateway(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.KonnectAIGatewayResolvedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.KonnectAIGatewayResolvedReason,
				)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.CertificateProvisionedReason,
				)
			},
		},
		{
			name: "happy path: Deployment and Service created, all conditions set",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newProgrammedKonnectAIGateway(),
				caSecret(),
			},
			// 1st reconcile: cert Secret created → returns early (owned Secret watch triggers next reconcile).
			// betweenReconciles marks the resulting (checksum-named) AIGatewayDataPlaneCertificate as Programmed.
			// 2nd reconcile: cert Secret + programmed AIGatewayDataPlaneCertificate present → Deployment + Service created.
			reconcileCount:    2,
			betweenReconciles: markGeneratedCertProgrammed,
			wantResult:        ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, recorder *events.FakeRecorder) {
				t.Helper()

				// Deployment exists.
				deploy := &appsv1.Deployment{}
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, deploy))

				// Service exists.
				svc := &corev1.Service{}
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName + "-ingress",
				}, svc))

				// All conditions set correctly.
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.KonnectAIGatewayResolvedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.KonnectAIGatewayResolvedReason,
				)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.CertificateProvisionedReason,
				)
				// Ready=False because the fake Deployment's rollout is not complete.
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.ReadyType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.WaitingToBecomeReadyReason,
				)

				// Events: 2nd reconcile must emit DeploymentCreated and ServiceCreated.
				events := drainEvents(recorder)
				assert.Contains(t, events, "Normal DeploymentCreated Deployment my-dp created")
				assert.Contains(t, events, "Normal ServiceCreated Ingress Service my-dp-ingress created")
			},
		},
		{
			name: "no ControlPlaneRef: Deployment and Service created without Konnect resolution or cert registration",
			objects: []client.Object{
				newReconcileAIGWDPNoControlPlaneRef(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()

				// Deployment exists, without the Konnect endpoint env vars or any cert wiring.
				deploy := &appsv1.Deployment{}
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, deploy))
				require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
				var envNames []string
				for _, e := range deploy.Spec.Template.Spec.Containers[0].Env {
					envNames = append(envNames, e.Name)
				}
				assert.NotContains(t, envNames, EnvKongClusterControlPlane)
				assert.NotContains(t, envNames, EnvKongClusterServerName)
				assert.NotContains(t, envNames, EnvClientCertPath)
				for _, vm := range deploy.Spec.Template.Spec.Containers[0].VolumeMounts {
					assert.NotEqual(t, KonnectCertVolumeName, vm.Name)
				}
				for _, v := range deploy.Spec.Template.Spec.Volumes {
					assert.NotEqual(t, KonnectCertVolumeName, v.Name)
				}

				// No AIGatewayDataPlaneCertificate is created: there's no KonnectAIGateway to register against.
				cert := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, cert)
				assert.True(t, apierrors.IsNotFound(err))

				aigwdp := getAIGWDP(t, cl)
				// Konnect-specific conditions are never set, so they can't block Ready.
				assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectAIGatewayResolvedType)))
				assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectCertificateRegisteredType)))
				// No cert was requested, so no certificate is provisioned and no condition is set either.
				assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType)))
			},
		},
		{
			name: "no ControlPlaneRef but certificateSecret Manual configured: no Deployment, mismatch surfaced",
			objects: []client.Object{
				newReconcileAIGWDPManualCert(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.CertificateControlPlaneRefMissingReason,
				)
				deploy := &appsv1.Deployment{}
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, deploy)
				assert.True(t, apierrors.IsNotFound(err))
			},
		},
		{
			name: "no ControlPlaneRef but certificateSecret Automatic configured: no Deployment, mismatch surfaced",
			objects: []client.Object{
				newReconcileAIGWDPAutomaticCertNoControlPlaneRef(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.CertificateControlPlaneRefMissingReason,
				)
				deploy := &appsv1.Deployment{}
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, deploy)
				assert.True(t, apierrors.IsNotFound(err))

				// No automatic certificate Secret should have been created either.
				secretList := &corev1.SecretList{}
				require.NoError(t, cl.List(t.Context(), secretList, client.InNamespace(reconcileTestNS)))
				assert.Empty(t, secretList.Items)
			},
		},
		{
			name: "Manual certificate: referenced secret not found, error returned",
			objects: []client.Object{
				newReconcileAIGWDPManualCertWithControlPlane(),
				newProgrammedKonnectAIGateway(),
			},
			wantErr: true,
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.CertificateSecretRefNotFoundReason,
				)
			},
		},
		{
			name: "Manual certificate: referenced secret invalid, no error, no Deployment",
			objects: []client.Object{
				newReconcileAIGWDPManualCertWithControlPlane(),
				newProgrammedKonnectAIGateway(),
				manualCertSecret(false),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.CertificateSecretInvalidReason,
				)
				deploy := &appsv1.Deployment{}
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, deploy)
				assert.True(t, apierrors.IsNotFound(err))
			},
		},
		{
			name: "Manual certificate: valid secret, Deployment mounts it directly, no automatic secret",
			objects: []client.Object{
				newReconcileAIGWDPManualCertWithControlPlane(),
				newProgrammedKonnectAIGateway(),
				manualCertSecret(true),
			},
			// 1st reconcile: registers the cert entity, not yet Programmed. 2nd
			// (after betweenReconciles marks it Programmed): Deployment created.
			reconcileCount:    2,
			betweenReconciles: markManualCertProgrammed,
			wantResult:        ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.CertificateProvisionedReason,
				)

				deploy := &appsv1.Deployment{}
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, deploy))
				require.NotEmpty(t, deploy.Spec.Template.Spec.Volumes)
				var certVolume *corev1.Volume
				for i := range deploy.Spec.Template.Spec.Volumes {
					if deploy.Spec.Template.Spec.Volumes[i].Name == KonnectCertVolumeName {
						certVolume = &deploy.Spec.Template.Spec.Volumes[i]
					}
				}
				require.NotNil(t, certVolume)
				require.NotNil(t, certVolume.Secret)
				assert.Equal(t, manualCertSecretName, certVolume.Secret.SecretName)
				assert.NotEmpty(t, deploy.Spec.Template.Annotations[pkgconsts.AIGatewayDataPlaneCertificateChecksumAnnotation])

				// No automatic certificate Secret should have been created.
				secretList := &corev1.SecretList{}
				require.NoError(t, cl.List(t.Context(), secretList, client.InNamespace(reconcileTestNS)))
				assert.Len(t, secretList.Items, 1, "only the manually-referenced Secret should exist")
			},
		},
		{
			name: "idempotency: third reconcile is noop, no create events",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newProgrammedKonnectAIGateway(),
				caSecret(),
			},
			// 1st: cert Secret created. betweenReconciles marks the cert Programmed.
			// 2nd: Deployment+Service created. 3rd: everything exists → noop.
			reconcileCount:    3,
			betweenReconciles: markGeneratedCertProgrammed,
			wantResult:        ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, recorder *events.FakeRecorder) {
				t.Helper()
				events := drainEvents(recorder)
				for _, e := range events {
					assert.NotContains(t, e, "Created", "3rd reconcile must not emit Created events, got: %s", e)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			aigwdp := newReconcileAIGWDP()
			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.objects...).
				WithStatusSubresource(aigwdp, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}).
				Build()

			recorder := events.NewFakeRecorder(30)
			r := newTestReconciler(base, recorder)

			count := tc.reconcileCount
			if count == 0 {
				count = 1
			}

			var result ctrl.Result
			var err error
			for i := range count {
				current := new(aigatewayv1alpha1.AIGatewayDataPlane)
				getErr := r.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, current)
				switch {
				case apierrors.IsNotFound(getErr):
					result, err = ctrl.Result{}, nil
				case getErr != nil:
					result, err = ctrl.Result{}, getErr
				default:
					result, err = r.Reconcile(t.Context(), current)
				}
				// All intermediate reconciles must not error; drain their events
				// so assertFn only sees events from the final reconcile.
				if i < count-1 {
					require.NoError(t, err, "intermediate reconcile %d should not error", i+1)
					drainEvents(recorder)
					if tc.betweenReconciles != nil {
						tc.betweenReconciles(t, base)
					}
				}
			}

			if tc.wantErr {
				require.Error(t, err)
				if tc.assertFn != nil {
					tc.assertFn(t, base, recorder)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantResult, result)

			if tc.assertFn != nil {
				tc.assertFn(t, base, recorder)
			}
		})
	}
}

func TestIngressServiceIsReady(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{
			name: "ClusterIP service is always ready",
			svc:  &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
			want: true,
		},
		{
			name: "NodePort service is always ready",
			svc:  &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
			want: true,
		},
		{
			name: "LoadBalancer with no ingress is not ready",
			svc:  &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}},
			want: false,
		},
		{
			name: "LoadBalancer with IP is ready",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
					},
				},
			},
			want: true,
		},
		{
			name: "LoadBalancer with hostname is ready",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}},
					},
				},
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ingressServiceIsReady(tc.svc))
		})
	}
}

func TestEnsureServiceReadyCondition(t *testing.T) {
	r := &Reconciler{}

	t.Run("ClusterIP sets ServiceReady=True and reports the ClusterIP", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		svc := &corev1.Service{
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIPs: []string{"10.0.0.1"}},
		}
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, svc))

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.ServiceReadyType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.ServiceReadyReason), cond.Reason)
		assert.Len(t, aigwdp.Status.Addresses, 1)
		assert.Equal(t, aigatewayv1alpha1.PrivateIPAddressSourceType, aigwdp.Status.Addresses[0].SourceType)
	})

	t.Run("LoadBalancer with no ingress sets ServiceReady=False", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		svc := &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}}
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, svc))

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.ServiceReadyType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.WaitingForAddressReason), cond.Reason)
		assert.Empty(t, aigwdp.Status.Addresses)
	})

	t.Run("LoadBalancer with IP sets ServiceReady=True and populates addresses", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		svc := &corev1.Service{
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.5"}},
				},
			},
		}
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, svc))

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.ServiceReadyType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Len(t, aigwdp.Status.Addresses, 1)
		assert.Equal(t, "203.0.113.5", aigwdp.Status.Addresses[0].Value)
		assert.Equal(t, aigatewayv1alpha1.PublicLoadBalancerAddressSourceType, aigwdp.Status.Addresses[0].SourceType)
	})
}

// TestReconciler_KonnectCertificateBlueGreenRotation verifies that when the
// mTLS certificate's content changes (a Secret updated in place, or any other
// cause), the operator registers the new certificate as a SEPARATE Konnect
// entity rather than overwriting the previous one, and keeps the previous
// entity registered for as long as the new one isn't yet Programmed on
// Konnect, so replicas still running with the old certificate (which
// doesn't get reloaded until they're restarted by the rollout) are never left
// presenting a certificate Konnect no longer trusts.
//
// This only exercises the structural guarantee (gated by certProgrammed,
// deterministic regardless of Deployment status bookkeeping). The further
// guarantee, that the old entity survives until the Deployment rollout is
// fully complete and not merely started, depends on the real apiserver's
// automatic metadata.generation bumping on every spec write, which the fake
// client used here does not simulate; that part is covered by
// TestAIGatewayDataPlaneReconciler_KonnectCertificateBlueGreenRotation in
// test/envtest instead.
func TestReconciler_KonnectCertificateBlueGreenRotation(t *testing.T) {
	scheme := managerscheme.Get()
	ctx := t.Context()

	aigwdp := newReconcileAIGWDPManualCertWithControlPlane()
	secretV1 := manualCertSecret(true)

	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(aigwdp, newProgrammedKonnectAIGateway(), secretV1).
		WithStatusSubresource(aigwdp, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}).
		Build()
	recorder := events.NewFakeRecorder(30)
	r := newTestReconciler(base, recorder)

	reconcile := func() {
		t.Helper()
		current := &aigatewayv1alpha1.AIGatewayDataPlane{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, current))
		_, err := r.Reconcile(ctx, current)
		require.NoError(t, err)
		drainEvents(recorder)
	}
	certNameFor := func(secret *corev1.Secret) string {
		return certEntityName(newReconcileAIGWDP(), certificateChecksum(secret))
	}
	certExists := func(name string) bool {
		t.Helper()
		err := base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: name}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{})
		if err != nil && !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
		return err == nil
	}
	markCertProgrammed := func(name string) {
		t.Helper()
		cert := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}
		require.NoError(t, base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: name}, cert))
		cert.Status.Conditions = []metav1.Condition{{
			Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status: metav1.ConditionTrue,
			Reason: "Programmed",
		}}
		require.NoError(t, base.Status().Update(ctx, cert))
	}

	certAName := certNameFor(secretV1)

	// 1st reconcile: registers the cert entity for V1; not yet Programmed, so
	// no Deployment exists yet.
	reconcile()
	assert.True(t, certExists(certAName))
	assert.True(t, apierrors.IsNotFound(
		base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, &appsv1.Deployment{})))

	// 2nd reconcile: cert A Programmed -> Deployment created, mounting secretV1.
	markCertProgrammed(certAName)
	reconcile()
	deploy := &appsv1.Deployment{}
	require.NoError(t, base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, deploy))
	assert.Equal(t, certificateChecksum(secretV1), deploy.Spec.Template.Annotations[pkgconsts.AIGatewayDataPlaneCertificateChecksumAnnotation])

	// Rotate the Secret's content in place (same name, new cert material):
	// exactly the "cert-manager renewed it" scenario the design targets.
	secretV2 := manualCertSecret(true)
	existing := &corev1.Secret{}
	require.NoError(t, base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: manualCertSecretName}, existing))
	existing.Data = secretV2.Data
	require.NoError(t, base.Update(ctx, existing))
	certBName := certNameFor(existing)
	require.NotEqual(t, certAName, certBName, "test secrets must produce different checksums")

	// 3rd reconcile: a new cert entity (B) is registered for V2, but it isn't
	// Programmed yet, so the Deployment must not be touched; cert A must
	// remain fully intact, since replicas still mounting V1 need Konnect to
	// keep trusting it.
	reconcile()
	assert.True(t, certExists(certAName), "old certificate must survive while the new one isn't Programmed yet")
	assert.True(t, certExists(certBName))
	require.NoError(t, base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, deploy))
	assert.Equal(t, certificateChecksum(secretV1), deploy.Spec.Template.Annotations[pkgconsts.AIGatewayDataPlaneCertificateChecksumAnnotation],
		"deployment must not roll to V2 before its certificate is Programmed on Konnect")

	// 4th reconcile: cert B Programmed -> Deployment rolls to V2. Cert A must
	// still exist immediately after this reconcile: cleanup only ever runs
	// once the Deployment's own rollout status confirms completion, and here
	// this is the very reconcile that just changed the spec.
	markCertProgrammed(certBName)
	reconcile()
	require.NoError(t, base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, deploy))
	assert.Equal(t, certificateChecksum(secretV2), deploy.Spec.Template.Annotations[pkgconsts.AIGatewayDataPlaneCertificateChecksumAnnotation])
	assert.True(t, certExists(certBName))
	assert.True(t, certExists(certAName),
		"old certificate must still exist right after the spec changes: the fake Deployment's status is never populated, so rollout is never reported complete")
}
