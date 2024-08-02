package konnect

import (
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// ReconciliationWatchOptionsForEntity returns the watch options for the given
// Konnect entity type.
func ReconciliationWatchOptionsForEntity[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
](
	cl client.Client,
	ent TEnt,
) []func(*ctrl.Builder) *ctrl.Builder {
	switch any(ent).(type) {
	case *configurationv1alpha1.KongRoute:
		return []func(*ctrl.Builder) *ctrl.Builder{}
	case *configurationv1alpha1.KongService:
		return []func(*ctrl.Builder) *ctrl.Builder{}
	case *configurationv1.KongConsumer:
		return []func(*ctrl.Builder) *ctrl.Builder{}
	case *konnectv1alpha1.KonnectControlPlane:
		return KonnectControlPlaneReconciliationWatchOptions(cl)
	default:
		panic(fmt.Sprintf("unsupported entity type %T", ent))
	}
}
