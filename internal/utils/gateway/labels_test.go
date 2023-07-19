package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
)

func TestLabelObjForGateway(t *testing.T) {
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
			dataplane := &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.input,
				},
			}
			LabelObjectAsGatewayManaged(dataplane)
			assert.Equal(t, tt.output, dataplane.GetLabels())
		})
	}
}
