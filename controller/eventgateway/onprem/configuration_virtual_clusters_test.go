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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

func TestBuildConfigurationVirtualClusters(t *testing.T) {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
	gateway.SetUID(types.UID("6a2f1c8e-5b3d-4e7a-9c1f-2d8b4a6e0f13"))
	created := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return created.Add(time.Duration(minutes) * time.Minute) }

	backendCluster := func(name, apiName string, minutes int) *configurationv1alpha1.EventGatewayBackendCluster {
		return fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: `+apiName+`
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    tls: {enabled: Disabled}
`)
	}
	virtualCluster := func(name, apiName, backendCluster string, minutes int) *configurationv1alpha1.EventGatewayVirtualCluster {
		return fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: `+backendCluster+`}}
  apiSpec:
    name: `+apiName+`
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: `+apiName+`
`)
	}
	consumePolicy := func(name, manifest string, minutes int) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
		return fromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](t, at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
`+manifest)
	}

	bc := backendCluster("bc", "bc", 0)
	// Left out as a duplicate: its virtual cluster is left out with it.
	bcDuplicate := backendCluster("bc-duplicate", "bc", 1)
	vc := virtualCluster("vc", "vc", "bc", 2)
	vcDuplicate := virtualCluster("vc-duplicate", "vc", "bc", 3)
	vcOfDuplicate := virtualCluster("vc-of-duplicate", "vc-of-duplicate", "bc-duplicate", 4)

	clusterPolicy := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, at(5), `
metadata: {name: acls}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: acls
    acls:
      name: acls
      config:
        rules:
        - action: allow
          resourceType: topic
          operations: [{name: read}]
          resourceNames: {type: stat, stat: [{match: orders}]}
`)
	// The child is older than its parent: it is added once its parent is.
	child := consumePolicy("child", `
    type: modifyHeaders
    parentPolicyRef: {name: parent}
    modifyHeaders:
      name: child
      config:
        actions: [{op: remove, remove: {key: x-header}}]
`, 6)
	parent := consumePolicy("parent", `
    type: schemaValidation
    schemaValidation:
      name: parent
      config: {type: json, json: {valueValidationAction: mark}}
`, 7)
	orphan := consumePolicy("orphan", `
    type: modifyHeaders
    parentPolicyRef: {name: missing}
    modifyHeaders:
      name: orphan
      config:
        actions: [{op: remove, remove: {key: x-header}}]
`, 8)
	withRegistry := consumePolicy("with-registry", `
    type: schemaValidation
    schemaValidation:
      name: with-registry
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {name: registry}, valueValidationAction: mark}}
`, 9)
	withMissingRegistry := consumePolicy("with-missing-registry", `
    type: schemaValidation
    schemaValidation:
      name: with-missing-registry
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {name: missing}, valueValidationAction: mark}}
`, 9)
	registry := fromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, at(1), `
metadata: {name: registry}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    type: confluent
    confluent:
      name: registry
      config: {endpoint: "https://registry.example.com", schemaType: json}
`)
	// Named like registry: one of another gateway, and a duplicate of this
	// one, left out. A policy referencing either is left out, not bound to
	// registry.
	otherRegistry := fromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, at(1), `
metadata: {name: other-registry}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: other}
  apiSpec:
    type: confluent
    confluent:
      name: registry
      config: {endpoint: "https://other.example.com", schemaType: json}
`)
	duplicateRegistry := fromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, at(2), `
metadata: {name: duplicate-registry}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    type: confluent
    confluent:
      name: registry
      config: {endpoint: "https://duplicate.example.com", schemaType: json}
`)
	withOtherRegistry := consumePolicy("with-other-registry", `
    type: schemaValidation
    schemaValidation:
      name: with-other-registry
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {name: other-registry}, valueValidationAction: mark}}
`, 10)
	withDuplicateRegistry := consumePolicy("with-duplicate-registry", `
    type: schemaValidation
    schemaValidation:
      name: with-duplicate-registry
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {name: duplicate-registry}, valueValidationAction: mark}}
`, 10)
	staticKey := fromYAML[configurationv1alpha1.EventGatewayStaticKey](t, at(1), `
