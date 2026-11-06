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
	"reflect"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
)

// An entity the policies in the configuration of a gateway reference is held
// with FinalizerInUse while they do. What is in use depends on the
// configuration, so the reconcile of the gateway decides (see holdEntities).
// When it can't, as the gateway is gone, the controller of the kind of entity
// releases an entity being deleted (see releaseOrphan).

var (
	schemaRegistryType = reflect.TypeFor[*configurationv1alpha1.EventGatewaySchemaRegistry]()
	staticKeyType      = reflect.TypeFor[*configurationv1alpha1.EventGatewayStaticKey]()
	tlsTrustBundleType = reflect.TypeFor[*configurationv1alpha1.EventGatewayTLSTrustBundle]()
	virtualClusterType = reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualCluster]()
	backendClusterType = reflect.TypeFor[*configurationv1alpha1.EventGatewayBackendCluster]()
	producePolicyType  = reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]()
	consumePolicyType  = reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy]()

	// heldKinds are the kinds of entity FinalizerInUse can hold. A produce or
	// consume policy is held while child policies name it as their parent:
	// without it, they aren't applied, e.g. fields are no longer encrypted. A
	// backend cluster is held while listener policies forward to one of its
	// virtual clusters, which can't be sent without it, as Konnect refuses to
	// delete it then: otherwise, deleting one takes its virtual clusters out of
	// the configuration. A data plane certificate is not one: nothing
	// references it, and it must stop being trusted as soon as it is deleted.
	// Neither is a listener: deleting one takes its policies out of the
	// configuration.
	heldKinds = []reflect.Type{schemaRegistryType, staticKeyType, tlsTrustBundleType, virtualClusterType, backendClusterType, producePolicyType, consumePolicyType}

	// produceParentOf and consumeParentOf return the parent policy of a
	// policy, when it names one.
	produceParentOf = parentPolicyFrom(configurationv1alpha1.RefsAtEventGatewayVirtualClusterProducePolicyParentPolicyRef,
		func(r configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef) (string, string) {
			return r.Namespace, r.Name
		})
	consumeParentOf = parentPolicyFrom(configurationv1alpha1.RefsAtEventGatewayVirtualClusterConsumePolicyParentPolicyRef,
		func(r configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef) (string, string) {
			return r.Namespace, r.Name
		})
)

// isHeldKind tells FinalizerInUse can hold the entities of the kind.
func isHeldKind(t reflect.Type) bool {
	return slices.Contains(heldKinds, t)
}

// heldReferencesOf returns the entities a policy references by object that
// FinalizerInUse can hold, its parent policy included.
func heldReferencesOf(obj entityObject) []reportedEntity {
	var refs []reportedEntity
	add := func(t reflect.Type, keys []client.ObjectKey) {
		for _, key := range keys {
			refs = append(refs, reportedEntity{typ: t, key: key})
		}
	}
	switch p := obj.(type) {
	case *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy:
		add(schemaRegistryType, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewaySchemaRegistry(p))
		add(staticKeyType, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewayStaticKey(p))
		if parent, ok := produceParentOf(p); ok {
			add(producePolicyType, []client.ObjectKey{parent})
		}
	case *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy:
		add(schemaRegistryType, configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRefsToEventGatewaySchemaRegistry(p))
		if parent, ok := consumeParentOf(p); ok {
			add(consumePolicyType, []client.ObjectKey{parent})
		}
	case *configurationv1alpha1.EventGatewayListenerPolicy:
		add(tlsTrustBundleType, configurationv1alpha1.EventGatewayListenerPolicyRefsToEventGatewayTLSTrustBundle(p))
		add(virtualClusterType, configurationv1alpha1.EventGatewayListenerPolicyRefsToEventGatewayVirtualCluster(p))
	}
	return refs
}

// newEntityOf returns an empty object of the kind of entity.
func newEntityOf(t reflect.Type) (client.Object, error) {
	entity, ok := reflect.TypeAssert[client.Object](reflect.New(t.Elem()))
	if !ok {
		return nil, fmt.Errorf("%s is not a client.Object", t)
	}
	return entity, nil
}

