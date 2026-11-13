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
	"bytes"
	"cmp"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// configuration is what gw-admin must have for a gateway, built from the
// entities attached to the gateway: the entities and the data plane
// certificates it receives, and the entities left out, with the reason.
type configuration struct {
	// entities is sent with PUT .../entities.
	entities gwadminapi.GatewayEntitySet
	// certificates are sent one by one: they are not part of entities.
	certificates []dataPlaneCertificate
	// objects names the object each entity of entities comes from, by the
	// path of the entity in the configuration, e.g. backend_clusters.0, so
	// that an error gw-admin reports on a field can be traced back to the
	// object.
	objects map[string]string
	// included are the objects whose entity is in entities.
	included []entityStatus
	// excluded are the objects left out of the configuration, with the
	// reason.
	excluded []excludedEntity
	// pushedKey is entities as they are compared with the entities gw-admin
	// holds (see sameEntities), encoded once for every gw-admin pod.
	pushedKey []byte
	// orderedPolicies are the virtual clusters and listeners whose policies
	// a chain orders: their order must also be compared when checking
	// gw-admin for drift.
	orderedPolicies []uuid.UUID
	// invalidChains prevent pushing a partial document or falling back to
	// implicit ordering while an owning declaration cannot be fulfilled.
	invalidChains []string
	// previous holds the entities of the last configuration built for the
	// gateway, by the UID of their object, and built those of this one: an
	// entity reading a Secret that is gone is sent as it was last.
	previous, built map[types.UID]any
	// namespace is the namespace of the gateway. An entity of another
	// namespace belongs to the gateway only when KongReferenceGrants allow
	// its references up to the gateway (see parentNotGranted).
	namespace string
	// secretHint tells how to make a Secret visible to the operator, when
	// its Secret cache is filtered by a label.
	secretHint string
	// entitiesInUse are the entities the policies in the configuration reference
	// by object, with those policies: FinalizerInUse holds them.
	entitiesInUse map[reportedEntity][]reportedEntity
	// claimed holds, by scope (see uniqueValue), the values entities of the
	// configuration took, with the object of each.
	claimed map[string]map[string]string
	// reserved holds, by policy name, the oldest policy of the gateway naming
	// it (see reservePolicyNames).
	reserved map[string]string
	// parentsNotGranted holds, by "Kind namespace/name", why each object of
	// another namespace than its parent can't reference it, or "" (see
	// parentNotGranted).
	parentsNotGranted map[string]string
}

// uniqueValue is a value of an entity that no other entity of the gateway may
// have in the same scope, across kinds, e.g. the name of a policy among every
// policy, as Konnect requires.
type uniqueValue struct {
	// scope names what the value is, e.g. "policy name".
	scope string
	value string
}

// claim records the unique values of the object, and returns why it can't have
// one, as another object of the configuration took it, or "".
func (c *configuration) claim(obj client.Object, kind string, values []uniqueValue) string {
	if c.claimed == nil {
		c.claimed = map[string]map[string]string{}
	}
	self := kind + " " + client.ObjectKeyFromObject(obj).String()
	for _, v := range values {
		if owner, ok := c.claimed[v.scope][v.value]; ok {
			return fmt.Sprintf("its %s %q is already the one of %s", v.scope, v.value, owner)
		}
		if owner, ok := c.reserved[v.value]; ok && v.scope == policyNameScope && owner != self {
			return fmt.Sprintf("its %s %q is already the one of %s", v.scope, v.value, owner)
		}
	}
	for _, v := range values {
		if c.claimed[v.scope] == nil {
			c.claimed[v.scope] = map[string]string{}
		}
		c.claimed[v.scope][v.value] = self
	}
	return ""
}

// policyNameScope is the scope of the names of the policies (see uniqueValue).
const policyNameScope = "policy name"

// policyName returns the name of a policy entity, as a unique value: Konnect
// refuses two policies of a gateway with the same name, whatever their kind or
// their virtual cluster or listener.
func policyName[E any](e E) []uniqueValue {
	value, err := jsonValue(e)
	if err != nil {
		return nil
	}
	fields, _ := value.(map[string]any)
	name, _ := fields["name"].(string)
	if name == "" {
		return nil
	}
	return []uniqueValue{{scope: policyNameScope, value: name}}
}

// entityObject is the object of an entity, which reports with its
// conditions whether it is in the configuration of its gateway.
type entityObject interface {
	client.Object
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
}

// entityStatus is what the status of an object reports of its entity.
type entityStatus struct {
	obj entityObject
	// secretRefValid is the SecretRefValid condition of the object, which
	// tells whether the Secrets it reads resolve, or nil when it reads none.
	secretRefValid *metav1.Condition
	// kept tells the entity is sent as it was last, a Secret it reads being
	// unusable.
	kept       bool
	conditions []metav1.Condition
}

// dataPlaneCertificate is a data plane certificate of the gateway.
type dataPlaneCertificate struct {
	id   uuid.UUID
	key  client.ObjectKey
	body gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem
	obj  *configurationv1alpha1.EventGatewayDataPlaneCertificate
}

// excludedEntity is an object left out of the configuration of its gateway.
type excludedEntity struct {
	entityStatus

	reason  kcfgconsts.ConditionReason
	message string
	// blocks tells the object stops the configuration from being pushed:
	// left out, it would change what the gateway does, e.g. a policy no
	// longer applied (see configuration.blocked).
	blocks bool
}

