package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kong/gateway-operator/pkg/consts"
)

var kongPluginsAnnotationChangedPredicate = predicate.Funcs{
	CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
		_, ok := e.Object.GetAnnotations()[consts.PluginsAnnotationKey]
		return ok
	},
	UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return false
		}
		return e.ObjectNew.GetAnnotations()[consts.PluginsAnnotationKey] !=
			e.ObjectOld.GetAnnotations()[consts.PluginsAnnotationKey]
	},
	DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
		_, ok := e.Object.GetAnnotations()[consts.PluginsAnnotationKey]
		return ok
	},
}
