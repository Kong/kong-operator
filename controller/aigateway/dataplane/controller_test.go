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

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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
			ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
				Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
					Name: reconcileTestAIGWCPName,
				},
			},
		},
	}
}

// newProgrammedAIGWCP builds an AIGatewayControlPlane with Programmed=True and endpoints set.
func newProgrammedAIGWCP() *konnectv1alpha1.AIGatewayControlPlane {
	return &konnectv1alpha1.AIGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: reconcileTestNS,
			Name:      reconcileTestAIGWCPName,
		},
		Status: konnectv1alpha1.AIGatewayControlPlaneStatus{
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: metav1.ConditionTrue,
					Reason: "Programmed",
				},
			},
			Endpoints: &konnectv1alpha1.AIGatewayControlPlaneEndpoints{
				Configuration: "cp.example.com",
				Telemetry:     "tp.example.com",
			},
		},
	}
}

// newNotProgrammedAIGWCP builds an AIGatewayControlPlane with Programmed=False.
func newNotProgrammedAIGWCP() *konnectv1alpha1.AIGatewayControlPlane {
	aigwcp := newProgrammedAIGWCP()
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

// newProgrammedKonnectCert builds an AIGatewayDataPlaneCertificate with Programmed=True,
// modelling the state after the Konnect controller has registered it.
func newProgrammedKonnectCert() *configurationv1alpha1.AIGatewayDataPlaneCertificate {
	return &configurationv1alpha1.AIGatewayDataPlaneCertificate{
		ObjectMeta: metav1.ObjectMeta{Namespace: reconcileTestNS, Name: reconcileTestDPName},
		Spec: configurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: reconcileTestAIGWCPName},
			},
			APISpec: configurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
				Cert: configurationv1alpha1.SensitiveDataSource{
					Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: reconcileTestDPName, Key: corev1.TLSCertKey},
				},
				Title: reconcileTestDPName,
			},
		},
		Status: configurationv1alpha1.AIGatewayDataPlaneCertificateStatus{
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: metav1.ConditionTrue,
					Reason: "Programmed",
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
			name:       "AIGatewayDataPlane not found: no-op",
			objects:    nil,
			wantResult: ctrl.Result{},
		},
		{
			name: "AIGatewayControlPlane not found: error returned (runtime handles backoff), AIGatewayControlPlaneResolved=False",
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
					aigatewayv1alpha1.AIGatewayControlPlaneResolvedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.AIGatewayControlPlaneNotFoundReason,
				)
			},
		},
		{
			name: "AIGatewayControlPlane not yet programmed: error returned (runtime handles backoff), AIGatewayControlPlaneResolved=False",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newNotProgrammedAIGWCP(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			wantErr:    true,
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.AIGatewayControlPlaneResolvedType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.AIGatewayControlPlaneNotProgrammedReason,
				)
			},
		},
		{
			name: "CA secret missing: error returned, CertificateProvisioned=False",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newProgrammedAIGWCP(),
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
				newProgrammedAIGWCP(),
				caSecret(),
			},
			wantResult: ctrl.Result{},
			assertFn: func(t *testing.T, cl client.Client, _ *events.FakeRecorder) {
				t.Helper()
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.AIGatewayControlPlaneResolvedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.AIGatewayControlPlaneResolvedReason,
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
				newProgrammedAIGWCP(),
				caSecret(),
				newProgrammedKonnectCert(),
			},
			// 1st reconcile: cert Secret created → returns early (owned Secret watch triggers next reconcile).
			// 2nd reconcile: cert Secret + programmed AIGatewayDataPlaneCertificate present → Deployment + Service created.
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
					Namespace: reconcileTestNS, Name: reconcileTestDPName + "-ingress",
				}, svc))

				// All conditions set correctly.
				aigwdp := getAIGWDP(t, cl)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.AIGatewayControlPlaneResolvedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.AIGatewayControlPlaneResolvedReason,
				)
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.CertificateProvisionedType,
					metav1.ConditionTrue,
					aigatewayv1alpha1.CertificateProvisionedReason,
				)
				// Ready=False because the fake Deployment has no ready replicas yet.
				assertCondition(t, aigwdp,
					aigatewayv1alpha1.ReadyType,
					metav1.ConditionFalse,
					aigatewayv1alpha1.DependenciesNotReadyReason,
				)

				// Events: 2nd reconcile must emit DeploymentCreated and ServiceCreated.
				events := drainEvents(recorder)
				assert.Contains(t, events, "Normal DeploymentCreated Deployment my-dp created")
				assert.Contains(t, events, "Normal ServiceCreated Ingress Service my-dp-ingress created")
			},
		},
		{
			name: "idempotency: third reconcile is noop, no create events",
			objects: []client.Object{
				newReconcileAIGWDP(),
				newProgrammedAIGWCP(),
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
			aigwdp := newReconcileAIGWDP()
			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.objects...).
				WithStatusSubresource(aigwdp, &configurationv1alpha1.AIGatewayDataPlaneCertificate{}).
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
