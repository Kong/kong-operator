package watch

import (
	"context"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// MapRouteForClientCertificate also finds routes that have no generated KongService
// yet (or whose old binding was removed after revocation). External certificates
// and grants must never receive hybrid-routes ownership annotations.
func MapRouteForClientCertificate(cl client.Client, routeKind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var requests []reconcile.Request
		var key string
		switch obj.(type) {
		case *configurationv1alpha1.KongCertificate:
			requests = MapRouteForKongResource[*configurationv1alpha1.KongCertificate](routeKind)(ctx, obj)
			key = client.ObjectKeyFromObject(obj).String()
		case *configurationv1alpha1.KongReferenceGrant:
			requests = MapRouteForKongResource[*configurationv1alpha1.KongReferenceGrant](routeKind)(ctx, obj)
			key = obj.GetNamespace() + "/*"
		default:
			return nil
		}
		var services corev1.ServiceList
		if err := cl.List(ctx, &services, client.MatchingFields{index.ClientKongCertificateOnBackendServiceIndex: key}); err != nil {
			ctrllog.FromContext(ctx).Error(err, "Failed to list backend Services for client certificate change",
				"object", client.ObjectKeyFromObject(obj))
			return requests
		}
		for _, service := range services.Items {
			ref, err := metadata.ExtractClientCertificateRef(service.Annotations, service.Namespace)
			if err != nil || ref == nil || *ref.Namespace != obj.GetNamespace() {
				continue
			}
			if _, certificate := obj.(*configurationv1alpha1.KongCertificate); certificate && ref.Name != obj.GetName() {
				continue
			}
			// A grant update can revoke old permissions as well as add new ones.
			// Enqueue conservatively by target namespace, not just its current rules
			// or the backend Service namespace (which is not the KongService namespace).
			var mapper handler.MapFunc
			switch routeKind {
			case kindHTTPRoute:
				mapper = MapRouteForService(cl, gwtypes.HTTPRoute{})
			case kindGRPCRoute:
				mapper = MapRouteForService(cl, gwtypes.GRPCRoute{})
			case kindTLSRoute:
				mapper = MapRouteForService(cl, gwtypes.TLSRoute{})
			case kindTCPRoute:
				mapper = MapRouteForService(cl, gwtypes.TCPRoute{})
			case kindUDPRoute:
				mapper = MapRouteForService(cl, gwtypes.UDPRoute{})
			default:
				continue
			}
			requests = append(requests, mapper(ctx, &service)...)
		}
		return lo.Uniq(requests)
	}
}
