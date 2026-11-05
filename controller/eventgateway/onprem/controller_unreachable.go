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
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// An entity whose OnPremEventGateway, directly or through its parent
// entities, doesn't exist is part of no configuration: no reconcile of a
// gateway sets its status. One small controller per kind of entity reports
// it. Once the gateway exists, it stays silent: the reconcile of the gateway
// sets the status. The same goes for an entity whose parent entity doesn't
// exist, but only when the status of the entity was set for an
// OnPremEventGateway: nothing else tells whether it was meant for Konnect or
// on-prem, and the Konnect controllers report the others.

// The parent entities of each kind of entity, parentEntities, are generated
// in zz_generated_entities.go.

// parentEntity is a kind of parent entity of a kind of entity.
type parentEntity struct {
	// parent is an object of the kind of the parent.
	parent client.Object
	// list returns an empty list of the kind of the children.
	list func() client.ObjectList
	// field is the index field of the children by parent.
	field string
}

// childEntity is a kind of child entity of a kind of entity.
type childEntity struct {
	// child is the type of the child.
	child reflect.Type
	// list returns an empty list of the kind of the children.
	list func() client.ObjectList
	// field is the index field of the children by parent.
	field string
}

// childEntities are, by kind of entity, its kinds of child entity: the
// reverse of parentEntities.
var childEntities = func() map[reflect.Type][]childEntity {
	children := map[reflect.Type][]childEntity{}
	for child, parents := range parentEntities {
		for _, p := range parents {
			t := reflect.TypeOf(p.parent)
			children[t] = append(children[t], childEntity{child: child, list: p.list, field: p.field})
		}
	}
	return children
}()

// ancestorEntities returns the kinds of entity the kind of entity descends
// from, through any number of parents, by type, with an object of each: e.g.
// the virtual cluster and the backend cluster of a virtual cluster policy.
func ancestorEntities(t reflect.Type) map[reflect.Type]client.Object {
	ancestors := map[reflect.Type]client.Object{}
	pending := []reflect.Type{t}
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		for _, p := range parentEntities[current] {
			pt := reflect.TypeOf(p.parent)
			if _, ok := ancestors[pt]; ok {
				continue
			}
			ancestors[pt] = p.parent
			pending = append(pending, pt)
		}
	}
	return ancestors
}

// setupUnreachable sets up the controllers reporting the entities whose
// OnPremEventGateway or parent entity doesn't exist.
func (r *Reconciler) setupUnreachable(mgr ctrl.Manager) error {
	for _, entity := range entityTypes() {
		t := reflect.TypeOf(entity)
		name := ControllerName + "-unreachable-" + strings.ToLower(t.Elem().Name())
		b := ctrl.NewControllerManagedBy(mgr).
			Named(name).
			// A change to the Programmed condition triggers it too: whichever of
			// it and a reconcile of the gateway sets the status last, e.g. one of
			// a gateway the cache still holds once its cleanup completed,
			// reporting it being deleted, the entity is reported again.
			For(entity, builder.WithPredicates(predicate.Or[client.Object](predicate.GenerationChangedPredicate{}, programmedChanged())))
		// A change to an ancestor, its deletion included, triggers its
		// descendants of the kind: e.g. a virtual cluster policy whose backend
		// cluster is deleted can't reach its gateway anymore, although its
		// virtual cluster doesn't change. The status writes on the entities
		// don't change their generation, so they trigger nothing.
		ancestors := ancestorEntities(t)
		for _, at := range slices.SortedFunc(maps.Keys(ancestors), func(a, b reflect.Type) int { return strings.Compare(a.String(), b.String()) }) {
			b = b.Watches(ancestors[at], handler.EnqueueRequestsFromMapFunc(mapAncestorToDescendants(mgr.GetClient(), t)),
				builder.WithPredicates(predicate.GenerationChangedPredicate{}))
		}
		// The deletion of a gateway triggers the entities of the kind that
		// reach it: once it is gone, nothing else reports them, as they don't
		// change.
		b = b.Watches(&eventgatewayv1alpha1.OnPremEventGateway{}, handler.EnqueueRequestsFromMapFunc(mapGatewayToEntities(mgr.GetClient(), t)),
			builder.WithPredicates(onlyDeletions()))
		if err := b.Complete(reconcile.Func(func(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
			obj, ok := reflect.TypeAssert[client.Object](reflect.New(t.Elem()))
			if !ok {
				return ctrl.Result{}, fmt.Errorf("%s is not a client.Object", t)
			}
			return requeueOnConflict(ctrl.LoggerFrom(ctx), ctrl.Result{}, r.reportUnreachable(ctx, req.NamespacedName, obj))
		})); err != nil {
			return fmt.Errorf("failed to set up the %s controller: %w", name, err)
		}
	}
	return nil
}