// buildConfiguration builds the configuration of the gateway from the
// entities attached to it. An entity that can't be sent as it is left out,
// with a reason, and stops the configuration from being pushed unless it
// doesn't belong to it (see exclude, leaveOut and blocked), unless only a
// Secret it reads is gone or lacks the key read:
// it is then sent as it was in previous, the entities of the last
// configuration built for the gateway. The same entities always produce the same configuration: each
// entity has the UID and the creation time of its object. An error reading
// the cluster, other than an object that doesn't exist, is returned: leaving
// the entity out would delete it from gw-admin.
func buildConfiguration(
	ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway, secretHint string, previous map[types.UID]any,
) (configuration, error) {
	cfg := configuration{
		namespace:  gateway.Namespace,
		secretHint: secretHint,
		objects:    map[string]string{},
		entities:   noEntities(),
		previous:   previous,
		built:      map[types.UID]any{},
	}
	backendClusters, err := cfg.addGatewayEntities(ctx, cl, gateway)
	if err != nil {
		return configuration{}, err
	}
	resolver := uidResolver{Client: cl, read: map[uuid.UUID]client.ObjectKey{}}
	virtualClusters, err := cfg.addVirtualClusters(ctx, resolver, backendClusters)
	if err != nil {
		return configuration{}, err
	}
	// A policy name belongs to the oldest policy naming it, whatever its kind.
	if err := cfg.reservePolicyNames(ctx, cl, gateway, virtualClusters); err != nil {
		return configuration{}, err
	}
	// The policies reference the entities added so far.
	targets := referenceTargetsOf(&cfg, resolver.read)
	if err := cfg.addVirtualClusterPolicies(ctx, resolver, virtualClusters, targets); err != nil {
		return configuration{}, err
	}
	if err := cfg.addClusterPolicyChains(ctx, cl, virtualClusters); err != nil {
		return configuration{}, err
	}
	listeners, err := cfg.addListeners(ctx, resolver, gateway, targets)
	if err != nil {
		return configuration{}, err
	}
	if err := cfg.addListenerPolicyChains(ctx, cl, listeners); err != nil {
		return configuration{}, err
	}
	cfg.entitiesInUse = entitiesInUse(cfg.included, cfg.built)
	if err := cfg.addCertificates(ctx, cl, gateway); err != nil {
		return configuration{}, err
	}
	key, err := comparisonKey(withGWAdminDefaults(cfg.entities), cfg.orderedPolicies...)
	if err != nil {
		return configuration{}, fmt.Errorf("failed to encode the entities of the gateway: %w", err)
	}
	cfg.pushedKey = key
	return cfg, nil
}

// entityKind is a kind of entity of the configuration.
type entityKind[PT entityObject, E any] struct {
	// kind is the kind of the objects, e.g. EventGatewayBackendCluster.
	kind string
	// what names an entity of the kind in messages, e.g. "backend cluster".
	what string
	// request returns the Konnect SDK request of an object, reading what it
	// references with the client: its entity is converted from it.
	request func(context.Context, client.Client, PT) (any, error)
	// adjust, when set, adjusts the JSON payload of an entity, e.g. to
	// resolve its references. It is also applied to an entity kept from the
	// last configuration, so it must leave a payload it adjusted as it is.
	adjust func(map[string]any) error
	// name, when set, returns the name of an entity, which must be unique
	// among the entities of the kind.
	name func(E) string
	// unique, when set, returns the values of an entity no other entity of
	// the gateway may have, whatever its kind (see uniqueValue).
	unique func(E) []uniqueValue
	// conflictLeftOut tells an entity whose name or unique value an older one
	// of the gateway has is only left out (see leaveOut): the older one keeps
	// running, so leaving the newer out doesn't change what the gateway does.
	conflictLeftOut bool
	// list returns the path of the list of the configuration the entity of
	// an object is added to, e.g. backend_clusters, and the list.
	list func(PT) (string, *[]E)
}

// in returns the list of an entityKind whose entities all go to the list at
// the path.
func in[PT entityObject, E any](path string, entities *[]E) func(PT) (string, *[]E) {
	return func(PT) (string, *[]E) { return path, entities }
}

// addAttached lists into list, whose items are items, the objects of a kind
// attached to the gateway, by the index field of the kind by gateway, and
// adds their entities. It returns the objects as parents.
func addAttached[T any, PT interface {
	*T
	entityObject
}, E any](
	ctx context.Context, c *configuration, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway,
	list client.ObjectList, items *[]T, field string, k entityKind[PT, E],
) (parents, error) {
	if err := cl.List(ctx, list, client.MatchingFields{field: client.ObjectKeyFromObject(gateway).String()}); err != nil {
		return parents{}, fmt.Errorf("failed to list the %ss of the gateway: %w", k.kind, err)
	}
	objs := live[T, PT](*items)
	index, err := addEntities(ctx, c, cl, objs, k)
	if err != nil {
		return parents{}, err
	}
	p := parents{kind: k.kind, index: index}
	for _, obj := range objs {
		p.keys = append(p.keys, client.ObjectKeyFromObject(obj))
	}
	return p, nil
}

// live returns a pointer to a copy of each item not being deleted: an object
// being deleted is left out of the configuration without a reason, it is
// going away, unless FinalizerInUse holds it while policies use it, which only
// goes for the kinds it can hold.
func live[T any, PT interface {
	*T
	entityObject
}](items []T) []PT {
	objs := make([]PT, 0, len(items))
	for _, item := range items {
		if obj := PT(&item); isLive(obj) {
			objs = append(objs, obj)
		}
	}
	return objs
}

// isLive tells whether the object can be in the configuration: it isn't being
// deleted, or FinalizerInUse holds it (see live).
func isLive(obj client.Object) bool {
	return obj.GetDeletionTimestamp().IsZero() || (isHeldKind(reflect.TypeOf(obj)) && controllerutil.ContainsFinalizer(obj, FinalizerInUse))
}

