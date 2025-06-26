package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/pkg/consts"

	konnectv1alpha2 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha2"
)

func TestLabelObjectAsKonnectExtensionManaged(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  map[string]string
		output map[string]string
	}{
		{
			name: "a konnect extension with empty labels receives a konnect extension managed label",
			output: map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.KonnectExtensionManagedByLabelValue,
			},
		},
		{
			name:  "a konnect extension with no labels receives a konnect extension managed label",
			input: make(map[string]string),
			output: map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.KonnectExtensionManagedByLabelValue,
			},
		},
		{
			name: "a konnect extension with one label receives a konnect extension managed label in addition",
			input: map[string]string{
				"url": "konghq.com",
			},
			output: map[string]string{
				"url":                                "konghq.com",
				consts.GatewayOperatorManagedByLabel: consts.KonnectExtensionManagedByLabelValue,
			},
		},
		{
			name: "a konnect extension with several labels receives a konnect extension managed label in addition",
			input: map[string]string{
				"test1": "1",
				"test2": "2",
				"test3": "3",
				"test4": "4",
			},
			output: map[string]string{
				"test1":                              "1",
				"test2":                              "2",
				"test3":                              "3",
				"test4":                              "4",
				consts.GatewayOperatorManagedByLabel: consts.KonnectExtensionManagedByLabelValue,
			},
		},
		{
			name: "a konnect extension with an existing management label gets updated",
			input: map[string]string{
				"test1":                              "1",
				consts.GatewayOperatorManagedByLabel: "other",
			},
			output: map[string]string{
				"test1":                              "1",
				consts.GatewayOperatorManagedByLabel: consts.KonnectExtensionManagedByLabelValue,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			konnectExtension := &konnectv1alpha2.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.input,
				},
			}
			LabelObjectAsKonnectExtensionManaged(konnectExtension)
			assert.Equal(t, tt.output, konnectExtension.GetLabels())
		})
	}
}
