package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
)

type eventGatewayRefAccessor interface {
	objectWithParentRef
	GetEventGatewayRef() commonv1alpha1.ObjectRef
}

type eventGatewayBackendClusterRefAccessor interface {
	objectWithParentRef
	GetEventGatewayBackendClusterRef() commonv1alpha1.ObjectRef
}

type eventGatewayVirtualClusterRefAccessor interface {
	objectWithParentRef
	GetEventGatewayVirtualClusterRef() commonv1alpha1.ObjectRef
}

type portalRefAccessor interface {
	objectWithParentRef
	GetPortalRef() commonv1alpha1.ObjectRef
}

func getAPIAuthRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (types.NamespacedName, error) {
	// TODO: make this generic for all root dependent entities.

	if obj, ok := any(ent).(portalRefAccessor); ok {
		return getAPIAuthConfigurationRefFromParent[konnectv1alpha1.Portal](ctx, cl, obj)
	}
	if obj, ok := any(ent).(eventGatewayRefAccessor); ok {
		return getAPIAuthConfigurationRefFromParent[konnectv1alpha1.KonnectEventGateway](ctx, cl, obj)
	}
	if obj, ok := any(ent).(eventGatewayBackendClusterRefAccessor); ok {
		return getAPIAuthRefViaBackendCluster(ctx, cl, obj)
	}
	if obj, ok := any(ent).(eventGatewayVirtualClusterRefAccessor); ok {
		return getAPIAuthRefViaVirtualCluster(ctx, cl, obj)
	}

	return types.NamespacedName{},
		fmt.Errorf("unsupported entity type %T for getting APIAuthRef", ent)
}

// getAPIAuthRefViaBackendCluster resolves the APIAuth for an entity whose immediate
// parent is EventGatewayBackendCluster (e.g. EventGatewayVirtualCluster).
// It performs a two-hop lookup: entity → EGBC → KonnectEventGateway → APIAuth.
func getAPIAuthRefViaBackendCluster(
	ctx context.Context,
	cl client.Client,
	obj eventGatewayBackendClusterRefAccessor,
) (types.NamespacedName, error) {
	bcRef := obj.GetEventGatewayBackendClusterRef()
	if bcRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || bcRef.NamespacedRef == nil {
		return types.NamespacedName{},
			fmt.Errorf("invalid EventGatewayBackendCluster reference: must be a NamespacedRef with a non-nil NamespacedRef field")
	}
	if bcRef.NamespacedRef.Namespace != nil && *bcRef.NamespacedRef.Namespace != obj.GetNamespace() {
		// TODO https://github.com/Kong/kong-operator/issues/4134
		return types.NamespacedName{},
			fmt.Errorf("invalid EventGatewayBackendCluster reference: cross-namespace reference is not supported")
	}
	nn := types.NamespacedName{
		Name: bcRef.NamespacedRef.Name,
		// TODO https://github.com/Kong/kong-operator/issues/4134
		Namespace: obj.GetNamespace(),
	}
	var bc konnectv1alpha1.EventGatewayBackendCluster
	if err := cl.Get(ctx, nn, &bc); err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get EventGatewayBackendCluster %s: %w", nn, err)
	}
	return getAPIAuthConfigurationRefFromParent[konnectv1alpha1.KonnectEventGateway](ctx, cl, &bc)
}

// getAPIAuthRefViaVirtualCluster resolves the APIAuth for an entity whose immediate
// parent is EventGatewayVirtualCluster (e.g. EventGatewayVirtualClusterConsumePolicy).
// It performs a 3-hop lookup: entity → EGVC → EGBC → KonnectEventGateway → APIAuth.
func getAPIAuthRefViaVirtualCluster(
	ctx context.Context,
	cl client.Client,
	obj eventGatewayVirtualClusterRefAccessor,
) (types.NamespacedName, error) {
	bcRef := obj.GetEventGatewayVirtualClusterRef()
	if bcRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || bcRef.NamespacedRef == nil {
		return types.NamespacedName{},
			fmt.Errorf("invalid EventGatewayVirtualCluster reference: must be a NamespacedRef with a non-nil NamespacedRef field")
	}
	if bcRef.NamespacedRef.Namespace != nil && *bcRef.NamespacedRef.Namespace != obj.GetNamespace() {
		// TODO https://github.com/Kong/kong-operator/issues/4134
		return types.NamespacedName{},
			fmt.Errorf("invalid EventGatewayVirtualCluster reference: cross-namespace reference is not supported")
	}
	nn := types.NamespacedName{
		Name: bcRef.NamespacedRef.Name,
		// TODO https://github.com/Kong/kong-operator/issues/4134
		Namespace: obj.GetNamespace(),
	}
	var bc konnectv1alpha1.EventGatewayVirtualCluster
	if err := cl.Get(ctx, nn, &bc); err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get EventGatewayVirtualCluster %s: %w", nn, err)
	}
	return getAPIAuthRefViaBackendCluster(ctx, cl, &bc)
}
