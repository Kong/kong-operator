package konnect

import (
	"context"
	"errors"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/controller/pkg/log"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// mapKongServices enqueue requests for KongPlugin objects based on KongService annotations.
func (r *KongPluginReconciler) mapKongServices(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.GetLogger(ctx, "KongPlugin", r.developmentMode)
	kongService, ok := obj.(*configurationv1alpha1.KongService)
	if !ok {
		log.Error(logger, errors.New("cannot cast object to KongService"), "KongService mapping handler", obj)
		return []ctrl.Request{}
	}

	requests := []ctrl.Request{}
	if plugins, ok := kongService.Annotations["konghq.com/plugins"]; ok {
		for _, p := range strings.Split(plugins, ",") {
			kp := configurationv1.KongPlugin{}
			if r.client.Get(ctx, client.ObjectKey{Namespace: kongService.Namespace, Name: p}, &kp) == nil {
				requests = append(requests, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Namespace: kp.Namespace,
						Name:      kp.Name,
					},
				})
			}
		}
	}
	return requests
}

// mapKongPluginBindings enqueue requests for KongPlugins referenced by KongPluginBindings in their .spec.pluginRef field.
func (r *KongPluginReconciler) mapKongPluginBindings(ctx context.Context, obj client.Object) []reconcile.Request {
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
