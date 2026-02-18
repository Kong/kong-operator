package watch

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorerrors "github.com/kong/kong-operator/v2/internal/errors"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayClass - Watch Predicates
// -----------------------------------------------------------------------------

// GatewayClassMatchesController is a controller runtime watch predicate
// function which can be used to determine whether a given GatewayClass
// belongs to and is served by the current controller.
func GatewayClassMatchesController(obj client.Object) bool {
	gatewayClass, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		ctrllog.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return string(gatewayClass.Spec.ControllerName) == vars.ControllerName()
}
