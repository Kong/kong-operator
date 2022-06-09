package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestIsGatewayScheduled(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  *gatewayv1alpha2.Gateway
		output bool
	}{
		{
			name:   "a gateway with no conditions is not scheduled",
			input:  &gatewayv1alpha2.Gateway{},
			output: false,
		},
		{
			name: "a gateway with no scheduling condition is not scheduled",
			input: &gatewayv1alpha2.Gateway{
				Status: gatewayv1alpha2.GatewayStatus{
					Conditions: []metav1.Condition{{
						Type:   "other",
						Reason: "other",
						Status: metav1.ConditionTrue,
					}},
				},
			},
			output: false,
		},
		{
			name: "a gateway with a scheduling condition that is not True is not scheduled",
			input: &gatewayv1alpha2.Gateway{
				Status: gatewayv1alpha2.GatewayStatus{
					Conditions: []metav1.Condition{{
						Type:   string(gatewayv1alpha2.GatewayConditionScheduled),
						Reason: string(gatewayv1alpha2.GatewayReasonScheduled),
						Status: metav1.ConditionFalse,
					}},
				},
			},
			output: false,
		},
		{
			name: "a gateway with a scheduling condition that is True is scheduled",
			input: &gatewayv1alpha2.Gateway{
				Status: gatewayv1alpha2.GatewayStatus{
					Conditions: []metav1.Condition{{
						Type:   string(gatewayv1alpha2.GatewayConditionScheduled),
						Reason: string(gatewayv1alpha2.GatewayReasonScheduled),
						Status: metav1.ConditionTrue,
					}},
				},
			},
			output: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.output, IsGatewayScheduled(tt.input))
		})
	}
}

func TestIsGatewayReady(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  *gatewayv1alpha2.Gateway
		output bool
	}{
		{
			name:   "a gateway with no conditions is not ready",
			input:  &gatewayv1alpha2.Gateway{},
			output: false,
		},
		{
			name: "a gateway with no ready condition is not ready",
			input: &gatewayv1alpha2.Gateway{
				Status: gatewayv1alpha2.GatewayStatus{
					Conditions: []metav1.Condition{{
						Type:   "other",
						Reason: "other",
						Status: metav1.ConditionTrue,
					}},
				},
			},
			output: false,
		},
		{
			name: "a gateway with a ready condition that is not True is not ready",
			input: &gatewayv1alpha2.Gateway{
				Status: gatewayv1alpha2.GatewayStatus{
					Conditions: []metav1.Condition{{
						Type:   string(gatewayv1alpha2.GatewayConditionReady),
						Reason: string(gatewayv1alpha2.GatewayReasonReady),
						Status: metav1.ConditionFalse,
					}},
				},
			},
			output: false,
		},
		{
			name: "a gateway with a ready condition that is True is ready",
			input: &gatewayv1alpha2.Gateway{
				Status: gatewayv1alpha2.GatewayStatus{
					Conditions: []metav1.Condition{{
						Type:   string(gatewayv1alpha2.GatewayConditionReady),
						Reason: string(gatewayv1alpha2.GatewayReasonReady),
						Status: metav1.ConditionTrue,
					}},
				},
			},
			output: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.output, IsGatewayReady(tt.input))
		})
	}
}
