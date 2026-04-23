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

type portalRefAccessor interface {
	GetPortalRef() commonv1alpha1.ObjectRef
	GetNamespace() string
}

func getAPIAuthConfigurationRefFromPortal(
	ctx context.Context,
	cl client.Client,
	obj portalRefAccessor,
) (types.NamespacedName, error) {
	portalRef := obj.GetPortalRef()
	if portalRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef ||
		portalRef.NamespacedRef == nil {
		return types.NamespacedName{},
			fmt.Errorf("invalid PortalRef: must be a NamespacedRef with a non-nil NamespacedRef field")
	}

	nnPortal := types.NamespacedName{
		Name: portalRef.NamespacedRef.Name,
		// TODO: handle cross namespace refs
		Namespace: obj.GetNamespace(),
	}

	var portal konnectv1alpha1.Portal
	if err := cl.Get(ctx, nnPortal, &portal); err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get Portal %s", nnPortal)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	nnAPIAuth := types.NamespacedName{
		Name: portal.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
		// TODO: handle cross namespace refs
		Namespace: portal.GetNamespace(),
	}

	if err := cl.Get(ctx, nnAPIAuth, &apiAuth); err != nil {
		return types.NamespacedName{},
			fmt.Errorf("failed to get APIAuthConfiguration %s for Portal %s", nnAPIAuth, nnPortal)
	}

	return nnAPIAuth, nil
}
