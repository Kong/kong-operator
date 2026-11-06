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
	"slices"
	"strings"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

// The policies reference other entities: the parent policy or the schema
// registry or static key of a policy of a virtual cluster, the TLS trust
// bundles of a listener policy. The conversions, written for
// Konnect, resolve such a reference to the Konnect ID or to the name of the
// entity. Converted through uidResolver, both are the UID of its object, which
// is its id in the configuration, and the reference is then checked to be to
// an entity of the configuration of the gateway (see resolveReferences). The
// virtual cluster a listener policy forwards to is the exception: the
// conversion leaves it named, and resolveReferences sends it by its id (see
// virtualClusterAt).

// uidResolver reads the entities the policies reference with their UID as
// their Konnect ID and as their name. It records the objects it reads by UID,
// to name them in messages.
type uidResolver struct {
	client.Client

	read map[uuid.UUID]client.ObjectKey
}

// Get reads the object, with its UID as its Konnect ID and as its name, when
// it has them. The parent reference of an entity attached to an
// OnPremEventGateway is hidden: the conversions refuse a reference to such an
// entity.
func (r uidResolver) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := r.Client.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	uid := string(obj.GetUID())
	if id, err := uuid.Parse(uid); err == nil {
		r.read[id] = key
	}
	if e, ok := obj.(interface{ SetKonnectID(string) }); ok {
		e.SetKonnectID(uid)
	}
	switch e := obj.(type) {
	case *configurationv1alpha1.EventGatewaySchemaRegistry:
		e.Spec.ParentRef = withoutOnPremParent(e.Spec.ParentRef)
		if config := e.Spec.APISpec.EventGatewaySchemaRegistryConfig; config != nil && config.SchemaRegistryConfluent != nil {
			config.SchemaRegistryConfluent.Name = uid
		}
	case *configurationv1alpha1.EventGatewayStaticKey:
		e.Spec.ParentRef = withoutOnPremParent(e.Spec.ParentRef)
		e.Spec.APISpec.Name = uid
	case *configurationv1alpha1.EventGatewayTLSTrustBundle:
		e.Spec.ParentRef = withoutOnPremParent(e.Spec.ParentRef)
	}
	return nil
}

// withoutOnPremParent returns nil for a reference to an OnPremEventGateway,
// the reference otherwise.
func withoutOnPremParent(ref *commonv1alpha1.GatewayRef) *commonv1alpha1.GatewayRef {
	if ref != nil && ref.IsGroupKind(eventgatewayv1alpha1.GroupName, eventgatewayv1alpha1.OnPremEventGatewayKind) {
		return nil
	}
	return ref
}

// entityIDs are the entities of a kind in the configuration, by id, with
// their name.
type entityIDs struct {
	kind  string
	names map[uuid.UUID]string
	// withName tells a reference names the entity along with its id, as
	// gw-admin returns it.
	withName bool
}

// referenceTargets are the entities of the configuration the policies
// reference, by kind.
type referenceTargets struct {
	schemaRegistries entityIDs
	staticKeys       entityIDs
	tlsTrustBundles  entityIDs
	// virtualClusters are the virtual clusters of the configuration, by name:
	// a listener policy forwards to one by its name.
	virtualClusters map[string]uuid.UUID
	// read names the objects uidResolver read, by UID.
	read map[uuid.UUID]client.ObjectKey
}

// referenceTargetsOf returns the entities of the configuration the policies
// reference.
func referenceTargetsOf(c *configuration, read map[uuid.UUID]client.ObjectKey) referenceTargets {
	t := referenceTargets{
		schemaRegistries: entityIDs{kind: "EventGatewaySchemaRegistry", names: map[uuid.UUID]string{}},
		staticKeys:       entityIDs{kind: "EventGatewayStaticKey", names: map[uuid.UUID]string{}, withName: true},
		tlsTrustBundles:  entityIDs{kind: "EventGatewayTLSTrustBundle", names: map[uuid.UUID]string{}},
		virtualClusters:  map[string]uuid.UUID{},
		read:             read,
	}
	for _, e := range c.entities.SchemaRegistries {
		t.schemaRegistries.names[e.Id] = schemaRegistryName(e)
	}
	for _, e := range c.entities.StaticKeys {
		t.staticKeys.names[e.Id] = e.Name
	}
	for _, e := range c.entities.TlsTrustBundles {
		t.tlsTrustBundles.names[e.Id] = e.Name
	}
	for _, e := range c.entities.VirtualClusters {
		t.virtualClusters[e.Name] = e.Id
	}
	return t
}

