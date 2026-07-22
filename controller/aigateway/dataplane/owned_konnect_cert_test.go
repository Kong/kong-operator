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

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const (
	testCertSecretName = "aigwdp-cert-secret"
)

func newTestAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "aigateway.konghq.com/v1alpha1",
			Kind:       "AIGatewayDataPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-dp",
			Namespace:  "default",
			UID:        types.UID("aigwdp-uid-123"),
			Generation: 1,
		},
	}
}

func newTestAIGWCP() *konnectv1alpha1.KonnectAIGateway {
	return &konnectv1alpha1.KonnectAIGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-aigwcp",
			Namespace: "default",
		},
		Status: konnectv1alpha1.KonnectAIGatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.NewTime(time.Now()),
				},
			},
			Endpoints: &konnectv1alpha1.KonnectAIGatewayEndpoints{
				Configuration: "cp.example.com",
				Telemetry:     "tp.example.com",
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
		verifyCert      func(t *testing.T, cert configurationv1alpha1.AIGatewayDataPlaneCertificate)
	}{
		{
			name:           "creates cert when none exists",
			wantProgrammed: false,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: string(aigatewayv1alpha1.KonnectCertificateNotProgrammedReason),
			verifyCert: func(t *testing.T, cert configurationv1alpha1.AIGatewayDataPlaneCertificate) {
				t.Helper()
				assert.Equal(t, commonv1alpha1.ObjectRefTypeNamespacedRef, cert.Spec.AIGatewayRef.Type)
				require.NotNil(t, cert.Spec.AIGatewayRef.NamespacedRef)
				assert.Equal(t, "test-aigwcp", cert.Spec.AIGatewayRef.NamespacedRef.Name)
				assert.Nil(t, cert.Spec.AIGatewayRef.KonnectID)
				assert.Equal(t, configurationv1alpha1.SensitiveDataSourceTypeSecretRef, cert.Spec.APISpec.Cert.Type)
				require.NotNil(t, cert.Spec.APISpec.Cert.SecretRef)
				assert.Equal(t, testCertSecretName, cert.Spec.APISpec.Cert.SecretRef.Name)
				assert.Equal(t, "test-dp", cert.Spec.APISpec.Title)
				require.Len(t, cert.OwnerReferences, 1)
				assert.Equal(t, "test-dp", cert.OwnerReferences[0].Name)
				assert.Equal(t, types.UID("aigwdp-uid-123"), cert.OwnerReferences[0].UID)
				assert.True(t, *cert.OwnerReferences[0].Controller)
			},
		},
		{
			name:           "noop on second call with same inputs",
			preCall:        true,
			wantProgrammed: false,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: string(aigatewayv1alpha1.KonnectCertificateNotProgrammedReason),
		},
		{
			name: "cert already programmed by Konnect sets KonnectCertificateRegistered=True",
			extraObjs: []client.Object{
				&configurationv1alpha1.AIGatewayDataPlaneCertificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "AIGatewayDataPlaneCertificate",
					},
					ObjectMeta: metav1.ObjectMeta{Name: "test-dp", Namespace: "default"},
					Spec: configurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
						AIGatewayRef: commonv1alpha1.ObjectRef{
							Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "test-aigwcp"},
						},
						APISpec: configurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
							Cert: configurationv1alpha1.SensitiveDataSource{
								Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
								SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: testCertSecretName, Key: corev1.TLSCertKey},
							},
							Title: "test-dp",
						},
					},
					Status: configurationv1alpha1.AIGatewayDataPlaneCertificateStatus{
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
			wantCondReason: string(aigatewayv1alpha1.KonnectCertificateRegisteredReason),
		},
		{
			name: "sets failed condition and returns error on client error",
			interceptors: interceptor.Funcs{
				Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
					return assert.AnError
				},
			},
			wantProgrammed:  false,
			wantErrContains: "failed to apply AIGatewayDataPlaneCertificate",
			wantCondStatus:  metav1.ConditionFalse,
			wantCondReason:  string(aigatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
		},
		{
			name: "cert exists with Programmed=False: returns not-programmed condition",
			extraObjs: []client.Object{
				&configurationv1alpha1.AIGatewayDataPlaneCertificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "AIGatewayDataPlaneCertificate",
					},
					ObjectMeta: metav1.ObjectMeta{Name: "test-dp", Namespace: "default"},
					Spec: configurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
						AIGatewayRef: commonv1alpha1.ObjectRef{
							Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "test-aigwcp"},
						},
						APISpec: configurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
							Cert: configurationv1alpha1.SensitiveDataSource{
								Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
								SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: testCertSecretName, Key: corev1.TLSCertKey},
							},
							Title: "test-dp",
						},
					},
					Status: configurationv1alpha1.AIGatewayDataPlaneCertificateStatus{
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
			wantCondReason: string(aigatewayv1alpha1.KonnectCertificateNotProgrammedReason),
		},
		{
			name: "Get error after apply: sets RegistrationFailed condition and returns error",
			interceptors: interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.AIGatewayDataPlaneCertificate); ok {
						return assert.AnError
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantProgrammed:  false,
			wantErrContains: "failed to get AIGatewayDataPlaneCertificate",
			wantCondStatus:  metav1.ConditionFalse,
			wantCondReason:  string(aigatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh objects per subtest to avoid state bleed between cases.
			aigwdp := newTestAIGWDP()
			aigwcp := newTestAIGWCP()
			certSecret := newTestCertSecret()

			objs := append([]client.Object{aigwdp}, tc.extraObjs...)
			base := fake.NewClientBuilder().
				WithScheme(managerscheme.Get()).
				WithStatusSubresource(&configurationv1alpha1.AIGatewayDataPlaneCertificate{}).
				WithObjects(objs...).
				Build()
			cl := interceptor.NewClient(base, tc.interceptors)

			r := &Reconciler{
				Client:        cl,
				TypeConverter: managedfields.NewDeducedTypeConverter(),
				eventRecorder: events.NewFakeRecorder(10),
			}

			if tc.preCall {
				_, err := r.ensureKonnectCertificate(t.Context(), logr.Discard(), aigwdp, aigwcp, certSecret)
				require.NoError(t, err)
			}

			programmed, err := r.ensureKonnectCertificate(t.Context(), logr.Discard(), aigwdp, aigwcp, certSecret)

			if tc.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantProgrammed, programmed)

			cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectCertificateRegisteredType))
			require.NotNil(t, cond)
			assert.Equal(t, tc.wantCondStatus, cond.Status)
			assert.Equal(t, tc.wantCondReason, cond.Reason)

			if tc.verifyCert != nil {
				var cert configurationv1alpha1.AIGatewayDataPlaneCertificate
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: aigwdp.Name, Namespace: aigwdp.Namespace}, &cert))
				tc.verifyCert(t, cert)
			}
		})
	}
}