// programmedChanged lets through the updates that change the Programmed
// condition of an entity.
func programmedChanged() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldEntity, ok := e.ObjectOld.(entityObject)
			if !ok {
				return false
			}
			newEntity, ok := e.ObjectNew.(entityObject)
			if !ok {
				return false
			}
			programmed := string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
			oldCond := apimeta.FindStatusCondition(oldEntity.GetConditions(), programmed)
			newCond := apimeta.FindStatusCondition(newEntity.GetConditions(), programmed)
			if oldCond == nil || newCond == nil {
				return oldCond != newCond
			}
			return oldCond.Status != newCond.Status || oldCond.Reason != newCond.Reason || oldCond.Message != newCond.Message
		},
	}
}

// attachedEntity is a kind of entity attached to an OnPremEventGateway
// directly.
type attachedEntity struct {
	// entity is the type of the kind.
	entity reflect.Type
	// list returns an empty list of the kind.
	list func() client.ObjectList
	// field is the index field finding the entities of the kind attached to
	// a gateway, as namespace/name.
	field string
}

// attachedEntities are the kinds of entity attached to an OnPremEventGateway
// directly: the others reach their gateway through their parent entities
// (see parentEntities).
var attachedEntities = []attachedEntity{
	{
		entity: reflect.TypeFor[*configurationv1alpha1.EventGatewayBackendCluster](),
		list:   func() client.ObjectList { return &configurationv1alpha1.EventGatewayBackendClusterList{} },
		field:  index.IndexFieldEventGatewayBackendClusterOnOnPremEventGatewayRef,
	},
	{
		entity: reflect.TypeFor[*configurationv1alpha1.EventGatewayDataPlaneCertificate](),
		list:   func() client.ObjectList { return &configurationv1alpha1.EventGatewayDataPlaneCertificateList{} },
		field:  index.IndexFieldEventGatewayDataPlaneCertificateOnOnPremEventGatewayRef,
	},
	{
		entity: reflect.TypeFor[*configurationv1alpha1.EventGatewayListener](),
		list:   func() client.ObjectList { return &configurationv1alpha1.EventGatewayListenerList{} },
		field:  index.IndexFieldEventGatewayListenerOnOnPremEventGatewayRef,
	},
	{
		entity: reflect.TypeFor[*configurationv1alpha1.EventGatewaySchemaRegistry](),
		list:   func() client.ObjectList { return &configurationv1alpha1.EventGatewaySchemaRegistryList{} },
		field:  index.IndexFieldEventGatewaySchemaRegistryOnOnPremEventGatewayRef,
	},
	{
		entity: reflect.TypeFor[*configurationv1alpha1.EventGatewayStaticKey](),
		list:   func() client.ObjectList { return &configurationv1alpha1.EventGatewayStaticKeyList{} },
		field:  index.IndexFieldEventGatewayStaticKeyOnOnPremEventGatewayRef,
	},
	{
		entity: reflect.TypeFor[*configurationv1alpha1.EventGatewayTLSTrustBundle](),
		list:   func() client.ObjectList { return &configurationv1alpha1.EventGatewayTLSTrustBundleList{} },
		field:  index.IndexFieldEventGatewayTLSTrustBundleOnOnPremEventGatewayRef,
	},
}

// mapGatewayToEntities returns a MapFunc that enqueues the entities of the
// kind of target that reach the gateway: those attached to it, and their
// descendants. Only the attached kinds that are the target, or lead to it,
// are listed.
func mapGatewayToEntities(cl client.Client, target reflect.Type) handler.MapFunc {
	toDescendants := mapAncestorToDescendants(cl, target)
	leading := ancestorEntities(target)
	attached := slices.DeleteFunc(slices.Clone(attachedEntities), func(a attachedEntity) bool {
		_, leads := leading[a.entity]
		return a.entity != target && !leads
	})
	return func(ctx context.Context, gateway client.Object) []reconcile.Request {
		var requests []reconcile.Request
		for _, a := range attached {
			list := a.list()
			if err := cl.List(ctx, list, client.MatchingFields{a.field: client.ObjectKeyFromObject(gateway).String()}); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "failed to list the entities attached to a gateway", "gateway", client.ObjectKeyFromObject(gateway))
				continue
			}
			entities, err := entitiesOf(list)
			if err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "failed to read the entities attached to a gateway", "gateway", client.ObjectKeyFromObject(gateway))
				continue
			}
			for _, entity := range entities {
				if reflect.TypeOf(entity) == target {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(entity)})
					continue
				}
				requests = append(requests, toDescendants(ctx, entity)...)
			}
		}
		return requests
	}
}

