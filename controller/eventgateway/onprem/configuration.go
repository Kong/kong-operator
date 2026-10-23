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
	"encoding/json"
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
// entities attached to the gateway: the entities it receives, and the
// entities left out, with the reason.
type configuration struct {
	// entities is sent with PUT .../entities.
	entities gwadminapi.GatewayEntities
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
//
// TODO: add the virtual clusters and the listeners (#6061, #6062).
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
	if err := cfg.addGatewayEntities(ctx, cl, gateway); err != nil {
		return configuration{}, err
	}
	key, err := comparisonKey(withGWAdminDefaults(cfg.entities))
	if err != nil {
		return configuration{}, fmt.Errorf("failed to encode the entities of the gateway: %w", err)
	}
	cfg.pushedKey = key
	return cfg, nil
}

// addGatewayEntities adds the entities attached to the gateway itself.
func (c *configuration) addGatewayEntities(ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) error {
	var backendClusters configurationv1alpha1.EventGatewayBackendClusterList
	if err := addEntities(ctx, c, cl, gateway, &backendClusters, &backendClusters.Items, entityKind[*configurationv1alpha1.EventGatewayBackendCluster, gwadminapi.EventGatewayBackendClusterOnPrem]{
		kind: "EventGatewayBackendCluster", what: "backend cluster", path: "backend_clusters",
		field: index.IndexFieldEventGatewayBackendClusterOnOnPremEventGatewayRef,
		request: func(ctx context.Context, cl client.Client, bc *configurationv1alpha1.EventGatewayBackendCluster) (any, error) {
			return bc.ToCreateBackendClusterRequest(ctx, cl)
		},
		name:     func(e gwadminapi.EventGatewayBackendClusterOnPrem) string { return e.Name },
		entities: &c.entities.BackendClusters,
	}); err != nil {
		return err
	}

	var bundles configurationv1alpha1.EventGatewayTLSTrustBundleList
	if err := addEntities(ctx, c, cl, gateway, &bundles, &bundles.Items, entityKind[*configurationv1alpha1.EventGatewayTLSTrustBundle, gwadminapi.EventGatewayTLSTrustBundleOnPrem]{
		kind: "EventGatewayTLSTrustBundle", what: "TLS trust bundle", path: "tls_trust_bundles",
		field: index.IndexFieldEventGatewayTLSTrustBundleOnOnPremEventGatewayRef,
		request: func(ctx context.Context, cl client.Client, b *configurationv1alpha1.EventGatewayTLSTrustBundle) (any, error) {
			return b.ToCreateTLSTrustBundleRequest(ctx, cl)
		},
		name:     func(e gwadminapi.EventGatewayTLSTrustBundleOnPrem) string { return e.Name },
		entities: &c.entities.TlsTrustBundles,
	}); err != nil {
		return err
	}

	var registries configurationv1alpha1.EventGatewaySchemaRegistryList
	if err := addEntities(ctx, c, cl, gateway, &registries, &registries.Items, entityKind[*configurationv1alpha1.EventGatewaySchemaRegistry, gwadminapi.EventGatewaySchemaRegistryOnPrem]{
		kind: "EventGatewaySchemaRegistry", what: "schema registry", path: "schema_registries",
		field: index.IndexFieldEventGatewaySchemaRegistryOnOnPremEventGatewayRef,
		request: func(ctx context.Context, cl client.Client, r *configurationv1alpha1.EventGatewaySchemaRegistry) (any, error) {
			return r.ToSchemaRegistryCreate(ctx, cl)
		},
		name:     schemaRegistryName,
		entities: &c.entities.SchemaRegistries,
	}); err != nil {
		return err
	}

	var keys configurationv1alpha1.EventGatewayStaticKeyList
	return addEntities(ctx, c, cl, gateway, &keys, &keys.Items, entityKind[*configurationv1alpha1.EventGatewayStaticKey, gwadminapi.EventGatewayStaticKeyOnPrem]{
		kind: "EventGatewayStaticKey", what: "static key", path: "static_keys",
		field: index.IndexFieldEventGatewayStaticKeyOnOnPremEventGatewayRef,
		request: func(ctx context.Context, cl client.Client, k *configurationv1alpha1.EventGatewayStaticKey) (any, error) {
			return k.ToEventGatewayStaticKeyCreate(ctx, cl)
		},
		name:     func(e gwadminapi.EventGatewayStaticKeyOnPrem) string { return e.Name },
		entities: &c.entities.StaticKeys,
	})
}

// schemaRegistryName returns the name of a schema registry: Confluent is its
// only type.
func schemaRegistryName(e gwadminapi.EventGatewaySchemaRegistryOnPrem) string {
	registry, err := e.AsSchemaRegistryConfluent()
	if err != nil {
		return ""
	}
	return registry.Name
}

