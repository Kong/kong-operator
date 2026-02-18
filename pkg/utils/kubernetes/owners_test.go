package kubernetes

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestGetManagedByLabelSet(t *testing.T) {
	testCases := []struct {
		name     string
		object   *gwtypes.ControlPlane
		expected map[string]string
	}{
		{
			name: "Complete set of labels for a ControlPlane object",
			object: &gwtypes.ControlPlane{
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
