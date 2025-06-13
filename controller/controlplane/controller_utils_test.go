package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/gateway-operator/controller/pkg/controlplane"
	gwtypes "github.com/kong/gateway-operator/internal/types"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestControlPlaneSpecDeepEqual(t *testing.T) {
	testCases := []struct {
		name            string
		spec1           *gwtypes.ControlPlaneOptions
		spec2           *gwtypes.ControlPlaneOptions
		envVarsToIgnore []string
		equal           bool
	}{
		{
			name:  "not matching Extensions",
			spec1: &gwtypes.ControlPlaneOptions{},
			spec2: &gwtypes.ControlPlaneOptions{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			equal: false,
		},
		{
			name: "matching Extensions",
			spec1: &gwtypes.ControlPlaneOptions{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			equal: true,
		},
		{
			name: "different watch namespaces yield unequal specs",
			spec1: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2", "ns3"},
				},
			},
			equal: false,
		},
		{
			name: "the same watch namespaces yield equal specs",
			spec1: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
				},
			},
			equal: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.equal, controlplane.SpecDeepEqual(tc.spec1, tc.spec2, tc.envVarsToIgnore...))
		})
	}
}
