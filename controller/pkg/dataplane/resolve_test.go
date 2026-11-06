package dataplane

import (
	"cmp"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func newKonnectAIGW(ns, name string, programmed metav1.ConditionStatus) *konnectv1alpha1.KonnectAIGateway {
	return &konnectv1alpha1.KonnectAIGateway{
		Namespace: ns, Name: name,
		Status: konnectv1alpha1.KonnectAIGatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: programmed,
					Reason: string(programmed),
				},
			},
		},
	}
}

func Test_resolveControlPlane_OnPremReadiness(t *testing.T) {
	const (
		ns       = "test-ns"
		onpremNM = "my-onprem-aigwcp"
	)

	newOnPremAIGW := func(ready metav1.ConditionStatus) *aigatewayv1alpha1.OnPremAIGateway {
		return &aigatewayv1alpha1.OnPremAIGateway{
			Namespace: ns, Name: onpremNM,
			Status: aigatewayv1alpha1.OnPremAIGatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(aigatewayv1alpha1.ReadyType),
						Status: ready,
						Reason: string(ready),
					},
				},
			},
		}
	}

	newAIGWDP := func() *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{
			Namespace: ns, Name: "my-dp",
			Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				ControlPlaneRef: &aigatewayv1alpha1.ControlPlaneRef{
					Type:                aigatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef,
					OnPremNamespacedRef: &aigatewayv1alpha1.NamespacedRef{Name: onpremNM},
				},
			},
		}
	}

	scheme := managerscheme.Get()
	logger := zap.New()

	tests := []struct {
		name string
		// nil = not in cluster
		onprem            *aigatewayv1alpha1.OnPremAIGateway
		wantErr           bool
		wantConditionTrue bool
		wantReason        string
	}{
		{
			name:              "onprem not found: sets NotFound condition and returns error",
			onprem:            nil,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
		},
		{
			name:              "onprem not yet Ready: sets NotReady condition and returns transient error",
			onprem:            newOnPremAIGW(metav1.ConditionFalse),
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.OnPremAIGatewayNotReadyReason),
		},
		{
			name:              "onprem Ready: returns onprem and sets Resolved condition",
			onprem:            newOnPremAIGW(metav1.ConditionTrue),
			wantErr:           false,
			wantConditionTrue: true,
			wantReason:        string(aigatewayv1alpha1.ControlPlaneResolvedReason),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			if tc.onprem != nil {
				objects = append(objects, tc.onprem)
			}
			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objects...).
				Build()
			r := &testReconciler{Client: base, Config: testConfig}

			aigwdp := newAIGWDP()
			// Seed a stale resolution condition of the other (Konnect) kind to
			// verify it gets cleaned up when resolving the on-prem kind.
			aigwdp.Status.Conditions = []metav1.Condition{{
				Type:   string(aigatewayv1alpha1.KonnectAIGatewayResolvedType),
				Status: metav1.ConditionFalse,
				Reason: string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
			}}

			gotCP, err := r.resolveControlPlane(context.Background(), logger, aigwdp, ControlPlaneRef{
				Kind: testOnPremControlPlaneKind.Kind,
				Name: onpremNM,
			})

			if tc.wantErr {
				require.Error(t, err)
				// A not-found (or not-yet-Ready) control plane is an expected
				// transient state that the reconcile does not retry with error
				// backoff.
				assert.True(t,
					apierrors.IsNotFound(err) ||
						errors.Is(err, errControlPlaneNotProgrammed) ||
						errors.Is(err, errControlPlaneNotReady) ||
						errors.Is(err, errEndpointsMismatch),
					"expected a transient (non-retried) resolution error, got: %v", err)
				// The stale resolution condition of the other kind must still
				// have been removed.
				assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectAIGatewayResolvedType)),
					"stale resolution condition of the other control plane kind must be removed")
				cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.OnPremAIGatewayResolvedType))
				require.NotNil(t, cond, "OnPremAIGatewayResolved condition must be set")
				assert.Equal(t, tc.wantReason, cond.Reason)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				return
			}
			require.NoError(t, err)

			require.NotNil(t, gotCP.Object)
			assert.Equal(t, testOnPremControlPlaneKind.Kind, gotCP.Kind)
			assert.False(t, gotCP.IsKonnect)

			// The other kind's resolution condition must have been removed.
			assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectAIGatewayResolvedType)),
				"stale resolution condition of the other control plane kind must be removed")

			cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.OnPremAIGatewayResolvedType))
			require.NotNil(t, cond, "OnPremAIGatewayResolved condition must be set")
			assert.Equal(t, tc.wantReason, cond.Reason)
			if tc.wantConditionTrue {
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
			} else {
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
			}
		})
	}
}

