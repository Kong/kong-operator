package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestLabelObjForGateway(t *testing.T) {
	const testGatewayName = "test-gateway"

	for _, tt := range []struct {
		name     string
		input    map[string]string
		expected map[string]string
	}{
		{
			name: "a dataplane with empty labels receives gateway managed and gateway-name labels",
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
				consts.GatewayNameLabel:              testGatewayName,
			},
		},
		{
			name:  "a dataplane with no labels receives gateway managed and gateway-name labels",
			input: make(map[string]string),
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
				consts.GatewayNameLabel:              testGatewayName,
			},
		},
		{
			name: "a dataplane with one label receives gateway managed and gateway-name labels in addition",
			input: map[string]string{
				"url": "konghq.com",
			},
			expected: map[string]string{
				"url":                                "konghq.com",
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
				consts.GatewayNameLabel:              testGatewayName,
			},
		},
		{
			name: "a dataplane with several labels receives gateway managed and gateway-name labels in addition",
			input: map[string]string{
				"test1": "1",
				"test2": "2",
				"test3": "3",
				"test4": "4",
			},
			expected: map[string]string{
				"test1":                              "1",
				"test2":                              "2",
				"test3":                              "3",
				"test4":                              "4",
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
				consts.GatewayNameLabel:              testGatewayName,
			},
		},
		{
			name: "a dataplane with existing management and gateway-name labels gets updated",
			input: map[string]string{
				"test1":                              "1",
				consts.GatewayOperatorManagedByLabel: "other",
				consts.GatewayNameLabel:              "old-gateway",
			},
			expected: map[string]string{
				"test1":                              "1",
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
				consts.GatewayNameLabel:              testGatewayName,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataplane := &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.input,
				},
			}
			LabelObjectAsGatewayManaged(dataplane, testGatewayName)
			assert.Equal(t, tt.expected, dataplane.GetLabels())
		})
	}
}
