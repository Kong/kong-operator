package kubernetes

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestGetManagedByLabelSet(t *testing.T) {
	testCases := []struct {
		name     string
		object   *operatorv1beta1.ControlPlane
		expected map[string]string
	}{
		{
			name: "Complete set of labels for a ControlPlane object",
			object: &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test-namespace",
				},
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          consts.ControlPlaneManagedLabelValue,
				consts.GatewayOperatorManagedByNamespaceLabel: "test-namespace",
				consts.GatewayOperatorManagedByNameLabel:      "test",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GetManagedByLabelSet(tc.object)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Unexpected result. Got: %v, want: %v", result, tc.expected)
			}
		})
	}
}
