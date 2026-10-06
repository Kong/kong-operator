/*
Copyright 2025 Kong, Inc.

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
	"time"

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

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	pkgconsts "github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

const (
	testCASecretName      = "test-ca"
	testCASecretNamespace = "test-ns"
	testDPName            = "my-dp"

	reconcileTestNS      = testCASecretNamespace
	reconcileTestDPName  = testDPName
	reconcileTestKEGName = "my-keg"
)

// caSecret builds the cluster CA Secret used across Reconcile tests.
func caSecret() *corev1.Secret {
	return certificate.MustGenerateCASecret(
		testCASecretNamespace,
		testCASecretName,
		"Kong Test CA",
	)
}

// newReconcileEGDP builds the standard KegDataPlane used across Reconcile tests.
func newReconcileEGDP() *eventgatewayv1alpha1.KegDataPlane {
	return &eventgatewayv1alpha1.KegDataPlane{
		Namespace:  reconcileTestNS,
		Name:       reconcileTestDPName,
		UID:        types.UID("egdp-uid"),
		APIVersion: "eventgateway.konghq.com/v1alpha1",
		Kind:       "KegDataPlane",
		Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
			ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
				Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
					Name: reconcileTestKEGName,
				},
			},
		},
	}
}

// newProgrammedKEG builds a KonnectEventGateway with Programmed=True.
func newProgrammedKEG() *konnectv1alpha1.KonnectEventGateway {
	return &konnectv1alpha1.KonnectEventGateway{
		Namespace: reconcileTestNS,
		Name:      reconcileTestKEGName,
		Status: konnectv1alpha1.KonnectEventGatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.NewTime(time.Now()),
				},
			},
			KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
				ServerURL: "https://us.konghq.com",
				ID:        "keg-id-123",
			},
		},
	}
}

// newNotProgrammedKEG builds a KonnectEventGateway with Programmed=False.
func newNotProgrammedKEG() *konnectv1alpha1.KonnectEventGateway {
	keg := newProgrammedKEG()
	keg.Status.Conditions[0].Status = metav1.ConditionFalse
	return keg
}

// newTestReconciler builds a shared reconciler wired to cl and recorder.
// The fake client is wrapped with an interceptor that populates TypeMeta on
// KegDataPlane objects after Get, because the fake client does not set it.
func newTestReconciler(cl client.WithWatch, recorder *events.FakeRecorder) *sharedReconciler {
	wrapped := interceptor.NewClient(cl, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := c.Get(ctx, key, obj, opts...); err != nil {
				return err
			}
			if egdp, ok := obj.(*eventgatewayv1alpha1.KegDataPlane); ok {
				gvks, _, _ := c.Scheme().ObjectKinds(egdp)
				if len(gvks) > 0 {
					egdp.TypeMeta = metav1.TypeMeta{
						APIVersion: gvks[0].GroupVersion().String(),
						Kind:       gvks[0].Kind,
					}
				}
			}
			return nil
		},
	})
	return (&Reconciler{
		Client:                   wrapped,
		TypeConverter:            managedfields.NewDeducedTypeConverter(),
		eventRecorder:            recorder,
		ClusterCASecretName:      testCASecretName,
		ClusterCASecretNamespace: testCASecretNamespace,
		CertTTL:                  pkgconsts.DefaultCertTTL,
	}).base()
}

// getEGDP fetches the fresh KegDataPlane from the fake client.
func getEGDP(t *testing.T, cl client.Client) *eventgatewayv1alpha1.KegDataPlane {
	t.Helper()
	egdp := &eventgatewayv1alpha1.KegDataPlane{}
	err := cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, egdp)
	require.NoError(t, err)
	return egdp
}

// assertCondition checks a named status condition on egdp.
func assertCondition(t *testing.T, egdp *eventgatewayv1alpha1.KegDataPlane, condType consts.ConditionType, wantStatus metav1.ConditionStatus, wantReason consts.ConditionReason) {
	t.Helper()
	cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(condType))
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

const manualCertSecretName = "user-provided-cert"

// manualCertSecret builds a valid or invalid manually-referenced TLS Secret.
func manualCertSecret(valid bool) *corev1.Secret {
	s := &corev1.Secret{
		Namespace: reconcileTestNS, Name: manualCertSecretName,
	}
	if !valid {
		s.Data = map[string][]byte{"tls.crt": []byte("not-a-cert")}
		return s
	}
	// Valid for a year: the expected requeue for a valid certificate is the
	// 24h cap (see maxCertificateRequeue in controller/pkg/dataplane).
	now := time.Now()
	cert, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("user cert"),
		certificate.WithValidity(now.Add(-time.Hour), now.AddDate(1, 0, 0)),
	)
	s.Data = map[string][]byte{"tls.crt": cert, "tls.key": key}
	return s
}

// newReconcileEGDPManualCert builds a KegDataPlane referencing
// manualCertSecretName via Manual provisioning.
func newReconcileEGDPManualCert() *eventgatewayv1alpha1.KegDataPlane {
	egdp := newReconcileEGDP()
	egdp.Spec.CertificateSecret = &eventgatewayv1alpha1.CertificateSecret{
		Provisioning: new(eventgatewayv1alpha1.ManualCertificateProvisioning),
		SecretRef:    &eventgatewayv1alpha1.SecretRef{Name: manualCertSecretName},
	}
	return egdp
}

// certNameFor returns the EventGatewayDataPlaneCertificate name the
// reconciler derives for the given certificate Secret.
func certNameFor(secret *corev1.Secret) string {
	return shareddataplane.CertEntityName(reconcileTestDPName, shareddataplane.CertificateChecksum(secret))
}

// markCertProgrammedForSecret creates (or, if the reconciler already created
// it, marks) a Programmed=True EventGatewayDataPlaneCertificate for secret,
// named exactly as the real reconciler would name it (the name is
// checksum-derived, so it can't be known statically ahead of the Secret
// existing). Safe to call more than once.
func markCertProgrammedForSecret(t *testing.T, cl client.Client, secret *corev1.Secret) {
	t.Helper()

	certName := certNameFor(secret)
	cert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: certName}, cert); err != nil {
		require.True(t, apierrors.IsNotFound(err))
		cert = &configurationv1alpha1.EventGatewayDataPlaneCertificate{
			Namespace: reconcileTestNS, Name: certName,
			Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{
				GatewayRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: reconcileTestKEGName},
				},
				APISpec: configurationv1alpha1.EventGatewayDataPlaneCertificateAPISpec{
					Certificate: configurationv1alpha1.SensitiveDataSource{
						Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
						SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: secret.Name, Key: corev1.TLSCertKey},
					},
					Name: certName,
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

// markGeneratedCertProgrammed marks the EventGatewayDataPlaneCertificate for
// the automatically-generated mTLS certificate Secret as Programmed.
func markGeneratedCertProgrammed(t *testing.T, cl client.Client) {
	t.Helper()

	var secrets corev1.SecretList
	require.NoError(t, cl.List(t.Context(), &secrets,
		client.InNamespace(reconcileTestNS),
		client.MatchingLabels{pkgconsts.SecretKEGDataPlaneCertificateLabel: "true"},
	))
	require.Len(t, secrets.Items, 1, "expected exactly one automatically-generated certificate Secret")
	markCertProgrammedForSecret(t, cl, &secrets.Items[0])
}

// markManualCertProgrammed marks the EventGatewayDataPlaneCertificate for
// the manually-referenced certificate Secret as Programmed.
func markManualCertProgrammed(t *testing.T, cl client.Client) {
	t.Helper()

	secret := &corev1.Secret{}
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
		Namespace: reconcileTestNS, Name: manualCertSecretName,
	}, secret))
	markCertProgrammedForSecret(t, cl, secret)
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
		// wantRequeueAfter, when non-zero, is the RequeueAfter the final
		// reconcile is expected to return (within a few seconds), derived
		// from the user-owned certificate's validity period.
		wantRequeueAfter time.Duration
		wantErr          bool
		// betweenReconciles runs after each intermediate reconcile (i.e. every
		// call except the last), before the next one. Used to seed state that
		// depends on what a previous reconcile actually produced (e.g. marking
		// the real, checksum-named EventGatewayDataPlaneCertificate as
		// Programmed once the certificate Secret exists).
		betweenReconciles func(t *testing.T, cl client.Client)
		// assertFn runs after all reconcile calls to check cluster state.
		assertFn func(t *testing.T, cl client.Client, recorder *events.FakeRecorder)
	}{
		{
			name:       "KegDataPlane not found: no-op",
			objects:    nil,
			wantResult: ctrl.Result{},
		},
		{
			// A missing KonnectEventGateway is an expected, user-fixable state:
			// Reconcile returns no error and the control plane watch re-triggers
			// the reconcile once it appears.
			name: "KonnectEventGateway not found: no error, watch re-triggers, KonnectResolved=False",
			objects: []client.Object{
				newReconcileEGDP(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.KonnectEventGatewayResolvedType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.KonnectEventGatewayNotFoundReason,
				)
			},
		},
		{
			// A not yet Programmed KonnectEventGateway is an expected transient
			// state: Reconcile returns no error and the control plane watch
			// re-triggers the reconcile once it flips Programmed.
			name: "KonnectEventGateway not yet programmed: no error, watch re-triggers, KonnectResolved=False",
			objects: []client.Object{
				newReconcileEGDP(),
				newNotProgrammedKEG(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.KonnectEventGatewayResolvedType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedReason,
				)
			},
		},
		{
			name: "CA secret missing: error returned, CertificateProvisioned=False",
			objects: []client.Object{
				newReconcileEGDP(),
				newProgrammedKEG(),
			},
			wantErr: true,
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.UnableToProvisionReason,
				)
			},
		},
		{
			name: "certificate secret just created: first reconcile returns early, CertificateProvisioned=True",
			objects: []client.Object{
				newReconcileEGDP(),
				newProgrammedKEG(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.KonnectEventGatewayResolvedType,
					metav1.ConditionTrue,
					eventgatewayv1alpha1.KonnectEventGatewayResolvedReason,
				)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionTrue,
					eventgatewayv1alpha1.CertificateProvisionedReason,
				)
			},
		},
		{
			name: "happy path: Deployment and Service created, all conditions set",
			objects: []client.Object{
				newReconcileEGDP(),
				newProgrammedKEG(),
				caSecret(),
			},
			// 1st reconcile: cert Secret created → returns early (owned Secret watch triggers next reconcile).
			// betweenReconciles marks the resulting (checksum-named) EventGatewayDataPlaneCertificate as Programmed.
			// 2nd reconcile: cert Secret + programmed EventGatewayDataPlaneCertificate present → Deployment + Service created.
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
					Namespace: reconcileTestNS, Name: reconcileTestDPName + "-kafka",
				}, svc))

				// All conditions set correctly.
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.KonnectEventGatewayResolvedType,
					metav1.ConditionTrue,
					eventgatewayv1alpha1.KonnectEventGatewayResolvedReason,
				)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionTrue,
					eventgatewayv1alpha1.CertificateProvisionedReason,
				)
				// Ready=False because the fake Deployment's rollout is not complete.
				assertCondition(t, egdp,
					eventgatewayv1alpha1.ReadyType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.WaitingToBecomeReadyReason,
				)

				// Events: 2nd reconcile must emit DeploymentCreated and ServiceCreated.
				events := drainEvents(recorder)
				assert.Contains(t, events, "Normal DeploymentCreated Deployment my-dp created")
				assert.Contains(t, events, "Normal ServiceCreated Kafka Service my-dp-kafka created")
			},
		},
		{
			name: "Manual certificate: referenced secret not found, no error, no Deployment",
			objects: []client.Object{
				newReconcileEGDPManualCert(),
				newProgrammedKEG(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.CertificateSecretRefNotFoundReason,
				)
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, &appsv1.Deployment{})
				assert.True(t, apierrors.IsNotFound(err))
			},
		},
		{
			name: "Manual certificate: referenced secret invalid, no error, no Deployment",
			objects: []client.Object{
				newReconcileEGDPManualCert(),
				newProgrammedKEG(),
				manualCertSecret(false),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.CertificateSecretInvalidReason,
				)
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, &appsv1.Deployment{})
				assert.True(t, apierrors.IsNotFound(err))
			},
		},
		{
			// A certificate that becomes valid on its own needs no error backoff:
			// the reconcile requeues for the moment it is accepted.
			name: "Manual certificate: not yet valid secret, no error, requeued, no Deployment",
			objects: func() []client.Object {
				secret := manualCertSecret(true)
				secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey] = certificate.MustGenerateCertPEMFormat(
					certificate.WithCommonName("user cert"), certificate.WithValidity(time.Now().Add(time.Hour), time.Now().Add(2*time.Hour)),
				)
				return []client.Object{newReconcileEGDPManualCert(), newProgrammedKEG(), secret}
			}(),
			wantResult:       ctrl.Result{},
			wantRequeueAfter: 55 * time.Minute, // NotBefore (+1h) minus the 5m clock skew allowance
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.CertificateSecretInvalidReason,
				)
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, &appsv1.Deployment{})
				assert.True(t, apierrors.IsNotFound(err))
			},
		},
		{
			name: "Manual certificate: operator-provisioned secret is rejected and kept",
			objects: func() []client.Object {
				egdp := newReconcileEGDPManualCert()
				secret := manualCertSecret(true)
				secret.Labels = map[string]string{
					pkgconsts.SecretProvisioningLabelKey:         pkgconsts.SecretProvisioningAutomaticLabelValue,
					pkgconsts.SecretKEGDataPlaneCertificateLabel: "true",
				}
				k8sutils.SetOwnerForObject(secret, egdp)
				return []client.Object{egdp, newProgrammedKEG(), secret}
			}(),
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.CertificateSecretRefOperatorManagedReason,
				)
				err := cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, &appsv1.Deployment{})
				assert.True(t, apierrors.IsNotFound(err))
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: manualCertSecretName,
				}, &corev1.Secret{}), "the operator-provisioned Secret must not be deleted")
			},
		},
		{
			name: "Manual certificate: valid secret, Deployment mounts it directly, no automatic secret",
			objects: []client.Object{
				newReconcileEGDPManualCert(),
				newProgrammedKEG(),
				manualCertSecret(true),
			},
			// 1st reconcile: registers the cert entity, not yet Programmed. 2nd
			// (after betweenReconciles marks it Programmed): Deployment created.
			reconcileCount:    2,
			betweenReconciles: markManualCertProgrammed,
			wantResult:        ctrl.Result{},
			wantRequeueAfter:  24 * time.Hour, // manualCertSecret is valid for a year: capped at 24h
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				egdp := getEGDP(t, cl)
				assertCondition(t, egdp,
					eventgatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionTrue,
					eventgatewayv1alpha1.CertificateProvisionedReason,
				)

				secret := &corev1.Secret{}
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: manualCertSecretName,
				}, secret))

				// The certificate entity references the manual Secret.
				cert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: certNameFor(secret),
				}, cert))
				require.NotNil(t, cert.Spec.APISpec.Certificate.SecretRef)
				assert.Equal(t, manualCertSecretName, cert.Spec.APISpec.Certificate.SecretRef.Name)
				assert.Equal(t, map[string]string{
					pkgconsts.GatewayOperatorManagedByLabel:          pkgconsts.KEGDataPlaneManagedByLabelValue,
					pkgconsts.GatewayOperatorManagedByNameLabel:      reconcileTestDPName,
					pkgconsts.GatewayOperatorManagedByNamespaceLabel: reconcileTestNS,
				}, cert.Labels)

				deploy := &appsv1.Deployment{}
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
					Namespace: reconcileTestNS, Name: reconcileTestDPName,
				}, deploy))
				var certVolume *corev1.Volume
				for i := range deploy.Spec.Template.Spec.Volumes {
					if deploy.Spec.Template.Spec.Volumes[i].Name == KonnectCertVolumeName {
						certVolume = &deploy.Spec.Template.Spec.Volumes[i]
					}
				}
				require.NotNil(t, certVolume)
				require.NotNil(t, certVolume.Secret)
				assert.Equal(t, manualCertSecretName, certVolume.Secret.SecretName)
				assert.Equal(t, shareddataplane.CertificateChecksum(secret),
					deploy.Spec.Template.Annotations[pkgconsts.KEGDataPlaneCertificateChecksumAnnotation])

				// No automatic certificate Secret should have been created.
				secretList := &corev1.SecretList{}
				require.NoError(t, cl.List(t.Context(), secretList, client.InNamespace(reconcileTestNS)))
				assert.Len(t, secretList.Items, 1, "only the manually-referenced Secret should exist")
			},
		},
		{
			name: "idempotency: third reconcile is noop, no create events",
			objects: []client.Object{
				newReconcileEGDP(),
				newProgrammedKEG(),
				caSecret(),
			},
			// 1st: cert Secret created. 2nd: Deployment+Service created. 3rd: everything exists → noop.
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
			egdp := newReconcileEGDP()
			typeConverter := managedfields.NewDeducedTypeConverter()
			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithTypeConverters(typeConverter).
				WithObjects(tc.objects...).
				WithStatusSubresource(egdp, &configurationv1alpha1.EventGatewayDataPlaneCertificate{}).
				Build()

			recorder := events.NewFakeRecorder(30)
			r := newTestReconciler(base, recorder)
			r.TypeConverter = typeConverter

			count := tc.reconcileCount
			if count == 0 {
				count = 1
			}

			var result ctrl.Result
			var err error
			for i := range count {
				current := new(eventgatewayv1alpha1.KegDataPlane)
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
			if tc.wantRequeueAfter != 0 {
				assert.InDelta(t, tc.wantRequeueAfter.Seconds(), result.RequeueAfter.Seconds(), 5)
				result.RequeueAfter = tc.wantResult.RequeueAfter
			}
			assert.Equal(t, tc.wantResult, result)

			if tc.assertFn != nil {
				tc.assertFn(t, base, recorder)
			}
		})
	}
}

// TestReconciler_KonnectCertificateBlueGreenRotation verifies that when the
// manually-referenced mTLS certificate Secret is updated in place, the
// operator registers the new certificate as a SEPARATE
// EventGatewayDataPlaneCertificate rather than overwriting the previous one,
// keeps the previous one registered while the new one isn't Programmed yet,
// and only then rolls the Deployment (via the checksum annotation).
func TestReconciler_KonnectCertificateBlueGreenRotation(t *testing.T) {
	scheme := managerscheme.Get()
	ctx := t.Context()

	egdp := newReconcileEGDPManualCert()
	secretV1 := manualCertSecret(true)

	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(egdp, newProgrammedKEG(), secretV1).
		WithStatusSubresource(egdp, &configurationv1alpha1.EventGatewayDataPlaneCertificate{}).
		Build()
	recorder := events.NewFakeRecorder(30)
	r := newTestReconciler(base, recorder)

	reconcile := func() {
		t.Helper()
		current := &eventgatewayv1alpha1.KegDataPlane{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, current))
		_, err := r.Reconcile(ctx, current)
		require.NoError(t, err)
		drainEvents(recorder)
	}
	certExists := func(name string) bool {
		t.Helper()
		err := base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: name}, &configurationv1alpha1.EventGatewayDataPlaneCertificate{})
		if err != nil && !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
		return err == nil
	}
	deploymentChecksum := func() string {
		t.Helper()
		deploy := &appsv1.Deployment{}
		require.NoError(t, base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, deploy))
		return deploy.Spec.Template.Annotations[pkgconsts.KEGDataPlaneCertificateChecksumAnnotation]
	}

	certAName := certNameFor(secretV1)

	// 1st reconcile: registers the cert entity for V1; not yet Programmed, so
	// no Deployment exists yet.
	reconcile()
	assert.True(t, certExists(certAName))
	assert.True(t, apierrors.IsNotFound(
		base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName}, &appsv1.Deployment{})))

	// 2nd reconcile: cert A Programmed -> Deployment created, mounting secretV1.
	markCertProgrammedForSecret(t, base, secretV1)
	reconcile()
	assert.Equal(t, shareddataplane.CertificateChecksum(secretV1), deploymentChecksum())

	// Rotate the Secret's content in place (same name, new cert material).
	existing := &corev1.Secret{}
	require.NoError(t, base.Get(ctx, types.NamespacedName{Namespace: reconcileTestNS, Name: manualCertSecretName}, existing))
	existing.Data = manualCertSecret(true).Data
	require.NoError(t, base.Update(ctx, existing))
	certBName := certNameFor(existing)
	require.NotEqual(t, certAName, certBName, "test secrets must produce different checksums")

	// 3rd reconcile: cert B registered but not Programmed: the Deployment
	// must not roll yet, and cert A must remain.
	reconcile()
	assert.True(t, certExists(certAName), "old certificate must survive while the new one isn't Programmed yet")
	assert.True(t, certExists(certBName))
	assert.Equal(t, shareddataplane.CertificateChecksum(secretV1), deploymentChecksum(),
		"deployment must not roll to V2 before its certificate is Programmed on Konnect")

	// 4th reconcile: cert B Programmed -> Deployment rolls to V2. Cert A is
	// kept until the rollout is reported complete, which the fake client
	// never does.
	markCertProgrammedForSecret(t, base, existing)
	reconcile()
	assert.Equal(t, shareddataplane.CertificateChecksum(existing), deploymentChecksum())
	assert.True(t, certExists(certBName))
	assert.True(t, certExists(certAName))
}
