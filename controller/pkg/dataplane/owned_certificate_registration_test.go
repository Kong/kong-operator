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
		APIVersion: "aigateway.konghq.com/v1alpha1",
		Kind:       "AIGatewayDataPlane",
		Name:       "test-dp",
		Namespace:  "default",
		UID:        types.UID("aigwdp-uid-123"),
		Generation: 1,
	}
}

func newTestAIGWCP() *konnectv1alpha1.KonnectAIGateway {
	return &konnectv1alpha1.KonnectAIGateway{
		Name:      "test-aigwcp",
		Namespace: "default",
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
		Name:      testCertSecretName,
		Namespace: "default",
		Data: map[string][]byte{
			"tls.crt": []byte("---- BEGIN CERTIFICATE ----"),
			"tls.key": []byte("---- BEGIN KEY ----"),
		},
	}
}

func TestEnsureCertificateRegistration(t *testing.T) {
	certName := CertEntityName(newTestAIGWDP().Name, CertificateChecksum(newTestCertSecret()))

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
				require.NotNil(t, cert.Spec.AIGatewayRef.NamespacedRef)
				assert.Equal(t, "test-aigwcp", cert.Spec.AIGatewayRef.NamespacedRef.Name)
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
					APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
					Kind:       "AIGatewayDataPlaneCertificate",
					Name:       certName, Namespace: "default", Generation: 2,
					Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
						AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
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
								ObservedGeneration: 2,
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
			name: "cert programmed at an earlier generation only: returns not-programmed condition",
			extraObjs: []client.Object{
				&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
					APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
					Kind:       "AIGatewayDataPlaneCertificate",
					Name:       certName, Namespace: "default", Generation: 2,
					Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
						AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
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
								ObservedGeneration: 1,
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
					APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
					Kind:       "AIGatewayDataPlaneCertificate",
					Name:       certName, Namespace: "default",
					Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
						AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
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
			name: "Get NotFound after apply (cache not caught up): treated as not-programmed, no error",
			interceptors: interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate); ok {
						return apierrors.NewNotFound(corev1.Resource("aigatewaydataplanecertificates"), key.Name)
					}
					return c.Get(ctx, key, obj, opts...)
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

			r := &testReconciler{
				Config:        testConfig,
				Client:        cl,
				TypeConverter: managedfields.NewDeducedTypeConverter(),
				EventRecorder: events.NewFakeRecorder(10),
			}

			if tc.preCall {
				_, err := r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, resolvedTestCP(aigwcp), certSecret, CertificateChecksum(certSecret))
				require.NoError(t, err)
			}

			programmed, err := r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, resolvedTestCP(aigwcp), certSecret, CertificateChecksum(certSecret))

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

func TestEnsureCertificateRegistrationAdaptToExisting(t *testing.T) {
	certName := CertEntityName(newTestAIGWDP().Name, CertificateChecksum(newTestCertSecret()))
	const adaptedLabel = "adapted-from-existing"

	newReconciler := func(cl client.Client, calls *int) *testReconciler {
		cfg := testConfig
		cfg.Certificate.AdaptToExisting = func(desired, existing *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate) {
			*calls++
			desired.Labels[adaptedLabel] = existing.Name
		}
		return &testReconciler{
			Config:        cfg,
			Client:        cl,
			TypeConverter: managedfields.NewDeducedTypeConverter(),
			EventRecorder: events.NewFakeRecorder(10),
		}
	}
	newClient := func(objs []client.Object, funcs interceptor.Funcs) client.Client {
		return interceptor.NewClient(fake.NewClientBuilder().
			WithScheme(managerscheme.Get()).
			WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}).
			WithObjects(objs...).
			Build(), funcs)
	}
	getCert := func(t *testing.T, cl client.Client) aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
		t.Helper()
		var cert aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: certName, Namespace: "default"}, &cert))
		return cert
	}

	t.Run("not called when no certificate exists yet", func(t *testing.T) {
		aigwdp, certSecret := newTestAIGWDP(), newTestCertSecret()
		cl := newClient([]client.Object{aigwdp}, interceptor.Funcs{})
		var calls int
		_, err := newReconciler(cl, &calls).ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, resolvedTestCP(newTestAIGWCP()), certSecret, CertificateChecksum(certSecret))
		require.NoError(t, err)
		assert.Zero(t, calls)
		assert.NotContains(t, getCert(t, cl).Labels, adaptedLabel)
	})

	t.Run("called with the existing certificate before applying", func(t *testing.T) {
		aigwdp, certSecret := newTestAIGWDP(), newTestCertSecret()
		cl := newClient([]client.Object{aigwdp}, interceptor.Funcs{})
		var calls int
		r := newReconciler(cl, &calls)
		// The first call creates the certificate, the second one finds it.
		_, err := r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, resolvedTestCP(newTestAIGWCP()), certSecret, CertificateChecksum(certSecret))
		require.NoError(t, err)
		_, err = r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, resolvedTestCP(newTestAIGWCP()), certSecret, CertificateChecksum(certSecret))
		require.NoError(t, err)
		assert.Equal(t, 1, calls)
		assert.Equal(t, certName, getCert(t, cl).Labels[adaptedLabel])
	})

	t.Run("error reading the existing certificate is returned without applying", func(t *testing.T) {
		aigwdp, certSecret := newTestAIGWDP(), newTestCertSecret()
		var applied bool
		cl := newClient([]client.Object{aigwdp}, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate); ok {
					return assert.AnError
				}
				return c.Get(ctx, key, obj, opts...)
			},
			Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				applied = true
				return c.Apply(ctx, obj, opts...)
			},
		})
		var calls int
		_, err := newReconciler(cl, &calls).ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, resolvedTestCP(newTestAIGWCP()), certSecret, CertificateChecksum(certSecret))
		require.ErrorContains(t, err, "failed to get AIGatewayDataPlaneCertificate")
		assert.Zero(t, calls)
		assert.False(t, applied)
	})
}

