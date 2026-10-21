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

package onprem

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// onPremEventGatewayGVK is the GroupVersionKind of OnPremEventGateway.
var onPremEventGatewayGVK = eventgatewayv1alpha1.SchemeGroupVersion.WithKind("OnPremEventGateway")

// entityWithParent is an entity that can name its parent.
type entityWithParent interface {
	client.Object
	GetParentRef() commonv1alpha1.ObjectRef
	GetParentGVK() schema.GroupVersionKind
}

// mapEntityToOnPremEventGateway returns a MapFunc that enqueues the
// OnPremEventGateway an entity belongs to, if any. When a parent entity can't
// be read, it enqueues every OnPremEventGateway in the namespaces of the walk
// instead: the event is not delivered again, so dropping it could leave a
// gateway out of date.
func mapEntityToOnPremEventGateway(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gateway, ok, err := onPremEventGatewayOf(ctx, cl, obj)
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to find the OnPremEventGateway of an entity, reconciling every gateway of its namespaces",
				"type", fmt.Sprintf("%T", obj), "entity", client.ObjectKeyFromObject(obj))
			walkErr, ok := errors.AsType[*walkError](err)
			if !ok {
				return nil
			}
			return onPremEventGatewaysIn(ctx, cl, walkErr.namespaces...)
		}
		if !ok {
			return nil
		}
		return []reconcile.Request{{NamespacedName: gateway}}
	}
}

// onPremEventGatewayOf returns the OnPremEventGateway obj belongs to,
// directly or through its parent entities. The gateway itself is not read: a
// gateway that does not exist (yet) is still returned, so its reconcile is
// triggered when it exists.
//
// It returns false when the chain ends elsewhere, e.g. at a
// KonnectEventGateway, or cannot be followed, e.g. because a parent entity
// does not exist (yet): the gateway is then triggered by the parent entity
// itself, when it is created. It returns an error when a parent entity can't
// be read.
func onPremEventGatewayOf(ctx context.Context, cl client.Client, obj client.Object) (types.NamespacedName, bool, error) {
	var namespaces []string
	// Each step goes up to an entity of another kind, so the chain can't be
	// longer than the number of kinds, plus the gateway.
	for range len(entityGroupKinds) + 1 {
		entity, ok := obj.(entityWithParent)
		if !ok {
			return types.NamespacedName{}, false, nil
		}
		namespaces = append(namespaces, entity.GetNamespace())
		ref := entity.GetParentRef()
		if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref.NamespacedRef == nil {
			return types.NamespacedName{}, false, nil
		}
		parentNN := types.NamespacedName{
			Namespace: entity.GetNamespace(),
			Name:      ref.NamespacedRef.Name,
		}
		if ns := ref.NamespacedRef.Namespace; ns != nil && *ns != "" {
			parentNN.Namespace = *ns
		}

		parentGVK := entity.GetParentGVK()
		if parentGVK == onPremEventGatewayGVK {
			return parentNN, true, nil
		}
		if _, ok := entityGroupKinds[parentGVK.GroupKind()]; !ok {
			return types.NamespacedName{}, false, nil
		}

		o, err := cl.Scheme().New(parentGVK)
		if err != nil {
			return types.NamespacedName{}, false, &walkError{err: err, namespaces: append(namespaces, parentNN.Namespace)}
		}
		parent, ok := o.(client.Object)
		if !ok {
			return types.NamespacedName{}, false, nil
		}
		if err := cl.Get(ctx, parentNN, parent); err != nil {
			if apierrors.IsNotFound(err) {
				return types.NamespacedName{}, false, nil
			}
			return types.NamespacedName{}, false, &walkError{err: err, namespaces: append(namespaces, parentNN.Namespace)}
		}
		obj = parent
	}
	return types.NamespacedName{}, false, nil
}

// walkError is an error reading a parent entity during the walk to the
// gateway, with the namespaces the walk went through.
type walkError struct {
	err        error
	namespaces []string
}

func (e *walkError) Error() string { return e.err.Error() }
func (e *walkError) Unwrap() error { return e.err }

