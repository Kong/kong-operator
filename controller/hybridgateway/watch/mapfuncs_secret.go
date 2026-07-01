package watch

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
)

// MapHTTPRouteForClientCertSecret returns a handler.MapFunc that, given a Secret, lists all
// Services in the same namespace that reference it via the konghq.com/client-cert annotation,
// then returns reconcile.Requests for all HTTPRoutes backed by those Services.
func MapHTTPRouteForClientCertSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		return routesForClientCertSecret(ctx, cl, secret.Namespace, secret.Name, kindHTTPRoute)
	}
}

// MapTLSRouteForClientCertSecret returns a handler.MapFunc that, given a Secret, lists all
// Services in the same namespace that reference it via the konghq.com/client-cert annotation,
// then returns reconcile.Requests for all TLSRoutes backed by those Services.
func MapTLSRouteForClientCertSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		return routesForClientCertSecret(ctx, cl, secret.Namespace, secret.Name, kindTLSRoute)
	}
}

// MapTCPRouteForClientCertSecret returns a handler.MapFunc that, given a Secret, lists all
// Services in the same namespace that reference it via the konghq.com/client-cert annotation,
// then returns reconcile.Requests for all TCPRoutes backed by those Services.
func MapTCPRouteForClientCertSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		return routesForClientCertSecret(ctx, cl, secret.Namespace, secret.Name, kindTCPRoute)
	}
}

// routesForClientCertSecret finds all routes whose backend Services
// carry konghq.com/client-cert: <secretName>, and returns reconcile.Requests for them.
func routesForClientCertSecret(ctx context.Context, cl client.Client, secretNamespace, secretName, routeKind string) []reconcile.Request {
	svcList := &corev1.ServiceList{}
	if err := cl.List(ctx, svcList, client.InNamespace(secretNamespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, svc := range svcList.Items {
		if metadata.ExtractClientCertificate(svc.Annotations) != secretName {
			continue
		}
		var routeRequests []reconcile.Request
		var err error
		switch routeKind {
		case kindHTTPRoute:
			routeRequests, err = listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		case kindTLSRoute:
			routeRequests, err = listTLSRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		case kindTCPRoute:
			routeRequests, err = listTCPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		}
		if err != nil {
			continue
		}
		requests = append(requests, routeRequests...)
	}
	return requests
}
