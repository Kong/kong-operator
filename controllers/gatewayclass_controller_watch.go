package controllers

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayClassReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *GatewayClassReconciler) gatewayClassMatches(obj client.Object) bool {
	gwc, ok := obj.(*gatewayv1beta1.GatewayClass)
	if !ok {
		log.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return isGatewayClassControlled(gwc)
}

// isGatewayClassControlled returns boolean if the GatewayClass is controlled by this controller.
func isGatewayClassControlled(gwc *gatewayv1beta1.GatewayClass) bool {
	return string(gwc.Spec.ControllerName) == vars.ControllerName
}
