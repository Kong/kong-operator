package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// TestEventGatewayEntitiesKonnectEventGatewayIndex verifies that the entities
// directly under an Event Gateway are indexed by their KonnectEventGateway
// whether they reference it through spec.parentRef or the deprecated
// spec.gatewayRef.
func TestEventGatewayEntitiesKonnectEventGatewayIndex(t *testing.T) {
	meta := metav1.ObjectMeta{Name: "obj", Namespace: "ns"}

	entities := []struct {
		name      string
		extract   func(client.Object) []string
		newEntity func(*commonv1alpha1.GatewayRef, commonv1alpha1.ObjectRef) client.Object
	}{
		{
			name:    "EventGatewayBackendCluster",
			extract: eventGatewayBackendClusterOnKonnectEventGatewayRef,
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) client.Object {
				return &configurationv1alpha1.EventGatewayBackendCluster{ObjectMeta: meta, Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{ParentRef: p, GatewayRef: g}}
			},
		},
		{
			name:    "EventGatewayDataPlaneCertificate",
			extract: eventGatewayDataPlaneCertificateOnKonnectEventGatewayRef,
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) client.Object {
				return &configurationv1alpha1.EventGatewayDataPlaneCertificate{ObjectMeta: meta, Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{ParentRef: p, GatewayRef: g}}
			},
		},
		{
			name:    "EventGatewayListener",
			extract: eventGatewayListenerOnKonnectEventGatewayRef,
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) client.Object {
				return &configurationv1alpha1.EventGatewayListener{ObjectMeta: meta, Spec: configurationv1alpha1.EventGatewayListenerSpec{ParentRef: p, GatewayRef: g}}
			},
		},
		{
			name:    "EventGatewaySchemaRegistry",
			extract: eventGatewaySchemaRegistryOnKonnectEventGatewayRef,
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) client.Object {
				return &configurationv1alpha1.EventGatewaySchemaRegistry{ObjectMeta: meta, Spec: configurationv1alpha1.EventGatewaySchemaRegistrySpec{ParentRef: p, GatewayRef: g}}
			},
		},
		{
			name:    "EventGatewayStaticKey",
			extract: eventGatewayStaticKeyOnKonnectEventGatewayRef,
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) client.Object {
				return &configurationv1alpha1.EventGatewayStaticKey{ObjectMeta: meta, Spec: configurationv1alpha1.EventGatewayStaticKeySpec{ParentRef: p, GatewayRef: g}}
			},
		},
		{
			name:    "EventGatewayTLSTrustBundle",
			extract: eventGatewayTLSTrustBundleOnKonnectEventGatewayRef,
			newEntity: func(p *commonv1alpha1.GatewayRef, g commonv1alpha1.ObjectRef) client.Object {
				return &configurationv1alpha1.EventGatewayTLSTrustBundle{ObjectMeta: meta, Spec: configurationv1alpha1.EventGatewayTLSTrustBundleSpec{ParentRef: p, GatewayRef: g}}
			},
		},
	}

	const (
		group = "konnect.konghq.com"
		kind  = "KonnectEventGateway"
	)
	refs := []struct {
		name       string
		parentRef  *commonv1alpha1.GatewayRef
		gatewayRef commonv1alpha1.ObjectRef
		want       []string
	}{
		{
			name: "deprecated gatewayRef",
			gatewayRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			want: []string{"ns/gw"},
		},
		{
			name: "deprecated gatewayRef in another namespace",
			gatewayRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw", Namespace: new("other")},
			},
			want: []string{"other/gw"},
		},
		{
			name: "deprecated gatewayRef with konnectID is not indexed",
			gatewayRef: commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: new("8ae65120-cdec-4310-84c1-4b19caf67967"),
			},
		},
		{
			name:      "parentRef",
			parentRef: &commonv1alpha1.GatewayRef{Group: group, Kind: kind, Name: "gw"},
			want:      []string{"ns/gw"},
		},
		{
			name:      "parentRef in another namespace",
			parentRef: &commonv1alpha1.GatewayRef{Group: group, Kind: kind, Name: "gw", Namespace: new("other")},
			want:      []string{"other/gw"},
		},
		{
			name: "no reference is not indexed",
		},
	}

	for _, e := range entities {
		t.Run(e.name, func(t *testing.T) {
			for _, r := range refs {
				t.Run(r.name, func(t *testing.T) {
					assert.Equal(t, r.want, e.extract(e.newEntity(r.parentRef, r.gatewayRef)))
				})
			}
		})
	}
}
