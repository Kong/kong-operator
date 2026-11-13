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
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

func TestStaticKeyID(t *testing.T) {
	key := client.ObjectKey{Namespace: "keys", Name: "key"}
	id := staticKeyID(key)
	assert.Equal(t, uuid.Version(4), id.Version(), "gw-admin requires version 4 UUIDs")
	assert.Equal(t, uuid.RFC4122, id.Variant())
	assert.Equal(t, "694607a4-0432-4931-9eb5-71563b7ffa35", id.String(),
		"changing how the id is derived changes the id of every key: the records they encrypted could no longer be decrypted")
	assert.Equal(t, id, staticKeyID(key), "the same key, e.g. of a recreated object, keeps the id")
	assert.NotEqual(t, id, staticKeyID(client.ObjectKey{Namespace: "keys", Name: "other"}))
	assert.NotEqual(t, id, staticKeyID(client.ObjectKey{Namespace: "other", Name: "key"}))
	assert.NotEqual(t, staticKeyID(client.ObjectKey{Namespace: "a", Name: "b/c"}), staticKeyID(client.ObjectKey{Namespace: "a/b", Name: "c"}),
		"namespace and name can't be mixed up")
	assert.NotEqual(t, id, hashedEntityID(configurationv1alpha1.GroupVersion.WithKind("EventGatewaySchemaRegistry").GroupKind(), key),
		"objects of other kinds get other ids")
	assert.NotEqual(t, id, hashedEntityID(schema.GroupKind{Group: "other.konghq.com", Kind: "EventGatewayStaticKey"}, key),
		"objects of other groups get other ids")
}

// staticKeyObjects returns a gateway with a virtual cluster whose produce
// policy encrypts with the static key key, of the Secret key-secret.
func staticKeyObjects(t *testing.T) (*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayStaticKey, *corev1.Secret, []client.Object) {
	t.Helper()
	created := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	gateway := newTestGateway("gateway")
	backend := fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, created, `
metadata: {name: backend}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: backend
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    tls: {enabled: Disabled}
`)
	vc := fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, created, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	key := fromYAML[configurationv1alpha1.EventGatewayStaticKey](t, created, `
metadata: {name: key}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: key
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
	secret := &corev1.Secret{Name: "key-secret", Namespace: ns, Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")}}
	encrypt := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created, `
metadata: {name: encrypt}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: encrypt
    encrypt:
      name: encrypt
      config:
        failureMode: error
        partOfRecord: [value]
        encryptionKey: {type: static, static: {key: {name: key}}}
`)
	return gateway, key, secret, []client.Object{backend, vc, encrypt}
}

// encryptionKeyID returns the id of the static key the produce policy of the
// configuration encrypts with.
func encryptionKeyID(t *testing.T, cfg configuration) string {
	t.Helper()
	require.Len(t, cfg.entities.VirtualClusters, 1)
	require.Len(t, cfg.entities.VirtualClusters[0].ProducePolicies, 1)
	data, err := json.Marshal(cfg.entities.VirtualClusters[0].ProducePolicies[0])
	require.NoError(t, err)
	var policy struct {
		Config struct {
			EncryptionKey struct {
				Key struct {
					ID string `json:"id"`
				} `json:"key"`
			} `json:"encryption_key"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &policy))
	return policy.Config.EncryptionKey.Key.ID
}