// noNewUserOfDeletedEntities returns an error when the policy, created after an
// entity it references started being deleted, would start using it: it could
// keep the deletion waiting for as long as new policies come. As with Konnect, a
// policy that exists keeps using the entity until it drops the reference, even
// one changed to reference it since. A policy created in the same second as the
// deletion counts as existing, timestamps having no more precision.
func noNewUserOfDeletedEntities(ctx context.Context, cl client.Client, policy entityObject) error {
	for _, ref := range heldReferencesOf(policy) {
		entity, err := newEntityOf(ref.typ)
		if err != nil {
			return err
		}
		if err := cl.Get(ctx, ref.key, entity); err != nil {
			if apierrors.IsNotFound(err) {
				// The conversion reports it.
				continue
			}
			return fmt.Errorf("failed to read the %s %s: %w", ref.typ.Elem().Name(), ref.key, err)
		}
		deleted := entity.GetDeletionTimestamp()
		if deleted != nil && policy.GetCreationTimestamp().After(deleted.Time) {
			return fmt.Errorf("it references the %s %s, which is being deleted", ref.typ.Elem().Name(), ref.key)
		}
	}
	return nil
}

// entitiesInUse returns the entities the policies of the configuration
// reference by object, with the policies referencing each. A policy sent as it
// was last, a Secret it reads being gone (see entityStatus.kept), also holds
// what that configuration references, which its spec may no longer name: built
// holds the entities sent, by the UID of their object.
func entitiesInUse(included []entityStatus, built map[types.UID]any) map[reportedEntity][]reportedEntity {
	byUID := map[string]reportedEntity{}
	for _, e := range included {
		if t := reflect.TypeOf(e.obj); isHeldKind(t) {
			byUID[string(e.obj.GetUID())] = reportedEntity{typ: t, key: client.ObjectKeyFromObject(e.obj)}
		}
	}
	inUse := map[reportedEntity][]reportedEntity{}
	for _, e := range included {
		policy := reportedEntity{typ: reflect.TypeOf(e.obj), key: client.ObjectKeyFromObject(e.obj)}
		refs := heldReferencesOf(e.obj)
		if e.kept {
			refs = append(refs, sentReferences(built[e.obj.GetUID()], byUID, policy)...)
		}
		for _, ref := range refs {
			if !slices.Contains(inUse[ref], policy) {
				inUse[ref] = append(inUse[ref], policy)
			}
		}
	}
	// A virtual cluster in use holds its backend cluster, for the same
	// policies: it can't be sent without it.
	for _, e := range included {
		vc, ok := e.obj.(*configurationv1alpha1.EventGatewayVirtualCluster)
		if !ok || vc.Spec.EventGatewayBackendClusterRef.NamespacedRef == nil {
			continue
		}
		policies := inUse[reportedEntity{typ: virtualClusterType, key: client.ObjectKeyFromObject(vc)}]
		if len(policies) == 0 {
			continue
		}
		ref := vc.Spec.EventGatewayBackendClusterRef.NamespacedRef
		backendCluster := reportedEntity{typ: backendClusterType, key: refKey(vc, ref.Namespace, ref.Name)}
		for _, policy := range policies {
			if !slices.Contains(inUse[backendCluster], policy) {
				inUse[backendCluster] = append(inUse[backendCluster], policy)
			}
		}
	}
	return inUse
}

// sentReferences returns the held entities, of byUID, the entity sent
// references: the ids it holds, which are the UIDs of their objects, other than
// its own.
func sentReferences(sent any, byUID map[string]reportedEntity, self reportedEntity) []reportedEntity {
	value, err := jsonValue(sent)
	if err != nil {
		return nil
	}
	var refs []reportedEntity
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		case string:
			if ref, ok := byUID[v]; ok && ref != self {
				refs = append(refs, ref)
			}
		}
	}
	walk(value)
	return refs
}

// holdEntities puts FinalizerInUse on the entities of the gateway in use, and
// removes it from the others. An entity being deleted can't get it anymore: it
// goes away, and the policies referencing it are left out of the
// configuration. A nil inUse releases every entity, e.g. when the gateway is
// deleted. It returns the resource versions of the entities it patched, so
// that their status is then patched from them, not from the version read
// before, which would conflict.
func (r *Reconciler) holdEntities(
	ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, inUse map[reportedEntity][]reportedEntity,
) (ctrl.Result, map[reportedEntity]string, error) {
	var (
		res     ctrl.Result
		errs    []error
		patched = map[reportedEntity]string{}
	)
	for _, kind := range heldKinds {
		// The entities of the kind reaching the gateway, also through their
		// parents: a virtual cluster is the child of a backend cluster.
		for _, req := range mapGatewayToEntities(r.Client, kind)(ctx, gateway) {
			entity, err := newEntityOf(kind)
			if err != nil {
				return res, patched, err
			}
			if err := r.Get(ctx, req.NamespacedName, entity); err != nil {
				if !apierrors.IsNotFound(err) {
					errs = append(errs, fmt.Errorf("failed to read the %s %s: %w", kind.Elem().Name(), req.NamespacedName, err))
				}
				continue
			}
			key := reportedEntity{typ: kind, key: req.NamespacedName}
			_, used := inUse[key]
			if used && !entity.GetDeletionTimestamp().IsZero() {
				continue
			}
			version, changed, err := r.setInUseFinalizer(ctx, entity, used)
			switch {
			case apierrors.IsConflict(err):
				// It changed since it was read: tried again.
				res = ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}
			case err != nil && !apierrors.IsNotFound(err):
				errs = append(errs, err)
			case changed:
				patched[key] = version
			}
		}
	}
	return res, patched, errors.Join(errs...)
}

