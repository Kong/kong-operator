package gatewayclass

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"

	operatorerrors "github.com/kong/kong-operator/internal/errors"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/pkg/vars"
)

// Get returns a decorated GatewayClass object for the provided GatewayClass name. If the GatewayClass is
// not found or is not supported, an `ErrUnsupportedGatewayClass` error is returned with a descriptive message.
// If the GatewayClass is not accepted, an `ErrNotAcceptedGatewayClass` error is returned with a descriptive message.
func Get(ctx context.Context, cl client.Client, gatewayClassName string) (*Decorator, error) {
	if gatewayClassName == "" {
		return nil, operatorerrors.NewErrUnsupportedGateway("no GatewayClassName provided")
	}

	gwc := NewDecorator()
	if err := cl.Get(ctx, client.ObjectKey{Name: gatewayClassName}, gwc.GatewayClass); err != nil {
		return nil, fmt.Errorf("error while fetching GatewayClass %q: %w", gatewayClassName, err)
	}

	if string(gwc.Spec.ControllerName) != vars.ControllerName() {
		return nil, operatorerrors.NewErrUnsupportedGateway(fmt.Sprintf(
			"GatewayClass %q with %q ControllerName does not match expected %q",
			gatewayClassName,
			gwc.Spec.ControllerName,
			vars.ControllerName(),
		))
	}
	acceptedCondition, found := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.GatewayClassConditionStatusAccepted), gwc)
	if !found || acceptedCondition.Status != metav1.ConditionTrue {
		return nil, operatorerrors.NewErrNotAcceptedGatewayClass(gatewayClassName, acceptedCondition)
	}

	return gwc, nil
}
