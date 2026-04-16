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
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const (
	testKonnectGatewayID = "keg-konnect-id-abc"
	testCertSecretName   = "egdp-cert-secret"
)

func newTestEGDP() *eventgatewayv1alpha1.KegDataPlane {
	return &eventgatewayv1alpha1.KegDataPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "eventgateway.konghq.com/v1alpha1",
			Kind:       "KegDataPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-dp",
			Namespace:  "default",
			UID:        types.UID("egdp-uid-123"),
			Generation: 1,
		},
	}
}

func newTestKEG() *konnectv1alpha1.KonnectEventControlPlane {
	return &konnectv1alpha1.KonnectEventControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-keg",
			Namespace: "default",
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
				ID:        testKonnectGatewayID,
			},
		},
	}
}

func newTestCertSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testCertSecretName,
			Namespace: "default",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("---- BEGIN CERTIFICATE ----"),
			"tls.key": []byte("---- BEGIN KEY ----"),
		},
	}
}

func TestEnsureKonnectCertificate(t *testing.T) {
	oldID := "old-gateway-id"
	programmedGatewayID := testKonnectGatewayID
	secretRefType := konnectv1alpha1.SensitiveDataSourceTypeSecretRef

	tests := []struct {
		name         string
		extraObjs    []client.Object
		interceptors interceptor.Funcs
		// preCall performs a first call before the one under test (used for the noop scenario).
		preCall         bool
		wantProgrammed  bool
		wantErrContains string
		wantCondStatus  metav1.ConditionStatus
		wantCondReason  string
		verifyCert      func(t *testing.T, cert konnectv1alpha1.KonnectEventDataPlaneCertificate)
	}{
		{
			name:           "creates cert when none exists",
			wantProgrammed: false,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: string(eventgatewayv1alpha1.KonnectCertificateNotProgrammedReason),
			verifyCert: func(t *testing.T, cert konnectv1alpha1.KonnectEventDataPlaneCertificate) {
				t.Helper()
				assert.Equal(t, commonv1alpha1.ObjectRefTypeKonnectID, cert.Spec.GatewayRef.Type)
				require.NotNil(t, cert.Spec.GatewayRef.KonnectID)
				assert.Equal(t, testKonnectGatewayID, *cert.Spec.GatewayRef.KonnectID)
				require.NotNil(t, cert.Spec.Type)
				assert.Equal(t, konnectv1alpha1.SensitiveDataSourceTypeSecretRef, *cert.Spec.Type)
				require.NotNil(t, cert.Spec.SecretRef)
				assert.Equal(t, testCertSecretName, cert.Spec.SecretRef.Name)
				require.Len(t, cert.OwnerReferences, 1)
				assert.Equal(t, "test-dp", cert.OwnerReferences[0].Name)
				assert.Equal(t, types.UID("egdp-uid-123"), cert.OwnerReferences[0].UID)
				assert.True(t, *cert.OwnerReferences[0].Controller)
			},
		},
		{
			name:           "noop on second call with same inputs",
			preCall:        true,
			wantProgrammed: false,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: string(eventgatewayv1alpha1.KonnectCertificateNotProgrammedReason),
		},
		{
			name: "updates cert when gateway ID changes",
			extraObjs: []client.Object{
				&konnectv1alpha1.KonnectEventDataPlaneCertificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: konnectv1alpha1.GroupVersion.String(),
						Kind:       "KonnectEventDataPlaneCertificate",
					},
					ObjectMeta: metav1.ObjectMeta{Name: "test-dp", Namespace: "default"},
					Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type:      commonv1alpha1.ObjectRefTypeKonnectID,
							KonnectID: &oldID,
						},
						Type:      &secretRefType,
						SecretRef: &commonv1alpha1.NamespacedRef{Name: testCertSecretName},
					},
				},
			},
			wantProgrammed: false,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: string(eventgatewayv1alpha1.KonnectCertificateNotProgrammedReason),
			verifyCert: func(t *testing.T, cert konnectv1alpha1.KonnectEventDataPlaneCertificate) {
				t.Helper()
				require.NotNil(t, cert.Spec.GatewayRef.KonnectID)
				assert.Equal(t, testKonnectGatewayID, *cert.Spec.GatewayRef.KonnectID)
			},
		},
		{
			name: "cert already programmed by Konnect sets KonnectCertificateRegistered=True", extraObjs: []client.Object{
				&konnectv1alpha1.KonnectEventDataPlaneCertificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: konnectv1alpha1.GroupVersion.String(),
						Kind:       "KonnectEventDataPlaneCertificate",
					},
					ObjectMeta: metav1.ObjectMeta{Name: "test-dp", Namespace: "default"},
					Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type:      commonv1alpha1.ObjectRefTypeKonnectID,
							KonnectID: &programmedGatewayID,
						},
						Type:      &secretRefType,
						SecretRef: &commonv1alpha1.NamespacedRef{Name: testCertSecretName},
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
				},
			},
			wantProgrammed: true,
			wantCondStatus: metav1.ConditionTrue,
			wantCondReason: string(eventgatewayv1alpha1.KonnectCertificateRegisteredReason),
		},
		{
			name: "sets failed condition and returns error on client error",
			interceptors: interceptor.Funcs{
				Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
					return assert.AnError
				},
			},
			wantProgrammed:  false,
			wantErrContains: "failed to apply KonnectEventDataPlaneCertificate",
			wantCondStatus:  metav1.ConditionFalse,
			wantCondReason:  string(eventgatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
		},
		{
			name: "cert exists with Programmed=False: returns not-programmed condition",
			extraObjs: []client.Object{
				&konnectv1alpha1.KonnectEventDataPlaneCertificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: konnectv1alpha1.GroupVersion.String(),
						Kind:       "KonnectEventDataPlaneCertificate",
					},
					ObjectMeta: metav1.ObjectMeta{Name: "test-dp", Namespace: "default"},
					Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type:      commonv1alpha1.ObjectRefTypeKonnectID,
							KonnectID: &programmedGatewayID,
						},
						Type:      &secretRefType,
						SecretRef: &commonv1alpha1.NamespacedRef{Name: testCertSecretName},
					},
					Status: konnectv1alpha1.KonnectEventDataPlaneCertificateStatus{
						Conditions: []metav1.Condition{
							{
								Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
								Status:             metav1.ConditionFalse,
								Reason:             "Pending",
								LastTransitionTime: metav1.NewTime(time.Now()),
							},
						},
					},
				},
			},
			wantProgrammed: false,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: string(eventgatewayv1alpha1.KonnectCertificateNotProgrammedReason),
		},
		{
			name: "Get error after apply: sets RegistrationFailed condition and returns error",
			interceptors: interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*konnectv1alpha1.KonnectEventDataPlaneCertificate); ok {
						return assert.AnError
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantProgrammed:  false,
			wantErrContains: "failed to get KonnectEventDataPlaneCertificate",
			wantCondStatus:  metav1.ConditionFalse,
			wantCondReason:  string(eventgatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh objects per subtest to avoid state bleed between cases.
			egdp := newTestEGDP()
			keg := newTestKEG()
			certSecret := newTestCertSecret()

			objs := append([]client.Object{egdp}, tc.extraObjs...)
			base := fake.NewClientBuilder().
				WithScheme(managerscheme.Get()).
				WithStatusSubresource(&konnectv1alpha1.KonnectEventDataPlaneCertificate{}).
				WithObjects(objs...).
				Build()
			cl := interceptor.NewClient(base, tc.interceptors)

			r := &Reconciler{
				Client:        cl,
				typeConverter: managedfields.NewDeducedTypeConverter(),
				eventRecorder: events.NewFakeRecorder(10),
			}

			if tc.preCall {
				_, err := r.ensureKonnectCertificate(t.Context(), logr.Discard(), egdp, keg, certSecret)
				require.NoError(t, err)
			}

			programmed, err := r.ensureKonnectCertificate(t.Context(), logr.Discard(), egdp, keg, certSecret)

			if tc.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantProgrammed, programmed)

			cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.KonnectCertificateRegisteredType))
			require.NotNil(t, cond)
			assert.Equal(t, tc.wantCondStatus, cond.Status)
			assert.Equal(t, tc.wantCondReason, cond.Reason)

			if tc.verifyCert != nil {
				var cert konnectv1alpha1.KonnectEventDataPlaneCertificate
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: egdp.Name, Namespace: egdp.Namespace}, &cert))
				tc.verifyCert(t, cert)
			}
		})
	}
}