// testCertificateRegistration is the CertificateRegistration of a non-Konnect
// control plane kind, as the OnPremEventGateway kind sets it.
var testCertificateRegistration = &CertificateRegistrationConditions{
	Type:                      "CertificateRegistered",
	RegisteredReason:          "Registered",
	RegistrationFailedReason:  "RegistrationFailed",
	NotProgrammedReason:       "NotProgrammed",
	Target:                    "the control plane",
	NotProgrammedHint:         " by its controller",
	ReportProgrammedCondition: true,
	CreatedEventReason:        "OnPremCertificateCreated",
	CreatedEventAction:        "CreateOnPremCertificate",
	UpdatedEventReason:        "OnPremCertificateUpdated",
	UpdatedEventAction:        "UpdateOnPremCertificate",
}

func TestEnsureCertificateRegistrationEvents(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		registration             *CertificateRegistrationConditions
		wantCreated, wantUpdated string
	}{
		{
			name:        "Konnect",
			wantCreated: "Normal KonnectCertificateCreated AIGatewayDataPlaneCertificate",
			wantUpdated: "Normal KonnectCertificateUpdated AIGatewayDataPlaneCertificate",
		},
		{
			name:         "a kind with its own certificate registration",
			registration: testCertificateRegistration,
			wantCreated:  "Normal OnPremCertificateCreated AIGatewayDataPlaneCertificate",
			wantUpdated:  "Normal OnPremCertificateUpdated AIGatewayDataPlaneCertificate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aigwdp, certSecret := newTestAIGWDP(), newTestCertSecret()
			cl := fake.NewClientBuilder().
				WithScheme(managerscheme.Get()).
				WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}).
				WithObjects(aigwdp).
				Build()
			recorder := events.NewFakeRecorder(10)
			r := &testReconciler{
				Config:        testConfig,
				Client:        cl,
				TypeConverter: managedfields.NewDeducedTypeConverter(),
				EventRecorder: recorder,
			}
			cp := resolvedTestCP(newTestAIGWCP())
			cp.CertificateRegistration = tc.registration
			checksum := CertificateChecksum(certSecret)

			_, err := r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, cp, certSecret, checksum)
			require.NoError(t, err)
			require.Len(t, recorder.Events, 1)
			assert.Contains(t, <-recorder.Events, tc.wantCreated)

			// Another Secret holding the same certificate updates it.
			certSecret.Name = "another-cert-secret"
			_, err = r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, cp, certSecret, checksum)
			require.NoError(t, err)
			require.Len(t, recorder.Events, 1)
			assert.Contains(t, <-recorder.Events, tc.wantUpdated)
		})
	}
}

