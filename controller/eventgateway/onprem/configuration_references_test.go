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
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// TestReferencesByObjectCoversConfiguredReferences checks referencesByObject
// against the references the CRD generator declares in crd-from-oas/config.yaml:
// each reference of a policy to a schema registry, a static key or a TLS trust
// bundle must be refused when not by object, and accepted by object. A
// reference added there, or a field renamed, that referencesByObjectIn doesn't
// recognize fails here, rather than letting a reference by id or by name
// through on prem.
func TestReferencesByObjectCoversConfiguredReferences(t *testing.T) {
	data, err := os.ReadFile("../../../crd-from-oas/config.yaml")
	require.NoError(t, err)
	var config any
	require.NoError(t, yaml.Unmarshal(data, &config))

	policies := []string{"EventGatewayListenerPolicy", "EventGatewayVirtualClusterProducePolicy", "EventGatewayVirtualClusterConsumePolicy"}
	// A parent policy reference is by object only: it has no other form to
	// refuse.
	heldKindNames := []string{"EventGatewaySchemaRegistry", "EventGatewayStaticKey", "EventGatewayTLSTrustBundle"}
	type reference struct{ entity, path string }
	var references []reference
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if name, _ := v["name"].(string); slices.Contains(policies, name) {
				refs, _ := v["references"].([]any)
				for _, r := range refs {
					ref, _ := r.(map[string]any)
					kinds, _ := ref["kinds"].([]any)
					path, _ := ref["path"].(string)
					if len(kinds) == 1 && slices.Contains(heldKindNames, kinds[0].(string)) {
						references = append(references, reference{entity: name, path: path})
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(config)
	require.NotEmpty(t, references, "no reference found in the configuration of the generator")

	// specAt returns an API spec holding the reference at the path.
	specAt := func(path string, ref any) map[string]any {
		fields := strings.Split(strings.TrimPrefix(path, "spec.apiSpec."), ".")
		spec := map[string]any{}
		current := spec
		for _, field := range fields[:len(fields)-1] {
			next := map[string]any{}
			current[field] = next
			current = next
		}
		current[fields[len(fields)-1]] = ref
		return spec
	}
	for _, r := range references {
		t.Run(r.entity+" "+r.path, func(t *testing.T) {
			byID := any(map[string]any{"type": "id", "id": map[string]any{"id": uuid.NewString()}})
			byObject := any(map[string]any{"type": namespacedRefMember, namespacedRefMember: map[string]any{"name": "entity"}})
			if strings.HasSuffix(r.path, ".tlsTrustBundles") {
				byID = []any{map[string]any{"id": uuid.NewString()}}
				byObject = []any{map[string]any{namespacedRefMember: map[string]any{"name": "entity"}}}
			}
			err := referencesByObject(specAt(r.path, byID))
			require.Error(t, err)
			// The path of the reference, or of an item of a list of them.
			assert.Regexp(t, `^`+regexp.QuoteMeta(r.path)+`(\.\d+)? references `, err.Error())
			require.NoError(t, referencesByObject(specAt(r.path, byObject)))
		})
	}
}

// TestReferencesByObjectIgnoresLabels checks that a label whose key is the
// name of a reference field isn't read as a reference.
func TestReferencesByObjectIgnoresLabels(t *testing.T) {
	var spec configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec
	require.NoError(t, yaml.UnmarshalStrict([]byte(`
type: schemaValidation
schemaValidation:
  name: validate
  labels: {schemaRegistry: orders, tlsTrustBundles: bundle, key: static}
  config: {type: json, json: {valueValidationAction: mark}}
`), &spec))
	require.NoError(t, referencesByObject(spec))
}

func TestResolveReferences(t *testing.T) {
	registry, key, elsewhere := uuid.New(), uuid.New(), uuid.New()
	targets := referenceTargets{
		schemaRegistries: entityIDs{kind: "EventGatewaySchemaRegistry", names: map[uuid.UUID]string{registry: "registry"}},
		staticKeys:       entityIDs{kind: "EventGatewayStaticKey", names: map[uuid.UUID]string{key: "key"}, withName: true},
		read:             map[uuid.UUID]client.ObjectKey{elsewhere: {Namespace: ns, Name: "elsewhere"}},
	}

	// The conversions reference an entity by its Konnect ID or its name, both
	// its UID through uidResolver.
	t.Run("at the paths of the kind", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{
			"encryption_key": map[string]any{"type": "static", "key": map[string]any{"id": key.String()}},
			"encrypt_fields": []any{
				map[string]any{"encryption_key": map[string]any{"type": "static", "key": map[string]any{"id": key.String()}}},
				map[string]any{"encryption_key": map[string]any{"type": "aws", "arn": "arn"}},
			},
			"schema_source": map[string]any{"schema_registry": map[string]any{"name": registry.String()}},
			// Not a path of a produce policy: left as it is.
			"other": map[string]any{"schema_registry": map[string]any{"name": registry.String()}},
		}}
		require.NoError(t, targets.resolveReferences(producePolicyReferences)(payload))
		config := payload["config"].(map[string]any)
		assert.Equal(t, map[string]any{"id": key.String(), "name": "key"}, config["encryption_key"].(map[string]any)["key"],
			"a static key is named along with its id, as gw-admin returns it")
		fields := config["encrypt_fields"].([]any)
		assert.Equal(t, map[string]any{"id": key.String(), "name": "key"}, fields[0].(map[string]any)["encryption_key"].(map[string]any)["key"])
		assert.Equal(t, map[string]any{"type": "aws", "arn": "arn"}, fields[1].(map[string]any)["encryption_key"])
		assert.Equal(t, map[string]any{"id": registry.String()}, config["schema_source"].(map[string]any)["schema_registry"])
		assert.Equal(t, map[string]any{"name": registry.String()}, config["other"].(map[string]any)["schema_registry"])
	})

	t.Run("an object read that isn't in the configuration", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"schema_registry": map[string]any{"name": elsewhere.String()}}}
		require.EqualError(t, targets.resolveReferences(consumePolicyReferences)(payload),
			"it references the EventGatewaySchemaRegistry "+ns+"/elsewhere, which isn't in the configuration of the gateway")
	})

	t.Run("an id that isn't in the configuration", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"schema_registry": map[string]any{"id": uuid.NewString()}}}
		require.EqualError(t, targets.resolveReferences(consumePolicyReferences)(payload),
			"it references an EventGatewaySchemaRegistry that isn't in the configuration of the gateway")
	})

	t.Run("a name that isn't a UID", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"schema_registry": map[string]any{"name": "registry"}}}
		require.Error(t, targets.resolveReferences(consumePolicyReferences)(payload))
	})

	t.Run("a decryption with the static keys of a gateway without any", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"key_sources": []any{map[string]any{"type": "static"}}}}
		require.NoError(t, targets.resolveReferences(consumePolicyReferences)(payload))
		withoutKeys := targets
		withoutKeys.staticKeys = entityIDs{kind: "EventGatewayStaticKey", names: map[uuid.UUID]string{}}
		require.EqualError(t, withoutKeys.resolveReferences(consumePolicyReferences)(payload),
			"it decrypts with the static keys of the gateway, and no EventGatewayStaticKey is in the configuration of the gateway")
	})

	t.Run("a static key without a key", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"encryption_key": map[string]any{"type": "static"}}}
		require.NoError(t, targets.resolveReferences(producePolicyReferences)(payload))
	})

	t.Run("a path through something else than an object or a list", func(t *testing.T) {
		assert.Empty(t, objectsAt(map[string]any{"config": "x"}, []string{"config", "schema_registry"}))
		assert.Empty(t, objectsAt(map[string]any{"list": "x"}, []string{"list", "*"}))
		assert.Empty(t, objectsAt(map[string]any{"leaf": "x"}, []string{"leaf"}))
	})
}

