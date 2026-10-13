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

package onprem

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	multiinstanceai "github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestSetReadySkippingLicenseCondition(t *testing.T) {
	tests := []struct {
		name        string
		conditions  []metav1.Condition
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{
			name:       "no conditions",
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
		{
			name: "missing license does not gate Ready",
			conditions: []metav1.Condition{
				{
					Type:    string(aigatewayv1alpha1.LicenseValidType),
					Status:  metav1.ConditionFalse,
					Reason:  string(aigatewayv1alpha1.LicenseMissingReason),
					Message: "No enabled KongLicense resource found",
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
		{
			name: "provided license",
			conditions: []metav1.Condition{
				{
					Type:   string(aigatewayv1alpha1.LicenseValidType),
					Status: metav1.ConditionTrue,
					Reason: string(aigatewayv1alpha1.LicenseValidReason),
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
		{
			name: "other false condition gates Ready",
			conditions: []metav1.Condition{
				{
					Type:    string(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType),
					Status:  metav1.ConditionFalse,
					Reason:  "NotConfigured",
					Message: "no data planes configured",
				},
				{
					Type:   string(aigatewayv1alpha1.LicenseValidType),
					Status: metav1.ConditionTrue,
					Reason: string(aigatewayv1alpha1.LicenseValidReason),
				},
			},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  string(aigatewayv1alpha1.DependenciesNotReadyReason),
			wantMessage: "no data planes configured",
		},
		{
			name: "stale Ready=False is overwritten and ObservedGeneration set",
			conditions: []metav1.Condition{
				{
					Type:   string(aigatewayv1alpha1.ReadyType),
					Status: metav1.ConditionFalse,
					Reason: string(aigatewayv1alpha1.WaitingToBecomeReadyReason),
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			onprem := &aigatewayv1alpha1.OnPremAIGateway{
				Generation: 3,
			}
			onprem.SetConditions(tc.conditions)

			setReadySkippingLicenseCondition(onprem)

			ready, ok := k8sutils.GetCondition(aigatewayv1alpha1.ReadyType, onprem)
			require.True(t, ok, "Ready condition not set")
			assert.Equal(t, tc.wantStatus, ready.Status)
			assert.Equal(t, tc.wantReason, ready.Reason)
			assert.Equal(t, tc.wantMessage, ready.Message)
			assert.Equal(t, onprem.GetGeneration(), ready.ObservedGeneration)
		})
	}
}

func TestConfigFromSpec(t *testing.T) {
	tests := []struct {
		name string
		spec aigatewayv1alpha1.OnPremAIGatewaySpec
		want multiinstanceai.Config
	}{
		{
			name: "nil dataplaneSync",
			want: multiinstanceai.Config{},
		},
		{
			name: "nil interval and timeout",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{},
			},
			want: multiinstanceai.Config{},
		},
		{
			name: "interval and timeout set",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					IntervalSeconds: new(int32(10)),
					TimeoutSeconds:  new(int32(60)),
				},
			},
			want: multiinstanceai.Config{
				SyncInterval: 10 * time.Second,
				SyncTimeout:  60 * time.Second,
			},
		},
		{
			name: "only interval set",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					IntervalSeconds: new(int32(5)),
				},
			},
			want: multiinstanceai.Config{
				SyncInterval: 5 * time.Second,
			},
		},
		{
			name: "only timeout set",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					TimeoutSeconds: new(int32(30)),
				},
			},
			want: multiinstanceai.Config{
				SyncTimeout: 30 * time.Second,
			},
		},
		{
			name: "explicit zero interval and timeout",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					IntervalSeconds: new(int32(0)),
					TimeoutSeconds:  new(int32(0)),
				},
			},
			want: multiinstanceai.Config{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			onprem := &aigatewayv1alpha1.OnPremAIGateway{Spec: tc.spec}
			cfg := new(Reconciler).configFromSpec(onprem)
			assert.Equal(t, tc.want, cfg)
		})
	}
}
