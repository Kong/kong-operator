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

type eventGatewayListenerRefAccessor interface {
	objectWithParentRef
	GetEventGatewayListenerRef() commonv1alpha1.ObjectRef
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
		return getAPIAuthConfigurationRefFromParent[konnectv1alpha1.Portal](ctx, cl, obj, obj.GetParentRef())
	}
	if obj, ok := any(ent).(eventGatewayRefAccessor); ok {
		return getAPIAuthConfigurationRefFromParent[konnectv1alpha1.KonnectEventGateway](ctx, cl, obj, obj.GetParentRef())
	}
	if obj, ok := any(ent).(eventGatewayListenerRefAccessor); ok {
		return getAPIAuthRefViaEventGatewayListener(ctx, cl, obj)
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

// getAPIAuthRefViaEventGatewayListener resolves the APIAuth for an entity whose immediate
// parent is EventGatewayListener (e.g. EventGatewayListenerPolicy).
// It performs a two-hop lookup: entity -> EventGatewayListener -> KonnectEventGateway -> APIAuth.
func getAPIAuthRefViaEventGatewayListener(
	ctx context.Context,
	cl client.Client,
	obj eventGatewayListenerRefAccessor,
) (types.NamespacedName, error) {
	return getAPIAuthRefViaParent[
		konnectv1alpha1.EventGatewayListener,
		konnectv1alpha1.KonnectEventGateway,
	](ctx, cl, obj)
}

// getAPIAuthRefViaBackendCluster resolves the APIAuth for an entity whose immediate
// parent is EventGatewayBackendCluster (e.g. EventGatewayVirtualCluster).
// It performs a two-hop lookup: entity → EGBC → KonnectEventGateway → APIAuth.
func getAPIAuthRefViaBackendCluster(
	ctx context.Context,
	cl client.Client,
	obj eventGatewayBackendClusterRefAccessor,
) (types.NamespacedName, error) {
	return getAPIAuthRefViaParent[
		konnectv1alpha1.EventGatewayBackendCluster,
		konnectv1alpha1.KonnectEventGateway,
	](ctx, cl, obj)
}

// getAPIAuthRefViaVirtualCluster resolves the APIAuth for an entity whose immediate
// parent is EventGatewayVirtualCluster (e.g. EventGatewayVirtualClusterConsumePolicy).
// It performs a 3-hop lookup: entity → EGVC → EGBC → KonnectEventGateway → APIAuth.
func getAPIAuthRefViaVirtualCluster(
	ctx context.Context,
	cl client.Client,
	obj eventGatewayVirtualClusterRefAccessor,
) (types.NamespacedName, error) {
	vcRef := obj.GetEventGatewayVirtualClusterRef()
	if vcRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef ||
		vcRef.NamespacedRef == nil {
		return types.NamespacedName{},
			fmt.Errorf("invalid EventGatewayVirtualCluster reference: must be a NamespacedRef with a non-nil NamespacedRef field")
	}

	virtualCluster, nn, err := getParentForRef[konnectv1alpha1.EventGatewayVirtualCluster](ctx, cl, vcRef, obj.GetNamespace())
	if err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get EventGatewayVirtualCluster %s: %w", nn, err)
	}
	return getAPIAuthRefViaBackendCluster(ctx, cl, virtualCluster)
}

// getAPIAuthRefViaParent resolves the APIAuth for an entity whose immediate parent
// does not reference the KonnectAPIAuthConfiguration directly.
// It accepts 2 type parameters:
// - ParentT is the type of the immediate parent
// - RootT is the type of the root parent that references the APIAuth.
func getAPIAuthRefViaParent[
	ParentT parentT,
	RootT parentT,
	ParentTPtr interface {
		parentTPtr[ParentT]
		objectWithParentRef
	},
	RootTPtr parentWithAPIAuthTPtr[RootT],
](
	ctx context.Context,
	cl client.Client,
	obj objectWithParentRef,
) (types.NamespacedName, error) {
	parentRef := obj.GetParentRef()
	parentPtr, nn, err := getParentForRef[ParentT, ParentTPtr](ctx, cl, parentRef, obj.GetNamespace())
	if err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get %s %s: %w", constraints.EntityTypeName[ParentT](), nn, err)
	}
	return getAPIAuthConfigurationRefFromParent[RootT, RootTPtr](ctx, cl, parentPtr, parentPtr.GetParentRef())
}