func TestNotProgrammedMessage(t *testing.T) {
	r := &testReconciler{Config: testConfig}
	konnect := r.certificateRegistrationConditions(ResolvedControlPlane{})
	const generation = 3
	programmed := func(status metav1.ConditionStatus, observedGeneration int64, reason, message string) *metav1.Condition {
		return &metav1.Condition{
			Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: status,
			ObservedGeneration: observedGeneration, Reason: reason, Message: message,
		}
	}
	for _, tc := range []struct {
		name  string
		conds CertificateRegistrationConditions
		cond  *metav1.Condition
		want  string
	}{
		{
			name:  "Konnect, without a Programmed condition",
			conds: konnect,
			want:  "AIGatewayDataPlaneCertificate is not yet programmed on Konnect",
		},
		{
			name:  "Konnect, with a Programmed condition",
			conds: konnect,
			cond:  programmed(metav1.ConditionFalse, generation, "FailedToCreate", "Konnect refused it"),
			want:  "AIGatewayDataPlaneCertificate is not yet programmed on Konnect",
		},
		{
			name:  "without a Programmed condition, the hint tells what programs it",
			conds: *testCertificateRegistration,
			want:  "AIGatewayDataPlaneCertificate is not yet programmed on the control plane by its controller",
		},
		{
			name:  "the Programmed condition is passed through",
			conds: *testCertificateRegistration,
			cond:  programmed(metav1.ConditionFalse, generation, "LeftOut", "the gateway left it out"),
			want:  "AIGatewayDataPlaneCertificate is not yet programmed on the control plane: LeftOut: the gateway left it out",
		},
		{
			name:  "the Programmed condition without a message is passed through",
			conds: *testCertificateRegistration,
			cond:  programmed(metav1.ConditionFalse, generation, "Pending", ""),
			want:  "AIGatewayDataPlaneCertificate is not yet programmed on the control plane: Pending",
		},
		{
			name:  "a Programmed condition of an earlier generation is not",
			conds: *testCertificateRegistration,
			cond:  programmed(metav1.ConditionTrue, generation-1, "Programmed", ""),
			want:  "AIGatewayDataPlaneCertificate is not yet programmed on the control plane",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, r.notProgrammedMessage(tc.conds, tc.cond, generation))
		})
	}

	t.Run("the registration condition carries the Programmed condition of the certificate", func(t *testing.T) {
		aigwdp, certSecret := newTestAIGWDP(), newTestCertSecret()
		cl := fake.NewClientBuilder().
			WithScheme(managerscheme.Get()).
			WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}).
			WithObjects(aigwdp).
			Build()
		r := &testReconciler{
			Config:        testConfig,
			Client:        cl,
			TypeConverter: managedfields.NewDeducedTypeConverter(),
			EventRecorder: events.NewFakeRecorder(10),
		}
		cp := resolvedTestCP(newTestAIGWCP())
		cp.CertificateRegistration = testCertificateRegistration
		checksum := CertificateChecksum(certSecret)
		_, err := r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, cp, certSecret, checksum)
		require.NoError(t, err)

		var cert aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: aigwdp.Namespace, Name: CertEntityName(aigwdp.Name, checksum)}, &cert))
		cert.Status.Conditions = []metav1.Condition{{
			Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionFalse,
			ObservedGeneration: cert.Generation, Reason: "LeftOut", Message: "the gateway left it out",
			LastTransitionTime: metav1.Now(),
		}}
		require.NoError(t, cl.Status().Update(t.Context(), &cert))

		programmed, err := r.ensureCertificateRegistration(t.Context(), logr.Discard(), aigwdp, cp, certSecret, checksum)
		require.NoError(t, err)
		require.False(t, programmed)
		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, testCertificateRegistration.Type)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, testCertificateRegistration.NotProgrammedReason, cond.Reason)
		assert.Equal(t, "AIGatewayDataPlaneCertificate is not yet programmed on the control plane: LeftOut: the gateway left it out", cond.Message)
	})
}

func TestCheckCertificateProgrammedBeingDeleted(t *testing.T) {
	conds := CertificateRegistrationConditions{Type: "Registered", NotProgrammedReason: "NotProgrammed", Target: "the control plane"}
	cert := func(deleting bool) *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
		c := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
			Name: "cert", Namespace: "default", Generation: 2,
			Status: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateStatus{Conditions: []metav1.Condition{{
				Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionTrue,
				Reason: "Programmed", ObservedGeneration: 2, LastTransitionTime: metav1.Now(),
			}}},
		}
		if deleting {
			c.Finalizers = []string{"test/cleanup"}
			c.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		}
		return c
	}
	for _, tc := range []struct {
		name     string
		deleting bool
		want     bool
	}{
		{name: "programmed", want: true},
		{name: "programmed, but being deleted", deleting: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(cert(tc.deleting)).Build()
			r := newTestReconciler(cl, events.NewFakeRecorder(1))
			dp := newTestAIGWDP()
			got, err := r.checkCertificateProgrammed(t.Context(), dp, cert(false), conds)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			if tc.deleting {
				cond := apimeta.FindStatusCondition(dp.GetConditions(), conds.Type)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, "AIGatewayDataPlaneCertificate is being deleted: it is created again once it is gone", cond.Message)
			}
		})
	}
}
