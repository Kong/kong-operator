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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func newOnPremAIGW(ns, name string, ready metav1.ConditionStatus) *aigatewayv1alpha1.OnPremAIGateway {
	return &aigatewayv1alpha1.OnPremAIGateway{
		Namespace: ns, Name: name,
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

func Test_resolveOnPremAIGateway_NoControlPlaneRef(t *testing.T) {
	r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(managerscheme.Get()).Build()}

	tests := []struct {
		name            string
		controlPlaneRef *aigatewayv1alpha1.ControlPlaneRef
	}{
		{name: "ControlPlaneRef is nil"},
		{name: "ControlPlaneRef set but OnPremNamespacedRef is nil", controlPlaneRef: &aigatewayv1alpha1.ControlPlaneRef{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
				Namespace: "test-ns", Name: "my-dp",
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{ControlPlaneRef: tc.controlPlaneRef},
			}

			gotCP, err := r.resolveOnPremAIGateway(context.Background(), zap.New(), aigwdp)

			require.NoError(t, err)
			assert.Nil(t, gotCP)
			assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.OnPremAIGatewayResolvedType)),
				"no condition should be set when OnPremNamespacedRef is not configured")
		})
	}
}

func Test_resolveOnPremAIGateway(t *testing.T) {
	const (
		ns       = "test-ns"
		onpremNM = "my-onprem-aigwcp"
	)

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
		getErr            error // non-nil injects a GET error via interceptor
		wantCP            bool
		wantErr           bool
		wantConditionTrue bool
		wantReason        string
	}{
		{
			name:              "onprem not found: sets NotFound condition and returns error",
			onprem:            nil,
			wantCP:            false,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
		},
		{
			name:              "onprem not yet Ready: sets NotReady condition and returns error",
			onprem:            newOnPremAIGW(ns, onpremNM, metav1.ConditionFalse),
			wantCP:            false,
			wantErr:           true,
			wantConditionTrue: false,
			wantReason:        string(aigatewayv1alpha1.OnPremAIGatewayNotReadyReason),
		},
		{
			name:              "onprem Ready: returns onprem and sets Resolved condition",
			onprem:            newOnPremAIGW(ns, onpremNM, metav1.ConditionTrue),
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
			if tc.onprem != nil {
				objects = append(objects, tc.onprem)
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
			gotCP, err := r.resolveOnPremAIGateway(context.Background(), logger, aigwdp)

			if tc.wantErr {
				require.Error(t, err)
				// Condition is only set for domain errors (not-found / not-ready), not API errors.
				if tc.wantReason != "" {
					cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.OnPremAIGatewayResolvedType))
					require.NotNil(t, cond, "OnPremAIGatewayResolved condition must be set")
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
