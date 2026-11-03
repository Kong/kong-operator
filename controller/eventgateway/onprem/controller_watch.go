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
	"slices"
	"strings"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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
	gateway, ok, _, err := walkToOnPremEventGateway(ctx, cl, obj)
	return gateway, ok, err
}

// walkToOnPremEventGateway is onPremEventGatewayOf, also returning the parent
// entity of the walk that doesn't exist, when one doesn't: "its Kind
// namespace/name" for the parent of obj, "the Kind namespace/name it descends
// from" for one further up.
func walkToOnPremEventGateway(ctx context.Context, cl client.Client, obj client.Object) (types.NamespacedName, bool, string, error) {
	var namespaces []string
	// Each step goes up to an entity of another kind, so the chain can't be
	// longer than the number of kinds, plus the gateway.
	for step := range len(entityGroupKinds) + 1 {
		entity, ok := obj.(entityWithParent)
		if !ok {
			return types.NamespacedName{}, false, "", nil
		}
		namespaces = append(namespaces, entity.GetNamespace())
		ref := entity.GetParentRef()
		if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref.NamespacedRef == nil {
			return types.NamespacedName{}, false, "", nil
		}
		parentNN := refKey(entity, ref.NamespacedRef.Namespace, ref.NamespacedRef.Name)

		parentGVK := entity.GetParentGVK()
		if parentGVK == onPremEventGatewayGVK {
			return parentNN, true, "", nil
		}
		if _, ok := entityGroupKinds[parentGVK.GroupKind()]; !ok {
			return types.NamespacedName{}, false, "", nil
		}

		o, err := cl.Scheme().New(parentGVK)
		if err != nil {
			return types.NamespacedName{}, false, "", &walkError{err: err, namespaces: append(namespaces, parentNN.Namespace)}
		}
		parent, ok := o.(client.Object)
		if !ok {
			return types.NamespacedName{}, false, "", nil
		}
		if err := cl.Get(ctx, parentNN, parent); err != nil {
			if apierrors.IsNotFound(err) {
				missing := parentGVK.Kind + " " + parentNN.String()
				if step == 0 {
					return types.NamespacedName{}, false, "its " + missing, nil
				}
				return types.NamespacedName{}, false, "the " + missing + " it descends from", nil
			}
			return types.NamespacedName{}, false, "", &walkError{err: err, namespaces: append(namespaces, parentNN.Namespace)}
		}
		obj = parent
	}
	return types.NamespacedName{}, false, "", nil
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

// mapKongReferenceGrantToOnPremEventGateways returns a MapFunc that enqueues
// the OnPremEventGateways the KongReferenceGrant may concern: those of the
// namespaces it grants that reference a GWAdmin of its namespace, and those
// of the namespaces whose entities it allows to read Secrets of its
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
		// An entity is in the namespace of its gateway (see
		// configuration.entityID).
		return append(requests, onPremEventGatewaysIn(ctx, cl, namespacesGrantedSecrets(grant)...)...)
	}
}

// namespacesGrantedSecrets returns the namespaces whose entities the
// KongReferenceGrant allows to read Secrets of its namespace.
func namespacesGrantedSecrets(grant *configurationv1alpha1.KongReferenceGrant) []string {
	if !slices.ContainsFunc(grant.Spec.To, func(to configurationv1alpha1.ReferenceGrantTo) bool {
		return to.Group == "core" && to.Kind == "Secret"
	}) {
		return nil
	}
	var namespaces []string
	for _, from := range grant.Spec.From {
		ns := string(from.Namespace)
		if _, ok := entityGroupKinds[schema.GroupKind{Group: string(from.Group), Kind: string(from.Kind)}]; ok &&
			ns != grant.Namespace && !slices.Contains(namespaces, ns) {
			namespaces = append(namespaces, ns)
		}
	}
	return namespaces
}

