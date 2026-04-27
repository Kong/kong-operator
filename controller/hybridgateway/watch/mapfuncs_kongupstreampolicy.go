package watch

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
)

// MapHTTPRouteForKongUpstreamPolicy returns a handler.MapFunc that, given a KongUpstreamPolicy,
// lists all Services in its namespace that reference it via the konghq.com/upstream-policy
// annotation, then returns reconcile.Requests for all HTTPRoutes backed by those Services.
func MapHTTPRouteForKongUpstreamPolicy(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		policy, ok := obj.(*configurationv1beta1.KongUpstreamPolicy)
		if !ok {
			return nil
		}

		svcList := &corev1.ServiceList{}
		if err := cl.List(ctx, svcList, client.InNamespace(policy.Namespace)); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, svc := range svcList.Items {
			if svc.Annotations[configurationv1beta1.KongUpstreamPolicyAnnotationKey] != policy.Name {
				continue
			}
			routeRequests, err := listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
			if err != nil {
				continue
			}
			requests = append(requests, routeRequests...)
		}
		return requests
	}
}
