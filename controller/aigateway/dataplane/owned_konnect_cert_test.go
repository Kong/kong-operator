package dataplane

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
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

func Test_certEntityName(t *testing.T) {
	aigwdp := newTestAIGWDP()

	name1 := certEntityName(aigwdp, "abcdef1234567890")
	name2 := certEntityName(aigwdp, "1234567890abcdef")

	assert.NotEqual(t, name1, name2, "different checksums must produce different names")
	assert.Equal(t, name1, certEntityName(aigwdp, "abcdef1234567890"), "must be deterministic for the same inputs")
	assert.Contains(t, name1, aigwdp.Name, "name must remain traceable to the owning AIGatewayDataPlane")
}

func TestEnsureKonnectCertificate(t *testing.T) {
	certName := certEntityName(newTestAIGWDP(), certificateChecksum(newTestCertSecret()))

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
		verifyCert      func(t *testing.T, cert aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate)
	}{
		{
			name:           "creates cert when none exists",
			wantProgrammed: false,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: string(aigatewayv1alpha1.KonnectCertificateNotProgrammedReason),
			verifyCert: func(t *testing.T, cert aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate) {
				t.Helper()
				assert.Equal(t, commonv1alpha1.ObjectRefTypeNamespacedRef, cert.Spec.AIGatewayRef.Type)
				require.NotNil(t, cert.Spec.AIGatewayRef.NamespacedRef)
				assert.Equal(t, "test-aigwcp", cert.Spec.AIGatewayRef.NamespacedRef.Name)
				assert.Nil(t, cert.Spec.AIGatewayRef.KonnectID)
				assert.Equal(t, aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef, cert.Spec.APISpec.Cert.Type)
				require.NotNil(t, cert.Spec.APISpec.Cert.SecretRef)
				assert.Equal(t, testCertSecretName, cert.Spec.APISpec.Cert.SecretRef.Name)
				// Title must be checksum-derived, not just the CR's own K8s name: Konnect
				// enforces a "unique-certificate-per-entity" constraint scoped to
				// (gateway, title). A constant Title across rotations would make every
				// subsequent create collide with the previous one, silently reusing
				// (and never updating) the original Konnect-side certificate.
				assert.Equal(t, certName, cert.Spec.APISpec.Title)
				require.Len(t, cert.OwnerReferences, 1)
				assert.Equal(t, "test-dp", cert.OwnerReferences[0].Name)
				assert.Equal(t, types.UID("aigwdp-uid-123"), cert.OwnerReferences[0].UID)
				assert.True(t, *cert.OwnerReferences[0].Controller)
				assert.Equal(t, "test-dp", cert.Labels[consts.GatewayOperatorManagedByNameLabel])
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
				&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
						Kind:       "AIGatewayDataPlaneCertificate",
					},
					ObjectMeta: metav1.ObjectMeta{Name: certName, Namespace: "default"},
					Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
						AIGatewayRef: commonv1alpha1.ObjectRef{
							Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "test-aigwcp"},
						},
						APISpec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
							Cert: aiconfigurationv1alpha1.SensitiveDataSource{
								Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
								SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: testCertSecretName, Key: corev1.TLSCertKey},
							},
							Title: "test-dp",
						},
					},
					Status: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateStatus{
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
				&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
						Kind:       "AIGatewayDataPlaneCertificate",
					},
					ObjectMeta: metav1.ObjectMeta{Name: certName, Namespace: "default"},
					Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
						AIGatewayRef: commonv1alpha1.ObjectRef{
							Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "test-aigwcp"},
						},
						APISpec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
							Cert: aiconfigurationv1alpha1.SensitiveDataSource{
								Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
								SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: testCertSecretName, Key: corev1.TLSCertKey},
							},
							Title: "test-dp",
						},
					},
					Status: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateStatus{
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
					if _, ok := obj.(*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate); ok {
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
				WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}).
				WithObjects(objs...).
				Build()
			cl := interceptor.NewClient(base, tc.interceptors)

			r := &Reconciler{
				Client:        cl,
				TypeConverter: managedfields.NewDeducedTypeConverter(),
				eventRecorder: events.NewFakeRecorder(10),
			}

			if tc.preCall {
				_, err := r.ensureKonnectCertificate(t.Context(), logr.Discard(), aigwdp, aigwcp, certSecret, certificateChecksum(certSecret))
				require.NoError(t, err)
			}

			programmed, err := r.ensureKonnectCertificate(t.Context(), logr.Discard(), aigwdp, aigwcp, certSecret, certificateChecksum(certSecret))

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
				var cert aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate
				require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: certName, Namespace: aigwdp.Namespace}, &cert))
				tc.verifyCert(t, cert)
			}
		})
	}
}