// addEntities adds the entities of the objects, from the oldest, and returns
// the index of the entity of each object added in its list. Two entities of a
// kind with a name can't have the same name: the oldest one is kept.
func addEntities[PT entityObject, E any](ctx context.Context, c *configuration, cl client.Client, objs []PT, k entityKind[PT, E]) (map[client.ObjectKey]int, error) {
	objs = slices.Clone(objs)
	slices.SortFunc(objs, byCreation[PT])

	index := map[client.ObjectKey]int{}
	names := map[string]struct{}{}
	for _, obj := range objs {
		conv, ok, err := convertObject[PT, E](ctx, c, cl, obj, k.kind, k.request, k.adjust)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if k.name != nil {
			name := k.name(conv.entity)
			if _, ok := names[name]; ok {
				c.excludeConflict(conv.status, k.conflictLeftOut, fmt.Sprintf("another %s of the gateway is named %q", k.what, name))
				continue
			}
			names[name] = struct{}{}
		}
		if k.unique != nil {
			if message := c.claim(obj, k.kind, k.unique(conv.entity)); message != "" {
				c.excludeConflict(conv.status, k.conflictLeftOut, message)
				continue
			}
		}
		path, entities := k.list(obj)
		index[client.ObjectKeyFromObject(obj)] = len(*entities)
		c.record(fmt.Sprintf("%s.%d", path, len(*entities)), k.kind, conv.status, conv.entity)
		*entities = append(*entities, conv.entity)
	}
	return index, nil
}

// parents are the objects of a kind other entities are the children of. A
// child is in the configuration only when its parent is.
type parents struct {
	kind string
	// keys are every parent of the gateway, in the configuration or not.
	keys []client.ObjectKey
	// index is the index of each parent in the configuration in its list.
	index map[client.ObjectKey]int
}

// childrenOf lists into list, whose items are items, the objects whose
// parent, by the index field, is one of the parents. It returns those whose
// parent is in the configuration, with the index of their parent, and the
// keys of every object live returns, whether its parent is in the
// configuration or not.
func childrenOf[T any, PT interface {
	*T
	entityObject
}](
	ctx context.Context, c *configuration, cl client.Client, list client.ObjectList, items *[]T, field string, p parents,
) ([]PT, map[client.ObjectKey]int, []client.ObjectKey, error) {
	var (
		children    []PT
		keys        []client.ObjectKey
		parentIndex = map[client.ObjectKey]int{}
	)
	for _, parent := range p.keys {
		if err := cl.List(ctx, list, client.MatchingFields{field: parent.String()}); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list %T by %s: %w", list, field, err)
		}
		for _, child := range live[T, PT](*items) {
			key := client.ObjectKeyFromObject(child)
			keys = append(keys, key)
			// An object of another namespace no KongReferenceGrant lets
			// reference its parent doesn't belong to the gateway, whatever it
			// references: it is only left out, before its parent policy, if
			// any, is looked at, so that it can't hold back the configuration
			// of a gateway it may not belong to.
			message, err := c.parentNotGranted(ctx, cl, child, reflect.TypeOf(child).Elem().Name())
			if err != nil {
				return nil, nil, nil, err
			}
			if message != "" {
				c.leaveOutNotPermitted(entityStatus{obj: child}, message)
				continue
			}
			i, ok := p.index[parent]
			if !ok {
				// The parent is left out, and tells why: whether it holds back
				// the configuration is up to it.
				c.leaveOut(entityStatus{obj: child}, fmt.Sprintf("its %s %s isn't in the configuration", p.kind, parent))
				continue
			}
			parentIndex[key] = i
			children = append(children, child)
		}
	}
	return children, parentIndex, keys, nil
}

// addChildren adds the objects of a kind whose parent, by the index field, is
// one of the parents, listed into list, whose items are items. listOf returns
// the path and the list of the entities of the children of the parent at an
// index in its list, e.g. listeners.0.policies. A child of its own kind, of
// the parent parentOf returns when set, is added after its parent.
func addChildren[T any, PT interface {
	*T
	entityObject
}, E any](
	ctx context.Context, c *configuration, cl client.Client, p parents, list client.ObjectList, items *[]T, field string,
	listOf func(parent int) (string, *[]E), parentOf func(PT) (client.ObjectKey, bool), k entityKind[PT, E],
) error {
	children, parentIndex, _, err := childrenOf[T, PT](ctx, c, cl, list, items, field, p)
	if err != nil {
		return err
	}
	k.list = func(obj PT) (string, *[]E) { return listOf(parentIndex[client.ObjectKeyFromObject(obj)]) }
	// A parent policy must be attached to the same entity, e.g. virtual
	// cluster, as its children: Konnect refuses a parent of another one.
	sameParent := func(child, parent client.ObjectKey) bool { return parentIndex[child] == parentIndex[parent] }
	return addParentsFirst[PT, E](ctx, c, cl, children, parentOf, sameParent, k)
}

// addParentsFirst adds the objects, a parent, of their own kind, before its
// children: those without a parent first, then their children, and so on.
// One whose parent isn't added, or is attached to another entity than it,
// sameParent telling, is left out. parentOf, when set, returns the parent of
// an object.
func addParentsFirst[PT entityObject, E any](
	ctx context.Context, c *configuration, cl client.Client, objs []PT, parentOf func(PT) (client.ObjectKey, bool),
	sameParent func(child, parent client.ObjectKey) bool, k entityKind[PT, E],
) error {
	added := map[client.ObjectKey]int{}
	for pending := objs; len(pending) > 0; {
		var ready, waiting []PT
		for _, obj := range pending {
			if parentOf != nil {
				if parent, ok := parentOf(obj); ok {
					_, isAdded := added[parent]
					if !isAdded || !sameParent(client.ObjectKeyFromObject(obj), parent) {
						waiting = append(waiting, obj)
						continue
					}
				}
			}
			ready = append(ready, obj)
		}
		if len(ready) == 0 {
			return excludeOrphans(ctx, c, cl, waiting, parentOf, added)
		}
		index, err := addEntities(ctx, c, cl, ready, k)
		if err != nil {
			return err
		}
		maps.Copy(added, index)
		pending = waiting
	}
	return nil
}

