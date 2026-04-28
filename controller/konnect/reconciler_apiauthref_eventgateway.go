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
)

type eventGatewayRefAccessor interface {
	client.Object
	GetEventGatewayRef() commonv1alpha1.ObjectRef
}

func getAPIAuthConfigurationRefFromEventGateway(
	ctx context.Context,
	cl client.Client,
	obj eventGatewayRefAccessor,
) (types.NamespacedName, error) {
	gateway, nnGateway, err := getEventGatewayForRef(ctx, cl, obj.GetEventGatewayRef(), obj.GetNamespace())
	if err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get KonnectEventGateway for %s: %w", client.ObjectKeyFromObject(obj), err)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	nnAPIAuth := types.NamespacedName{
		Name:      gateway.GetKonnectAPIAuthConfigurationRef().Name,
		Namespace: gateway.GetNamespace(),
	}

	if err := cl.Get(ctx, nnAPIAuth, &apiAuth); err != nil {
		return types.NamespacedName{},
			fmt.Errorf("failed to get APIAuthConfiguration %s for KonnectEventGateway %s: %w", nnAPIAuth, nnGateway, err)
	}

	return nnAPIAuth, nil
}
