package controllers

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"
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
	return decorateGatewayClass(gwc).isControlled()
}
