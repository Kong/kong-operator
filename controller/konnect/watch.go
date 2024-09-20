package konnect

import (
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/constraints"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// ReconciliationWatchOptionsForEntity returns the watch options for the given
// Konnect entity type.
func ReconciliationWatchOptionsForEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	cl client.Client,
	ent TEnt,
) []func(*ctrl.Builder) *ctrl.Builder {
	switch any(ent).(type) {
	case *configurationv1beta1.KongConsumerGroup:
		return KongConsumerGroupReconciliationWatchOptions(cl)
	case *configurationv1.KongConsumer:
		return KongConsumerReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongRoute:
		return KongRouteReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongService:
		return KongServiceReconciliationWatchOptions(cl)
	case *konnectv1alpha1.KonnectGatewayControlPlane:
		return KonnectGatewayControlPlaneReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongPluginBinding:
		return KongPluginBindingReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongUpstream:
		return KongUpstreamReconciliationWatchOptions(cl)
	case *configurationv1alpha1.CredentialBasicAuth:
		return CredentialBasicAuthReconciliationWatchOptions(cl)
	default:
		panic(fmt.Sprintf("unsupported entity type %T", ent))
	}
}
