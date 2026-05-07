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

func getAPIAuthConfigurationRefFromParent[
	ParentT parentT,
	ParentTPtr parentTPtr[ParentT],
](
	ctx context.Context,
	cl client.Client,
	obj objectWithParentRef,
) (types.NamespacedName, error) {
	parentRef := obj.GetParentRef()
	if parentRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef ||
		parentRef.NamespacedRef == nil {
		return types.NamespacedName{},
			fmt.Errorf("invalid parent reference: must be a NamespacedRef with a non-nil NamespacedRef field")
	}

	nnParent := types.NamespacedName{
		Name: parentRef.NamespacedRef.Name,
		// TODO: handle cross namespace refs
		Namespace: obj.GetNamespace(),
	}

	var p ParentT
	var parent ParentTPtr = &p
	if err := cl.Get(ctx, nnParent, parent); err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get %T %s: %w", parent, nnParent, err)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	nnAPIAuth := types.NamespacedName{
		Name: parent.GetKonnectAPIAuthConfigurationRef().Name,
		// TODO: handle cross namespace refs
		Namespace: parent.GetNamespace(),
	}

	if err := cl.Get(ctx, nnAPIAuth, &apiAuth); err != nil {
		return types.NamespacedName{},
			fmt.Errorf(
				"failed to get APIAuthConfiguration %s for %T %s: %w",
				nnAPIAuth, parent, nnParent, err,
			)
	}

	return nnAPIAuth, nil
}
