package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// TestEventGatewayListenerPolicyDestinationInAnotherNamespace verifies that a
// listener policy may forward to a virtual cluster of another namespace: the
// reference is resolved, and declared as a sibling reference, so that a
// KongReferenceGrant in that namespace must allow it.
func TestEventGatewayListenerPolicyDestinationInAnotherNamespace(t *testing.T) {
	policy := &EventGatewayListenerPolicy{}
	require.NoError(t, yaml.Unmarshal([]byte(`
metadata: {name: forward, namespace: listeners}
spec:
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: {name: orders, namespace: tenants}}}
`), policy))

	t.Run("it is a sibling reference a KongReferenceGrant must allow", func(t *testing.T) {
		require.Contains(t, policy.CrossNamespaceSiblingReferences(), commonv1alpha1.CrossNamespaceReferenceCheck{
			FromGVK:       metav1.GroupVersionKind{Group: GroupVersion.Group, Version: GroupVersion.Version, Kind: "EventGatewayListenerPolicy"},
			ToGVK:         metav1.GroupVersionKind{Group: GroupVersion.Group, Version: GroupVersion.Version, Kind: "EventGatewayVirtualCluster"},
			FromNamespace: "listeners",
			ToNamespace:   "tenants",
			ToName:        "orders",
		})
	})

	t.Run("it resolves to the Konnect ID of the virtual cluster", func(t *testing.T) {
		vc := &EventGatewayVirtualCluster{Name: "orders", Namespace: "tenants"}
		vc.SetKonnectID("vc-konnect-id")
		scheme := runtime.NewScheme()
		require.NoError(t, AddToScheme(scheme))
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vc).Build()

		ids, err := resolveEventGatewayListenerPolicyForwardToVirtualClusterConfigPortMappingDestination(t.Context(), cl, policy)
		require.NoError(t, err)
		require.Equal(t, []string{"vc-konnect-id"}, ids)
	})

	t.Run("a destination of its namespace needs no KongReferenceGrant", func(t *testing.T) {
		local := policy.DeepCopy()
		local.Spec.APISpec.ForwardToVirtualClust.Config.PortMapping.Destination.Namespace = ""
		for _, c := range local.CrossNamespaceSiblingReferences() {
			require.NotEqual(t, "EventGatewayVirtualCluster", c.ToGVK.Kind)
		}
	})
}
