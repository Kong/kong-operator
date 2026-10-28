/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gwadminsync

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// RequestsFunc returns the requests of a controller for a GWAdmin, e.g. one
// for each object it pushes to the GWAdmin.
type RequestsFunc func(ctx context.Context, gwAdmin types.NamespacedName) []reconcile.Request

// Watch makes the controller watch what the Syncer reads of a GWAdmin: a
// change to the GWAdmin, e.g. once it publishes its API, to its Secrets,
// e.g. once the operator client certificate is issued, or to the endpoints
// of its API Service, as gw-admin keeps nothing across a restart and a new
// or restarted pod changes them, triggers the requests of the GWAdmin. What
// the Syncer keeps of a deleted GWAdmin is forgotten.
//
// It also makes the controller reconcile the object TryLock wakes, once the
// lock it waits for is released.
func (s *Syncer) Watch(b *builder.Builder, requests RequestsFunc) *builder.Builder {
	return b.
		Watches(&operatorv1alpha1.GWAdmin{}, handler.EnqueueRequestsFromMapFunc(mapGWAdmin(requests)),
			builder.WithPredicates(servingChanged(), s.forgetDeleted())).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(mapSecret(requests))).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(mapEndpointSlice(requests))).
		WatchesRawSource(source.Channel(s.wakeupChannel(), &handler.EnqueueRequestForObject{}))
}

// forgetDeleted lets every event through, and forgets what the Syncer keeps
// of a deleted GWAdmin.
func (s *Syncer) forgetDeleted() predicate.Funcs {
	return predicate.Funcs{DeleteFunc: func(e event.DeleteEvent) bool {
		s.forgetGWAdmin(client.ObjectKeyFromObject(e.Object))
		return true
	}}
}

// servingChanged lets through the changes of a GWAdmin its controllers read:
// whether it is being deleted, the API it publishes and its TLS material,
// which a Syncer reads, and its cp endpoint, which they publish.
func servingChanged() predicate.Funcs {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		oldGW, okOld := e.ObjectOld.(*operatorv1alpha1.GWAdmin)
		newGW, okNew := e.ObjectNew.(*operatorv1alpha1.GWAdmin)
		if !okOld || !okNew {
			return true
		}
		return oldGW.DeletionTimestamp.IsZero() != newGW.DeletionTimestamp.IsZero() ||
			!equality.Semantic.DeepEqual(oldGW.Status.API, newGW.Status.API) ||
			!equality.Semantic.DeepEqual(oldGW.Status.TLS, newGW.Status.TLS) ||
			!equality.Semantic.DeepEqual(oldGW.Status.CP, newGW.Status.CP)
	}}
}

// mapGWAdmin returns a MapFunc that enqueues the requests of the GWAdmin.
func mapGWAdmin(requests RequestsFunc) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		return requests(ctx, client.ObjectKeyFromObject(obj))
	}
}

// mapSecret returns a MapFunc that enqueues the requests of the GWAdmin a
// Secret belongs to.
func mapSecret(requests RequestsFunc) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gwAdmin, ok := gwadminref.OfSecret(obj)
		if !ok {
			return nil
		}
		return requests(ctx, gwAdmin)
	}
}

// mapEndpointSlice returns a MapFunc that enqueues the requests of the
// GWAdmin whose API Service the EndpointSlice belongs to.
func mapEndpointSlice(requests RequestsFunc) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gwAdmin, ok := ofEndpointSlice(obj)
		if !ok {
			return nil
		}
		return requests(ctx, gwAdmin)
	}
}

// ofEndpointSlice returns the GWAdmin whose API Service the EndpointSlice
// belongs to, by its labels, and false when it belongs to no API Service of
// a GWAdmin.
func ofEndpointSlice(obj client.Object) (types.NamespacedName, bool) {
	labels := obj.GetLabels()
	name := labels[consts.GatewayOperatorManagedByNameLabel]
	if name == "" {
		return types.NamespacedName{}, false
	}
	for k, v := range apiEndpointSliceLabels(name) {
		if labels[k] != v {
			return types.NamespacedName{}, false
		}
	}
	return types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}, true
}