// mapAncestorToDescendants returns a MapFunc that enqueues the descendants of
// an entity of the kind of target, found through the index fields of the
// kinds between them.
func mapAncestorToDescendants(cl client.Client, target reflect.Type) handler.MapFunc {
	// The kinds leading to the target: only those are walked.
	leading := ancestorEntities(target)
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var requests []reconcile.Request
		seen := map[reflect.Type]map[client.ObjectKey]struct{}{}
		pending := []client.Object{obj}
		for len(pending) > 0 {
			current := pending[0]
			pending = pending[1:]
			for _, c := range childEntities[reflect.TypeOf(current)] {
				if _, leads := leading[c.child]; c.child != target && !leads {
					continue
				}
				list := c.list()
				if err := cl.List(ctx, list, client.MatchingFields{c.field: client.ObjectKeyFromObject(current).String()}); err != nil {
					ctrl.LoggerFrom(ctx).Error(err, "failed to list the children of an entity", "entity", client.ObjectKeyFromObject(current))
					continue
				}
				children, err := entitiesOf(list)
				if err != nil {
					ctrl.LoggerFrom(ctx).Error(err, "failed to read the children of an entity", "entity", client.ObjectKeyFromObject(current))
					continue
				}
				for _, child := range children {
					key := client.ObjectKeyFromObject(child)
					if seen[c.child] == nil {
						seen[c.child] = map[client.ObjectKey]struct{}{}
					}
					if _, ok := seen[c.child][key]; ok {
						continue
					}
					seen[c.child][key] = struct{}{}
					if c.child == target {
						requests = append(requests, reconcile.Request{NamespacedName: key})
					}
					pending = append(pending, child)
				}
			}
		}
		return requests
	}
}

// reportUnreachable sets the Programmed condition of the entity to false when
// its OnPremEventGateway doesn't exist, or when its parent entity doesn't and
// its status was set for an OnPremEventGateway: the entity is then Detached,
// naming the entity that doesn't exist, also over the Detached condition the
// reconcile of its gateway set, when it was in its configuration (see
// reportDetached): it doesn't name the gateway, so that reconcile leaves it
// alone. The reconcile of a gateway being
// deleted reports its entities itself. An entity that reaches no
// OnPremEventGateway anymore, e.g. one now under a KonnectEventGateway, loses
// the Programmed condition set for one: the Konnect controllers set theirs.
func (r *Reconciler) reportUnreachable(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	if err := r.Get(ctx, key, obj); err != nil {
		return client.IgnoreNotFound(err)
	}
	entity, isEntity := obj.(entityObject)
	if !isEntity || !obj.GetDeletionTimestamp().IsZero() {
		return nil
	}
	gatewayKey, ok, missing, err := walkToOnPremEventGateway(ctx, r.Client, obj)
	switch {
	case err != nil:
		return err
	case missing != "":
		if !setForOnPremEventGateway(entity) {
			return nil
		}
		return r.setEntityConditions(ctx, entity, nil, entityCondition(entity, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
			metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityDetachedReason,
			fmt.Sprintf("Not in the configuration of its OnPremEventGateway: %s does not exist", missing)))
	case !ok:
		if !setForOnPremEventGateway(entity) {
			return nil
		}
		return r.setEntityConditions(ctx, entity, []string{string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)})
	}
	var gateway eventgatewayv1alpha1.OnPremEventGateway
	switch err := r.Get(ctx, gatewayKey, &gateway); {
	case apierrors.IsNotFound(err):
		return r.setEntityConditions(ctx, entity, nil, entityCondition(entity, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
			metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityGatewayNotFoundReason,
			fmt.Sprintf("The OnPremEventGateway %s does not exist", gatewayKey)))
	case err != nil:
		return err
	}
	return nil
}

// setForOnPremEventGateway reports whether the Programmed condition of the
// entity was set by this controller: by its own reasons, or by the reason the
// Konnect controllers use too, with the message this controller sets with it
// (see reportEntities).
func setForOnPremEventGateway(entity entityObject) bool {
	cond := apimeta.FindStatusCondition(entity.GetConditions(), string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
	if cond == nil {
		return false
	}
	switch kcfgconsts.ConditionReason(cond.Reason) {
	case eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason,
		eventgatewayv1alpha1.OnPremEntityDetachedReason,
		eventgatewayv1alpha1.OnPremEntityInvalidReason,
		eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason,
		eventgatewayv1alpha1.OnPremEntityNotRegisteredReason,
		eventgatewayv1alpha1.OnPremEntityGatewayNotFoundReason:
		return true
	case eventgatewayv1alpha1.OnPremEntityProgrammedReason:
		return strings.HasPrefix(cond.Message, inConfigurationOfPrefix)
	}
	return false
}

// entitiesOf returns the objects of a list.
func entitiesOf(list client.ObjectList) ([]client.Object, error) {
	items, err := apimeta.ExtractList(list)
	if err != nil {
		return nil, err
	}
	objs := make([]client.Object, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(client.Object); ok {
			objs = append(objs, obj)
		}
	}
	return objs, nil
}
