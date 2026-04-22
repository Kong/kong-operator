package konnect

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
)

func getAPIAuthRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (types.NamespacedName, error) {
	switch e := any(ent).(type) {
	case *konnectv1alpha1.IdentityProviderRequest:
		return getAPIAuthConfigurationRefFromPortal(ctx, cl, e)
	default:
		return types.NamespacedName{}, fmt.Errorf("unsupported entity type %T for getting APIAuthRef", e)
	}
}
