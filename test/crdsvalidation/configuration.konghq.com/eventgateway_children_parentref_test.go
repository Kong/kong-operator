package configuration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestEventGatewayChildrenParentRefImmutableOnceInKonnect covers the Event
// Gateway entities under another entity (virtual cluster, listener policy,
// virtual cluster policies): their parent reference is immutable once they
// have been created in Konnect, even when they are not Programmed, so an
// entity that exists in Konnect can't be moved under another parent, e.g. one
// attached to an OnPremEventGateway.
func TestEventGatewayChildrenParentRefImmutableOnceInKonnect(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	t.Run("EventGatewayVirtualCluster", func(t *testing.T) {
		runChildParentRefImmutableOnceInKonnect(t, cfg, fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: virtual-cluster
    dnsLabel: virtual-cluster
    aclMode: enforce_on_gateway
    authentication: [{type: anonymous, anonymous: {}}]
    namespace: {mode: hide_prefix, prefix: tenant_}
`), "spec.eventGatewayBackendClusterRef")
	})
	t.Run("EventGatewayListenerPolicy", func(t *testing.T) {
		runChildParentRefImmutableOnceInKonnect(t, cfg, validListenerPolicy(ns.Name), "spec.eventGatewayListenerRef")
	})

	const modifyHeaders = `
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: virtual-cluster}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders:
      name: add-header
      config:
        actions: [{op: set, set: {key: x-header, value: value}}]
`
	t.Run("EventGatewayVirtualClusterProducePolicy", func(t *testing.T) {
		runChildParentRefImmutableOnceInKonnect(t, cfg,
			fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, ns.Name, modifyHeaders),
			"spec.eventGatewayVirtualClusterRef")
	})
	t.Run("EventGatewayVirtualClusterConsumePolicy", func(t *testing.T) {
		runChildParentRefImmutableOnceInKonnect(t, cfg,
			fromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](t, ns.Name, modifyHeaders),
			"spec.eventGatewayVirtualClusterRef")
	})
	t.Run("EventGatewayVirtualClusterPolicy", func(t *testing.T) {
		runChildParentRefImmutableOnceInKonnect(t, cfg, fromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, ns.Name, `
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: virtual-cluster}}
  apiSpec:
    type: acls
    acls:
      name: acls
      config:
        rules:
          - action: allow
            resourceType: topic
            operations: [{name: read}]
            resourceNames:
              type: stat
              stat: [{match: orders.*}]
`), "spec.eventGatewayVirtualClusterRef")
	})
}

// fromYAML returns an object of type T, named "test-*" in the namespace, with
// the given spec.
func fromYAML[T any, TPtr interface {
	*T
	client.Object
}](t *testing.T, namespace, manifest string) TPtr {
	t.Helper()
	var obj T
	require.NoError(t, yaml.Unmarshal([]byte(manifest), &obj))
	ptr := TPtr(&obj)
	ptr.SetGenerateName("test-")
	ptr.SetNamespace(namespace)
	return ptr
}

func runChildParentRefImmutableOnceInKonnect[
	T interface {
		client.Object
		DeepCopy() T
		SetConditions([]metav1.Condition)
		SetKonnectID(string)
		SetParentRef(commonv1alpha1.ObjectRef)
	},
](
	t *testing.T,
	cfg *rest.Config,
	valid T,
	refField string,
) {
	t.Helper()

	notProgrammed := []metav1.Condition{{
		Type:               "Programmed",
		Status:             metav1.ConditionFalse,
		Reason:             "KonnectAPIOpFailed",
		LastTransitionTime: metav1.Now(),
	}}
	withStatus := func(konnectID string) T {
		obj := valid.DeepCopy()
		obj.SetConditions(notProgrammed)
		obj.SetKonnectID(konnectID)
		return obj
	}
	moveToAnotherParent := func(obj T) {
		obj.SetParentRef(commonv1alpha1.ObjectRef{
			Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "another-parent"},
		})
	}

	common.TestCasesGroup[T]{
		{
			Name:                       "parent ref cannot change once created in Konnect, even when not programmed",
			TestObject:                 withStatus("5f7a9b1e-0c3d-4e8f-9a2b-6c1d3e5f7a9b"),
			Update:                     moveToAnotherParent,
			ExpectedUpdateErrorMessage: new(refField + " is immutable once the entity has been created in Konnect"),
		},
		{
			Name:       "parent ref can change when not programmed and not created in Konnect",
			TestObject: withStatus(""),
			Update:     moveToAnotherParent,
		},
	}.RunWithConfig(t, cfg, scheme.Get())
}
