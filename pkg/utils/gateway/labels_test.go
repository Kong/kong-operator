package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
)

func TestLabelObjForGateway(t *testing.T) {
	for _, tt := range []struct {
		name     string
		input    map[string]string
		expected map[string]string
	}{
		{
			name: "a dataplane with empty labels receives a gateway managed label",
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
			},
		},
		{
			name:  "a dataplane with no labels receives a gateway managed label",
			input: make(map[string]string),
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
			},
		},
		{
			name: "a dataplane with one label receives a gateway managed label in addition",
			input: map[string]string{
				"url": "konghq.com",
			},
			expected: map[string]string{
				"url":                                "konghq.com",
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
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
			expected: map[string]string{
				"test1":                              "1",
				"test2":                              "2",
				"test3":                              "3",
				"test4":                              "4",
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
			},
		},
		{
			name: "a dataplane with an existing management label gets updated",
			input: map[string]string{
				"test1":                              "1",
				consts.GatewayOperatorManagedByLabel: "other",
			},
			expected: map[string]string{
				"test1":                              "1",
				consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataplane := &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.input,
				},
			}
			LabelObjectAsGatewayManaged(dataplane)
			assert.Equal(t, tt.expected, dataplane.GetLabels())
		})
	}
}
