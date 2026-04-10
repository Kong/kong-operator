package mcpserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestOwnerControlPlaneName(t *testing.T) {
	tests := []struct {
		name      string
		mcpServer *konnectv1alpha1.MCPServer
		expected  string
	}{
		{
			name: "matching kind and APIVersion returns CP name",
			mcpServer: &konnectv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: konnectv1alpha2.GroupVersion.String(),
							Kind:       "KonnectGatewayControlPlane",
							Name:       "my-cp",
						},
					},
				},
			},
			expected: "my-cp",
		},
		{
			name:      "no owner references returns empty string",
			mcpServer: &konnectv1alpha1.MCPServer{},
			expected:  "",
		},
		{
			name: "wrong kind returns empty string",
			mcpServer: &konnectv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: konnectv1alpha2.GroupVersion.String(),
							Kind:       "SomethingElse",
							Name:       "my-cp",
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "wrong APIVersion returns empty string",
			mcpServer: &konnectv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "wrong.api/v1",
							Kind:       "KonnectGatewayControlPlane",
							Name:       "my-cp",
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "multiple owner refs, only matching one returns name",
			mcpServer: &konnectv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "other.api/v1",
							Kind:       "OtherResource",
							Name:       "other",
						},
						{
							APIVersion: konnectv1alpha2.GroupVersion.String(),
							Kind:       "KonnectGatewayControlPlane",
							Name:       "my-cp",
						},
					},
				},
			},
			expected: "my-cp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ownerControlPlaneName(tt.mcpServer)
			assert.Equal(t, tt.expected, result)
		})
	}
}
