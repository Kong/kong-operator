package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

func Test_pruneGatewayStatusConds(t *testing.T) {
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
			gateway = pruneGatewayStatusConds(gateway)
			assert.LessOrEqual(t, len(gateway.Status.Conditions), 7)
			assert.Equal(t, tt.output, gateway.Status.Conditions)
		})
	}
}

func Test_labelObjForGateway(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  map[string]string
		output map[string]string
	}{
		{
			name: "a dataplane with empty labels receives a gateway managed label",
			output: map[string]string{
				consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue,
			},
		},
		{
			name:  "a dataplane with no labels receives a gateway managed label",
			input: make(map[string]string),
			output: map[string]string{
				consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue,
			},
		},
		{
			name: "a dataplane with one label receives a gateway managed label in addition",
			input: map[string]string{
				"url": "konghq.com",
			},
			output: map[string]string{
				"url":                                 "konghq.com",
				consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue,
			},
		},
		{
			name: "a dataplane with several labels receives a gateway managed label in addition",
			input: map[string]string{
				"test1": "1",
				"test2": "2",
				"test3": "3",
				"test4": "4",
			},
			output: map[string]string{
				"test1":                               "1",
				"test2":                               "2",
				"test3":                               "3",
				"test4":                               "4",
				consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue,
			},
		},
		{
			name: "a dataplane with an existing management label gets updated",
			input: map[string]string{
				"test1":                               "1",
				consts.GatewayOperatorControlledLabel: "other",
			},
			output: map[string]string{
				"test1":                               "1",
				consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataplane := &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.input,
				},
			}
			labelObjForGateway(dataplane)
			assert.Equal(t, tt.output, dataplane.GetLabels())
		})
	}
}