// excludeOrphans leaves out the objects whose parent, of their own kind, isn't
// in the configuration. One no KongReferenceGrant lets reference its parent, or
// another entity, in another namespace reports it, as when its parent is in
// the configuration, and stops the configuration from being pushed (see
// siblingsNotGranted). One whose parent is left out, directly or through its
// own parent, goes along with it: the parent tells why. One whose parent
// doesn't exist, is being deleted, or is in a cycle of parents stops the
// configuration from being pushed: without its parent, it isn't applied, e.g.
// fields are no longer encrypted.
func excludeOrphans[PT entityObject](
	ctx context.Context, c *configuration, cl client.Client, objs []PT, parentOf func(PT) (client.ObjectKey, bool), added map[client.ObjectKey]int,
) error {
	pending := make([]PT, 0, len(objs))
	for _, obj := range objs {
		message, err := siblingsNotGranted(ctx, cl, obj)
		if err != nil {
			return err
		}
		if message != "" {
			c.exclude(entityStatus{obj: obj}, eventgatewayv1alpha1.OnPremEntityReferenceNotPermittedReason, message)
			continue
		}
		pending = append(pending, obj)
	}
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(pending); i++ {
			obj := pending[i]
			parent, _ := parentOf(obj)
			if !c.isExcluded(reflect.TypeOf(obj), parent) {
				continue
			}
			c.leaveOut(entityStatus{obj: obj}, fmt.Sprintf("its parent policy %s isn't in the configuration", parent))
			pending = slices.Delete(pending, i, i+1)
			i--
			changed = true
		}
	}
	for _, obj := range pending {
		parent, _ := parentOf(obj)
		message := fmt.Sprintf("its parent policy %s doesn't exist, is being deleted, or is in a cycle of parent policies", parent)
		if _, ok := added[parent]; ok {
			message = fmt.Sprintf("its parent policy %s is attached to another virtual cluster", parent)
		}
		c.exclude(entityStatus{obj: obj}, eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
	}
	return nil
}

// converted is an object converted into its entity.
type converted[E any] struct {
	entity E
	status entityStatus
}

// convertObject converts the object into its entity, from its Konnect SDK
// request, whose JSON payload adjust, when set, adjusts, e.g. to resolve its
// references. It leaves the object out, with the reason, when its UID is not a
// version 4 UUID, it references an object or reads a Secret of another
// namespace no KongReferenceGrant allows, or it can't be converted, unless
// only a Secret it reads is gone or lacks the key
// read: its entity is then the one of the last configuration built for the
// gateway. It returns false for an object left out, and an error when what it
// references can't be read, other than an object that doesn't exist.
func convertObject[PT entityObject, E any](
	ctx context.Context, c *configuration, cl client.Client, obj PT, kind string,
	request func(context.Context, client.Client, PT) (any, error), adjust func(map[string]any) error,
) (converted[E], bool, error) {
	conv := converted[E]{status: entityStatus{obj: obj}}
	refs := sensitiveDataSecretRefs(obj)
	setSecretRefValid := func(problem string) {
		if len(refs) > 0 {
			conv.status.secretRefValid = secretRefValidCondition(obj, problem)
		}
	}
	// Anyone may name the gateway from another namespace: an object no
	// KongReferenceGrant lets in doesn't belong to it, and is only left out,
	// whatever else is wrong with it.
	message, err := c.parentNotGranted(ctx, cl, obj, kind)
	if err != nil {
		return conv, false, err
	}
	if message != "" {
		c.leaveOutNotPermitted(conv.status, message)
		return conv, false, nil
	}
	id, message := c.entityID(obj)
	if message != "" {
		c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
		return conv, false, nil
	}
	message, err = siblingsNotGranted(ctx, cl, obj)
	if err != nil {
		return conv, false, err
	}
	if message != "" {
		if keepBeingDeleted(c, obj, id, adjust, &conv) {
			return conv, true, nil
		}
		c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityReferenceNotPermittedReason, message)
		return conv, false, nil
	}
	message, err = secretRefsNotGranted(ctx, cl, obj, refs, kind)
	if err != nil {
		return conv, false, err
	}
	if message != "" {
		if keepBeingDeleted(c, obj, id, adjust, &conv) {
			// As for an object left out: its Secrets aren't read.
			setSecretRefValid("The Secrets it reads aren't read: " + message)
			return conv, true, nil
		}
		c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason, message)
		return conv, false, nil
	}

	reader := &referenceReader{Client: cl, secrets: map[client.ObjectKey]*corev1.Secret{}}
	req, err := request(ctx, reader, obj)
	if err == nil {
		err = convertEntity(req, obj, id, &conv.entity, adjust)
	}
	if reader.err != nil && !apierrors.IsNotFound(reader.err) {
		return conv, false, fmt.Errorf("failed to read a reference of %s %s: %w", kind, client.ObjectKeyFromObject(obj), reader.err)
	}
	if err == nil {
		// A Secret key holding an empty value converts, but can't be sent.
		if problem := c.secretProblem(obj, refs, reader.secrets); problem != "" {
			err = errors.New(problem)
		}
	}
	if err == nil {
		setSecretRefValid("")
		return conv, true, nil
	}
	secretErr := c.secretProblem(obj, refs, reader.secrets)
	if secretErr == "" {
		if reader.readAll(obj, refs) {
			setSecretRefValid("")
		}
		c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityInvalidReason, err.Error())
		return conv, false, nil
	}
	setSecretRefValid(secretErr)
	last, ok := c.previous[obj.GetUID()].(E)
	if !ok {
		c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityInvalidReason, secretErr)
		return conv, false, nil
	}
	// What the entity references may have changed since: it is adjusted
	// again, e.g. to check its references are still in the configuration.
	// Left out, it is still the last entity of the object, to be kept once
	// its references are back.
	if err := convertEntity(last, obj, id, &conv.entity, adjust); err != nil {
		c.built[obj.GetUID()] = last
		c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityInvalidReason, err.Error())
		return conv, false, nil
	}
	conv.status.kept = true
	return conv, true, nil
}

