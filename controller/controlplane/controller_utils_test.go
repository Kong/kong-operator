package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"

	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"

	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestControlPlaneSpecDeepEqual(t *testing.T) {
	testCases := []struct {
		name  string
		spec1 *gwtypes.ControlPlaneOptions
		spec2 *gwtypes.ControlPlaneOptions
		equal bool
	}{
		{
			name: "different watch namespaces yield unequal specs",
			spec1: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv2beta1.WatchNamespaces{
					Type: operatorv2beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv2beta1.WatchNamespaces{
					Type: operatorv2beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2", "ns3"},
				},
			},
			equal: false,
		},
		{
			name: "the same watch namespaces yield equal specs",
			spec1: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv2beta1.WatchNamespaces{
					Type: operatorv2beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv2beta1.WatchNamespaces{
					Type: operatorv2beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
				},
			},
			equal: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.equal, controlplane.SpecDeepEqual(tc.spec1, tc.spec2))
		})
	}
}