// setInUseFinalizer adds FinalizerInUse to the entity, or removes it, with an
// optimistic lock, and returns its resource version once patched, and whether
// it was.
func (r *Reconciler) setInUseFinalizer(ctx context.Context, entity client.Object, held bool) (string, bool, error) {
	if controllerutil.ContainsFinalizer(entity, FinalizerInUse) == held {
		return "", false, nil
	}
	updated, ok := entity.DeepCopyObject().(client.Object)
	if !ok {
		return "", false, fmt.Errorf("failed to copy %T", entity)
	}
	if held {
		controllerutil.AddFinalizer(updated, FinalizerInUse)
	} else {
		controllerutil.RemoveFinalizer(updated, FinalizerInUse)
	}
	if err := r.Patch(ctx, updated, client.MergeFromWithOptions(entity, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
			return "", false, err
		}
		return "", false, fmt.Errorf("failed to update finalizer %s of the %s %s: %w", FinalizerInUse, reflect.TypeOf(entity).Elem().Name(), client.ObjectKeyFromObject(entity), err)
	}
	return updated.GetResourceVersion(), true, nil
}

// releaseOrphan removes FinalizerInUse from an entity being deleted that no
// reconcile of a gateway can release: its gateway doesn't exist or is being
// deleted, e.g. the finalizer of the gateway was removed by hand, or the entity
// was moved to a gateway that doesn't exist. While the gateway is there, its
// reconcile decides.
//
// An entity can't get FinalizerInUse without its gateway existing, so a gateway
// the cache doesn't hold is one deleted since, and an entity released when one
// of the same name is being created is left out of its configuration, as any
// entity deleted before it is held.
func (r *Reconciler) releaseOrphan(ctx context.Context, obj client.Object) error {
	if !isHeldKind(reflect.TypeOf(obj)) || !controllerutil.ContainsFinalizer(obj, FinalizerInUse) {
		return nil
	}
	gatewayKey, ok, _, err := walkToOnPremEventGateway(ctx, r.Client, obj)
	if err != nil {
		return err
	}
	if ok {
		var gateway eventgatewayv1alpha1.OnPremEventGateway
		switch err := r.Get(ctx, gatewayKey, &gateway); {
		case err == nil:
			if gateway.DeletionTimestamp.IsZero() {
				return nil
			}
		case !apierrors.IsNotFound(err):
			return err
		}
	}
	_, res, err := patch.WithoutFinalizer(ctx, r.Client, obj, FinalizerInUse)
	if err != nil {
		return client.IgnoreNotFound(err)
	}
	if !res.IsZero() {
		// Nothing triggers it again: a change of its finalizers doesn't change
		// its generation.
		return apierrors.NewConflict(schema.GroupResource{Resource: reflect.TypeOf(obj).Elem().Name()}, obj.GetName(),
			errors.New("the entity changed since it was read"))
	}
	return nil
}

// heldMessage tells, for an entity of the configuration, that it is being
// deleted and is kept while policies reference it.
func heldMessage(cfg configuration, e entityStatus) string {
	if e.obj.GetDeletionTimestamp().IsZero() {
		return ""
	}
	policies := cfg.entitiesInUse[reportedEntity{typ: reflect.TypeOf(e.obj), key: client.ObjectKeyFromObject(e.obj)}]
	if len(policies) == 0 {
		return ""
	}
	names := make([]string, 0, len(policies))
	for _, p := range policies {
		names = append(names, p.typ.Elem().Name()+" "+p.key.Name)
	}
	slices.Sort(names)
	return fmt.Sprintf("; being deleted, it is kept while these policies reference it: %s", strings.Join(names, ", "))
}
