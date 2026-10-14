package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// TestEventGatewayEntitiesParentRef covers the entities directly under an
// Event Gateway, which reference it through spec.parentRef or the deprecated
// spec.gatewayRef.
func TestEventGatewayEntitiesParentRef(t *testing.T) {
	type entity interface {
		GetParentRef() commonv1alpha1.ObjectRef
		SetParentRef(commonv1alpha1.ObjectRef)
		GetEventGatewayRef() commonv1alpha1.ObjectRef
		GetGatewayRef() commonv1alpha1.ObjectRef
		GetParentGVK() schema.GroupVersionKind
	}

	var (
		legacyRef = commonv1alpha1.ObjectRef{
			Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw", Namespace: new("other")},
		}
		parentRef  = &commonv1alpha1.GatewayRef{Group: "konnect.konghq.com", Kind: "KonnectEventGateway", Name: "gw", Namespace: new("other")}
		konnectGVK = schema.GroupVersionKind{Group: "konnect.konghq.com", Version: "v1alpha1", Kind: "KonnectEventGateway"}
	)

	tests := []struct {
		name string
		// newEntity returns the entity with the given parentRef and gatewayRef
		// and a function reading them back.
		newEntity func(*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) (entity, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef))
	}{
		{
			name: "EventGatewayBackendCluster",
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) (entity, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef)) {
				obj := &EventGatewayBackendCluster{Spec: EventGatewayBackendClusterSpec{ParentRef: p, GatewayRef: g}}
				return obj, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) {
					return obj.Spec.ParentRef, obj.Spec.GatewayRef
				}
			},
		},
		{
			name: "EventGatewayDataPlaneCertificate",
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) (entity, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef)) {
				obj := &EventGatewayDataPlaneCertificate{Spec: EventGatewayDataPlaneCertificateSpec{ParentRef: p, GatewayRef: g}}
				return obj, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) {
					return obj.Spec.ParentRef, obj.Spec.GatewayRef
				}
			},
		},
		{
			name: "EventGatewayListener",
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) (entity, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef)) {
				obj := &EventGatewayListener{Spec: EventGatewayListenerSpec{ParentRef: p, GatewayRef: g}}
				return obj, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) {
					return obj.Spec.ParentRef, obj.Spec.GatewayRef
				}
			},
		},
		{
			name: "EventGatewaySchemaRegistry",
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) (entity, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef)) {
				obj := &EventGatewaySchemaRegistry{Spec: EventGatewaySchemaRegistrySpec{ParentRef: p, GatewayRef: g}}
				return obj, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) {
					return obj.Spec.ParentRef, obj.Spec.GatewayRef
				}
			},
		},
		{
			name: "EventGatewayStaticKey",
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) (entity, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef)) {
				obj := &EventGatewayStaticKey{Spec: EventGatewayStaticKeySpec{ParentRef: p, GatewayRef: g}}
				return obj, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) {
					return obj.Spec.ParentRef, obj.Spec.GatewayRef
				}
			},
		},
		{
			name: "EventGatewayTLSTrustBundle",
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) (entity, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef)) {
				obj := &EventGatewayTLSTrustBundle{Spec: EventGatewayTLSTrustBundleSpec{ParentRef: p, GatewayRef: g}}
				return obj, func() (*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) {
					return obj.Spec.ParentRef, obj.Spec.GatewayRef
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("the deprecated gatewayRef is returned as is", func(t *testing.T) {
				obj, _ := tc.newEntity(nil, legacyRef)
				require.Equal(t, legacyRef, obj.GetParentRef())
				require.Equal(t, legacyRef, obj.GetEventGatewayRef())
				require.Equal(t, legacyRef, obj.GetGatewayRef())
				require.Equal(t, konnectGVK, obj.GetParentGVK())
			})
			t.Run("parentRef is returned as a namespaced ref", func(t *testing.T) {
				obj, _ := tc.newEntity(parentRef, commonv1alpha1.ObjectRef{})
				require.Equal(t, legacyRef, obj.GetParentRef())
				require.Equal(t, legacyRef, obj.GetEventGatewayRef())
				require.Equal(t, legacyRef, obj.GetGatewayRef())
				require.Equal(t, konnectGVK, obj.GetParentGVK())
			})
			t.Run("SetParentRef sets the deprecated gatewayRef and clears parentRef", func(t *testing.T) {
				obj, refs := tc.newEntity(parentRef, commonv1alpha1.ObjectRef{})
				obj.SetParentRef(legacyRef)
				gotParentRef, gotGatewayRef := refs()
				require.Nil(t, gotParentRef)
				require.Equal(t, legacyRef, gotGatewayRef)
			})
		})
	}
}
