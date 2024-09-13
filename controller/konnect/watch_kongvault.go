package konnect

import (
	"context"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// KongVaultReconciliationWatchOptions returns the watch options for KongVault.
func KongVaultReconciliationWatchOptions(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongVault{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(kongVaultRefersToKonnectGatewayControlPlane()),
				),
			)
		},
	}
}

func kongVaultRefersToKonnectGatewayControlPlane() func(obj client.Object) bool {
	return func(obj client.Object) bool {
		kongVault, ok := obj.(*configurationv1alpha1.KongVault)
		if !ok {
			ctrllog.FromContext(context.Background()).Error(
				operatorerrors.ErrUnexpectedObject,
				"failed to run predicate function",
				"expected", "KongVault", "found", reflect.TypeOf(obj),
			)
			return false
		}

		cpRef := kongVault.Spec.ControlPlaneRef
		return cpRef != nil && cpRef.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef
	}
}