// keepBeingDeleted sets conv to the entity of an object being deleted, which
// FinalizerInUse holds for the policies of the last configuration sent, as it
// was in that configuration, and tells whether it did. Revoking a grant the
// object needs, e.g. deleting a namespace with its KongReferenceGrants, must
// not hold back its deletion, as against Konnect: excluded, it would stop the
// push that drops the policies using it, which alone releases it. The entity
// sent is the one gw-admin already has, so nothing the grant protects is read.
func keepBeingDeleted[E any](c *configuration, obj entityObject, id uuid.UUID, adjust func(map[string]any) error, conv *converted[E]) bool {
	if obj.GetDeletionTimestamp().IsZero() {
		return false
	}
	last, ok := c.previous[obj.GetUID()].(E)
	if !ok {
		return false
	}
	if err := convertEntity(last, obj, id, &conv.entity, adjust); err != nil {
		return false
	}
	conv.status.kept = true
	return true
}

// allObjects returns the objects of the configuration, included or not.
func (c *configuration) allObjects() []entityObject {
	objs := make([]entityObject, 0, len(c.included)+len(c.excluded))
	for _, e := range c.included {
		objs = append(objs, e.obj)
	}
	for _, e := range c.excluded {
		objs = append(objs, e.obj)
	}
	return objs
}

// refreshResourceVersions sets the resource versions of the objects of the
// configuration patched since they were read, e.g. their finalizers (see
// holdEntities), so that their status is patched from them.
func (c *configuration) refreshResourceVersions(versions map[reportedEntity]string) {
	refresh := func(obj entityObject) {
		if version, ok := versions[reportedEntity{typ: reflect.TypeOf(obj), key: client.ObjectKeyFromObject(obj)}]; ok {
			obj.SetResourceVersion(version)
		}
	}
	for _, e := range c.included {
		refresh(e.obj)
	}
	for _, e := range c.excluded {
		refresh(e.obj)
	}
}

// record records the object an entity of the configuration comes from, at
// the path of the entity, and includes the object in the configuration.
func (c *configuration) record(path, kind string, status entityStatus, entity any) {
	c.objects[path] = kind + " " + client.ObjectKeyFromObject(status.obj).String()
	c.built[status.obj.GetUID()] = entity
	c.included = append(c.included, status)
}

// exclude leaves the object out of the configuration, with the reason, and
// stops the configuration from being pushed: the object can't be sent as it
// is, and pushing the configuration without it would change what the gateway
// does, e.g. drop a policy encrypting records. An object reading Secrets whose
// SecretRefValid condition isn't known yet reports that its Secrets aren't
// read.
func (c *configuration) exclude(status entityStatus, reason kcfgconsts.ConditionReason, message string) {
	c.addExcluded(status, reason, message, true)
}

// leaveOut leaves the object out of the configuration, with the reason,
// without stopping the configuration from being pushed: the object doesn't
// belong to it, e.g. it is in another namespace no KongReferenceGrant lets in,
// which must not hold back the
// gateway, or it is left out along with its parent, which tells why.
func (c *configuration) leaveOut(status entityStatus, message string) {
	c.addExcluded(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, message, false)
}

// excludeConflict leaves out an object conflicting with an older one: it
// stops the configuration from being pushed, unless leftOut (see exclude and
// leaveOut).
func (c *configuration) excludeConflict(status entityStatus, leftOut bool, message string) {
	if leftOut {
		c.leaveOut(status, message)
		return
	}
	c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
}

// leaveOutNotPermitted leaves out, like leaveOut, an object of another
// namespace no KongReferenceGrant lets reference its parent: it doesn't belong
// to the gateway.
func (c *configuration) leaveOutNotPermitted(status entityStatus, message string) {
	c.addExcluded(status, eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason, message, false)
}

func (c *configuration) addExcluded(status entityStatus, reason kcfgconsts.ConditionReason, message string, blocks bool) {
	if status.secretRefValid == nil && len(sensitiveDataSecretRefs(status.obj)) > 0 {
		status.secretRefValid = secretRefValidCondition(status.obj, "The Secrets it reads aren't read: "+message)
	}
	c.excluded = append(c.excluded, excludedEntity{entityStatus: status, reason: reason, message: message, blocks: blocks})
}

// leftOutNotPermitted tells the object is left out of the configuration as it
// doesn't belong to the gateway: no KongReferenceGrant lets it reference its
// parent of another namespace (see leaveOutNotPermitted).
func (c *configuration) leftOutNotPermitted(t reflect.Type, key client.ObjectKey) bool {
	return slices.ContainsFunc(c.excluded, func(e excludedEntity) bool {
		return !e.blocks && e.reason == eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason &&
			reflect.TypeOf(e.obj) == t && client.ObjectKeyFromObject(e.obj) == key
	})
}

