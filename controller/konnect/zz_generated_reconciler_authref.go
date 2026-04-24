package konnect

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	// TODO: make this generic for all root dependent entities.
	obj, ok := any(ent).(portalRefAccessor)
	if ok {
		return getAPIAuthConfigurationRefFromPortal(ctx, cl, obj)
	}

	return types.NamespacedName{},
		fmt.Errorf("unsupported entity type %T for getting APIAuthRef", ent)
}
