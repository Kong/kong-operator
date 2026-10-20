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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
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
