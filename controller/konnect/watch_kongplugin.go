package konnect

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/pkg/annotations"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// mapKongServices enqueue requests for KongPlugin objects based on KongService annotations.
func (r *KongPluginReconciler) mapKongServices(ctx context.Context, obj client.Object) []ctrl.Request {
	logger := log.GetLogger(ctx, "KongPlugin", r.developmentMode)
	kongService, ok := obj.(*configurationv1alpha1.KongService)
	if !ok {
		log.Error(logger, errors.New("cannot cast object to KongService"), "KongService mapping handler", obj)
		return []ctrl.Request{}
	}

	return mapObjectRequestsForItsPlugins(kongService)
}

// mapKongRoutes enqueue requests for KongPlugin objects based on KongRoute annotations.
func (r *KongPluginReconciler) mapKongRoutes(ctx context.Context, obj client.Object) []ctrl.Request {
	logger := log.GetLogger(ctx, "KongPlugin", r.developmentMode)
	kongRoute, ok := obj.(*configurationv1alpha1.KongRoute)
	if !ok {
		log.Error(logger, errors.New("cannot cast object to KongRoute"), "KongRoute mapping handler", obj)
		return []ctrl.Request{}
	}

	return mapObjectRequestsForItsPlugins(kongRoute)
}

// mapKongPluginBindings enqueue requests for KongPlugins referenced by KongPluginBindings in their .spec.pluginRef field.
func (r *KongPluginReconciler) mapKongPluginBindings(ctx context.Context, obj client.Object) []ctrl.Request {
	logger := log.GetLogger(ctx, "KongPlugin", r.developmentMode)
	kongPluginBinding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		log.Error(logger, errors.New("cannot cast object to KongPluginBinding"), "KongPluginBinding mapping handler", obj)
		return []ctrl.Request{}
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

func mapObjectRequestsForItsPlugins(obj client.Object) []ctrl.Request {
	var (
		namespace = obj.GetNamespace()
		plugins   = annotations.ExtractPlugins(obj)
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