metadata: {name: key}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: key
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
	keySecret := &corev1.Secret{Name: "key-secret", Namespace: ns, Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")}}
	encrypt := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, at(9), `
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
        encryptionKey: {type: static, static: {key: {namespacedRef: {name: key}}}}
`)
	// Of another namespace, attached to a backend cluster of the gateway: left
	// out, whatever its parent reference says.
	vcOtherNamespace := virtualCluster("vc-other-namespace", "vc-other-namespace", "bc", 14)
	vcOtherNamespace.SetNamespace("other")
	vcOtherNamespace.Spec.EventGatewayBackendClusterRef.NamespacedRef.Namespace = new(ns)

	cl := newFakeClient(t,
		bc, bcDuplicate, vc, vcDuplicate, vcOfDuplicate, clusterPolicy, child, parent, orphan, withRegistry, withMissingRegistry,
		registry, staticKey, keySecret, encrypt, vcOtherNamespace,
		otherRegistry, duplicateRegistry, withOtherRegistry, withDuplicateRegistry,
	)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)

	excluded := map[string]string{}
	for _, e := range cfg.excluded {
		excluded[reflect.TypeOf(e.obj).Elem().Name()+" "+e.obj.GetName()] = e.message
	}
	assert.Equal(t, map[string]string{
		"EventGatewayBackendCluster bc-duplicate":                       `another backend cluster of the gateway is named "bc"`,
		"EventGatewayVirtualCluster vc-duplicate":                       `another virtual cluster of the gateway is named "vc"`,
		"EventGatewayVirtualCluster vc-of-duplicate":                    "its EventGatewayBackendCluster " + ns + "/bc-duplicate isn't in the configuration",
		"EventGatewayVirtualClusterConsumePolicy orphan":                "its parent policy " + ns + "/missing isn't in the configuration",
		"EventGatewayVirtualClusterConsumePolicy with-missing-registry": "resolving spec.apiSpec.schemaValidation.config.confluentSchemaRegistry.schemaRegistry references: EventGatewaySchemaRegistry " + ns + "/missing: eventgatewayschemaregistries.configuration.konghq.com \"missing\" not found",
		"EventGatewayVirtualCluster vc-other-namespace":                 "it must be in the namespace of the OnPremEventGateway, " + ns,
		"EventGatewaySchemaRegistry duplicate-registry":                 `another schema registry of the gateway is named "registry"`,
		"EventGatewayVirtualClusterConsumePolicy with-other-registry": "it references the EventGatewaySchemaRegistry " + ns +
			"/other-registry, which isn't in the configuration of the gateway",
		"EventGatewayVirtualClusterConsumePolicy with-duplicate-registry": "it references the EventGatewaySchemaRegistry " + ns +
			"/duplicate-registry, which isn't in the configuration of the gateway",
	}, excluded)

	got, err := json.Marshal(cfg.entities)
	require.NoError(t, err)
	var entities struct {
		VirtualClusters []struct {
			ID              string            `json:"id"`
			Name            string            `json:"name"`
			Destination     map[string]string `json:"destination"`
			ClusterPolicies []map[string]any  `json:"cluster_policies"`
			ConsumePolicies []map[string]any  `json:"consume_policies"`
			ProducePolicies []map[string]any  `json:"produce_policies"`
		} `json:"virtual_clusters"`
	}
	require.NoError(t, json.Unmarshal(got, &entities))

	require.Len(t, entities.VirtualClusters, 1)
	gotVC := entities.VirtualClusters[0]
	assert.Equal(t, string(vc.UID), gotVC.ID)
	assert.Equal(t, map[string]string{"id": string(bc.UID)}, gotVC.Destination, "a virtual cluster forwards to its backend cluster by its UID")
	require.Len(t, gotVC.ClusterPolicies, 1)
	assert.Equal(t, string(clusterPolicy.UID), gotVC.ClusterPolicies[0]["id"])
	assert.Equal(t, "acls", gotVC.ClusterPolicies[0]["type"])
	require.Len(t, gotVC.ConsumePolicies, 3)
	assert.Equal(t, string(parent.UID), gotVC.ConsumePolicies[0]["id"], "a parent comes before its children")
	assert.Equal(t, string(withRegistry.UID), gotVC.ConsumePolicies[1]["id"])
	assert.Equal(t, string(child.UID), gotVC.ConsumePolicies[2]["id"])
	assert.Equal(t, string(parent.UID), gotVC.ConsumePolicies[2]["parent_policy_id"], "a child references its parent by its UID")
	assert.Equal(t, "2026-10-19T10:07:00Z", gotVC.ConsumePolicies[0]["created_at"])
	registryRef := gotVC.ConsumePolicies[1]["config"].(map[string]any)["schema_registry"]
	assert.Equal(t, map[string]any{"id": string(registry.UID)}, registryRef, "a schema registry is referenced by its UID")
	require.Len(t, gotVC.ProducePolicies, 1)
	keyRef := gotVC.ProducePolicies[0]["config"].(map[string]any)["encryption_key"].(map[string]any)["key"]
	assert.Equal(t, map[string]any{"id": string(staticKey.UID), "name": "key"}, keyRef, "a static key is referenced by its UID, with its name as gw-admin returns it")

	assert.Equal(t, "EventGatewayVirtualClusterConsumePolicy "+client.ObjectKeyFromObject(child).String()+" (virtual_clusters.0.consume_policies.2.config)",
		cfg.describeField("virtual_clusters.0.consume_policies.2.config"))
}
