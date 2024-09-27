package konnect

import (
	"context"
	"fmt"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"

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
	case *configurationv1alpha1.KongCredentialBasicAuth:
		return kongCredentialBasicAuthReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongCredentialAPIKey:
		return kongCredentialAPIKeyReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongCredentialACL:
		return kongCredentialACLReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongCACertificate:
		return KongCACertificateReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongCertificate:
		return KongCertificateReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongTarget:
		return KongTargetReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongVault:
		return KongVaultReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongKey:
		return KongKeyReconciliationWatchOptions(cl)
	case *configurationv1alpha1.KongKeySet:
		return KongKeySetReconciliationWatchOptions(cl)
	default:
		panic(fmt.Sprintf("unsupported entity type %T", ent))
	}
}

// objRefersToKonnectGatewayControlPlane returns true if the object
// refers to a KonnectGatewayControlPlane.
func objRefersToKonnectGatewayControlPlane[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](obj client.Object) bool {
	ent, ok := obj.(TEnt)
	if !ok {
		ctrllog.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", constraints.EntityTypeName[T](), "found", reflect.TypeOf(obj),
		)
		return false
	}

	cpRef, ok := getControlPlaneRef(ent).Get()
	if !ok {
		return false
	}
	return cpRef.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef
}
