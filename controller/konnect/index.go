package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
)

// ReconciliationIndexOption contains required options of index for a kind of object required for reconciliation.
type ReconciliationIndexOption struct {
	IndexObject  client.Object
	IndexField   string
	ExtractValue client.IndexerFunc
}

// controlPlaneKonnectNamespacedRefAsSlice returns a slice of strings representing
// the KonnectNamespacedRef of the object.
func controlPlaneKonnectNamespacedRefAsSlice[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt) []string {
	cpRef, ok := controlPlaneRefIsKonnectNamespacedRef(ent)
	if !ok {
		return nil
	}

	konnectRef := cpRef.KonnectNamespacedRef
	// NOTE: cross namespace refs are not supported yet.
	// TODO: https://github.com/Kong/kubernetes-configuration/issues/36
	// Specifying the same namespace is optional and is supported.
	if konnectRef.Namespace != "" && konnectRef.Namespace != ent.GetNamespace() {
		return nil
	}

	return []string{ent.GetNamespace() + "/" + konnectRef.Name}
}
