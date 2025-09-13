package konnect

import (
	"context"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/internal/utils/index"
)

func enqueueKonnectExtensionsForKonnectGatewayControlPlane(cl client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
		if !ok {
			return nil
		}

		konnectExtensionList := konnectv1alpha2.KonnectExtensionList{}
		if err := cl.List(
			ctx,
			&konnectExtensionList,
			client.InNamespace(cp.Namespace),
			client.MatchingFields{
				index.IndexFieldKonnectExtensionOnKonnectGatewayControlPlane: cp.Name,
			},
		); err != nil {
			return nil
		}

		return lo.Map(konnectExtensionList.Items, func(ext konnectv1alpha2.KonnectExtension, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&ext),
			}
		})
	}
}