func TestResolveListenerPolicyReferences(t *testing.T) {
	bundle, vc := uuid.New(), uuid.New()
	targets := referenceTargets{
		tlsTrustBundles: entityIDs{kind: "EventGatewayTLSTrustBundle", names: map[uuid.UUID]string{bundle: "bundle"}},
		virtualClusters: map[string]uuid.UUID{"vc": vc},
		read:            map[uuid.UUID]client.ObjectKey{},
	}

	t.Run("the TLS trust bundles verifying the clients", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"client_authentication": map[string]any{
			"tls_trust_bundles": []any{map[string]any{"id": bundle.String()}},
		}}}
		require.NoError(t, targets.resolveReferences(listenerPolicyReferences)(payload))
		assert.Equal(t, []any{map[string]any{"id": bundle.String()}},
			payload["config"].(map[string]any)["client_authentication"].(map[string]any)["tls_trust_bundles"])

		payload = map[string]any{"config": map[string]any{"client_authentication": map[string]any{
			"tls_trust_bundles": []any{map[string]any{"id": uuid.NewString()}},
		}}}
		require.EqualError(t, targets.resolveReferences(listenerPolicyReferences)(payload),
			"it references an EventGatewayTLSTrustBundle that isn't in the configuration of the gateway")
	})

	t.Run("a virtual cluster forwarded to by name is sent by its id", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"destination": map[string]any{"name": "vc"}}}
		require.NoError(t, targets.resolveReferences(listenerPolicyReferences)(payload))
		assert.Equal(t, map[string]any{"id": vc.String()}, payload["config"].(map[string]any)["destination"])
	})

	t.Run("a virtual cluster that isn't in the configuration", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"destination": map[string]any{"name": "unknown"}}}
		require.EqualError(t, targets.resolveReferences(listenerPolicyReferences)(payload),
			`it forwards to the virtual cluster "unknown", which isn't in the configuration of the gateway`)
	})

	t.Run("a virtual cluster forwarded to by id", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"destination": map[string]any{"id": uuid.NewString()}}}
		require.EqualError(t, targets.resolveReferences(listenerPolicyReferences)(payload),
			"it forwards to a virtual cluster by id: name a virtual cluster of the gateway instead")
	})

	t.Run("a destination already resolved is left as it is", func(t *testing.T) {
		payload := map[string]any{"config": map[string]any{"destination": map[string]any{"name": "vc"}}}
		resolve := targets.resolveReferences(listenerPolicyReferences)
		require.NoError(t, resolve(payload))
		require.NoError(t, resolve(payload))
		assert.Equal(t, map[string]any{"id": vc.String()}, payload["config"].(map[string]any)["destination"])
	})
}
