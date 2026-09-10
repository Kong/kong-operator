package watch

import (
	"context"
	"slices"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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

// MapGRPCRouteForClientCertSecret returns a handler.MapFunc that, given a Secret, lists all
// Services in the same namespace that reference it via the konghq.com/client-cert annotation,
// then returns reconcile.Requests for all GRPCRoutes backed by those Services.
func MapGRPCRouteForClientCertSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		return routesForClientCertSecret(ctx, cl, secret.Namespace, secret.Name, kindGRPCRoute)
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

// MapUDPRouteForClientCertSecret returns a handler.MapFunc that, given a Secret, lists all
// Services in the same namespace that reference it via the konghq.com/client-cert annotation,
// then returns reconcile.Requests for all UDPRoutes backed by those Services.
func MapUDPRouteForClientCertSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		return routesForClientCertSecret(ctx, cl, secret.Namespace, secret.Name, kindUDPRoute)
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
		case kindGRPCRoute:
			routeRequests, err = listGRPCRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		case kindTLSRoute:
			routeRequests, err = listTLSRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		case kindTCPRoute:
			routeRequests, err = listTCPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		case kindUDPRoute:
			routeRequests, err = listUDPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		}
		if err != nil {
			continue
		}
		requests = append(requests, routeRequests...)
	}
	return requests
}

// MapHTTPRouteForPluginConfigSecret returns a handler.MapFunc mapping a Secret to the HTTPRoutes
// whose ExtensionRef KongPlugins source their configuration from it.
func MapHTTPRouteForPluginConfigSecret(cl client.Client) handler.MapFunc {
	return mapRoutesForPluginConfigSecret[gwtypes.HTTPRouteList](cl, index.KongPluginsOnHTTPRouteIndex)
}

// MapGRPCRouteForPluginConfigSecret returns a handler.MapFunc mapping a Secret to the GRPCRoutes
// whose ExtensionRef KongPlugins source their configuration from it.
func MapGRPCRouteForPluginConfigSecret(cl client.Client) handler.MapFunc {
	return mapRoutesForPluginConfigSecret[gwtypes.GRPCRouteList](cl, index.KongPluginsOnGRPCRouteIndex)
}

// mapRoutesForPluginConfigSecret returns reconcile.Requests for the routes listed by indexName that
// reference a KongPlugin sourcing its configuration from the given Secret via spec.configFrom or
// spec.configPatches.
func mapRoutesForPluginConfigSecret[
	TList gwtypes.SupportedRouteList,
	TListPtr gwtypes.SupportedRouteListPtr[TList],
](cl client.Client, indexName string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		var requests []reconcile.Request
		for _, pluginKey := range kongPluginsForConfigSecret(ctx, cl, secret.Namespace, secret.Name) {
			var list TList
			var listPtr TListPtr = &list
			if err := cl.List(ctx, listPtr, client.MatchingFields{indexName: pluginKey}); err != nil {
				// Map functions cannot return an error, so log the dropped reconcile.
				log.Error(ctrllog.FromContext(ctx), err, "Failed to list routes for KongPlugin", "kongplugin", pluginKey)
				continue
			}
			requests = append(requests, requestsForRouteList(listPtr)...)
		}
		return lo.Uniq(requests)
	}
}

// requestsForRouteList returns one reconcile.Request per route in the list.
func requestsForRouteList(list client.ObjectList) []reconcile.Request {
	var requests []reconcile.Request
	appendRoute := func(namespace, name string) {
		requests = append(requests, reconcile.Request{Namespace: namespace, Name: name})
	}
	switch l := list.(type) {
	case *gwtypes.HTTPRouteList:
		for _, route := range l.Items {
			appendRoute(route.Namespace, route.Name)
		}
	case *gwtypes.GRPCRouteList:
		for _, route := range l.Items {
			appendRoute(route.Namespace, route.Name)
		}
	}
	return requests
}

// kongPluginsForConfigSecret returns the "namespace/name" keys of the KongPlugins in the given
// namespace that source their configuration from the given Secret, in the format expected by the
// KongPluginsOn*Route field indexes.
func kongPluginsForConfigSecret(ctx context.Context, cl client.Client, secretNamespace, secretName string) []string {
	plugins := &configurationv1.KongPluginList{}
	if err := cl.List(ctx, plugins, client.InNamespace(secretNamespace)); err != nil {
		// Map functions cannot return an error, so log the dropped reconcile.
		log.Error(ctrllog.FromContext(ctx), err, "Failed to list KongPlugins for config Secret",
			"secret", secretNamespace+"/"+secretName)
		return nil
	}

	var keys []string
	for _, plugin := range plugins.Items {
		if !slices.Contains(utils.KongPluginSecretNames(&plugin), secretName) {
			continue
		}
		keys = append(keys, plugin.Namespace+"/"+plugin.Name)
	}
	return keys
}
