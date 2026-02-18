package specialized

import (
	"context"
	"errors"
	"reflect"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"

	operatorerrors "github.com/kong/kong-operator/v2/internal/errors"
	"github.com/kong/kong-operator/v2/internal/utils/gatewayclass"
)

// -----------------------------------------------------------------------------
// AIGatewayReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) aiGatewayHasMatchingGatewayClass(obj client.Object) bool {
	aigateway, ok := obj.(*operatorv1alpha1.AIGateway)
	if !ok {
		ctrllog.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "Gateway", "found", reflect.TypeOf(obj),
		)
		return false
	}

	_, err := gatewayclass.Get(context.Background(), r.Client, aigateway.Spec.GatewayClassName)
	if err != nil {
		// filtering here is just an optimization, the reconciler will check the
		// class as well. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		return !errors.As(err, &operatorerrors.ErrUnsupportedGatewayClass{}) &&
			!errors.As(err, &operatorerrors.ErrNotAcceptedGatewayClass{})
	}

	return true
}

// -----------------------------------------------------------------------------
// AIGatewayReconciler - Watch Mapping Funcs
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) listAIGatewaysForGatewayClass(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	gatewayClass, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		ctrllog.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return
	}

	aigateways := new(operatorv1alpha1.AIGatewayList)
	if err := r.List(ctx, aigateways); err != nil {
		ctrllog.FromContext(ctx).Error(err, "could not list aigateways in map func")
		return
	}

	for _, aigateway := range aigateways.Items {
		if aigateway.Spec.GatewayClassName == gatewayClass.Name {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: aigateway.Namespace,
					Name:      aigateway.Name,
				},
			})
		}
	}

	return
}

// listAIGatewaysForReferenceGrants lists AIGateways whose group, kind and namespace appeared in `spec.from` of ReferenceGrants.
// The listed AIGateways in are allowed to reference the resources in the `spec.to` of the ReferenceGrant.
func (r *AIGatewayReconciler) listAIGatewaysForReferenceGrants(ctx context.Context, obj client.Object) []reconcile.Request {
	referenceGrant, ok := obj.(*gatewayv1beta1.ReferenceGrant)
	if !ok {
		ctrllog.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "ReferenceGrant", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	namespaces := []string{}
	for _, from := range referenceGrant.Spec.From {
		if from.Group != gatewayv1beta1.Group(operatorv1alpha1.SchemeGroupVersion.Group) || from.Kind != gatewayv1beta1.Kind("AIGateway") {
			continue
		}
		ns := string(from.Namespace)
		namespaces = append(namespaces, ns)
	}

	namespaces = lo.Uniq(namespaces)
	reqs := []reconcile.Request{}
	for _, ns := range namespaces {
		aigateways := new(operatorv1alpha1.AIGatewayList)
		namespacedClient := client.NewNamespacedClient(r.Client, ns)
		if err := namespacedClient.List(ctx, aigateways); err != nil {
			ctrllog.FromContext(ctx).Error(err, "could not list aigateways in namespace", "namespace", ns)
			return nil
		}
		for _, aigateway := range aigateways.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: aigateway.Namespace,
					Name:      aigateway.Name,
				},
			})
		}
	}
	return reqs
}

// referenceGrantReferencesAIGateway is the predicate function for watching ReferenceGrants.
// It returns true if `AIGateway` type is included in the `spec.from`.
func referenceGrantReferencesAIGateway(obj client.Object) bool {
	referenceGrant, ok := obj.(*gatewayv1beta1.ReferenceGrant)
	if !ok {
		return false
	}
	return lo.ContainsBy(referenceGrant.Spec.From, func(from gatewayv1beta1.ReferenceGrantFrom) bool {
		return from.Group == gatewayv1beta1.Group(operatorv1alpha1.SchemeGroupVersion.Group) && from.Kind == gatewayv1beta1.Kind("AIGateway")
	})
}
