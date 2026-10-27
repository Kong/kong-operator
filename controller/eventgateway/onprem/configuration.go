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

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
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
	// previous holds the entities of the last configuration built for the
	// gateway, by the UID of their object, and built those of this one: an
	// entity reading a Secret that is gone is sent as it was last.
	previous, built map[types.UID]any
	// namespace is the namespace of the gateway: every entity of the
	// configuration must be in it.
	namespace string
	// secretHint tells how to make a Secret visible to the operator, when
	// its Secret cache is filtered by a label.
	secretHint string
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
	kept bool
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
}

// buildConfiguration builds the configuration of the gateway from the
// entities attached to it. An entity that can't be sent as it is left out,
// with a reason, unless only a Secret it reads is gone or lacks the key read:
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
	// The policies reference the entities added so far.
	targets := referenceTargetsOf(&cfg, resolver.read)
	if err := cfg.addVirtualClusterPolicies(ctx, resolver, virtualClusters, targets); err != nil {
		return configuration{}, err
	}
	if err := cfg.addListeners(ctx, resolver, gateway, targets); err != nil {
		return configuration{}, err
	}
	if err := cfg.addCertificates(ctx, cl, gateway); err != nil {
		return configuration{}, err
	}
	key, err := comparisonKey(withGWAdminDefaults(cfg.entities))
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
// going away.
func live[T any, PT interface {
	*T
	entityObject
}](items []T) []PT {
	objs := make([]PT, 0, len(items))
	for _, item := range items {
		obj := PT(&item)
		if obj.GetDeletionTimestamp().IsZero() {
			objs = append(objs, obj)
		}
	}
	return objs
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
				c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityInvalidReason, fmt.Sprintf("another %s of the gateway is named %q", k.what, name))
				continue
			}
			names[name] = struct{}{}
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
// keys of every object not being deleted: the others are left out.
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
			i, ok := p.index[parent]
			if !ok {
				c.exclude(entityStatus{obj: child}, eventgatewayv1alpha1.OnPremEntityInvalidReason,
					fmt.Sprintf("its %s %s isn't in the configuration", p.kind, parent))
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
	return addParentsFirst[PT, E](ctx, c, cl, children, parentOf, k)
}

// addParentsFirst adds the objects, a parent, of their own kind, before its
// children: those without a parent first, then their children, and so on.
// One whose parent isn't added is left out. parentOf, when set, returns the
// parent of an object.
func addParentsFirst[PT entityObject, E any](
	ctx context.Context, c *configuration, cl client.Client, objs []PT, parentOf func(PT) (client.ObjectKey, bool), k entityKind[PT, E],
) error {
	added := map[client.ObjectKey]int{}
	for pending := objs; len(pending) > 0; {
		var ready, waiting []PT
		for _, obj := range pending {
			if parentOf != nil {
				if parent, ok := parentOf(obj); ok {
					if _, ok := added[parent]; !ok {
						waiting = append(waiting, obj)
						continue
					}
				}
			}
			ready = append(ready, obj)
		}
		if len(ready) == 0 {
			for _, obj := range waiting {
				parent, _ := parentOf(obj)
				c.exclude(entityStatus{obj: obj}, eventgatewayv1alpha1.OnPremEntityInvalidReason,
					fmt.Sprintf("its parent policy %s isn't in the configuration", parent))
			}
			return nil
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

// converted is an object converted into its entity.
type converted[E any] struct {
	entity E
	status entityStatus
}

// convertObject converts the object into its entity, from its Konnect SDK
// request, whose JSON payload adjust, when set, adjusts, e.g. to resolve its
// references. It leaves the object out, with the reason, when it is in
// another namespace than the gateway, its UID is not a version 4 UUID, it
// reads a Secret of another namespace no KongReferenceGrant allows, or it
// can't be converted, unless only a Secret it reads is gone or lacks the key
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
	id, message := c.entityID(obj)
	if message != "" {
		c.exclude(conv.status, eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
		return conv, false, nil
	}
	message, err := secretRefsNotGranted(ctx, cl, obj, refs, kind)
	if err != nil {
		return conv, false, err
	}
	if message != "" {
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

// record records the object an entity of the configuration comes from, at
// the path of the entity, and includes the object in the configuration.
func (c *configuration) record(path, kind string, status entityStatus, entity any) {
	c.objects[path] = kind + " " + client.ObjectKeyFromObject(status.obj).String()
	c.built[status.obj.GetUID()] = entity
	c.included = append(c.included, status)
}

// exclude leaves the object out of the configuration, with the reason. An
// object reading Secrets whose SecretRefValid condition isn't known yet
// reports that its Secrets aren't read.
func (c *configuration) exclude(status entityStatus, reason kcfgconsts.ConditionReason, message string) {
	if status.secretRefValid == nil && len(sensitiveDataSecretRefs(status.obj)) > 0 {
		status.secretRefValid = secretRefValidCondition(status.obj, "The Secrets it reads aren't read: "+message)
	}
	c.excluded = append(c.excluded, excludedEntity{entityStatus: status, reason: reason, message: message})
}

// byCreation orders objects from the oldest to the newest, then by name.
func byCreation[PT client.Object](a, b PT) int {
	return cmp.Or(
		a.GetCreationTimestamp().Compare(b.GetCreationTimestamp().Time),
		cmp.Compare(a.GetName(), b.GetName()),
	)
}

// asEntities returns the entities held in lastEntities for a gateway, or nil.
func asEntities(v any) map[types.UID]any {
	entities, _ := v.(map[types.UID]any)
	return entities
}

// entityID returns the id of the entity in gw-admin, its UID, or why the
// entity is left out: it is in another namespace than the gateway, or its UID
// is not a version 4 UUID. Every entity of the configuration must be in the
// namespace of the gateway, like its parent: a parent reference may name
// another namespace, which would let an entity of any namespace into the
// configuration of the gateway.
func (c *configuration) entityID(obj client.Object) (uuid.UUID, string) {
	if obj.GetNamespace() != c.namespace {
		return uuid.UUID{}, fmt.Sprintf("it must be in the namespace of the OnPremEventGateway, %s", c.namespace)
	}
	id, ok := uidV4(obj)
	if !ok {
		return uuid.UUID{}, fmt.Sprintf("its UID %q is not a version 4 UUID", obj.GetUID())
	}
	return id, ""
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
		if _, ok := secret.Data[ref.Key]; !ok {
			return fmt.Sprintf("the Secret %s has no key %q", key, ref.Key)
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
