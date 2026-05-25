package konnect

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// eventGatewayChildBoundToKonnect returns a predicate that admits EventGateway
// children (Listener, BackendCluster, VirtualCluster, ListenerPolicy) only
// when their parent KonnectEventGateway is Konnect-managed. On-premise
// children are filtered out so they are handled by the dedicated on-prem
// reconciler. Children whose parent cannot be resolved are admitted so the
// Konnect reconciler can surface the missing-reference error. Kinds that
// reference an EventGatewayListener (e.g. ListenerPolicy) are resolved with
// one extra hop through the Listener's GetGatewayRef.
func eventGatewayChildBoundToKonnect(cl client.Client) func(client.Object) bool {
	return func(obj client.Object) bool {
		gwRef, ok := resolveParentGatewayRef(cl, obj)
		if !ok {
			return false
		}
		if gwRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || gwRef.NamespacedRef == nil {
			return true
		}
		namespace := obj.GetNamespace()
		if gwRef.NamespacedRef.Namespace != nil && *gwRef.NamespacedRef.Namespace != "" {
			namespace = *gwRef.NamespacedRef.Namespace
		}
		var eg konnectv1alpha1.KonnectEventGateway
		if err := cl.Get(context.Background(), client.ObjectKey{
			Namespace: namespace,
			Name:      gwRef.NamespacedRef.Name,
		}, &eg); err != nil {
			return true
		}
		return eg.Spec.Environment != konnectv1alpha1.EventGatewayEnvironmentOnPremise
	}
}

// resolveParentGatewayRef returns the namespaced ObjectRef pointing at the
// parent KonnectEventGateway for obj. Direct gateway-referencers return their
// own ref; listener-referencers (e.g. ListenerPolicy) are resolved with one
// extra hop through the EventGatewayListener.
func resolveParentGatewayRef(cl client.Client, obj client.Object) (commonv1alpha1.ObjectRef, bool) {
	if ref, ok := obj.(interface {
		GetGatewayRef() commonv1alpha1.ObjectRef
	}); ok {
		return ref.GetGatewayRef(), true
	}
	ref, ok := obj.(interface {
		GetEventGatewayListenerRef() commonv1alpha1.ObjectRef
	})
	if !ok {
		return commonv1alpha1.ObjectRef{}, false
	}
	listenerRef := ref.GetEventGatewayListenerRef()
	if listenerRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || listenerRef.NamespacedRef == nil {
		return commonv1alpha1.ObjectRef{}, true
	}
	namespace := obj.GetNamespace()
	if listenerRef.NamespacedRef.Namespace != nil && *listenerRef.NamespacedRef.Namespace != "" {
		namespace = *listenerRef.NamespacedRef.Namespace
	}
	var listener konnectv1alpha1.EventGatewayListener
	if err := cl.Get(context.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      listenerRef.NamespacedRef.Name,
	}, &listener); err != nil {
		return commonv1alpha1.ObjectRef{}, true
	}
	return listener.GetGatewayRef(), true
}