// isExcluded tells the object is left out of the configuration.
func (c *configuration) isExcluded(t reflect.Type, key client.ObjectKey) bool {
	return slices.ContainsFunc(c.excluded, func(e excludedEntity) bool {
		return reflect.TypeOf(e.obj) == t && client.ObjectKeyFromObject(e.obj) == key
	})
}

// blocked returns why the configuration can't be pushed: the objects that
// can't be sent and stop it (see exclude), or nil.
func (c *configuration) blocked() error {
	var blocking []excludedEntity
	for _, e := range c.excluded {
		if e.blocks {
			blocking = append(blocking, e)
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	return invalidEntitiesError{entities: blocking}
}

// invalidEntitiesError tells the configuration of a gateway isn't pushed:
// some of its entities can't be sent. gw-admin keeps the configuration it
// has.
type invalidEntitiesError struct {
	entities []excludedEntity
	// serving is the revision of the last configuration that could be sent,
	// pushed instead, or 0 when there is none.
	serving int64
}

func (e invalidEntitiesError) Error() string {
	problems := make([]string, 0, len(e.entities))
	for _, entity := range e.entities {
		problems = append(problems, fmt.Sprintf("%s %s: %s",
			reflect.TypeOf(entity.obj).Elem().Name(), client.ObjectKeyFromObject(entity.obj), entity.message))
	}
	slices.Sort(problems)
	keeps := "gw-admin keeps the one it has"
	if e.serving != 0 {
		keeps = fmt.Sprintf("gw-admin pods are pushed revision %d, the last that could be sent", e.serving)
	}
	return "The configuration isn't pushed, " + keeps + ": these entities can't be sent: " + strings.Join(problems, "; ")
}

// byCreation orders objects from the oldest to the newest, then by name and
// namespace: objects of several namespaces may share a name and a creation
// second, and come from the cache in any order.
func byCreation[PT client.Object](a, b PT) int {
	return cmp.Or(
		a.GetCreationTimestamp().Compare(b.GetCreationTimestamp().Time),
		cmp.Compare(a.GetName(), b.GetName()),
		cmp.Compare(a.GetNamespace(), b.GetNamespace()),
	)
}

// asEntities returns the entities held in lastEntities for a gateway, or nil.
func asEntities(v any) map[types.UID]any {
	entities, _ := v.(map[types.UID]any)
	return entities
}

// entityID returns the id of the entity in gw-admin, its UID, or why the
// entity is left out: its UID is not a version 4 UUID.
func (c *configuration) entityID(obj client.Object) (uuid.UUID, string) {
	id, ok := uidV4(obj)
	if !ok {
		return uuid.UUID{}, fmt.Sprintf("its UID %q is not a version 4 UUID", obj.GetUID())
	}
	return id, ""
}

// parentNotGranted returns why the object can't reference its parent, in
// another namespace, or "" when a KongReferenceGrant allows it or the parent
// is in its namespace. The grant of each reference, from the entity to its
// parent up to the gateway, is what lets an entity of another namespace into
// the configuration of the gateway: an object without it doesn't belong to the
// gateway, and is only left out, as anyone may name the gateway from another
// namespace, which must not hold back its configuration. It is checked once
// per object for the configuration.
func (c *configuration) parentNotGranted(ctx context.Context, cl client.Client, obj client.Object, kind string) (string, error) {
	entity, ok := obj.(entityWithParent)
	if !ok {
		return "", nil
	}
	ref := entity.GetParentRef()
	if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref.NamespacedRef == nil {
		return "", nil
	}
	parent := refKey(obj, ref.NamespacedRef.Namespace, ref.NamespacedRef.Name)
	if parent.Namespace == obj.GetNamespace() {
		return "", nil
	}
	self := kind + " " + client.ObjectKeyFromObject(obj).String()
	if message, ok := c.parentsNotGranted[self]; ok {
		return message, nil
	}
	message, err := notGrantedMessage(crossnamespace.CheckKongReferenceGrantForResource(
		ctx, cl, obj.GetNamespace(), parent.Namespace, parent.Name,
		metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(kind)), metav1.GroupVersionKind(entity.GetParentGVK()),
	))
	if err != nil {
		return "", err
	}
	if c.parentsNotGranted == nil {
		c.parentsNotGranted = map[string]string{}
	}
	c.parentsNotGranted[self] = message
	return message, nil
}

// siblingsNotGranted returns why the object can't reference another entity
// than its parent, e.g. a schema registry or a parent policy, in another
// namespace, or "" when a KongReferenceGrant allows every such reference. The
// object belongs to the gateway: without the reference it can't be sent.
func siblingsNotGranted(ctx context.Context, cl client.Client, obj client.Object) (string, error) {
	return notGrantedMessage(crossnamespace.CheckSiblingReferences(ctx, cl, obj))
}

// notGrantedMessage returns why a reference isn't allowed, from the error of
// its KongReferenceGrant check, or "" without one. It returns the error when
// it isn't a reference not granted, e.g. the grants couldn't be read.
func notGrantedMessage(err error) (string, error) {
	if err == nil {
		return "", nil
	}
	notGranted, ok := errors.AsType[*crossnamespace.ReferenceNotGrantedError](err)
	if !ok {
		return "", err
	}
	return fmt.Sprintf("no KongReferenceGrant allows it to reference the %s %s/%s", notGranted.ToGVK.Kind, notGranted.ToNamespace, notGranted.ToName), nil
}

// secretRefsNotGranted returns why the object can't read a Secret of refs,
// the Secrets it reads, in another namespace, or "" when a
// KongReferenceGrant allows every such Secret.
func secretRefsNotGranted(
	ctx context.Context, cl client.Client, obj client.Object, refs []configurationv1alpha1.SensitiveDataSecretRef, kind string,
) (string, error) {
	from := metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(kind))
	secretGVK := metav1.GroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	checked := map[client.ObjectKey]struct{}{}
	for _, ref := range refs {
		key := secretKey(obj, ref)
		if _, ok := checked[key]; ok || key.Namespace == obj.GetNamespace() {
			continue
		}
		checked[key] = struct{}{}
		err := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, obj.GetNamespace(), key.Namespace, key.Name, from, secretGVK)
		if crossnamespace.IsReferenceNotGranted(err) {
			return fmt.Sprintf("no KongReferenceGrant allows it to read the Secret %s", key), nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", nil
}

// secretProblem returns why a Secret of refs, the Secrets the object reads,
// can't be used, from the Secrets read to convert it: it doesn't exist, or
// lacks the key read.
func (c *configuration) secretProblem(obj client.Object, refs []configurationv1alpha1.SensitiveDataSecretRef, secrets map[client.ObjectKey]*corev1.Secret) string {
	for _, ref := range refs {
		key := secretKey(obj, ref)
		secret, read := secrets[key]
		if !read {
			continue
		}
		if secret == nil {
			return fmt.Sprintf("the Secret %s doesn't exist%s", key, c.secretHint)
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return fmt.Sprintf("the Secret %s has no key %q", key, ref.Key)
		}
		if len(value) == 0 {
			// gw-admin refuses an empty secret value: it is a problem of the
			// Secret, which may be filled in later, not of the entity.
			return fmt.Sprintf("the key %q of the Secret %s is empty", ref.Key, key)
		}
	}
	return ""
}

// secretKey returns the key of the Secret the reference names.
func secretKey(obj client.Object, ref configurationv1alpha1.SensitiveDataSecretRef) client.ObjectKey {
	return refKey(obj, ref.Namespace, ref.Name)
}

// refKey returns the key of the object a reference of obj names, in the
// namespace of obj unless it names one.
func refKey(obj client.Object, namespace *string, name string) client.ObjectKey {
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: name}
	if namespace != nil && *namespace != "" {
		key.Namespace = *namespace
	}
	return key
}