// TestBuildConfigurationStaticKeyRecreated checks the id of a static key, and
// of the references to it, survives the recreation of its object with the
// same value, and that the policies using it still hold it.
func TestBuildConfigurationStaticKeyRecreated(t *testing.T) {
	gateway, key, secret, objects := staticKeyObjects(t)
	cl := newFakeClient(t, append(objects, key, secret)...)
	build := func(previous map[types.UID]any) configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", previous)
		require.NoError(t, err)
		require.Empty(t, cfg.excluded)
		require.Len(t, cfg.entities.StaticKeys, 1)
		return cfg
	}
	held := func(cfg configuration, k *configurationv1alpha1.EventGatewayStaticKey) []reportedEntity {
		return cfg.entitiesInUse[reportedEntity{typ: reflect.TypeOf(k), key: client.ObjectKeyFromObject(k)}]
	}
	encrypt := reportedEntity{typ: producePolicyType, key: client.ObjectKey{Namespace: ns, Name: "encrypt"}}

	first := build(nil)
	id := first.entities.StaticKeys[0].Id
	assert.NotEqual(t, string(key.UID), id.String())
	assert.Equal(t, id.String(), encryptionKeyID(t, first), "the policy references the key by its id")
	assert.Equal(t, []reportedEntity{encrypt}, held(first, key))

	// The object is recreated, with the same Secret: another UID.
	require.NoError(t, cl.Delete(t.Context(), key))
	recreated := key.DeepCopy()
	recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, cl.Create(t.Context(), recreated))
	second := build(first.built)
	assert.Equal(t, id, second.entities.StaticKeys[0].Id, "a recreated key keeps its id: the records it encrypted are still decrypted")
	assert.Equal(t, id.String(), encryptionKeyID(t, second))
	assert.Equal(t, []reportedEntity{encrypt}, held(second, recreated), "the policy still holds the key")
	assert.Equal(t, []reportedEntity{encrypt}, entitiesInUseBySent(second.entities, second)[reportedEntity{typ: staticKeyType, key: client.ObjectKeyFromObject(recreated)}],
		"the configuration sent holds the key by its id too")

	// Its Secret gone, it is sent as it was last, under the same id.
	require.NoError(t, cl.Delete(t.Context(), secret))
	third, err := buildConfiguration(t.Context(), cl, gateway, "", second.built)
	require.NoError(t, err)
	require.Len(t, third.entities.StaticKeys, 1)
	assert.Equal(t, id, third.entities.StaticKeys[0].Id, "a key kept as it was keeps its id")
	assert.Equal(t, id.String(), encryptionKeyID(t, third))
	assert.Equal(t, []reportedEntity{encrypt}, held(third, recreated))

	// Its value changed: the id stays, as it tells nothing of the value.
	changed := secret.DeepCopy()
	changed.ResourceVersion = ""
	changed.Data = map[string][]byte{"key": []byte("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")}
	require.NoError(t, cl.Create(t.Context(), changed))
	fourth := build(third.built)
	assert.Equal(t, id, fourth.entities.StaticKeys[0].Id)
	assert.Equal(t, id.String(), encryptionKeyID(t, fourth))
}

// TestEntitiesInUseBySentStaticKeyLeftOut checks a static key the last
// configuration sent uses stays held when it is left out of the current one,
// e.g. once the KongReferenceGrant letting it in is gone: the entities sent
// reference it by its id, which its object gives whether it is in the
// configuration or not.
func TestEntitiesInUseBySentStaticKeyLeftOut(t *testing.T) {
	for name, uid := range map[string]types.UID{
		"left out":                        types.UID(uuid.NewString()),
		"with a UID that isn't a v4 UUID": "11111111-1111-1111-1111-111111111111",
	} {
		t.Run(name, func(t *testing.T) {
			key := &configurationv1alpha1.EventGatewayStaticKey{}
			key.SetNamespace(ns)
			key.SetName("key")
			key.SetUID(uid)
			encrypt := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
			encrypt.SetNamespace(ns)
			encrypt.SetName("encrypt")
			encrypt.SetUID(types.UID(uuid.NewString()))
			cfg := configuration{
				namespace: ns,
				included:  []entityStatus{{obj: encrypt}},
				excluded:  []excludedEntity{{obj: key}},
			}

			var sent gwadminapi.GatewayEntitySet
			require.NoError(t, json.Unmarshal([]byte(`{"virtual_clusters": [{"id": "`+uuid.NewString()+`", "name": "vc",
				"produce_policies": [{"id": "`+string(encrypt.UID)+`", "name": "encrypt", "type": "encrypt",
				  "config": {"failure_mode": "error", "part_of_record": ["value"],
				    "encryption_key": {"type": "static", "key": {"id": "`+staticKeyID(client.ObjectKeyFromObject(key)).String()+`"}}}}]}]}`), &sent))

			assert.Equal(t, []reportedEntity{{typ: producePolicyType, key: client.ObjectKeyFromObject(encrypt)}},
				entitiesInUseBySent(sent, cfg)[reportedEntity{typ: staticKeyType, key: client.ObjectKeyFromObject(key)}])
		})
	}
}