// entityKind is a kind of entity attached to the gateway itself.
type entityKind[PT entityObject, E any] struct {
	// kind is the kind of the objects, e.g. EventGatewayBackendCluster.
	kind string
	// what names an entity of the kind in messages, e.g. "backend cluster".
	what string
	// path is the list of the entities in the configuration, e.g.
	// backend_clusters.
	path string
	// field is the index field of the objects by gateway.
	field string
	// request returns the Konnect SDK request of an object, reading its
	// Secrets with the client: its entity is converted from it.
	request func(context.Context, client.Client, PT) (any, error)
	// name returns the name of an entity.
	name func(E) string
	// entities is the list of the configuration the entities are added to.
	entities *[]E
}

// addEntities lists the objects of the kind attached to the gateway into
// list, whose items are items, and adds their entities, from the oldest. Two
// entities of a kind can't have the same name: the oldest one is kept.
func addEntities[T any, PT interface {
	*T
	entityObject
}, E any](
	ctx context.Context, c *configuration, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway,
	list client.ObjectList, items *[]T, k entityKind[PT, E],
) error {
	if err := cl.List(ctx, list, client.MatchingFields{k.field: client.ObjectKeyFromObject(gateway).String()}); err != nil {
		return fmt.Errorf("failed to list the %ss of the gateway: %w", k.kind, err)
	}
	objs := make([]PT, 0, len(*items))
	for i := range *items {
		objs = append(objs, PT(&(*items)[i]))
	}
	slices.SortFunc(objs, func(a, b PT) int {
		return cmp.Or(
			a.GetCreationTimestamp().Compare(b.GetCreationTimestamp().Time),
			cmp.Compare(a.GetName(), b.GetName()),
		)
	})

	names := map[string]struct{}{}
	for _, obj := range objs {
		if !obj.GetDeletionTimestamp().IsZero() {
			continue
		}
		status := entityStatus{obj: obj}
		refs := sensitiveDataSecretRefs(obj)
		readsSecrets := len(refs) > 0
		setSecretRefValid := func(problem string) {
			if readsSecrets {
				status.secretRefValid = secretRefValidCondition(obj, problem)
			}
		}
		exclude := func(reason kcfgconsts.ConditionReason, message string) {
			if status.secretRefValid == nil {
				setSecretRefValid("The Secrets it reads aren't read: " + message)
			}
			c.excluded = append(c.excluded, excludedEntity{entityStatus: status, reason: reason, message: message})
		}
		id, message := c.entityID(obj)
		if message != "" {
			exclude(eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
			continue
		}
		message, err := secretRefsNotGranted(ctx, cl, obj, refs, k.kind)
		if err != nil {
			return err
		}
		if message != "" {
			exclude(eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason, message)
			continue
		}

		reader := &referenceReader{Client: cl, secrets: map[client.ObjectKey]*corev1.Secret{}}
		var entity E
		req, err := k.request(ctx, reader, obj)
		if err == nil {
			err = convertEntity(req, obj, id, &entity)
		}
		if reader.err != nil && !apierrors.IsNotFound(reader.err) {
			return fmt.Errorf("failed to read a reference of %s %s: %w", k.kind, client.ObjectKeyFromObject(obj), reader.err)
		}
		secretErr := ""
		if err != nil {
			secretErr = c.secretProblem(obj, refs, reader.secrets)
			if secretErr == "" {
				if reader.readAll(obj, refs) {
					setSecretRefValid("")
				}
				exclude(eventgatewayv1alpha1.OnPremEntityInvalidReason, err.Error())
				continue
			}
		} else {
			setSecretRefValid("")
		}
		if secretErr != "" {
			setSecretRefValid(secretErr)
			last, ok := c.previous[obj.GetUID()].(E)
			if !ok {
				exclude(eventgatewayv1alpha1.OnPremEntityInvalidReason, secretErr)
				continue
			}
			entity, status.kept = last, true
		}

		name := k.name(entity)
		if _, ok := names[name]; ok {
			exclude(eventgatewayv1alpha1.OnPremEntityInvalidReason, fmt.Sprintf("another %s of the gateway is named %q", k.what, name))
			continue
		}
		names[name] = struct{}{}
		c.objects[fmt.Sprintf("%s.%d", k.path, len(*k.entities))] = k.kind + " " + client.ObjectKeyFromObject(obj).String()
		*k.entities = append(*k.entities, entity)
		c.built[obj.GetUID()] = entity
		c.included = append(c.included, status)
	}
	return nil
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
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: ref.Name}
	if ref.Namespace != nil && *ref.Namespace != "" {
		key.Namespace = *ref.Namespace
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
	for path := field; path != ""; {
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
func convertEntity(from any, obj client.Object, id uuid.UUID, to any) error {
	value, err := jsonValue(from)
	if err != nil {
		return fmt.Errorf("failed to convert %T: %w", from, err)
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("failed to convert %T: not a JSON object", from)
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