// onPremEventGatewaysIn returns a request for each OnPremEventGateway in the
// namespaces.
func onPremEventGatewaysIn(ctx context.Context, cl client.Client, namespaces ...string) []reconcile.Request {
	var requests []reconcile.Request
	seen := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		var gateways eventgatewayv1alpha1.OnPremEventGatewayList
		if err := cl.List(ctx, &gateways, client.InNamespace(ns)); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to list OnPremEventGateways", "namespace", ns)
			continue
		}
		for i := range gateways.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gateways.Items[i])})
		}
	}
	return requests
}

// gwAdminServingChanged lets through the changes of a GWAdmin its gateways
// read: whether it is being deleted, and the API it publishes.
func gwAdminServingChanged() predicate.Funcs {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		oldGW, okOld := e.ObjectOld.(*operatorv1alpha1.GWAdmin)
		newGW, okNew := e.ObjectNew.(*operatorv1alpha1.GWAdmin)
		if !okOld || !okNew {
			return true
		}
		return oldGW.DeletionTimestamp.IsZero() != newGW.DeletionTimestamp.IsZero() ||
			!equality.Semantic.DeepEqual(oldGW.Status.API, newGW.Status.API)
	}}
}

// mapGWAdminToOnPremEventGateways returns a MapFunc that enqueues the
// OnPremEventGateways that reference the GWAdmin.
func mapGWAdminToOnPremEventGateways(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		return requestsForGWAdmin(ctx, cl, client.ObjectKeyFromObject(obj))
	}
}

// mapKongReferenceGrantToOnPremEventGateways returns a MapFunc that enqueues
// the OnPremEventGateways the KongReferenceGrant may allow to use their
// GWAdmin: those of the namespaces it grants that reference a GWAdmin of its
// namespace. The other gateways don't depend on it.
func mapKongReferenceGrantToOnPremEventGateways(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		grant, ok := obj.(*configurationv1alpha1.KongReferenceGrant)
		if !ok {
			return nil
		}
		var requests []reconcile.Request
		for _, ns := range gwadminref.NamespacesGrantedBy(grant) {
			var gateways eventgatewayv1alpha1.OnPremEventGatewayList
			if err := cl.List(ctx, &gateways, client.InNamespace(ns)); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "failed to list OnPremEventGateways", "namespace", ns)
				continue
			}
			for i := range gateways.Items {
				if gateways.Items[i].GWAdminKey().Namespace == grant.Namespace {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gateways.Items[i])})
				}
			}
		}
		return requests
	}
}

// mapEndpointSliceToOnPremEventGateways returns a MapFunc that enqueues the
// OnPremEventGateways that reference the GWAdmin whose API Service the
// EndpointSlice belongs to.
func mapEndpointSliceToOnPremEventGateways(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gwAdmin, ok := gwAdminOfEndpointSlice(obj)
		if !ok {
			return nil
		}
		return requestsForGWAdmin(ctx, cl, gwAdmin)
	}
}

// gwAdminOfEndpointSlice returns the GWAdmin whose API Service the
// EndpointSlice belongs to, by its labels, and false when it belongs to no
// API Service of a GWAdmin.
func gwAdminOfEndpointSlice(obj client.Object) (client.ObjectKey, bool) {
	labels := obj.GetLabels()
	name := labels[consts.GatewayOperatorManagedByNameLabel]
	if name == "" {
		return client.ObjectKey{}, false
	}
	for k, v := range apiEndpointSliceLabels(name) {
		if labels[k] != v {
			return client.ObjectKey{}, false
		}
	}
	return client.ObjectKey{Namespace: obj.GetNamespace(), Name: name}, true
}

// requestsForGWAdmin returns a request for each OnPremEventGateway that
// references the GWAdmin, in any namespace.
func requestsForGWAdmin(ctx context.Context, cl client.Client, gwAdmin client.ObjectKey) []reconcile.Request {
	gateways, err := gwadminref.GatewaysOf(ctx, cl, gwAdmin)
	if err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to find the OnPremEventGateways of a GWAdmin", "gwAdmin", gwAdmin)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(gateways))
	for i := range gateways {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gateways[i])})
	}
	return requests
}
