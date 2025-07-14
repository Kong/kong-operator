package konnect

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1beta1"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/modules/manager/logging"
)

// mapPluginsFromAnnotation enqueue requests for KongPlugins based on
// provided object's annotations.
func mapPluginsFromAnnotation[
	T interface {
		configurationv1alpha1.KongService |
			configurationv1alpha1.KongRoute |
			configurationv1.KongConsumer |
			configurationv1beta1.KongConsumerGroup
		GetTypeName() string
	},
](loggingMode logging.Mode) func(ctx context.Context, obj client.Object) []ctrl.Request {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		_, ok := any(obj).(*T)
		if !ok {
			entityTypeName := constraints.EntityTypeName[T]()
			logger := log.GetLogger(ctx, entityTypeName, loggingMode)
			log.Error(logger,
				fmt.Errorf("cannot cast object to %s", entityTypeName),
				fmt.Sprintf("%s mapping handler", entityTypeName),
			)
			return []ctrl.Request{}
		}

		var (
			namespace = obj.GetNamespace()
			plugins   = metadata.ExtractPlugins(obj)
			requests  = make([]ctrl.Request, 0, len(plugins))
		)

		for _, p := range plugins {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: namespace,
					Name:      p,
				},
			})
		}
		return requests
	}
}

// mapKongPluginBindings enqueue requests for KongPlugins referenced by KongPluginBindings in their .spec.pluginRef field.
func (r *KongPluginReconciler) mapKongPluginBindings(ctx context.Context, obj client.Object) []ctrl.Request {
	logger := log.GetLogger(ctx, "KongPlugin", r.loggingMode)
	kongPluginBinding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		log.Error(logger,
			errors.New("cannot cast object to KongPluginBinding"),
			"KongPluginBinding mapping handler",
		)
		return []ctrl.Request{}
	}

	// If the KongPluginBinding is unmanaged (created not using an annotation,
	// and thus not having KongPlugin as an owner), do not enqueue it.
	if !ownerRefIsAnyKongPlugin(kongPluginBinding) {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: kongPluginBinding.Namespace,
				Name:      kongPluginBinding.Spec.PluginReference.Name,
			},
		},
	}
}