// sensitiveDataSecretRefs returns the Secrets the object reads through its
// sensitive data sources, if any.
func sensitiveDataSecretRefs(obj client.Object) []configurationv1alpha1.SensitiveDataSecretRef {
	withSecrets, ok := obj.(interface {
		GetSensitiveDataSecretRefs() []configurationv1alpha1.SensitiveDataSecretRef
	})
	if !ok {
		return nil
	}
	return withSecrets.GetSensitiveDataSecretRefs()
}

// referenceReader is the client an entity reads the objects it references
// with, e.g. its Secrets, when it is converted. It records the last error
// reading one, and the Secrets read, nil for one that doesn't exist, which
// tell why the conversion failed (see configuration.secretProblem).
type referenceReader struct {
	client.Client

	err     error
	secrets map[client.ObjectKey]*corev1.Secret
}

// Get reads the object, recording the error, or the Secret.
func (r *referenceReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	err := r.Client.Get(ctx, key, obj, opts...)
	if err != nil {
		r.err = err
	}
	if secret, ok := obj.(*corev1.Secret); ok && (err == nil || apierrors.IsNotFound(err)) {
		r.secrets[key] = nil
		if err == nil {
			r.secrets[key] = secret
		}
	}
	return err
}

// readAll tells every Secret of refs, the Secrets the object reads, was read.
func (r *referenceReader) readAll(obj client.Object, refs []configurationv1alpha1.SensitiveDataSecretRef) bool {
	for _, ref := range refs {
		if r.secrets[secretKey(obj, ref)] == nil {
			return false
		}
	}
	return true
}

// describeField names the object a field gw-admin reports comes from, e.g.
// "EventGatewayBackendCluster ns/name (backend_clusters.0.name)", or returns
// the field as it is when it comes from none.
func (c configuration) describeField(field string) string {
	// The entities are sent under entities, along with the version of the
	// configuration.
	for path := strings.TrimPrefix(field, "entities."); path != ""; {
		if object, ok := c.objects[path]; ok {
			return fmt.Sprintf("%s (%s)", object, field)
		}
		i := strings.LastIndex(path, ".")
		if i < 0 {
			break
		}
		path = path[:i]
	}
	return field
}

// convertEntity converts a Konnect SDK request into the gw-admin type of an
// entity, with the id and the creation time of its object. A field set to
// null is left out: it is how the SDK sends an unset optional field, which
// leaving it out means to gw-admin too. A field gw-admin doesn't know is
// refused, naming the field: dropped silently, it would change what the
// entity does, and gw-admin rejects the unknown properties it is sent anyway.
// adjust, when set, adjusts the JSON payload before it is decoded, e.g. to
// resolve its references: its error is the error of the conversion.
func convertEntity(from any, obj client.Object, id uuid.UUID, to any, adjust func(map[string]any) error) error {
	value, err := jsonValue(from)
	if err != nil {
		return fmt.Errorf("failed to convert %T: %w", from, err)
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("failed to convert %T: not a JSON object", from)
	}
	if adjust != nil {
		if err := adjust(fields); err != nil {
			return err
		}
	}
	fields["id"] = id.String()
	fields["created_at"] = createdAt(obj).Format(time.RFC3339)
	withoutNulls(fields)

	data, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("failed to convert %T: %w", from, err)
	}
	if err := json.Unmarshal(data, to); err != nil {
		return fmt.Errorf("failed to convert to %T: %w", to, err)
	}
	decoded, err := jsonValue(to)
	if err != nil {
		return fmt.Errorf("failed to convert to %T: %w", to, err)
	}
	if field := unknownField(fields, decoded, ""); field != "" {
		return fmt.Errorf("it sets the field %s, which gw-admin doesn't support", field)
	}
	return nil
}

