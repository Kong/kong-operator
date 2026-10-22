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
	// secretsRead tells the Secrets the object reads were read, or tried:
	// its SecretRefValid condition reports whether they resolve.
	secretsRead bool
	// secretErr is why a Secret the object reads can't be read, e.g. it
	// doesn't exist.
	secretErr string
}

// excludedEntity is an object left out of the configuration of its gateway.
type excludedEntity struct {
	entityStatus

	reason  kcfgconsts.ConditionReason
	message string
}

// buildConfiguration builds the configuration of the gateway from the
// entities attached to it. An entity that can't be sent as it is left out,
// with a reason, unless only a Secret it reads is gone: it is then sent as it
// was in previous, the entities of the last configuration built for the
// gateway. The same entities always produce the same configuration: each
// entity has the UID and the creation time of its object. An error reading
// the cluster, other than an object that doesn't exist, is returned: leaving
// the entity out would delete it from gw-admin.
//
// TODO: add the other kinds of entities (#6060, #6061, #6062).
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
	if err := cfg.addBackendClusters(ctx, cl, gateway); err != nil {
		return configuration{}, err
	}
	return cfg, nil
}

// addBackendClusters adds the backend clusters of the gateway.
func (c *configuration) addBackendClusters(ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) error {
	backendClusters, err := backendClustersOf(ctx, cl, gateway)
	if err != nil {
		return err
	}
	return addEntities(ctx, c, cl, backendClusters, entityKind[*configurationv1alpha1.EventGatewayBackendCluster, gwadminapi.EventGatewayBackendClusterOnPrem]{
		kind: "EventGatewayBackendCluster",
		what: "backend cluster",
		path: "backend_clusters",
		convert: func(ctx context.Context, cl client.Client, bc *configurationv1alpha1.EventGatewayBackendCluster, id uuid.UUID) (gwadminapi.EventGatewayBackendClusterOnPrem, error) {
			var entity gwadminapi.EventGatewayBackendClusterOnPrem
			req, err := bc.ToCreateBackendClusterRequest(ctx, cl)
			if err != nil {
				return entity, err
			}
			return entity, convertEntity(req, bc, id, &entity)
		},
		name:     func(e gwadminapi.EventGatewayBackendClusterOnPrem) string { return e.Name },
		entities: &c.entities.BackendClusters,
	})
}

// backendClustersOf returns the backend clusters attached to the gateway.
func backendClustersOf(ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) ([]*configurationv1alpha1.EventGatewayBackendCluster, error) {
	var list configurationv1alpha1.EventGatewayBackendClusterList
	if err := cl.List(ctx, &list, client.MatchingFields{
		index.IndexFieldEventGatewayBackendClusterOnOnPremEventGatewayRef: client.ObjectKeyFromObject(gateway).String(),
	}); err != nil {
		return nil, fmt.Errorf("failed to list the EventGatewayBackendClusters of the gateway: %w", err)
	}
	return pointersTo(list.Items), nil
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
	// convert converts an object into its entity, with its id, reading its
	// Secrets with the client.
	convert func(context.Context, client.Client, PT, uuid.UUID) (E, error)
	// name returns the name of an entity.
	name func(E) string
	// entities is the list of the configuration the entities are added to.
	entities *[]E
}

// addEntities adds the entities of the objects, from the oldest. Two
// entities of a kind can't have the same name: the oldest one is kept.
func addEntities[PT entityObject, E any](ctx context.Context, c *configuration, cl client.Client, objs []PT, k entityKind[PT, E]) error {
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
		exclude := func(reason kcfgconsts.ConditionReason, message string) {
			c.excluded = append(c.excluded, excludedEntity{entityStatus: status, reason: reason, message: message})
		}
		id, message := c.entityID(obj)
		if message != "" {
			exclude(eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
			continue
		}
		message, err := secretRefsNotGranted(ctx, cl, obj, k.kind)
		if err != nil {
			return err
		}
		if message != "" {
			exclude(eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason, message)
			continue
		}

		reader := &readErrRecorder{Client: cl}
		entity, err := k.convert(ctx, reader, obj, id)
		if reader.err != nil && !apierrors.IsNotFound(reader.err) {
			return fmt.Errorf("failed to read a reference of %s %s: %w", k.kind, client.ObjectKeyFromObject(obj), reader.err)
		}
		status.secretsRead = len(sensitiveDataSecretRefs(obj)) > 0
		if err != nil {
			if !reader.secretNotFound {
				exclude(eventgatewayv1alpha1.OnPremEntityInvalidReason, err.Error())
				continue
			}
			status.secretErr = err.Error() + c.secretHint
			last, ok := c.previous[obj.GetUID()].(E)
			if !ok {
				exclude(eventgatewayv1alpha1.OnPremEntityInvalidReason, status.secretErr)
				continue
			}
			entity = last
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

// pointersTo returns a pointer to each item.
func pointersTo[T any](items []T) []*T {
	ptrs := make([]*T, 0, len(items))
	for i := range items {
		ptrs = append(ptrs, &items[i])
	}
	return ptrs
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

// secretRefsNotGranted returns why the object can't read a Secret it
// references in another namespace, or "" when a KongReferenceGrant allows
// every such reference.
func secretRefsNotGranted(ctx context.Context, cl client.Client, obj client.Object, kind string) (string, error) {
	from := metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(kind))
	secret := metav1.GroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	for _, ref := range sensitiveDataSecretRefs(obj) {
		if ref.Namespace == nil || *ref.Namespace == "" || *ref.Namespace == obj.GetNamespace() {
			continue
		}
		err := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, obj.GetNamespace(), *ref.Namespace, ref.Name, from, secret)
		if crossnamespace.IsReferenceNotGranted(err) {
			return fmt.Sprintf("no KongReferenceGrant allows it to read the Secret %s/%s", *ref.Namespace, ref.Name), nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", nil
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

// readErrRecorder is the client an entity reads the objects it references
// with, e.g. its Secrets, when it is converted, recording the last error
// reading one: one that doesn't exist leaves the entity out, unless it is a
// Secret (see buildConfiguration), any other is retried.
type readErrRecorder struct {
	client.Client

	err            error
	secretNotFound bool
}

// Get reads the object, recording the error.
func (r *readErrRecorder) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	err := r.Client.Get(ctx, key, obj, opts...)
	if err != nil {
		_, isSecret := obj.(*corev1.Secret)
		r.err, r.secretNotFound = err, isSecret && apierrors.IsNotFound(err)
	}
	return err
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
