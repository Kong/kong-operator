package konnect

import (
	"context"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func listKonnectExtensionsByKonnectGatewayControlPlane(
	ctx context.Context, cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) ([]konnectv1alpha1.KonnectExtension, error) {
	l := konnectv1alpha1.KonnectExtensionList{}
	err := cl.List(
		ctx, &l,
		client.InNamespace(cp.Namespace),
		client.MatchingFields{
			IndexFieldKonnectExtensionOnKonnectGatewayControlPlane: cp.Name,
		},
	)

	return l.Items, err
}

func enqueueKonnectExtensionsForKonnectGatewayControlPlane(cl client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
		if !ok {
			return nil
		}

		konnectExtensions, err := listKonnectExtensionsByKonnectGatewayControlPlane(ctx, cl, cp)
		if err != nil {
			return nil
		}

		reqs := make([]reconcile.Request, 0, len(konnectExtensions))
		for _, ke := range konnectExtensions {
			if ke.Spec.KonnectControlPlane.ControlPlaneRef.Type == commonv1alpha1.ControlPlaneRefKonnectNamespacedRef &&
				ke.Spec.KonnectControlPlane.ControlPlaneRef.KonnectNamespacedRef.Namespace == cp.Namespace &&
				ke.Spec.KonnectControlPlane.ControlPlaneRef.KonnectNamespacedRef.Name == cp.Name {
				reqs = append(reqs, reconcile.Request{
					NamespacedName: k8stypes.NamespacedName{
						Namespace: ke.Namespace,
						Name:      ke.Name,
					},
				})
			}
		}
		return reqs
	}
}
