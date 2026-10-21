package watch

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// MapGatewayForKongCertificate enqueues Gateways referencing an externally managed certificate.
func MapGatewayForKongCertificate(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		if _, ok := obj.(*configurationv1alpha1.KongCertificate); !ok {
			return nil
		}
		var gateways gwtypes.GatewayList
		if err := cl.List(ctx, &gateways, client.MatchingFields{
			index.TLSKongCertificatesOnGatewayIndex: client.ObjectKeyFromObject(obj).String(),
		}); err != nil {
			ctrllog.FromContext(ctx).Error(err, "Failed to list Gateways referencing KongCertificate",
				"certificate", client.ObjectKeyFromObject(obj))
			return nil
		}
		requests := make([]reconcile.Request, 0, len(gateways.Items))
		for _, gateway := range gateways.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gateway)})
		}
		return requests
	}
}

// MapGatewayForKongReferenceGrant enqueues Gateways whose generated SNI references may be affected.
func MapGatewayForKongReferenceGrant(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		grant, ok := obj.(*configurationv1alpha1.KongReferenceGrant)
		if !ok {
			return nil
		}
		var gateways gwtypes.GatewayList
		var requests []reconcile.Request
		for _, from := range grant.Spec.From {
			if string(from.Group) != configurationv1alpha1.GroupVersion.Group || from.Kind != "KongSNI" {
				continue
			}
			if err := cl.List(ctx, &gateways, client.InNamespace(string(from.Namespace))); err != nil {
				ctrllog.FromContext(ctx).Error(err, "Failed to list Gateways affected by KongReferenceGrant",
					"grant", client.ObjectKeyFromObject(grant))
				return nil
			}
			for _, gateway := range gateways.Items {
				for _, listener := range gateway.Spec.Listeners {
					if listener.TLS == nil {
						continue
					}
					for _, ref := range listener.TLS.CertificateRefs {
						if refs.IsKongCertificateRef(ref) && ref.Namespace != nil && string(*ref.Namespace) == grant.Namespace {
							requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gateway)})
						}
					}
				}
			}
		}
		return requests
	}
}

// MapGatewayForTLSSecret returns a handler.MapFunc that, given a Secret object,
// finds all Gateways that reference this Secret in their listener TLS configuration
// using the TLSCertificateSecretsOnGateway index.
// It returns a slice of reconcile.Requests for each matching Gateway, enabling efficient
// event handling and reconciliation when a TLS Secret changes.
func MapGatewayForTLSSecret(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		// Use the index to find Gateways that reference this Secret.
		secretKey := fmt.Sprintf("%s/%s", secret.Namespace, secret.Name)
		gateways := &gwtypes.GatewayList{}
		err := cl.List(ctx, gateways, client.MatchingFields{
			index.TLSCertificateSecretsOnGatewayIndex: secretKey,
		})

		if err != nil {
			return nil
		}

		requests := make([]reconcile.Request, 0, len(gateways.Items))
		for _, gw := range gateways.Items {
			requests = append(requests, reconcile.Request{
				Namespace: gw.Namespace,
				Name:      gw.Name,
			})
		}

		return requests
	}
}

// MapGatewayForGatewayClass returns a handler.MapFunc that, given a GatewayClass object, lists
// all Gateways referencing it (using the GatewayClassOnGatewayIndex) and enqueues them.
// The Gateway translation reads the GatewayClass's konghq.com/tags annotation when tagging the
// KongCertificate and KongSNI built from listener TLS, so a change to the GatewayClass has to
// re-reconcile the Gateways under it.
func MapGatewayForGatewayClass(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gatewayClass, ok := obj.(*gwtypes.GatewayClass)
		if !ok {
			return nil
		}

		gateways := &gwtypes.GatewayList{}
		if err := cl.List(ctx, gateways, client.MatchingFields{
			index.GatewayClassOnGatewayIndex: gatewayClass.Name,
		}); err != nil {
			return nil
		}

		requests := make([]reconcile.Request, 0, len(gateways.Items))
		for _, gateway := range gateways.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&gateway),
			})
		}
		return requests
	}
}

// MapGatewayForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// lists all Gateways that could be affected by this ReferenceGrant. This includes Gateways
// that reference TLS Secrets or KongCertificates in the ReferenceGrant's namespace.
// It returns a slice of reconcile.Requests for each matching Gateway, enabling efficient
// event handling and reconciliation when a ReferenceGrant changes.
func MapGatewayForReferenceGrant(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		rg, ok := k8sutils.AsReferenceGrant(obj)
		if !ok {
			return nil
		}

		allowsCertificates := false
		for _, to := range rg.Spec.To {
			if (to.Group == "" && to.Kind == "Secret") ||
				(string(to.Group) == configurationv1alpha1.GroupVersion.Group && to.Kind == "KongCertificate") {
				allowsCertificates = true
				break
			}
		}
		if !allowsCertificates {
			return nil
		}

		gateways := &gwtypes.GatewayList{}
		err := cl.List(ctx, gateways)
		if err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, gw := range gateways.Items {
			if hasMatchingCrossNamespaceCertificateRef(gw, rg) {
				requests = append(requests, reconcile.Request{
					Namespace: gw.Namespace,
					Name:      gw.Name,
				})
			}
		}
		return requests
	}
}

// hasMatchingCrossNamespaceCertificateRef checks if a Gateway has any listener TLS certificate references
// that are cross-namespace and match the given ReferenceGrant's permissions.
func hasMatchingCrossNamespaceCertificateRef(gw gwtypes.Gateway, rg *gwtypes.ReferenceGrant) bool {
	for _, listener := range gw.Spec.Listeners {
		if listener.TLS == nil || listener.TLS.CertificateRefs == nil {
			continue
		}
		for _, certRef := range listener.TLS.CertificateRefs {
			if !refs.IsKongCertificateRef(certRef) &&
				((certRef.Group != nil && *certRef.Group != "") || (certRef.Kind != nil && *certRef.Kind != "Secret")) {
				continue
			}
			certNamespace := gw.Namespace
			if certRef.Namespace != nil && *certRef.Namespace != "" {
				certNamespace = string(*certRef.Namespace)
			}
			// Check if this is a cross-namespace reference to the ReferenceGrant's namespace.
			if certNamespace == rg.Namespace && gw.Namespace != rg.Namespace {
				// Check if the ReferenceGrant allows this Gateway's namespace to reference.
				for _, from := range rg.Spec.From {
					if from.Kind == "Gateway" && (from.Group == "" || from.Group == gwtypes.GroupName) {
						if string(from.Namespace) == gw.Namespace {
							return true
						}
					}
				}
			}
		}
	}
	return false
}
