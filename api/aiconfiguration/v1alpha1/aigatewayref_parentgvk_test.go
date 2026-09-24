package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestAIGatewayRefParentGVK(t *testing.T) {
	tests := []struct {
		name string
		ref  AIGatewayRef
		want schema.GroupVersionKind
	}{
		{
			name: "unset kind and group default to the KonnectAIGateway GVK (mirroring the CRD defaults)",
			ref: AIGatewayRef{
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			want: schema.GroupVersionKind{
				Group:   "konnect.konghq.com",
				Version: "v1alpha1",
				Kind:    "KonnectAIGateway",
			},
		},
		{
			name: "explicit KonnectAIGateway kind resolves the konnect group version",
			ref: AIGatewayRef{
				Group:         AIGatewayRefGroupKonnect,
				Kind:          AIGatewayRefKindKonnect,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			want: schema.GroupVersionKind{
				Group:   "konnect.konghq.com",
				Version: "v1alpha1",
				Kind:    "KonnectAIGateway",
			},
		},
		{
			name: "OnPremAIGateway kind resolves the aigateway group version",
			ref: AIGatewayRef{
				Group:         AIGatewayRefGroupOnPrem,
				Kind:          AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			want: schema.GroupVersionKind{
				Group:   "aigateway.konghq.com",
				Version: "v1alpha1",
				Kind:    "OnPremAIGateway",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.ref.ParentGVK())
		})
	}
}
