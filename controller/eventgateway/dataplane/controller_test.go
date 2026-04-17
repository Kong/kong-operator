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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	pkgconsts "github.com/kong/kong-operator/v2/pkg/consts"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

const (
	reconcileTestNS      = testCASecretNamespace
	reconcileTestDPName  = testDPName
	reconcileTestKEGName = "my-keg"
)

// newReconcileEGDP builds the standard KegDataPlane used across Reconcile tests.
func newReconcileEGDP() *eventgatewayv1alpha1.KegDataPlane {
	return &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: reconcileTestNS,
			Name:      reconcileTestDPName,
			UID:       types.UID("egdp-uid"),
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "eventgateway.konghq.com/v1alpha1",
			Kind:       "KegDataPlane",
		},
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

// newProgrammedKEG builds a KonnectEventControlPlane with Programmed=True.
func newProgrammedKEG() *konnectv1alpha1.KonnectEventControlPlane {
	return &konnectv1alpha1.KonnectEventControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: reconcileTestNS,
			Name:      reconcileTestKEGName,
		},
		Status: konnectv1alpha1.KonnectEventControlPlaneStatus{
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

// newNotProgrammedKEG builds a KonnectEventControlPlane with Programmed=False.
func newNotProgrammedKEG() *konnectv1alpha1.KonnectEventControlPlane {
	keg := newProgrammedKEG()
	keg.Status.Conditions[0].Status = metav1.ConditionFalse
	return keg
}

// newTestReconciler builds a Reconciler wired to cl and recorder.
// The fake client is wrapped with an interceptor that populates TypeMeta on
// KegDataPlane objects after Get, because the fake client does not set it.
func newTestReconciler(cl client.WithWatch, recorder *events.FakeRecorder) *Reconciler {
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
	return &Reconciler{
		Client:                   wrapped,
		typeConverter:            managedfields.NewDeducedTypeConverter(),
		eventRecorder:            recorder,
		ClusterCASecretName:      testCASecretName,
		ClusterCASecretNamespace: testCASecretNamespace,
		CertTTL:                  pkgconsts.DefaultCertTTL,
	}
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

// newProgrammedKonnectCert builds a KonnectEventDataPlaneCertificate with Programmed=True,
// modelling the state after the Konnect controller has registered it.
func newProgrammedKonnectCert() *konnectv1alpha1.KonnectEventDataPlaneCertificate {
	gatewayID := "keg-id-123"
	secretRefType := konnectv1alpha1.SensitiveDataSourceTypeSecretRef
	return &konnectv1alpha1.KonnectEventDataPlaneCertificate{
		ObjectMeta: metav1.ObjectMeta{Namespace: reconcileTestNS, Name: reconcileTestDPName},
		Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: &gatewayID,
			},
			Type:      &secretRefType,
			SecretRef: &commonv1alpha1.NamespacedRef{Name: reconcileTestDPName},
		},
		Status: konnectv1alpha1.KonnectEventDataPlaneCertificateStatus{
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.NewTime(time.Now()),
				},
			},
		},
	}
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
		// assertFn runs after all reconcile calls to check cluster state.
		assertFn func(t *testing.T, cl client.Client, recorder *events.FakeRecorder)
	}{
		{
			name:       "KegDataPlane not found: no-op",
			objects:    nil,
			wantResult: ctrl.Result{},
		},
		{
			name: "KonnectEventControlPlane not found: error returned (runtime handles backoff), KonnectResolved=False",
			objects: []client.Object{
				newReconcileEGDP(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			wantErr:    true,
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
			name: "KonnectEventControlPlane not yet programmed: error returned (runtime handles backoff), KonnectResolved=False",
			objects: []client.Object{
				newReconcileEGDP(),
				newNotProgrammedKEG(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			wantErr:    true,
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
				newProgrammedKonnectCert(),
			},
			// 1st reconcile: cert Secret created → returns early (owned Secret watch triggers next reconcile).
			// 2nd reconcile: cert Secret + programmed KonnectEventDataPlaneCertificate present → Deployment + Service created.
			reconcileCount: 2,
			wantResult:     ctrl.Result{},
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
				// Ready=False because the fake Deployment has no ready replicas yet.
				assertCondition(t, egdp,
					eventgatewayv1alpha1.ReadyType,
					metav1.ConditionFalse,
					eventgatewayv1alpha1.DependenciesNotReadyReason,
				)

				// Events: 2nd reconcile must emit DeploymentCreated and ServiceCreated.
				events := drainEvents(recorder)
				assert.Contains(t, events, "Normal DeploymentCreated Deployment my-dp created")
				assert.Contains(t, events, "Normal ServiceCreated Kafka Service my-dp-kafka created")
			},
		},
		{
			name: "idempotency: third reconcile is noop, no create events",
			objects: []client.Object{
				newReconcileEGDP(),
				newProgrammedKEG(),
				caSecret(),
				newProgrammedKonnectCert(),
			},
			// 1st: cert Secret created. 2nd: Deployment+Service created. 3rd: everything exists → noop.
			reconcileCount: 3,
			wantResult:     ctrl.Result{},
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
			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.objects...).
				WithStatusSubresource(egdp, &konnectv1alpha1.KonnectEventDataPlaneCertificate{}).
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
				result, err = r.Reconcile(t.Context(), reconcile.Request{
					NamespacedName: types.NamespacedName{Namespace: reconcileTestNS, Name: reconcileTestDPName},
				})
				// All intermediate reconciles must not error; drain their events
				// so assertFn only sees events from the final reconcile.
				if i < count-1 {
					require.NoError(t, err, "intermediate reconcile %d should not error", i+1)
					drainEvents(recorder)
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