// resolve replaces a reference with a reference by id to an entity of the
// configuration, with its name when withName is set. The reference holds the
// UID of the object of the entity, as its id or as its name (see
// uidResolver).
func (t referenceTargets) resolve(ids entityIDs, ref map[string]any) error {
	s, _ := ref["id"].(string)
	if s == "" {
		s, _ = ref["name"].(string)
	}
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	name, known := ids.names[id]
	if err != nil || !known {
		if key, ok := t.read[id]; ok && err == nil {
			return fmt.Errorf("it references the %s %s, which isn't in the configuration of the gateway", ids.kind, key)
		}
		return fmt.Errorf("it references an %s that isn't in the configuration of the gateway", ids.kind)
	}
	clear(ref)
	ref["id"] = id.String()
	if ids.withName {
		ref["name"] = name
	}
	return nil
}

// entityReference is where the payload of a kind of policy may reference
// another entity of the configuration.
type entityReference struct {
	// path is the path of the reference in the payload, by JSON field, "*"
	// standing for every item of a list, e.g. config.schema_registry.
	path []string
	// resolve resolves the reference at the path.
	resolve func(referenceTargets, map[string]any) error
}

// schemaRegistryAt is the schema registry a reference at the path names.
func schemaRegistryAt(path ...string) entityReference {
	return entityReference{path: path, resolve: func(t referenceTargets, ref map[string]any) error {
		return t.resolve(t.schemaRegistries, ref)
	}}
}

// staticKeyAt is the static key an encryption key at the path references,
// when the encryption key is static: {type: static, key: {...}}.
func staticKeyAt(path ...string) entityReference {
	return entityReference{path: path, resolve: func(t referenceTargets, encryptionKey map[string]any) error {
		if encryptionKey["type"] != "static" {
			return nil
		}
		ref, ok := encryptionKey["key"].(map[string]any)
		if !ok {
			return nil
		}
		return t.resolve(t.staticKeys, ref)
	}}
}

// tlsTrustBundleAt is the TLS trust bundle a reference at the path names.
func tlsTrustBundleAt(path ...string) entityReference {
	return entityReference{path: path, resolve: func(t referenceTargets, ref map[string]any) error {
		return t.resolve(t.tlsTrustBundles, ref)
	}}
}

// virtualClusterAt is the virtual cluster a destination at the path names,
// by the name of its entity: it is sent by its id instead, as gw-admin keeps
// the name but gives the data planes an empty id for it. A destination by id
// is refused, a Konnect id meaning nothing to gw-admin, unless it is the id
// of a virtual cluster of the configuration, as in a policy kept from the
// last configuration.
func virtualClusterAt(path ...string) entityReference {
	return entityReference{path: path, resolve: func(t referenceTargets, destination map[string]any) error {
		if v, ok := destination["id"]; ok {
			s, _ := v.(string)
			if id, err := uuid.Parse(s); err == nil && slices.Contains(slices.Collect(maps.Values(t.virtualClusters)), id) {
				return nil
			}
			return fmt.Errorf("it forwards to a virtual cluster by id: name a virtual cluster of the gateway instead")
		}
		name, ok := destination["name"].(string)
		if !ok {
			return nil
		}
		id, ok := t.virtualClusters[name]
		if !ok {
			return fmt.Errorf("it forwards to the virtual cluster %q, which isn't in the configuration of the gateway", name)
		}
		clear(destination)
		destination["id"] = id.String()
		return nil
	}}
}

// staticKeySourceAt is the static keys of the gateway a decryption key source
// at the path uses, when it is static: {type: static}. gw-admin refuses the
// whole configuration of a gateway without static keys then.
func staticKeySourceAt(path ...string) entityReference {
	return entityReference{path: path, resolve: func(t referenceTargets, source map[string]any) error {
		if source["type"] != "static" || len(t.staticKeys.names) > 0 {
			return nil
		}
		return fmt.Errorf("it decrypts with the static keys of the gateway, and no %s is in the configuration of the gateway", t.staticKeys.kind)
	}}
}

// The references of the policies, as gw-admin places them: the schema
// registry of a schema validation policy and of the schema source of a
// transcode policy, the static key of an encryption, the static keys a
// decryption uses, the TLS trust bundles verifying the clients of a TLS
// listener, and the virtual cluster a listener forwards to.
var (
	consumePolicyReferences = []entityReference{
		schemaRegistryAt("config", "schema_registry"),
		schemaRegistryAt("config", "schema_source", "schema_registry"),
		staticKeySourceAt("config", "key_sources", "*"),
	}
	producePolicyReferences = []entityReference{
		schemaRegistryAt("config", "schema_registry"),
		schemaRegistryAt("config", "schema_source", "schema_registry"),
		staticKeyAt("config", "encryption_key"),
		staticKeyAt("config", "encrypt_fields", "*", "encryption_key"),
	}
	listenerPolicyReferences = []entityReference{
		tlsTrustBundleAt("config", "client_authentication", "tls_trust_bundles", "*"),
		virtualClusterAt("config", "destination"),
	}
)

