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
)

func getAPIAuthConfigurationRefFromParent[
	ParentT parentT,
	ParentTPtr parentWithAPIAuthTPtr[ParentT],
](
	ctx context.Context,
	cl client.Client,
	obj client.Object,
	parentRef commonv1alpha1.ObjectRef,
) (types.NamespacedName, error) {
	parent, nnParent, err := getParentForRef[ParentT, ParentTPtr](ctx, cl, parentRef, obj.GetNamespace())
	if err != nil {
		return types.NamespacedName{}, err
	}

	authRef := parent.GetKonnectAPIAuthConfigurationRef()
	nnAPIAuth, err := getAPIAuthConfigurationRefNN(ctx, cl, parent, authRef.Name, authRef.Namespace)
	if err != nil {
		return types.NamespacedName{},
			fmt.Errorf("failed to resolve APIAuthConfiguration for %T %s: %w", parent, nnParent, err)
	}

	return nnAPIAuth, nil
}
