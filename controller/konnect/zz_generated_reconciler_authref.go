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
	// TODO: make this generic for all root dependent entities.

	if obj, ok := any(ent).(portalRefAccessor); ok {
		return getAPIAuthConfigurationRefFromPortal(ctx, cl, obj)
	}
	if obj, ok := any(ent).(eventGatewayRefAccessor); ok {
		return getAPIAuthConfigurationRefFromEventGateway(ctx, cl, obj)
	}

	return types.NamespacedName{},
		fmt.Errorf("unsupported entity type %T for getting APIAuthRef", ent)
}

var _ eventGatewayRefAccessor = (*konnectv1alpha1.KonnectEventDataPlaneCertificate)(nil)