// namespacedRefMember is the only way a policy of an OnPremEventGateway can
// reference another entity: as the type of a tagged union, or as the member of
// one without a type.
const namespacedRefMember = "namespacedRef"

// checkPolicyReferences returns why a policy can't reference what it does, on
// prem: see referencesByObject and noNewUserOfDeletedEntities.
func checkPolicyReferences(ctx context.Context, cl client.Client, policy entityObject, apiSpec any) error {
	if err := referencesByObject(apiSpec); err != nil {
		return err
	}
	return noNewUserOfDeletedEntities(ctx, cl, policy)
}

// referencesByObject returns an error when the API spec of a policy references
// a schema registry, a static key or a TLS trust bundle other than by object:
// an id or a name means nothing to gw-admin, whose entities are identified by
// the UID of their object, and such a reference wouldn't hold the entity (see
// entitiesInUse). It must be called on the spec, before the conversion turns a
// reference by object into a reference by id or by name.
func referencesByObject(apiSpec any) error {
	value, err := jsonValue(apiSpec)
	if err != nil {
		return fmt.Errorf("failed to read the references of the policy: %w", err)
	}
	return referencesByObjectIn(value, "spec.apiSpec")
}

// referencesByObjectIn is referencesByObject for the JSON value at the path.
// Only the union members in use are encoded, so one left set in another member
// doesn't count.
func referencesByObjectIn(v any, path string) error {
	switch v := v.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			field := path + "." + key
			var err error
			switch {
			case key == "schemaRegistry":
				err = notByObjectError(field, "its schema registry", schemaRegistryRefForm(v[key]))
			case key == "key" && strings.HasSuffix(path, ".static"):
				err = notByObjectError(field, "its static key", untypedRefForm(v[key]))
			case key == "tlsTrustBundles":
				bundles, _ := v[key].([]any)
				for i, bundle := range bundles {
					if err = notByObjectError(fmt.Sprintf("%s.%d", field, i), "a TLS trust bundle", untypedRefForm(bundle)); err != nil {
						break
					}
				}
			default:
				err = referencesByObjectIn(v[key], field)
			}
			if err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			if err := referencesByObjectIn(item, fmt.Sprintf("%s.%d", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// notByObjectError returns the error of a reference at the field in the form,
// or nil for the empty form of a reference by object.
func notByObjectError(field, what, form string) error {
	if form == "" {
		return nil
	}
	return fmt.Errorf("%s references %s by %s: with an OnPremEventGateway, only %s is allowed", field, what, form, namespacedRefMember)
}

// schemaRegistryRefForm returns how a reference to a schema registry that isn't
// by namespacedRef names it, or "". One without a type, as a consume schema
// validation policy may set, names the object directly: it means the same, but
// with an OnPremEventGateway every reference is written with namespacedRef.
func schemaRegistryRefForm(ref any) string {
	fields, _ := ref.(map[string]any)
	switch refType, _ := fields["type"].(string); refType {
	case namespacedRefMember:
		return ""
	case "":
		return "a name set directly"
	default:
		return refType
	}
}

// untypedRefForm returns how a reference without a type, whose set member tells
// its form, names the entity when it isn't by object, or "".
func untypedRefForm(ref any) string {
	fields, _ := ref.(map[string]any)
	if _, ok := fields[namespacedRefMember]; ok {
		return ""
	}
	for _, form := range []string{"id", "name"} {
		if _, ok := fields[form]; ok {
			return form
		}
	}
	return ""
}

// resolveReferences returns an adjustment of the payload of a policy that
// rewrites its references, at the paths of its kind, in order: the first
// reference that can't be resolved leaves the policy out. A payload it
// rewrote is left as it is, so that it also checks the references of a policy
// kept from the last configuration.
func (t referenceTargets) resolveReferences(refs []entityReference) func(map[string]any) error {
	return func(payload map[string]any) error {
		for _, ref := range refs {
			for _, obj := range objectsAt(payload, ref.path) {
				if err := ref.resolve(t, obj); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// objectsAt returns the JSON objects at the path of the value, "*" standing
// for every item of a list.
func objectsAt(v any, path []string) []map[string]any {
	values := []any{v}
	for _, field := range path {
		var next []any
		for _, value := range values {
			switch value := value.(type) {
			case []any:
				if field == "*" {
					next = append(next, value...)
				}
			case map[string]any:
				if child, ok := value[field]; ok && field != "*" {
					next = append(next, child)
				}
			}
		}
		values = next
	}
	objs := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if obj, ok := value.(map[string]any); ok {
			objs = append(objs, obj)
		}
	}
	return objs
}
