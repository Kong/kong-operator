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
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
