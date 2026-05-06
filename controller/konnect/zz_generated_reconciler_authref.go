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
	client.Object
	GetEventGatewayRef() commonv1alpha1.ObjectRef
	parentRefGetter
}

type portalRefAccessor interface {
	client.Object
	GetPortalRef() commonv1alpha1.ObjectRef
	parentRefGetter
}

type parentRefGetter interface {
	GetParentRef() commonv1alpha1.ObjectRef
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

	return types.NamespacedName{},
		fmt.Errorf("unsupported entity type %T for getting APIAuthRef", ent)
}