// secretReaders are the kinds of entity reading values from Secrets: the
// index field of the Secrets they read, and a list of the kind.
var secretReaders = []struct {
	field string
	list  func() client.ObjectList
}{
	{index.IndexFieldEventGatewayBackendClusterOnSecretRef, func() client.ObjectList { return &configurationv1alpha1.EventGatewayBackendClusterList{} }},
	{index.IndexFieldEventGatewayDataPlaneCertificateOnSecretRef, func() client.ObjectList { return &configurationv1alpha1.EventGatewayDataPlaneCertificateList{} }},
	{index.IndexFieldEventGatewayListenerPolicyOnSecretRef, func() client.ObjectList { return &configurationv1alpha1.EventGatewayListenerPolicyList{} }},
	{index.IndexFieldEventGatewaySchemaRegistryOnSecretRef, func() client.ObjectList { return &configurationv1alpha1.EventGatewaySchemaRegistryList{} }},
	{index.IndexFieldEventGatewayStaticKeyOnSecretRef, func() client.ObjectList { return &configurationv1alpha1.EventGatewayStaticKeyList{} }},
	{index.IndexFieldEventGatewayTLSTrustBundleOnSecretRef, func() client.ObjectList { return &configurationv1alpha1.EventGatewayTLSTrustBundleList{} }},
}

// mapSecretToOnPremEventGateways returns a MapFunc that enqueues the
// OnPremEventGateways of the entities reading the Secret: one created, e.g.
// one they were left out for, or changed, e.g. a key rotated, is pushed to
// gw-admin.
func mapSecretToOnPremEventGateways(cl client.Client) handler.MapFunc {
	toGateway := mapEntityToOnPremEventGateway(cl)
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var requests []reconcile.Request
		for _, reader := range secretReaders {
			list := reader.list()
			var items []runtime.Object
			err := cl.List(ctx, list, client.MatchingFields{reader.field: client.ObjectKeyFromObject(obj).String()})
			if err == nil {
				items, err = apimeta.ExtractList(list)
			}
			if err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "failed to list the entities reading a Secret", "secret", client.ObjectKeyFromObject(obj), "type", fmt.Sprintf("%T", list))
				continue
			}
			for _, item := range items {
				entity, ok := item.(client.Object)
				if !ok {
					continue
				}
				for _, req := range toGateway(ctx, entity) {
					if !slices.Contains(requests, req) {
						requests = append(requests, req)
					}
				}
			}
		}
		return requests
	}
}

// mapGatewayToGatewaysOfItsGWAdmin returns a MapFunc that enqueues the
// OnPremEventGateways that reference the GWAdmin of the gateway.
func mapGatewayToGatewaysOfItsGWAdmin(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gateway, ok := obj.(*eventgatewayv1alpha1.OnPremEventGateway)
		if !ok {
			return nil
		}
		return requestsForGWAdmin(ctx, cl, gateway.GWAdminKey())
	}
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

// expectedGateways returns the ids of the gateways the gw-admin of the gateway
// must serve: every OnPremEventGateway allowed to use the GWAdmin, in any
// namespace, not being deleted, and whose UID is a version 4 UUID. gw-admin
// replaces the whole set on every call.
func expectedGateways(ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) ([]uuid.UUID, error) {
	return gatewaysServedBy(ctx, cl, gateway.GWAdminKey())
}

// gatewaysServedBy returns the ids of the gateways the gw-admin of the
// GWAdmin must serve (see expectedGateways).
func gatewaysServedBy(ctx context.Context, cl client.Client, gwAdmin client.ObjectKey) ([]uuid.UUID, error) {
	gateways, err := gwadminref.GatewaysOf(ctx, cl, gwAdmin)
	if err != nil {
		return nil, err
	}
	gateways, err = gwadminref.GatewaysAllowedToUse(ctx, cl, gwAdmin, gateways)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(gateways))
	for i := range gateways {
		g := &gateways[i]
		if !g.DeletionTimestamp.IsZero() {
			continue
		}
		id, ok := uidV4(g)
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return ids, nil
}
