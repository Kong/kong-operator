package watch

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

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
				NamespacedName: client.ObjectKey{
					Namespace: gw.Namespace,
					Name:      gw.Name,
				},
			})
		}

		return requests
	}
}

// MapGatewayForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// lists all Gateways that could be affected by this ReferenceGrant. This includes Gateways
// that reference TLS Secrets in the ReferenceGrant's namespace from a different namespace.
// It returns a slice of reconcile.Requests for each matching Gateway, enabling efficient
// event handling and reconciliation when a ReferenceGrant changes.
func MapGatewayForReferenceGrant(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		rg, ok := obj.(*gwtypes.ReferenceGrant)
		if !ok {
			return nil
		}

		// Check if this ReferenceGrant allows referencing Secrets.
		allowsSecrets := false
		for _, to := range rg.Spec.To {
			if to.Kind == "Secret" {
				allowsSecrets = true
				break
			}
		}
		if !allowsSecrets {
			return nil
		}

		gateways := &gwtypes.GatewayList{}
		err := cl.List(ctx, gateways)
		if err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, gw := range gateways.Items {
			if hasMatchingCrossNamespaceSecretRef(gw, rg) {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKey{
						Namespace: gw.Namespace,
						Name:      gw.Name,
					},
				})
			}
		}
		return requests
	}
}

// hasMatchingCrossNamespaceSecretRef checks if a Gateway has any listener TLS certificate references
// that are cross-namespace and match the given ReferenceGrant's permissions.
func hasMatchingCrossNamespaceSecretRef(gw gwtypes.Gateway, rg *gwtypes.ReferenceGrant) bool {
	for _, listener := range gw.Spec.Listeners {
		if listener.TLS == nil || listener.TLS.CertificateRefs == nil {
			continue
		}
		for _, certRef := range listener.TLS.CertificateRefs {
			if certRef.Kind != nil && *certRef.Kind != "Secret" {
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
