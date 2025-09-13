package gatewayclass

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/utils/gatewayclass"
	"github.com/kong/gateway-operator/internal/utils/index"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// -----------------------------------------------------------------------------
// GatewayClassReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *Reconciler) gatewayClassMatches(obj client.Object) bool {
	gwc, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		ctrllog.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return gatewayclass.DecorateGatewayClass(gwc).IsControlled()
}

// listGatewayClassesForGatewayConfig is a watch predicate which finds all GatewayClasses
// that use a specific GatewayConfiguration.
func (r *Reconciler) listGatewayClassesForGatewayConfig(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)

	gatewayConfig, ok := obj.(*operatorv1beta1.GatewayConfiguration)
	if !ok {
		logger.Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayConfiguration", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	var gatewayClassList gatewayv1.GatewayClassList
	if err := r.List(ctx, &gatewayClassList, client.MatchingFields{
		index.GatewayClassOnGatewayConfigurationIndex: client.ObjectKeyFromObject(gatewayConfig).String(),
	}); err != nil {
		ctrllog.FromContext(ctx).Error(
			fmt.Errorf("unexpected error occurred while listing GatewayClass resources"),
			"failed to run map funcs",
			"error", err.Error(),
		)
		return nil
	}

	var recs []reconcile.Request
	for _, gwc := range gatewayClassList.Items {
		if gwc.Spec.ParametersRef == nil {
			continue
		}

		params := gwc.Spec.ParametersRef
		if string(params.Group) == operatorv1beta1.SchemeGroupVersion.Group &&
			string(params.Kind) == "GatewayConfiguration" &&
			params.Name == gatewayConfig.Name {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: gwc.Name,
				},
			})
		}
	}

	return recs
}