func Test_resolveControlPlane(t *testing.T) {
	const (
		ns       = "test-ns"
		aigwcpNM = "my-aigwcp"
	)

	newAIGWDP := func() *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{
			Namespace: ns, Name: "my-dp",
			Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				ControlPlaneRef: &aigatewayv1alpha1.ControlPlaneRef{
					KonnectNamespacedRef: &aigatewayv1alpha1.NamespacedRef{Name: aigwcpNM},
				},
			},
		}
	}

	scheme := managerscheme.Get()
	logger := zap.New()

	tests := []struct {
		name string
		// nil = not in cluster
		aigwcp            *konnectv1alpha1.KonnectAIGateway
		getErr            error
		refKind           string
		endpointCheck     func(cp ControlPlaneObject) error
		wantCP            bool
		wantErr           bool
		wantErrContains   string
		wantErrIs         error
		wantNoCondition   bool
		wantConditionTrue bool
		wantReason        string
		wantMessage       string
	}{
		{
			name:            "unsupported control plane kind: errors and sets no condition",
			refKind:         "UnSupportedAIGateway",
			wantErr:         true,
			wantErrContains: `unsupported control plane kind "UnSupportedAIGateway"`,
			wantNoCondition: true,
		},
		{
			name:              "aigwcp not found: sets NotFound condition and returns error",
			aigwcp:            nil,
			wantCP:            false,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
		},
		{
			name:              "aigwcp not yet programmed: sets NotProgrammed condition and returns error",
			aigwcp:            newKonnectAIGW(ns, aigwcpNM, metav1.ConditionFalse),
			wantCP:            false,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.KonnectAIGatewayNotProgrammedReason),
		},
		{
			name:              "aigwcp programmed: returns aigwcp and sets Resolved condition",
			aigwcp:            newKonnectAIGW(ns, aigwcpNM, metav1.ConditionTrue),
			wantCP:            true,
			wantErr:           false,
			wantConditionTrue: true,
			wantReason:        string(aigatewayv1alpha1.ControlPlaneResolvedReason),
		},
		{
			name:   "aigwcp programmed with unusable endpoints: sets EndpointMismatch condition and returns transient error",
			aigwcp: newKonnectAIGW(ns, aigwcpNM, metav1.ConditionTrue),
			endpointCheck: func(ControlPlaneObject) error {
				return errors.New(`endpoint "https://2cf352e764.us.cp.konghq.tech" is not private, server URL "https://us.svc.konghq.tech/api" is`)
			},
			wantCP:            true,
			wantErr:           true,
			wantErrIs:         errEndpointsMismatch,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.KonnectAIGatewayEndpointMismatchReason),
			wantMessage:       `endpoint "https://2cf352e764.us.cp.konghq.tech" is not private, server URL "https://us.svc.konghq.tech/api" is`,
		},
		{
			name:    "GET returns unexpected error: propagated to caller",
			getErr:  assert.AnError,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			if tc.aigwcp != nil {
				objects = append(objects, tc.aigwcp)
			}
			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objects...).
				Build()
			var cl client.Client = base
			if tc.getErr != nil {
				getErr := tc.getErr
				cl = interceptor.NewClient(base, interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return getErr
					},
				})
			}
			// The endpoint check under test belongs to the kind config, so it
			// has to be overridden on a copy of the shared test config.
			cfg := testConfig
			cfg.ControlPlanes = []ControlPlaneKindConfig{testControlPlaneKind, testOnPremControlPlaneKind}
			cfg.ControlPlanes[0].EndpointCheck = tc.endpointCheck

			r := &testReconciler{Client: cl, Config: cfg}

			aigwdp := newAIGWDP()
			gotCP, err := r.resolveControlPlane(context.Background(), logger, aigwdp, ControlPlaneRef{
				Kind: cmp.Or(tc.refKind, testControlPlaneKind.Kind),
				Name: aigwcpNM,
			})

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrContains != "" {
					require.ErrorContains(t, err, tc.wantErrContains)
				}
				if tc.wantErrIs != nil {
					assert.ErrorIs(t, err, tc.wantErrIs)
				}
				if tc.wantNoCondition {
					assert.Nil(t, gotCP.Object)
					assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectAIGatewayResolvedType)),
						"no resolution condition must be set")
					return
				}
				// Condition is only set for domain errors (not-found / not-programmed), not API errors.
				if tc.wantReason != "" {
					cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectAIGatewayResolvedType))
					require.NotNil(t, cond, "KonnectAIGatewayResolved condition must be set")
					assert.Equal(t, tc.wantReason, cond.Reason)
					assert.Equal(t, metav1.ConditionFalse, cond.Status)
					if tc.wantMessage != "" {
						assert.Equal(t, tc.wantMessage, cond.Message)
					}
				}
				return
			}
			require.NoError(t, err)

			if tc.wantCP {
				require.NotNil(t, gotCP.Object)
			} else {
				assert.Nil(t, gotCP.Object)
			}

			cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectAIGatewayResolvedType))
			require.NotNil(t, cond, "KonnectAIGatewayResolved condition must be set")
			assert.Equal(t, tc.wantReason, cond.Reason)
			if tc.wantConditionTrue {
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
			} else {
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
			}
		})
	}
}

