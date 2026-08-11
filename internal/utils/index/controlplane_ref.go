package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
)

// indexKonnectGatewayControlPlaneRef returns a function that extracts the KonnectGatewayControlPlane reference from the
// object and returns it as a slice of strings for indexing.
//
// The extraction is a pure function of the object: it must not depend on live
// cluster state (e.g. by resolving the referenced ControlPlane via a client
// Get). Index ExtractValueFns are invoked identically on Add and Delete to
// determine which index bucket to add/remove a key from; if the result can
// change between those calls (e.g. because the referenced object no longer
// exists at Delete time), the cache's index desyncs from its item store and
// later Lists fail with "cache contained <nil>, which is not an Object".
func indexKonnectGatewayControlPlaneRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](_ client.Client) client.IndexerFunc {
	return func(obj client.Object) []string {
		o, ok := obj.(TEnt)
		if !ok {
			return nil
		}
		return controlPlaneRefAsSlice(o)
	}
}

// controlPlaneRefAsSlice returns a slice of strings representing the KonnectNamespacedRef of the object,
// derived purely from the ref fields on the object itself (namespace defaults to the entity's own
// namespace, mirroring controlplane.GetCPForRef's defaulting), without resolving the referenced
// ControlPlane against the cluster.
func controlPlaneRefAsSlice[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt) []string {
	cpRef, ok := controlPlaneRefIsKonnectNamespacedRef(ent)
	if !ok {
		return nil
	}
	ns := ent.GetNamespace()
	if cpRef.KonnectNamespacedRef.Namespace != "" {
		ns = cpRef.KonnectNamespacedRef.Namespace
	}
	return []string{ns + "/" + cpRef.KonnectNamespacedRef.Name}
}

// controlPlaneRefIsKonnectNamespacedRef returns:
// - the ControlPlane KonnectNamespacedRef of the object if it is a KonnectNamespacedRef.
// - a boolean indicating if the object has a KonnectNamespacedRef.
func controlPlaneRefIsKonnectNamespacedRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt) (commonv1alpha1.ControlPlaneRef, bool) {
	cpRef, ok := controlplane.GetControlPlaneRef(ent).Get()
	if !ok {
		return commonv1alpha1.ControlPlaneRef{}, false
	}
	return cpRef, cpRef.KonnectNamespacedRef != nil &&
		cpRef.Type == commonv1alpha1.ControlPlaneRefKonnectNamespacedRef
}
