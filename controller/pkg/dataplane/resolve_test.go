package dataplane

import (
	"cmp"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
						errors.Is(err, errControlPlaneNotReady),
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
		wantCP            bool
		wantErr           bool
		wantErrContains   string
		wantNoCondition   bool
		wantConditionTrue bool
		wantReason        string
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
			r := &testReconciler{Client: cl, Config: testConfig}

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
