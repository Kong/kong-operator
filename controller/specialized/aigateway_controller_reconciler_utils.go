package specialized

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/api/gateway-operator/v1alpha1"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/utils/gatewayclass"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// AIGatewayReconciler - Filter Functions
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) verifyGatewayClassSupport(
	ctx context.Context,
	aigateway *v1alpha1.AIGateway,
) (
	*gatewayclass.Decorator,
	error,
) {
	if aigateway.Spec.GatewayClassName == "" {
		return nil, operatorerrors.ErrUnsupportedGateway
	}

	gwc := gatewayclass.NewDecorator()
	if err := r.Client.Get(ctx, client.ObjectKey{
		Name: aigateway.Spec.GatewayClassName,
	},
		gwc.GatewayClass,
	); err != nil {
		return nil, err
	}

	if string(gwc.Spec.ControllerName) != vars.ControllerName() {
		return nil, operatorerrors.ErrUnsupportedGateway
	}

	return gwc, nil
}