// jsonValue returns the JSON representation of v as a generic value, numbers
// kept as they are.
func jsonValue(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// unknownField returns the path of the first field of sent, e.g.
// tls.unknown, that decoded, the same JSON once decoded into the gw-admin
// type, doesn't have: a field the type doesn't know, which decoding drops.
// A field set to its zero value is not reported: the type may leave it out
// when empty, and leaving it out means the same. It relies on the gw-admin
// types encoding a field under the name they decode it from, as the
// generated types do.
func unknownField(sent, decoded any, path string) string {
	switch sent := sent.(type) {
	case map[string]any:
		decoded, _ := decoded.(map[string]any)
		for _, key := range slices.Sorted(maps.Keys(sent)) {
			field := key
			if path != "" {
				field = path + "." + key
			}
			child, ok := decoded[key]
			if !ok {
				if !isZero(sent[key]) {
					return field
				}
				continue
			}
			if f := unknownField(sent[key], child, field); f != "" {
				return f
			}
		}
	case []any:
		decoded, _ := decoded.([]any)
		for i := range min(len(sent), len(decoded)) {
			if f := unknownField(sent[i], decoded[i], path+"."+strconv.Itoa(i)); f != "" {
				return f
			}
		}
	}
	return ""
}

// isZero reports whether the JSON value is the zero value of its type.
func isZero(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case bool:
		return !v
	case string:
		return v == ""
	case json.Number:
		f, err := v.Float64()
		return err == nil && f == 0
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	}
	return false
}

// withoutNulls removes the object fields set to null from the JSON value, at
// any depth.
func withoutNulls(v any) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if child == nil {
				delete(v, k)
				continue
			}
			withoutNulls(child)
		}
	case []any:
		for _, child := range v {
			withoutNulls(child)
		}
	}
}

// addCertificates adds the data plane certificates attached to the gateway,
// from the oldest, or leaves them out with a reason. One being deleted is left
// out without a reason, it is going away: as any certificate the
// configuration doesn't hold, the push deletes it from gw-admin (see push).
func (c *configuration) addCertificates(ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) error {
	var list configurationv1alpha1.EventGatewayDataPlaneCertificateList
	if err := cl.List(ctx, &list, client.MatchingFields{
		index.IndexFieldEventGatewayDataPlaneCertificateOnOnPremEventGatewayRef: client.ObjectKeyFromObject(gateway).String(),
	}); err != nil {
		return fmt.Errorf("failed to list the EventGatewayDataPlaneCertificates of the gateway: %w", err)
	}
	certificates := live[configurationv1alpha1.EventGatewayDataPlaneCertificate](list.Items)
	slices.SortFunc(certificates, byCreation)
	for _, cert := range certificates {
		if err := c.addCertificate(ctx, cl, cert); err != nil {
			return err
		}
	}
	return nil
}

// addCertificate adds a data plane certificate attached to the gateway, or
// leaves it out with a reason. One whose Secret is gone is left out, not kept
// as it was, as other entities are: gw-admin stops trusting it. No
// certificate is recorded in built, so previous never holds one.
func (c *configuration) addCertificate(ctx context.Context, cl client.Client, cert *configurationv1alpha1.EventGatewayDataPlaneCertificate) error {
	// A certificate left out doesn't hold back the configuration: gw-admin
	// only stops trusting it, which is how one is revoked, e.g. by deleting
	// its Secret.
	excluded := len(c.excluded)
	defer func() {
		for i := excluded; i < len(c.excluded); i++ {
			c.excluded[i].blocks = false
		}
	}()
	conv, ok, err := convertObject[*configurationv1alpha1.EventGatewayDataPlaneCertificate, certificateEntity](
		ctx, c, cl, cert, "EventGatewayDataPlaneCertificate",
		func(ctx context.Context, cl client.Client, cert *configurationv1alpha1.EventGatewayDataPlaneCertificate) (any, error) {
			return cert.ToCreateEventGatewayDataPlaneCertificateRequest(ctx, cl)
		},
		func(fields map[string]any) error {
			// gw-admin accepts any string as a certificate.
			certificate, _ := fields["certificate"].(string)
			return validateCertificates(certificate)
		})
	if err != nil || !ok {
		return err
	}
	// convertObject checked the UID is a version 4 UUID.
	id, _ := uidV4(cert)
	c.certificates = append(c.certificates, dataPlaneCertificate{
		id: id, key: client.ObjectKeyFromObject(cert), body: conv.entity.UpsertEventGatewayDataPlaneCertificateRequestOnPrem, obj: cert,
	})
	c.included = append(c.included, conv.status)
	return nil
}

// certificateEntity is a data plane certificate converted as an entity: the
// request registering it, and its id, which goes in the path of the request.
type certificateEntity struct {
	gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem

	ID string `json:"id"`
}

// validateCertificates checks that data holds at least one PEM certificate,
// and no PEM block of another type: text around the blocks is ignored.
func validateCertificates(data string) error {
	rest := []byte(data)
	var found int
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return fmt.Errorf("the certificate holds a PEM block of type %q", block.Type)
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("the certificate is invalid: %w", err)
		}
		found++
	}
	if found == 0 {
		return errors.New("the certificate is not a PEM certificate")
	}
	return nil
}
