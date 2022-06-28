package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestPruneGatewayStatusConds(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  []metav1.Condition
		output []metav1.Condition
	}{
		{
			name:   "a gateway with no status conditions will not be pruned",
			input:  []metav1.Condition{},
			output: []metav1.Condition{},
		},
		{
			name: "a gateway with 7 status conditions will not be pruned",
			input: []metav1.Condition{
				{Message: "1"}, {Message: "2"}, {Message: "3"}, {Message: "4"},
				{Message: "5"}, {Message: "6"}, {Message: "7"},
			},
			output: []metav1.Condition{
				{Message: "1"}, {Message: "2"}, {Message: "3"}, {Message: "4"},
				{Message: "5"}, {Message: "6"}, {Message: "7"},
			},
		},
		{
			name: "a gateway with 8 status conditions will be pruned to 7",
			input: []metav1.Condition{
				{Message: "1"}, {Message: "2"}, {Message: "3"}, {Message: "4"},
				{Message: "5"}, {Message: "6"}, {Message: "7"}, {Message: "8"},
			},
			output: []metav1.Condition{
				{Message: "2"}, {Message: "3"}, {Message: "4"}, {Message: "5"},
				{Message: "6"}, {Message: "7"}, {Message: "8"},
			},
		},
		{
			name: "a gateway with 10 status conditions will be pruned to 7",
			input: []metav1.Condition{
				{Message: "1"}, {Message: "2"}, {Message: "3"}, {Message: "4"},
				{Message: "5"}, {Message: "6"}, {Message: "7"}, {Message: "8"},
				{Message: "9"}, {Message: "10"},
			},
			output: []metav1.Condition{
				{Message: "4"}, {Message: "5"}, {Message: "6"}, {Message: "7"},
				{Message: "8"}, {Message: "9"}, {Message: "10"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &gatewayv1alpha2.Gateway{
				Status: gatewayv1alpha2.GatewayStatus{
					Conditions: tt.input,
				},
			}
			gateway = PruneGatewayStatusConds(gateway)
			assert.LessOrEqual(t, len(gateway.Status.Conditions), 7)
			assert.Equal(t, tt.output, gateway.Status.Conditions)
		})
	}
}

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
