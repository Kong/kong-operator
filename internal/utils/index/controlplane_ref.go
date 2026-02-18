package index

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"

	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
)

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
	cpRef, ok := controlplane.GetControlPlaneRef(ent).Get()
	if !ok {
		return nil
	}
	cp, err := controlplane.GetCPForRef(context.Background(), cl, cpRef, ent.GetNamespace())
	if err != nil {
		return nil
	}
	return []string{client.ObjectKeyFromObject(cp).String()}
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