func Test_resolveControlPlane_CertificateConditions(t *testing.T) {
	const (
		ns       = "test-ns"
		onpremNM = "my-onprem-aigwcp"
	)
	registration := &CertificateRegistrationConditions{
		Type:                     "CertificateRegistered",
		RegisteredReason:         "Registered",
		RegistrationFailedReason: "RegistrationFailed",
		NotProgrammedReason:      "NotProgrammed",
		Target:                   "the control plane",
	}
	notReady := func(context.Context, client.Client, ControlPlaneObject) (any, error) {
		return nil, &DependenciesNotReadyError{Message: "the CA isn't published yet"}
	}
	// newAIGWDP returns a data plane whose certificate was provisioned and
	// registered before the dependencies of its control plane went missing.
	newAIGWDP := func() *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{
			Namespace: ns, Name: "my-dp", Generation: 2,
			Status: aigatewayv1alpha1.AIGatewayDataPlaneStatus{Conditions: []metav1.Condition{
				{Type: registration.Type, Status: metav1.ConditionTrue, Reason: registration.RegisteredReason},
				{Type: testConfig.Conditions.CertificateProvisionedType, Status: metav1.ConditionTrue, Reason: testConfig.Conditions.CertificateProvisionedReason},
			}},
		}
	}
	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Namespace: ns, Name: onpremNM,
		Status: aigatewayv1alpha1.OnPremAIGatewayStatus{Conditions: []metav1.Condition{{
			Type: string(aigatewayv1alpha1.ReadyType), Status: metav1.ConditionTrue, Reason: "Ready",
		}}},
	}
	resolveNamed := func(t *testing.T, kind ControlPlaneKindConfig, name string) (*aigatewayv1alpha1.AIGatewayDataPlane, error) {
		t.Helper()
		cfg := testConfig
		cfg.ControlPlanes = []ControlPlaneKindConfig{testControlPlaneKind, kind}
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(onprem).Build()
		r := &testReconciler{Client: cl, Config: cfg}
		aigwdp := newAIGWDP()
		_, err := r.resolveControlPlane(t.Context(), zap.New(), aigwdp, ControlPlaneRef{Kind: kind.Kind, Name: name})
		return aigwdp, err
	}
	resolve := func(t *testing.T, kind ControlPlaneKindConfig) (*aigatewayv1alpha1.AIGatewayDataPlane, error) {
		t.Helper()
		return resolveNamed(t, kind, onpremNM)
	}
	assertCondition := func(t *testing.T, aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, condType string, status metav1.ConditionStatus, reason, message string) {
		t.Helper()
		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, condType)
		require.NotNil(t, cond, condType)
		assert.Equal(t, status, cond.Status, condType)
		assert.Equal(t, reason, cond.Reason, condType)
		if message != "" {
			assert.Equal(t, message, cond.Message, condType)
		}
		assert.Equal(t, aigwdp.Generation, cond.ObservedGeneration, condType)
	}

	t.Run("dependencies not ready make the certificate conditions of a kind registering it Unknown", func(t *testing.T) {
		kind := testOnPremControlPlaneKind
		kind.Dependencies = notReady
		kind.CertificateRegistration = registration
		aigwdp, err := resolve(t, kind)
		require.ErrorIs(t, err, errControlPlaneNotReady)
		require.NotErrorIs(t, err, errControlPlaneGone, "a dependency not ready yet may become ready")
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotReadyReason, "the CA isn't published yet")
		assertCondition(t, aigwdp, registration.Type, metav1.ConditionUnknown, kind.Conditions.NotReadyReason, "the CA isn't published yet")
		assertCondition(t, aigwdp, testConfig.Conditions.CertificateProvisionedType, metav1.ConditionUnknown, kind.Conditions.NotReadyReason, "the CA isn't published yet")
	})

	t.Run("dependencies not ready keep the certificate conditions of a kind not registering it", func(t *testing.T) {
		kind := testOnPremControlPlaneKind
		kind.Dependencies = notReady
		aigwdp, err := resolve(t, kind)
		require.ErrorIs(t, err, errControlPlaneNotReady)
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotReadyReason, "")
		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, testConfig.Conditions.CertificateProvisionedType)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("dependencies that can't be read make the certificate conditions of a kind registering it Unknown", func(t *testing.T) {
		kind := testOnPremControlPlaneKind
		kind.Dependencies = func(context.Context, client.Client, ControlPlaneObject) (any, error) {
			return nil, assert.AnError
		}
		kind.CertificateRegistration = registration
		aigwdp, err := resolve(t, kind)
		require.ErrorIs(t, err, assert.AnError)
		require.NotErrorIs(t, err, errControlPlaneNotReady, "a read error is retried with backoff")
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotReadyReason, err.Error())
		assertCondition(t, aigwdp, registration.Type, metav1.ConditionUnknown, kind.Conditions.NotReadyReason, err.Error())
		assertCondition(t, aigwdp, testConfig.Conditions.CertificateProvisionedType, metav1.ConditionUnknown, kind.Conditions.NotReadyReason, err.Error())

		// Its Deployment rolled out, and Ready was True before: Ready doesn't
		// say the certificate is fine while it can't be told.
		deployment := &appsv1.Deployment{
			Namespace: aigwdp.Namespace, Name: aigwdp.Name, Generation: 1,
			Spec:   appsv1.DeploymentSpec{Replicas: new(int32(1))},
			Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, ReadyReplicas: 1},
		}
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(deployment).WithStatusSubresource(deployment).Build()
		require.NoError(t, (&testReconciler{Client: cl, Config: testConfig}).ensureReadyStatus(t.Context(), aigwdp))
		assertCondition(t, aigwdp, testConfig.Conditions.ReadyType, metav1.ConditionFalse, testConfig.Conditions.DependenciesNotReadyReason, err.Error())
	})

	t.Run("a control plane that can't be read", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(onprem).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return assert.AnError
			},
		}).Build()
		read := func(kind ControlPlaneKindConfig) (*aigatewayv1alpha1.AIGatewayDataPlane, error) {
			cfg := testConfig
			cfg.ControlPlanes = []ControlPlaneKindConfig{testControlPlaneKind, kind}
			aigwdp := newAIGWDP()
			_, err := (&testReconciler{Client: cl, Config: cfg}).resolveControlPlane(t.Context(), zap.New(), aigwdp, ControlPlaneRef{Kind: kind.Kind, Name: onpremNM})
			return aigwdp, err
		}

		kind := testOnPremControlPlaneKind
		kind.CertificateRegistration = registration
		aigwdp, err := read(kind)
		require.ErrorIs(t, err, assert.AnError)
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotReadyReason, err.Error())
		assertCondition(t, aigwdp, registration.Type, metav1.ConditionUnknown, kind.Conditions.NotReadyReason, err.Error())

		// A kind not registering its certificate keeps its conditions.
		aigwdp, err = read(testOnPremControlPlaneKind)
		require.ErrorIs(t, err, assert.AnError)
		require.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, testOnPremControlPlaneKind.Conditions.ResolvedType))
	})

	t.Run("dependencies telling the control plane is gone", func(t *testing.T) {
		kind := testOnPremControlPlaneKind
		kind.Dependencies = func(context.Context, client.Client, ControlPlaneObject) (any, error) {
			return nil, &DependenciesNotReadyError{Message: "the control plane is being deleted", ControlPlaneGone: true}
		}
		aigwdp, err := resolve(t, kind)
		require.ErrorIs(t, err, errControlPlaneNotReady)
		require.ErrorIs(t, err, errControlPlaneGone)
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotReadyReason, "the control plane is being deleted")
	})

	t.Run("a disabled kind is reported without reading its control plane", func(t *testing.T) {
		kind := testOnPremControlPlaneKind
		kind.CertificateRegistration = registration
		kind.DisabledMessage = "OnPremAIGateway control planes are not supported"
		kind.NewObject = func() ControlPlaneObject {
			require.FailNow(t, "a disabled kind must not be read")
			return nil
		}
		aigwdp, err := resolve(t, kind)
		require.ErrorIs(t, err, errControlPlaneKindDisabled)
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotFoundReason, kind.DisabledMessage)
		assertCondition(t, aigwdp, registration.Type, metav1.ConditionUnknown, kind.Conditions.NotFoundReason, kind.DisabledMessage)
	})

	t.Run("a missing control plane makes the certificate conditions of a kind registering it Unknown", func(t *testing.T) {
		kind := testOnPremControlPlaneKind
		kind.CertificateRegistration = registration
		aigwdp, err := resolveNamed(t, kind, "missing")
		require.True(t, apierrors.IsNotFound(err), "got %v", err)
		require.ErrorIs(t, err, errControlPlaneGone)
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotFoundReason, kind.Conditions.NotFoundMessage)
		assertCondition(t, aigwdp, registration.Type, metav1.ConditionUnknown, kind.Conditions.NotFoundReason, kind.Conditions.NotFoundMessage)
		assertCondition(t, aigwdp, testConfig.Conditions.CertificateProvisionedType, metav1.ConditionUnknown, kind.Conditions.NotFoundReason, kind.Conditions.NotFoundMessage)
	})

	t.Run("a missing control plane keeps the certificate conditions of a kind not registering it", func(t *testing.T) {
		kind := testOnPremControlPlaneKind
		aigwdp, err := resolveNamed(t, kind, "missing")
		require.True(t, apierrors.IsNotFound(err), "got %v", err)
		assertCondition(t, aigwdp, kind.Conditions.ResolvedType, metav1.ConditionFalse, kind.Conditions.NotFoundReason, "")
		for _, condType := range []string{registration.Type, testConfig.Conditions.CertificateProvisionedType} {
			cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, condType)
			require.NotNil(t, cond, condType)
			assert.Equal(t, metav1.ConditionTrue, cond.Status, condType)
		}
	})
}
