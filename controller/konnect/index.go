package konnect

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
)

// ReconciliationIndexOption contains required options of index for a kind of object required for reconciliation.
type ReconciliationIndexOption struct {
	IndexObject  client.Object
	IndexField   string
	ExtractValue client.IndexerFunc
}

// indexKonnectGatewayControlPlaneRef returns a function that extracts the KonnectGatewayControlPlane reference from the
// object and returns it as a slice of strings for indexing.
func indexKonnectGatewayControlPlaneRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](cl client.Client) client.IndexerFunc {
	return func(obj client.Object) []string {
		o, ok := obj.(TEnt)
		if !ok {
			return nil
		}
		return controlPlaneRefAsSlice(o, cl)
	}
}

// controlPlaneRefAsSlice returns a slice of strings representing the KonnectNamespacedRef of the object.
func controlPlaneRefAsSlice[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt, cl client.Client) []string {
	cpRef, ok := getControlPlaneRef(ent).Get()
	if !ok {
		return nil
	}
	cp, err := getCPForRef(context.Background(), cl, cpRef, ent.GetNamespace())
	if err != nil {
		return nil
	}
	return []string{client.ObjectKeyFromObject(cp).String()}
}