// TestBuildConfigurationStaticKeyExcluded checks a policy whose static key is
// left out names the key, although the key is referenced by its id, not the
// UID of its object.
func TestBuildConfigurationStaticKeyExcluded(t *testing.T) {
	gateway, key, _, objects := staticKeyObjects(t)
	// Without its Secret, nor a configuration sent before: left out.
	cl := newFakeClient(t, append(objects, key)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)

	excluded := map[string]string{}
	for _, e := range cfg.excluded {
		excluded[reflect.TypeOf(e.obj).Elem().Name()+" "+e.obj.GetName()] = e.message
	}
	assert.Contains(t, excluded, "EventGatewayStaticKey key")
	assert.Equal(t, "it references the EventGatewayStaticKey "+ns+"/key, which isn't in the configuration of the gateway",
		excluded["EventGatewayVirtualClusterProducePolicy encrypt"])
}

// TestBuildConfigurationStaticKeysOfTwoNamespaces checks static keys of the
// same name in two namespaces get two ids, and a policy references the one it
// names.
func TestBuildConfigurationStaticKeysOfTwoNamespaces(t *testing.T) {
	const other = "other"
	gateway, key, secret, objects := staticKeyObjects(t)
	otherKey := key.DeepCopy()
	otherKey.SetNamespace(other)
	otherKey.SetUID(types.UID(uuid.NewString()))
	otherKey.Spec.ParentRef.Namespace = new(ns)
	otherKey.Spec.APISpec.Name = "other-key"
	otherSecret := secret.DeepCopy()
	otherSecret.Namespace = other
	// The key of the other namespace belongs to the gateway, and the encrypt
	// policy may reference it.
	keyToGateway := &configurationv1alpha1.KongReferenceGrant{
		Name: "keys", Namespace: ns,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayStaticKey", Namespace: other}},
			To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "eventgateway.konghq.com", Kind: "OnPremEventGateway"}},
		},
	}
	policyToKey := &configurationv1alpha1.KongReferenceGrant{
		Name: "policies", Namespace: other,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayVirtualClusterProducePolicy", Namespace: ns}},
			To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "configuration.konghq.com", Kind: "EventGatewayStaticKey"}},
		},
	}
	// The encrypt policy encrypts with the key of the other namespace.
	encrypt := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC), `
metadata: {name: encrypt}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: encrypt
    encrypt:
      name: encrypt
      config:
        failureMode: error
        partOfRecord: [value]
        encryptionKey: {type: static, static: {key: {name: key, namespace: `+other+`}}}
`)
	objects = slices.DeleteFunc(objects, func(o client.Object) bool { return o.GetName() == "encrypt" })
	cl := newFakeClient(t, append(objects, encrypt, key, secret, otherKey, otherSecret, keyToGateway, policyToKey)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, cfg.excluded)

	ids := map[string]string{}
	for _, e := range cfg.entities.StaticKeys {
		ids[e.Name] = e.Id.String()
	}
	assert.Equal(t, map[string]string{
		"key":       staticKeyID(client.ObjectKeyFromObject(key)).String(),
		"other-key": staticKeyID(client.ObjectKeyFromObject(otherKey)).String(),
	}, ids)
	assert.NotEqual(t, ids["key"], ids["other-key"])
	assert.Equal(t, ids["other-key"], encryptionKeyID(t, cfg), "the policy references the key of the namespace it names")
}
