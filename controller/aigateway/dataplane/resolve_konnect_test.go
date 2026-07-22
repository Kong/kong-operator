package dataplane

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
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

func Test_resolveKonnectAIGateway(t *testing.T) {
	const (
		ns       = "test-ns"
		aigwcpNM = "my-aigwcp"
	)

	newAIGWDP := func() *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "my-dp"},
			Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
					KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{Name: aigwcpNM},
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
		getErr            error // non-nil injects a GET error via interceptor
		wantCP            bool
		wantErr           bool
		wantConditionTrue bool
		wantReason        string
	}{
		{
			name:              "aigwcp not found: sets NotFound condition and returns error",
			aigwcp:            nil,
			wantCP:            false,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.KonnectAIGatewayNotFoundReason),
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
			wantReason:        string(aigatewayv1alpha1.KonnectAIGatewayResolvedReason),
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
			r := &Reconciler{Client: cl}

			aigwdp := newAIGWDP()
			gotCP, err := r.resolveKonnectAIGateway(context.Background(), logger, aigwdp)

			if tc.wantErr {
				require.Error(t, err)
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
				require.NotNil(t, gotCP)
			} else {
				assert.Nil(t, gotCP)
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