// managedCert builds an AIGatewayDataPlaneCertificate CR carrying the same
// managed-by labels ensureKonnectCertificate sets, so it's discoverable by
// cleanupStaleKonnectCertificates' List call.
func managedCert(name string, aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
	return &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: aigwdp.Namespace,
			Labels:    selectorLabelsForAIGatewayDataPlane(aigwdp),
		},
	}
}

func Test_cleanupStaleKonnectCertificates(t *testing.T) {
	aigwdp := newTestAIGWDP()

	t.Run("deletes every managed cert except the current one", func(t *testing.T) {
		current := managedCert("test-dp-current", aigwdp)
		stale1 := managedCert("test-dp-stale1", aigwdp)
		stale2 := managedCert("test-dp-stale2", aigwdp)
		// A cert belonging to a different AIGatewayDataPlane must never be touched.
		otherDPCert := managedCert("other-dp-current", &aigatewayv1alpha1.AIGatewayDataPlane{
			ObjectMeta: metav1.ObjectMeta{Name: "other-dp", Namespace: "default"},
		})

		cl := fake.NewClientBuilder().
			WithScheme(managerscheme.Get()).
			WithObjects(current, stale1, stale2, otherDPCert).
			Build()
		r := &Reconciler{Client: cl, eventRecorder: events.NewFakeRecorder(10)}

		err := r.cleanupStaleKonnectCertificates(t.Context(), logr.Discard(), aigwdp, current.Name)
		require.NoError(t, err)

		assert.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: current.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}),
			"current cert must survive")
		assert.True(t, apierrors.IsNotFound(cl.Get(t.Context(), types.NamespacedName{Name: stale1.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{})),
			"stale1 must be deleted")
		assert.True(t, apierrors.IsNotFound(cl.Get(t.Context(), types.NamespacedName{Name: stale2.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{})),
			"stale2 must be deleted")
		assert.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: otherDPCert.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}),
			"cert belonging to a different AIGatewayDataPlane must not be touched")
	})

	t.Run("no-op when only the current cert exists", func(t *testing.T) {
		current := managedCert("test-dp-current", aigwdp)
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(current).Build()
		r := &Reconciler{Client: cl, eventRecorder: events.NewFakeRecorder(10)}

		err := r.cleanupStaleKonnectCertificates(t.Context(), logr.Discard(), aigwdp, current.Name)
		require.NoError(t, err)

		assert.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: current.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}))
	})

	t.Run("List error is propagated", func(t *testing.T) {
		base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return assert.AnError
			},
		})
		r := &Reconciler{Client: cl, eventRecorder: events.NewFakeRecorder(10)}

		err := r.cleanupStaleKonnectCertificates(t.Context(), logr.Discard(), aigwdp, "test-dp-current")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list AIGatewayDataPlaneCertificates")
	})

	t.Run("non-NotFound Delete error is propagated", func(t *testing.T) {
		stale := managedCert("test-dp-stale", aigwdp)
		base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(stale).Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return assert.AnError
			},
		})
		r := &Reconciler{Client: cl, eventRecorder: events.NewFakeRecorder(10)}

		err := r.cleanupStaleKonnectCertificates(t.Context(), logr.Discard(), aigwdp, "test-dp-current")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete stale AIGatewayDataPlaneCertificate")
	})
}
