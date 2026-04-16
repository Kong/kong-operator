package dataplane

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func newKeg(ns, name string, programmed metav1.ConditionStatus) *konnectv1alpha1.KonnectEventControlPlane {
	return &konnectv1alpha1.KonnectEventControlPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Status: konnectv1alpha1.KonnectEventControlPlaneStatus{
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status:             programmed,
					Reason:             string(programmed),
					LastTransitionTime: metav1.NewTime(time.Now()),
				},
			},
		},
	}
}

func Test_resolveKonnectEventGateway(t *testing.T) {
	const (
		ns      = "test-ns"
		kegName = "my-keg"
	)

	newEGDP := func() *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "my-dp"},
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{Name: kegName},
				},
			},
		}
	}

	scheme := managerscheme.Get()
	logger := zap.New()

	tests := []struct {
		name string
		// nil = not in cluster
		keg               *konnectv1alpha1.KonnectEventControlPlane
		getErr            error // non-nil injects a GET error via interceptor
		wantKeg           bool
		wantErr           bool
		wantConditionTrue bool
		wantReason        string
	}{
		{
			name:              "keg not found: sets NotFound condition and returns error",
			keg:               nil,
			wantKeg:           false,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(eventgatewayv1alpha1.KonnectEventGatewayNotFoundReason),
		},
		{
			name:              "keg not yet programmed: sets NotProgrammed condition and returns error",
			keg:               newKeg(ns, kegName, metav1.ConditionFalse),
			wantKeg:           false,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedReason),
		},
		{
			name:              "keg programmed: returns keg and sets Resolved condition",
			keg:               newKeg(ns, kegName, metav1.ConditionTrue),
			wantKeg:           true,
			wantErr:           false,
			wantConditionTrue: true,
			wantReason:        string(eventgatewayv1alpha1.KonnectEventGatewayResolvedReason),
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
			if tc.keg != nil {
				objects = append(objects, tc.keg)
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

			egdp := newEGDP()
			gotKeg, err := r.resolveKonnectEventGateway(context.Background(), logger, egdp)

			if tc.wantErr {
				require.Error(t, err)
				// Condition is only set for domain errors (not-found / not-programmed), not API errors.
				if tc.wantReason != "" {
					cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType))
					require.NotNil(t, cond, "KonnectEventGatewayResolved condition must be set")
					assert.Equal(t, tc.wantReason, cond.Reason)
					assert.Equal(t, metav1.ConditionFalse, cond.Status)
				}
				return
			}
			require.NoError(t, err)

			if tc.wantKeg {
				require.NotNil(t, gotKeg)
			} else {
				assert.Nil(t, gotKeg)
			}

			cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType))
			require.NotNil(t, cond, "KonnectEventGatewayResolved condition must be set")
			assert.Equal(t, tc.wantReason, cond.Reason)
			if tc.wantConditionTrue {
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
			} else {
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
			}
		})
	}
}
