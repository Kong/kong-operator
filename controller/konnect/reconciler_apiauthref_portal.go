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

func getAPIAuthConfigurationRefFromPortal[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (types.NamespacedName, error) {
	var portalRef commonv1alpha1.ObjectRef
	// TODO: add a method like GetParentRef() to non root entities to avoid this type switch.
	switch e := any(ent).(type) {
	case *konnectv1alpha1.IdentityProviderRequest:
		portalRef = e.Spec.PortalRef
	case *konnectv1alpha1.PortalTeam:
		portalRef = e.Spec.PortalRef
	default:
		return types.NamespacedName{}, fmt.Errorf("unsupported entity type %T for getting PortalRef", e)
	}

	nnPortal := types.NamespacedName{
		Name: portalRef.NamespacedRef.Name,
		// TODO: handle cross namespace refs
		Namespace: ent.GetNamespace(),
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
