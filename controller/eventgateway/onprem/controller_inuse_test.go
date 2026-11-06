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
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// schemaRegistryFixture builds the objects of a gateway with a virtual
// cluster and schema registries, and its policies referencing them.
type schemaRegistryFixture struct {
	t       *testing.T
	gateway *eventgatewayv1alpha1.OnPremEventGateway
	created time.Time
}

// fixtureDeletion is when, in minutes, the entities of a fixture being deleted
// started being: after the objects created around them, unless told otherwise.
const fixtureDeletion = 1000

func newSchemaRegistryFixture(t *testing.T) schemaRegistryFixture {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
	gateway.SetUID(types.UID("7b3e2d9f-6c4e-4f8b-8d2a-3e9c5b7f1a24"))
	return schemaRegistryFixture{t: t, gateway: gateway, created: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
}

func (f schemaRegistryFixture) at(minutes int) time.Time {
	return f.created.Add(time.Duration(minutes) * time.Minute)
}

func (f schemaRegistryFixture) virtualCluster() []client.Object {
	return []client.Object{
		fromYAML[configurationv1alpha1.EventGatewayBackendCluster](f.t, f.at(0), `
metadata: {name: bc}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: bc
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    tls: {enabled: Disabled}
`),
		fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](f.t, f.at(1), `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: bc}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`),
	}
}

func (f schemaRegistryFixture) registry(name, gateway string, minutes int) *configurationv1alpha1.EventGatewaySchemaRegistry {
	return fromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: `+gateway+`}
  apiSpec:
    type: confluent
    confluent:
      name: `+name+`
      config: {endpoint: "https://registry.example.com", schemaType: avro}
`)
}

func (f schemaRegistryFixture) producePolicy(name, apiSpec string, minutes int) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
	return fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
`+apiSpec)
}

func (f schemaRegistryFixture) consumePolicy(name, apiSpec string, minutes int) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
	return fromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
`+apiSpec)
}

// validateWith is a produce policy validating records with the schema registry
// the reference names.
func (f schemaRegistryFixture) validateWith(name, schemaRegistry string, minutes int) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
	return f.producePolicy(name, `
    type: schemaValidation
    schemaValidation:
      name: `+name+`
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: `+schemaRegistry+`, valueValidationAction: reject}}
`, minutes)
}

func (f schemaRegistryFixture) objects(objs ...client.Object) client.Client {
	return newFakeClient(f.t, append(f.virtualCluster(), objs...)...)
}

// policiesOf returns the entities of the produce and consume policies of the
// virtual cluster of the configuration.
func policiesOf(t *testing.T, cfg configuration) (produce, consume []map[string]any) {
	t.Helper()
	got, err := json.Marshal(cfg.entities)
	require.NoError(t, err)
	var entities struct {
		VirtualClusters []struct {
			ConsumePolicies []map[string]any `json:"consume_policies"`
			ProducePolicies []map[string]any `json:"produce_policies"`
		} `json:"virtual_clusters"`
	}
	require.NoError(t, json.Unmarshal(got, &entities))
	require.Len(t, entities.VirtualClusters, 1)
	return entities.VirtualClusters[0].ProducePolicies, entities.VirtualClusters[0].ConsumePolicies
}

// at returns the value at the path of nested objects.
func at(t *testing.T, v any, path ...string) any {
	t.Helper()
	for _, key := range path {
		m, ok := v.(map[string]any)
		require.True(t, ok, "no object at %q in %v", key, v)
		v = m[key]
	}
	return v
}

func excludedMessages(cfg configuration) map[string]string {
	excluded := map[string]string{}
	for _, e := range cfg.excluded {
		excluded[reflect.TypeOf(e.obj).Elem().Name()+" "+e.obj.GetName()] = e.message
	}
	return excluded
}

func TestBuildConfigurationSchemaRegistryObjectReferences(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	orders := f.registry("orders", "gateway", 2)
	elsewhere := f.registry("elsewhere", "other-gateway", 3)

	validateConfluent := f.validateWith("validate-confluent", `{type: namespacedRef, namespacedRef: {name: orders}}`, 4)
	// A schema validation of type json parses without a schema: it names no
	// registry, and a transcode policy can be its child.
	validateJSON := f.producePolicy("validate-json", `
    type: schemaValidation
    schemaValidation:
      name: validate-json
      config: {type: json, json: {valueValidationAction: reject}}
`, 5)
	// A transcode policy is the child of a schema validation policy.
	transcodeAvro := f.producePolicy("transcode-avro", `
    type: transcode
    parentPolicyRef: {name: validate-json}
    transcode:
      name: transcode-avro
      config:
        outputFormat: avro
        avro: {failureMode: reject, schemaSource: {type: reference, reference: {subject: orders-value, version: latest, schemaRefDestination: {type: none, none: {}}, schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}}}}
`, 6)
	parent := f.consumePolicy("parent", `
    type: schemaValidation
    schemaValidation:
      name: parent
      config: {type: json, json: {valueValidationAction: mark}}
`, 7)
	transcodeJSON := f.consumePolicy("transcode-json", `
    type: transcode
    parentPolicyRef: {name: parent}
    transcode:
      name: transcode-json
      config:
        outputFormat: json
        json: {failureMode: error, schemaSource: {type: reference, reference: {subject: orders-value, version: latest, schemaRefDestination: {type: none, none: {}}, schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}}}}
`, 8)
	ofAnotherGateway := f.validateWith("of-another-gateway", `{type: namespacedRef, namespacedRef: {name: elsewhere}}`, 10)

	cl := f.objects(orders, elsewhere, validateJSON, validateConfluent, transcodeAvro, parent, transcodeJSON, ofAnotherGateway)
	cfg, err := buildConfiguration(t.Context(), cl, f.gateway, "", nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"EventGatewayVirtualClusterProducePolicy of-another-gateway": "it references the EventGatewaySchemaRegistry " + ns +
			"/elsewhere, which isn't in the configuration of the gateway",
	}, excludedMessages(cfg))

	produce, consume := policiesOf(t, cfg)
	require.Len(t, produce, 3)
	require.Len(t, consume, 2)
	byID := map[types.UID]map[string]any{}
	for _, p := range append(produce, consume...) {
		byID[types.UID(p["id"].(string))] = p
	}
	byRegistry := map[string]any{"id": string(orders.UID)}
	assert.Equal(t, byRegistry, at(t, byID[validateConfluent.UID], "config", "schema_registry"), "a produce schema validation names its registry by object")
	assert.Equal(t, byRegistry, at(t, byID[transcodeAvro.UID], "config", "schema_source", "schema_registry"), "a produce transcode names its registry by object")
	assert.Equal(t, byRegistry, at(t, byID[transcodeJSON.UID], "config", "schema_source", "schema_registry"), "a consume transcode names its registry by object")

	var registriesInUse int
	for e := range cfg.entitiesInUse {
		if e.typ == schemaRegistryType {
			registriesInUse++
		}
	}
	require.Equal(t, 1, registriesInUse, "a registry of another gateway isn't held")
	assert.ElementsMatch(t, []string{
		"EventGatewayVirtualClusterProducePolicy validate-confluent",
		"EventGatewayVirtualClusterProducePolicy transcode-avro",
		"EventGatewayVirtualClusterConsumePolicy transcode-json",
	}, policiesUsing(cfg, orders), "only policies in the configuration hold a registry")
}

// policiesUsing returns the policies holding the entity in the configuration.
func policiesUsing(cfg configuration, entity entityObject) []string {
	var names []string
	for _, p := range cfg.entitiesInUse[reportedEntity{typ: reflect.TypeOf(entity), key: client.ObjectKeyFromObject(entity)}] {
		names = append(names, p.typ.Elem().Name()+" "+p.key.Name)
	}
	return names
}

func (f schemaRegistryFixture) staticKey(name string, minutes int) *configurationv1alpha1.EventGatewayStaticKey {
	return fromYAML[configurationv1alpha1.EventGatewayStaticKey](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: `+name+`
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
}

func (f schemaRegistryFixture) keySecret() *corev1.Secret {
	return &corev1.Secret{Name: "key-secret", Namespace: ns, Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")}}
}

func (f schemaRegistryFixture) encrypt(name, key string, minutes int) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
	return f.producePolicy(name, `
    type: encrypt
    encrypt:
      name: `+name+`
      config:
        failureMode: error
        partOfRecord: [value]
        encryptionKey: {type: static, static: {key: `+key+`}}
`, minutes)
}

func (f schemaRegistryFixture) trustBundle(name string, minutes int) *configurationv1alpha1.EventGatewayTLSTrustBundle {
	return fromYAML[configurationv1alpha1.EventGatewayTLSTrustBundle](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: `+name+`
    config: {trustedCa: {type: inline, value: ca-pem}}
`)
}

func (f schemaRegistryFixture) listener() *configurationv1alpha1.EventGatewayListener {
	return fromYAML[configurationv1alpha1.EventGatewayListener](f.t, f.at(0), `
metadata: {name: listener}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec: {name: listener, addresses: ["0.0.0.0"], ports: [19092]}
`)
}

func (f schemaRegistryFixture) tlsSecret() *corev1.Secret {
	return &corev1.Secret{Name: "tls", Namespace: ns, Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")}}
}

func (f schemaRegistryFixture) tlsServer(name, bundles string, minutes int) *configurationv1alpha1.EventGatewayListenerPolicy {
	return fromYAML[configurationv1alpha1.EventGatewayListenerPolicy](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: tlsServer
    tlsServer:
      name: `+name+`
      config:
        certificates:
          - certificate: {type: secretRef, secretRef: {name: tls, key: tls.crt}}
            key: {type: secretRef, secretRef: {name: tls, key: tls.key}}
        clientAuthentication:
          mode: required
          tlsTrustBundles: `+bundles+`
`)
}

func (f schemaRegistryFixture) forward(name, destination string, minutes int) *configurationv1alpha1.EventGatewayListenerPolicy {
	return fromYAML[configurationv1alpha1.EventGatewayListenerPolicy](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: `+name+`
      config:
        type: portMapping
        portMapping: {advertisedHost: localhost, destination: `+destination+`}
`)
}

// namedVirtualCluster is a virtual cluster of the backend cluster of
// virtualCluster, whose object name differs from the name of its entity.
func (f schemaRegistryFixture) namedVirtualCluster(name, apiName string, minutes int) *configurationv1alpha1.EventGatewayVirtualCluster {
	return fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: bc}}
  apiSpec:
    name: `+apiName+`
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: `+apiName+`
`)
}

func TestBuildConfigurationVirtualClusterInUse(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	orders := f.namedVirtualCluster("orders-vc", "orders", 2)
	forward := f.forward("forward", `{namespacedRef: {name: orders-vc}}`, 3)

	destinationOf := func(t *testing.T, cfg configuration) any {
		t.Helper()
		got, err := json.Marshal(cfg.entities)
		require.NoError(t, err)
		var entities struct {
			Listeners []struct {
				Policies []map[string]any `json:"policies"`
			} `json:"listeners"`
		}
		require.NoError(t, json.Unmarshal(got, &entities))
		require.Len(t, entities.Listeners, 1)
		require.Len(t, entities.Listeners[0].Policies, 1)
		return at(t, entities.Listeners[0].Policies[0], "config", "destination")
	}

	t.Run("it is named by its object name, sent by its id, and held", func(t *testing.T) {
		cfg, err := buildConfiguration(t.Context(), f.objects(orders, f.listener(), forward), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Empty(t, excludedMessages(cfg))
		assert.Equal(t, map[string]any{"id": string(orders.UID)}, destinationOf(t, cfg))
		assert.Equal(t, []string{"EventGatewayListenerPolicy forward"}, policiesUsing(cfg, orders))
	})

	t.Run("being deleted while held, it stays with the policy", func(t *testing.T) {
		held := orders.DeepCopy()
		held.SetFinalizers([]string{FinalizerInUse})
		held.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		cfg, err := buildConfiguration(t.Context(), f.objects(held, f.listener(), forward), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Empty(t, excludedMessages(cfg))
		assert.Equal(t, map[string]any{"id": string(orders.UID)}, destinationOf(t, cfg))
		assert.Equal(t, "; being deleted, it is kept while these policies reference it: EventGatewayListenerPolicy forward",
			heldMessage(cfg, keptStatus(t, cfg, orders)))
	})

	t.Run("being deleted without the finalizer, the policy is left out", func(t *testing.T) {
		gone := orders.DeepCopy()
		gone.SetFinalizers([]string{"example.com/hold"})
		gone.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		cfg, err := buildConfiguration(t.Context(), f.objects(gone, f.listener(), forward), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"EventGatewayListenerPolicy forward": "it references the EventGatewayVirtualCluster " + ns + "/orders-vc, which isn't in the configuration of the gateway",
		}, excludedMessages(cfg))
		assert.Empty(t, cfg.entitiesInUse)
	})

	t.Run("a virtual cluster of another gateway is reported", func(t *testing.T) {
		otherBackend := fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, f.at(1), `
metadata: {name: other-bc}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: other-gateway}
  apiSpec:
    name: other-bc
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    tls: {enabled: Disabled}
`)
		elsewhere := f.namedVirtualCluster("elsewhere", "elsewhere", 2)
		elsewhere.Spec.EventGatewayBackendClusterRef.NamespacedRef.Name = "other-bc"
		policy := f.forward("forward", `{namespacedRef: {name: elsewhere}}`, 3)
		cfg, err := buildConfiguration(t.Context(), f.objects(otherBackend, elsewhere, f.listener(), policy), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"EventGatewayListenerPolicy forward": "it references the EventGatewayVirtualCluster " + ns + "/elsewhere, which isn't in the configuration of the gateway",
		}, excludedMessages(cfg))
		assert.Empty(t, cfg.entitiesInUse)
	})
}

func TestBuildConfigurationStaticKeysAndTrustBundlesInUse(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	key := f.staticKey("key", 2)
	unusedKey := f.staticKey("unused-key", 3)
	bundle := f.trustBundle("bundle", 4)
	encrypt := f.encrypt("encrypt", `{namespacedRef: {name: key}}`, 5)
	tls := f.tlsServer("tls", `[{namespacedRef: {name: bundle}}]`, 6)

	cfg, err := buildConfiguration(t.Context(), f.objects(f.keySecret(), f.tlsSecret(), key, unusedKey, bundle, f.listener(), encrypt, tls), f.gateway, "", nil)
	require.NoError(t, err)
	assert.Empty(t, excludedMessages(cfg))
	assert.Equal(t, []string{"EventGatewayVirtualClusterProducePolicy encrypt"}, policiesUsing(cfg, key))
	assert.Equal(t, []string{"EventGatewayListenerPolicy tls"}, policiesUsing(cfg, bundle), "the policies of the listeners count")
	assert.Empty(t, policiesUsing(cfg, unusedKey))

	t.Run("being deleted while held, they stay with the policies using them", func(t *testing.T) {
		key, bundle := key.DeepCopy(), bundle.DeepCopy()
		for _, held := range []client.Object{key, bundle} {
			held.SetFinalizers([]string{FinalizerInUse})
			held.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		}
		cfg, err := buildConfiguration(t.Context(), f.objects(f.keySecret(), f.tlsSecret(), key, bundle, f.listener(), encrypt, tls), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Empty(t, excludedMessages(cfg))
		assert.Len(t, cfg.entities.StaticKeys, 1)
		assert.Len(t, cfg.entities.TlsTrustBundles, 1)
		assert.Len(t, cfg.entities.Listeners[0].Policies, 1)
	})

	t.Run("a data plane certificate being deleted isn't kept by the finalizer", func(t *testing.T) {
		certificate := fromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, f.at(7), `
metadata: {name: certificate}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    certificate: {type: inline, value: not-read}
`)
		certificate.SetFinalizers([]string{FinalizerInUse})
		certificate.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		cfg, err := buildConfiguration(t.Context(), f.objects(certificate), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Empty(t, cfg.certificates)
		assert.Empty(t, excludedMessages(cfg), "it is going away: it is left out without a reason")
	})
}

func TestReferencesByObjectIn(t *testing.T) {
	// Paths no fixture of the configuration tests goes through.
	t.Run("every encryption key of an encryptFields policy", func(t *testing.T) {
		value := map[string]any{"encryptFields": map[string]any{"config": map[string]any{"encryptFields": []any{
			map[string]any{"encryptionKey": map[string]any{"static": map[string]any{"key": map[string]any{"namespacedRef": map[string]any{"name": "key"}}}}},
			map[string]any{"encryptionKey": map[string]any{"static": map[string]any{"key": map[string]any{"id": "x"}}}},
		}}}}
		require.EqualError(t, referencesByObjectIn(value, "spec.apiSpec"),
			"spec.apiSpec.encryptFields.config.encryptFields.1.encryptionKey.static.key references its static key by id: with an OnPremEventGateway, only namespacedRef is allowed")
	})
	t.Run("a key that isn't of a static encryption key isn't a reference", func(t *testing.T) {
		value := map[string]any{"certificates": []any{map[string]any{"key": map[string]any{"name": "tls", "type": "secretRef"}}}}
		require.NoError(t, referencesByObjectIn(value, "spec.apiSpec"))
	})
}

func TestBuildConfigurationReferencesOnlyByObject(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	orders := f.registry("orders", "gateway", 2)
	uid := string(orders.UID)
	const notByObject = " references its schema registry by %s: with an OnPremEventGateway, only namespacedRef is allowed"
	transcodeSource := func(ref string) string {
		return `{type: reference, reference: {subject: orders-value, version: latest, schemaRefDestination: {type: none, none: {}}, schemaRegistry: ` + ref + `}}`
	}

	testCases := []struct {
		name     string
		policies []client.Object
		excluded map[string]string
	}{
		{
			name:     "a produce schema validation by name, even the UID of the registry",
			policies: []client.Object{f.validateWith("p", `{type: name, name: {name: "`+uid+`"}}`, 3)},
			excluded: map[string]string{"EventGatewayVirtualClusterProducePolicy p": "spec.apiSpec.schemaValidation.config.confluentSchemaRegistry.schemaRegistry" + fmt.Sprintf(notByObject, "name")},
		},
		{
			name:     "a produce schema validation by id",
			policies: []client.Object{f.validateWith("p", `{type: id, id: {id: 6f1d3c9a-2b7e-4c5d-8a1f-9e3b7c2d4f60}}`, 3)},
			excluded: map[string]string{"EventGatewayVirtualClusterProducePolicy p": "spec.apiSpec.schemaValidation.config.confluentSchemaRegistry.schemaRegistry" + fmt.Sprintf(notByObject, "id")},
		},
		{
			name: "a produce transcode by id, left out, not its parent",
			policies: []client.Object{
				f.validateWith("parent", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3),
				f.producePolicy("p", `
    type: transcode
    parentPolicyRef: {name: parent}
    transcode:
      name: p
      config:
        outputFormat: avro
        avro: {failureMode: reject, schemaSource: `+transcodeSource(`{type: id, id: {id: 6f1d3c9a-2b7e-4c5d-8a1f-9e3b7c2d4f60}}`)+`}
`, 4),
			},
			excluded: map[string]string{"EventGatewayVirtualClusterProducePolicy p": "spec.apiSpec.transcode.config.avro.schemaSource.reference.schemaRegistry" + fmt.Sprintf(notByObject, "id")},
		},
		{
			name: "a consume transcode by name, left out, not its parent",
			policies: []client.Object{
				f.consumePolicy("parent", `
    type: schemaValidation
    schemaValidation:
      name: parent
      config: {type: json, json: {valueValidationAction: mark}}
`, 3),
				f.consumePolicy("p", `
    type: transcode
    parentPolicyRef: {name: parent}
    transcode:
      name: p
      config:
        outputFormat: json
        json: {failureMode: error, schemaSource: `+transcodeSource(`{type: name, name: {name: "`+uid+`"}}`)+`}
`, 4),
			},
			excluded: map[string]string{"EventGatewayVirtualClusterConsumePolicy p": "spec.apiSpec.transcode.config.json.schemaSource.reference.schemaRegistry" + fmt.Sprintf(notByObject, "name")},
		},
		{
			name:     "the type alone decides: a reference by name with a stray namespacedRef",
			policies: []client.Object{f.validateWith("p", `{type: name, name: {name: "`+uid+`"}, namespacedRef: {name: orders}}`, 3)},
			excluded: map[string]string{"EventGatewayVirtualClusterProducePolicy p": "spec.apiSpec.schemaValidation.config.confluentSchemaRegistry.schemaRegistry" + fmt.Sprintf(notByObject, "name")},
		},
		{
			name: "a consume schema validation naming its registry directly, without type namespacedRef",
			policies: []client.Object{f.consumePolicy("p", `
    type: schemaValidation
    schemaValidation:
      name: p
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {name: orders}, valueValidationAction: mark}}
`, 3)},
			excluded: map[string]string{"EventGatewayVirtualClusterConsumePolicy p": "spec.apiSpec.schemaValidation.config.confluentSchemaRegistry.schemaRegistry" + fmt.Sprintf(notByObject, "a name set directly")},
		},
		{
			name: "a consume schema validation by namespacedRef",
			policies: []client.Object{f.consumePolicy("p", `
    type: schemaValidation
    schemaValidation:
      name: p
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}, valueValidationAction: mark}}
`, 3)},
			excluded: map[string]string{},
		},
		{
			name:     "an encrypt policy by the id of its static key",
			policies: []client.Object{f.encrypt("p", `{id: "`+string(f.staticKey("key", 3).UID)+`"}`, 4)},
			excluded: map[string]string{"EventGatewayVirtualClusterProducePolicy p": "spec.apiSpec.encrypt.config.encryptionKey.static.key references its static key by id: with an OnPremEventGateway, only namespacedRef is allowed"},
		},
		{
			name:     "an encrypt policy by the name of its static key",
			policies: []client.Object{f.encrypt("p", `{name: key}`, 4)},
			excluded: map[string]string{"EventGatewayVirtualClusterProducePolicy p": "spec.apiSpec.encrypt.config.encryptionKey.static.key references its static key by name: with an OnPremEventGateway, only namespacedRef is allowed"},
		},
		{
			name:     "an encrypt policy by object",
			policies: []client.Object{f.staticKey("key", 3), f.keySecret(), f.encrypt("p", `{namespacedRef: {name: key}}`, 4)},
			excluded: map[string]string{},
		},
		{
			name:     "a listener policy with a TLS trust bundle by id, among those by object",
			policies: []client.Object{f.trustBundle("bundle", 3), f.listener(), f.tlsSecret(), f.tlsServer("p", `[{namespacedRef: {name: bundle}}, {id: "6f1d3c9a-2b7e-4c5d-8a1f-9e3b7c2d4f60"}]`, 4)},
			excluded: map[string]string{"EventGatewayListenerPolicy p": "spec.apiSpec.tlsServer.config.clientAuthentication.tlsTrustBundles.1 references a TLS trust bundle by id: with an OnPremEventGateway, only namespacedRef is allowed"},
		},
		{
			name:     "a listener policy with a TLS trust bundle by name",
			policies: []client.Object{f.trustBundle("bundle", 3), f.listener(), f.tlsSecret(), f.tlsServer("p", `[{name: bundle}]`, 4)},
			excluded: map[string]string{"EventGatewayListenerPolicy p": "spec.apiSpec.tlsServer.config.clientAuthentication.tlsTrustBundles.0 references a TLS trust bundle by name: with an OnPremEventGateway, only namespacedRef is allowed"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := buildConfiguration(t.Context(), f.objects(append(tc.policies, orders)...), f.gateway, "", nil)
			require.NoError(t, err)
			assert.Equal(t, tc.excluded, excludedMessages(cfg))
			if len(tc.excluded) > 0 {
				for _, policies := range cfg.entitiesInUse {
					for _, policy := range policies {
						assert.NotEqual(t, "p", policy.key.Name, "a policy left out doesn't hold an entity")
					}
				}
			}
		})
	}
}

func TestBuildConfigurationHeldSchemaRegistry(t *testing.T) {
	f := newSchemaRegistryFixture(t)

	t.Run("a registry being deleted that FinalizerInUse holds stays in the configuration", func(t *testing.T) {
		orders := f.registry("orders", "gateway", 2)
		orders.SetFinalizers([]string{FinalizerInUse})
		orders.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		policy := f.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)

		cfg, err := buildConfiguration(t.Context(), f.objects(orders, policy), f.gateway, "", nil)
		require.NoError(t, err)

		require.Len(t, cfg.entities.SchemaRegistries, 1)
		assert.Empty(t, excludedMessages(cfg))
		produce, _ := policiesOf(t, cfg)
		require.Len(t, produce, 1)
		assert.Equal(t, map[string]any{"id": string(orders.UID)}, at(t, produce[0], "config", "schema_registry"))
		assert.NotEmpty(t, policiesUsing(cfg, orders))

		var registryStatus entityStatus
		for _, e := range cfg.included {
			if e.obj.GetUID() == orders.UID {
				registryStatus = e
			}
		}
		require.NotNil(t, registryStatus.obj)
		assert.Equal(t, "; being deleted, it is kept while these policies reference it: EventGatewayVirtualClusterProducePolicy validate", heldMessage(cfg, registryStatus))
	})

	t.Run("a registry being deleted without FinalizerInUse is left out with its policies", func(t *testing.T) {
		orders := f.registry("orders", "gateway", 2)
		orders.SetFinalizers([]string{"example.com/hold"})
		orders.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		policy := f.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)

		cfg, err := buildConfiguration(t.Context(), f.objects(orders, policy), f.gateway, "", nil)
		require.NoError(t, err)

		assert.Empty(t, cfg.entities.SchemaRegistries)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterProducePolicy validate": "it references the EventGatewaySchemaRegistry " + ns +
				"/orders, which isn't in the configuration of the gateway",
		}, excludedMessages(cfg))
		assert.Empty(t, cfg.entitiesInUse)
	})

	t.Run("the held message tells apart the policies of each kind, whatever their name", func(t *testing.T) {
		orders := f.registry("orders", "gateway", 2)
		orders.SetFinalizers([]string{FinalizerInUse})
		orders.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		produce := f.validateWith("same", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)
		consume := f.consumePolicy("same", `
    type: schemaValidation
    schemaValidation:
      name: same-consume
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}, valueValidationAction: mark}}
`, 4)
		cfg, err := buildConfiguration(t.Context(), f.objects(orders, produce, consume), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Equal(t, "; being deleted, it is kept while these policies reference it: EventGatewayVirtualClusterConsumePolicy same, EventGatewayVirtualClusterProducePolicy same",
			heldMessage(cfg, keptStatus(t, cfg, orders)))
	})
}

func TestReleaseOrphan(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	deleted := func(obj client.Object) client.Object {
		obj.SetFinalizers([]string{FinalizerInUse, "example.com/keep"})
		obj.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		return obj
	}
	gateway := func(deleting bool) *eventgatewayv1alpha1.OnPremEventGateway {
		g := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
		if deleting {
			g.SetFinalizers([]string{"example.com/keep"})
			g.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		}
		return g
	}

	testCases := []struct {
		name         string
		entity       client.Object
		objects      []client.Object
		wantReleased bool
	}{
		{name: "a registry whose gateway doesn't exist", entity: deleted(f.registry("orders", "gateway", 1)), wantReleased: true},
		{name: "a static key whose gateway doesn't exist", entity: deleted(f.staticKey("key", 1)), wantReleased: true},
		{name: "a TLS trust bundle whose gateway doesn't exist", entity: deleted(f.trustBundle("bundle", 1)), wantReleased: true},
		{name: "a virtual cluster whose gateway doesn't exist", entity: deleted(f.namedVirtualCluster("orders-vc", "orders", 1)), objects: f.virtualCluster()[:1], wantReleased: true},
		{name: "a virtual cluster whose backend cluster doesn't exist", entity: deleted(f.namedVirtualCluster("orders-vc", "orders", 1)), objects: []client.Object{gateway(false)}, wantReleased: true},
		{name: "a virtual cluster whose gateway is there", entity: deleted(f.namedVirtualCluster("orders-vc", "orders", 1)), objects: append(f.virtualCluster()[:1:1], gateway(false))},
		{name: "a registry whose gateway is being deleted", entity: deleted(f.registry("orders", "gateway", 1)), objects: []client.Object{gateway(true)}, wantReleased: true},
		{name: "a registry whose gateway is there: its reconcile decides", entity: deleted(f.registry("orders", "gateway", 1)), objects: []client.Object{gateway(false)}},
		{name: "a registry moved to a gateway that doesn't exist, another being there", entity: deleted(f.registry("orders", "ghost", 1)), objects: []client.Object{gateway(false)}, wantReleased: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := newFakeClient(t, append(tc.objects, tc.entity)...)
			r := &Reconciler{Client: cl}
			fresh := reflect.New(reflect.TypeOf(tc.entity).Elem()).Interface().(client.Object)
			require.NoError(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(tc.entity), fresh))
			assert.Equal(t, tc.wantReleased, !controllerutil.ContainsFinalizer(read(t, cl, tc.entity), FinalizerInUse))
			assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, tc.entity), "example.com/keep"), "the other finalizers stay")
		})
	}

	t.Run("a live registry keeps the finalizer until it is deleted", func(t *testing.T) {
		live := f.registry("orders", "gateway", 1)
		live.SetFinalizers([]string{FinalizerInUse})
		cl := newFakeClient(t, live)
		r := &Reconciler{Client: cl}
		require.NoError(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(live), &configurationv1alpha1.EventGatewaySchemaRegistry{}))
		assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, live), FinalizerInUse))
	})

	t.Run("a kind it can't hold is left alone", func(t *testing.T) {
		certificate := fromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, f.at(1), `
metadata: {name: certificate}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    certificate: {type: inline, value: not-read}
`)
		cl := newFakeClient(t, deleted(certificate))
		r := &Reconciler{Client: cl}
		require.NoError(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(certificate), &configurationv1alpha1.EventGatewayDataPlaneCertificate{}))
		assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, certificate), FinalizerInUse))
	})

	t.Run("a conflict is returned, for the entity to be tried again", func(t *testing.T) {
		orders := deleted(f.registry("orders", "gateway", 1))
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
				return apierrors.NewConflict(schema.GroupResource{Resource: "eventgatewayschemaregistries"}, "orders", errors.New("changed"))
			},
		}, orders)
		r := &Reconciler{Client: cl}
		err := r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(orders), &configurationv1alpha1.EventGatewaySchemaRegistry{})
		assert.True(t, apierrors.IsConflict(err), "got %v", err)
	})
}

func TestHoldEntities(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	used := f.registry("used", "gateway", 1)
	unused := f.registry("unused", "gateway", 2)
	unused.SetFinalizers([]string{FinalizerInUse})
	deleting := f.registry("deleting", "gateway", 3)
	deleting.SetFinalizers([]string{FinalizerInUse})
	deleting.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
	ofAnotherGateway := f.registry("of-another-gateway", "other-gateway", 4)
	ofAnotherGateway.SetFinalizers([]string{FinalizerInUse})
	usedKey := f.staticKey("used-key", 5)
	unusedKey := f.staticKey("unused-key", 6)
	unusedKey.SetFinalizers([]string{FinalizerInUse})
	backendCluster := f.virtualCluster()[0]
	usedVC := f.namedVirtualCluster("used-vc", "used", 9)
	unusedVC := f.namedVirtualCluster("unused-vc", "unused", 10)
	unusedVC.SetFinalizers([]string{FinalizerInUse})
	usedBundle := f.trustBundle("used-bundle", 7)
	unusedBundle := f.trustBundle("unused-bundle", 8)
	unusedBundle.SetFinalizers([]string{FinalizerInUse})

	cl := newFakeClient(t, used, unused, deleting, ofAnotherGateway, usedKey, unusedKey, usedBundle, unusedBundle, backendCluster, usedVC, unusedVC)
	r := &Reconciler{Client: cl}
	policy := reportedEntity{typ: reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](), key: client.ObjectKey{Namespace: ns, Name: "policy"}}

	res, _, err := r.holdEntities(t.Context(), f.gateway, map[reportedEntity][]reportedEntity{
		{typ: schemaRegistryType, key: client.ObjectKeyFromObject(used)}:       {policy},
		{typ: schemaRegistryType, key: client.ObjectKeyFromObject(deleting)}:   {policy},
		{typ: staticKeyType, key: client.ObjectKeyFromObject(usedKey)}:         {policy},
		{typ: tlsTrustBundleType, key: client.ObjectKeyFromObject(usedBundle)}: {policy},
		{typ: virtualClusterType, key: client.ObjectKeyFromObject(usedVC)}:     {policy},
		// A registry named as an entity of another kind in use isn't in use.
		{typ: staticKeyType, key: client.ObjectKeyFromObject(unused)}: {policy},
	})
	require.NoError(t, err)
	assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, usedKey), FinalizerInUse), "a static key in use is held")
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, unusedKey), FinalizerInUse), "a static key no policy uses is released")
	assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, usedBundle), FinalizerInUse), "a TLS trust bundle in use is held")
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, unusedBundle), FinalizerInUse), "a TLS trust bundle no policy uses is released")
	assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, usedVC), FinalizerInUse), "a virtual cluster in use is held, reached through its backend cluster")
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, unusedVC), FinalizerInUse), "a virtual cluster no policy uses is released")
	assert.True(t, res.IsZero())
	assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, used), FinalizerInUse), "a registry in use is held")
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, unused), FinalizerInUse), "a registry no policy uses is released")
	assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, deleting), FinalizerInUse), "a registry being deleted stays held while in use")
	assert.True(t, controllerutil.ContainsFinalizer(read(t, cl, ofAnotherGateway), FinalizerInUse), "the registries of other gateways are left alone")

	res, _, err = r.holdEntities(t.Context(), f.gateway, nil)
	require.NoError(t, err)
	assert.True(t, res.IsZero())
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, used), FinalizerInUse), "every entity of the gateway is released")
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, usedKey), FinalizerInUse), "every entity of the gateway is released")
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, usedBundle), FinalizerInUse), "every entity of the gateway is released")
	assert.False(t, controllerutil.ContainsFinalizer(read(t, cl, usedVC), FinalizerInUse), "every entity of the gateway is released")
	err = cl.Get(t.Context(), client.ObjectKeyFromObject(deleting), &configurationv1alpha1.EventGatewaySchemaRegistry{})
	assert.True(t, apierrors.IsNotFound(err), "a registry being deleted goes away once released, got %v", err)
}

// TestInUseWiring checks the reconcile of a gateway holds and releases the
// entities in use, not only the helpers doing it.
func TestInUseWiring(t *testing.T) {
	inConfiguration := func(s schemaRegistryFixture, registry *configurationv1alpha1.EventGatewaySchemaRegistry) []client.Object {
		return append(s.virtualCluster(), registry, s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: `+registry.Name+`}}`, 3))
	}
	withObjects := func(objs ...client.Object) func(*pushFixture) []client.Object {
		return func(f *pushFixture) []client.Object {
			return append(append(f.gwAdmin.Objects(), f.gateway), objs...)
		}
	}

	t.Run("a reconcile holds what the configuration uses and releases the rest", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		used := s.registry("used", "gateway", 1)
		unused := s.registry("unused", "gateway", 2)
		unused.SetFinalizers([]string{FinalizerInUse})
		f := newPushFixture(t, interceptor.Funcs{}, withObjects(append(inConfiguration(s, used), unused)...))

		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		assert.True(t, controllerutil.ContainsFinalizer(read(t, f.cl, used), FinalizerInUse))
		assert.False(t, controllerutil.ContainsFinalizer(read(t, f.cl, unused), FinalizerInUse))
	})

	t.Run("the cleanup of a gateway releases its entities", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		held := s.registry("held", "gateway", 1)
		held.SetFinalizers([]string{FinalizerInUse})
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			f.gateway.SetDeletionTimestamp(new(metav1.Now()))
			return withObjects(inConfiguration(s, held)...)(f)
		})

		_, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.False(t, controllerutil.ContainsFinalizer(read(t, f.cl, held), FinalizerInUse))
	})

	t.Run("an entity that can't be released doesn't hold the deletion of the gateway", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		held := s.registry("held", "gateway", 1)
		held.SetFinalizers([]string{FinalizerInUse})
		denied := interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*configurationv1alpha1.EventGatewaySchemaRegistry); ok {
					return apierrors.NewForbidden(schema.GroupResource{Resource: "eventgatewayschemaregistries"}, obj.GetName(), errors.New("denied"))
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}
		f := newPushFixture(t, denied, func(f *pushFixture) []client.Object {
			f.gateway.SetDeletionTimestamp(new(metav1.Now()))
			return withObjects(inConfiguration(s, held)...)(f)
		})

		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		err = f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.gateway), &eventgatewayv1alpha1.OnPremEventGateway{})
		assert.True(t, apierrors.IsNotFound(err), "the gateway goes away: %v", err)
		assert.True(t, controllerutil.ContainsFinalizer(read(t, f.cl, held), FinalizerInUse), "left to releaseOrphan")
	})
}

func TestNewUserOfDeletedEntity(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	const (
		before = fixtureDeletion - 1
		after  = fixtureDeletion + 1
	)
	beingDeleted := func(obj client.Object) client.Object {
		obj.SetFinalizers([]string{FinalizerInUse})
		obj.SetDeletionTimestamp(new(metav1.NewTime(f.at(fixtureDeletion))))
		return obj
	}
	kinds := []struct {
		name string
		// entity is the entity being deleted, and objects what it takes for its
		// policy to be in the configuration.
		entity  func() client.Object
		objects func() []client.Object
		policy  func(minutes int) client.Object
		kind    string
		message string
	}{
		{
			name:    "schema registry",
			entity:  func() client.Object { return f.registry("entity", "gateway", 1) },
			objects: func() []client.Object { return nil },
			policy: func(minutes int) client.Object {
				return f.validateWith("p", `{type: namespacedRef, namespacedRef: {name: entity}}`, minutes)
			},
			kind:    "EventGatewayVirtualClusterProducePolicy p",
			message: "it references the EventGatewaySchemaRegistry " + ns + "/entity, which is being deleted",
		},
		{
			name:    "static key",
			entity:  func() client.Object { return f.staticKey("entity", 1) },
			objects: func() []client.Object { return []client.Object{f.keySecret()} },
			policy: func(minutes int) client.Object {
				return f.encrypt("p", `{namespacedRef: {name: entity}}`, minutes)
			},
			kind:    "EventGatewayVirtualClusterProducePolicy p",
			message: "it references the EventGatewayStaticKey " + ns + "/entity, which is being deleted",
		},
		{
			name:    "TLS trust bundle",
			entity:  func() client.Object { return f.trustBundle("entity", 1) },
			objects: func() []client.Object { return []client.Object{f.listener(), f.tlsSecret()} },
			policy: func(minutes int) client.Object {
				return f.tlsServer("p", `[{namespacedRef: {name: entity}}]`, minutes)
			},
			kind:    "EventGatewayListenerPolicy p",
			message: "it references the EventGatewayTLSTrustBundle " + ns + "/entity, which is being deleted",
		},
		{
			name:    "virtual cluster",
			entity:  func() client.Object { return f.namedVirtualCluster("entity", "entity", 1) },
			objects: func() []client.Object { return []client.Object{f.listener()} },
			policy: func(minutes int) client.Object {
				return f.forward("p", `{namespacedRef: {name: entity}}`, minutes)
			},
			kind:    "EventGatewayListenerPolicy p",
			message: "it references the EventGatewayVirtualCluster " + ns + "/entity, which is being deleted",
		},
	}
	for _, k := range kinds {
		build := func(t *testing.T, minutes int) (configuration, client.Client, entityObject) {
			t.Helper()
			entity := beingDeleted(k.entity())
			cl := f.objects(append(k.objects(), entity, k.policy(minutes))...)
			// No previous configuration: what follows also holds after a restart
			// of the operator, which forgets it.
			cfg, err := buildConfiguration(t.Context(), cl, f.gateway, "", nil)
			require.NoError(t, err)
			return cfg, cl, entity.(entityObject)
		}

		t.Run(k.name+": a policy created after the deletion is left out and holds nothing", func(t *testing.T) {
			cfg, _, entity := build(t, after)
			assert.Equal(t, map[string]string{k.kind: k.message}, excludedMessages(cfg))
			assert.Empty(t, policiesUsing(cfg, entity))
		})

		t.Run(k.name+": a policy created before the deletion keeps using it", func(t *testing.T) {
			cfg, _, entity := build(t, before)
			assert.Empty(t, excludedMessages(cfg))
			assert.Equal(t, []string{k.kind}, policiesUsing(cfg, entity))
		})

		t.Run(k.name+": a policy created as the deletion started counts as existing", func(t *testing.T) {
			cfg, _, entity := build(t, fixtureDeletion)
			assert.Empty(t, excludedMessages(cfg))
			assert.Equal(t, []string{k.kind}, policiesUsing(cfg, entity))
		})

		t.Run(k.name+": released when a new policy is the only one referencing it", func(t *testing.T) {
			cfg, cl, entity := build(t, after)
			r := &Reconciler{Client: cl}
			_, _, err := r.holdEntities(t.Context(), f.gateway, cfg.entitiesInUse)
			require.NoError(t, err)
			err = cl.Get(t.Context(), client.ObjectKeyFromObject(entity), entity)
			assert.True(t, apierrors.IsNotFound(err), "it goes away: %v", err)
		})
	}

	t.Run("an existing policy changed to reference it still can, as with Konnect", func(t *testing.T) {
		orders := beingDeleted(f.registry("orders", "gateway", 1)).(*configurationv1alpha1.EventGatewaySchemaRegistry)
		edited := f.validateWith("edited", `{type: namespacedRef, namespacedRef: {name: orders}}`, before)
		edited.Generation = 2
		cfg, err := buildConfiguration(t.Context(), f.objects(orders, edited), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Empty(t, excludedMessages(cfg))
		assert.Equal(t, []string{"EventGatewayVirtualClusterProducePolicy edited"}, policiesUsing(cfg, orders))
	})

	t.Run("a new and an existing policy: only the existing one holds it", func(t *testing.T) {
		orders := beingDeleted(f.registry("orders", "gateway", 1)).(*configurationv1alpha1.EventGatewaySchemaRegistry)
		existing := f.validateWith("existing", `{type: namespacedRef, namespacedRef: {name: orders}}`, before)
		late := f.validateWith("late", `{type: namespacedRef, namespacedRef: {name: orders}}`, after)
		cfg, err := buildConfiguration(t.Context(), f.objects(orders, existing, late), f.gateway, "", nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterProducePolicy late": "it references the EventGatewaySchemaRegistry " + ns + "/orders, which is being deleted",
		}, excludedMessages(cfg))
		assert.Equal(t, []string{"EventGatewayVirtualClusterProducePolicy existing"}, policiesUsing(cfg, orders))
	})

	t.Run("an entity that can't be read fails the build, the policy isn't left out", func(t *testing.T) {
		orders := f.registry("orders", "gateway", 1)
		policy := f.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 2)
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*configurationv1alpha1.EventGatewaySchemaRegistry); ok {
					return apierrors.NewServiceUnavailable("unavailable")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}, append(f.virtualCluster(), orders, policy)...)
		_, err := buildConfiguration(t.Context(), cl, f.gateway, "", nil)
		require.ErrorContains(t, err, "unavailable")
	})
}

// TestHoldFailureDoesNotHoldBackThePush checks an entity that can't be held
// leaves the configuration pushed and the status telling so, and is tried again.
func TestHoldFailureDoesNotHoldBackThePush(t *testing.T) {
	objects := func(s schemaRegistryFixture) func(*pushFixture) []client.Object {
		return func(f *pushFixture) []client.Object {
			return append(append(f.gwAdmin.Objects(), f.gateway),
				append(s.virtualCluster(), s.registry("orders", "gateway", 1),
					s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3))...)
		}
	}
	patchingRegistry := func(fail func(name string) error) interceptor.Funcs {
		return interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*configurationv1alpha1.EventGatewaySchemaRegistry); ok {
					return fail(obj.GetName())
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}
	}
	pushed := func(t *testing.T, f *pushFixture) {
		t.Helper()
		assert.NotEmpty(t, f.pod().recorded(), "the configuration is pushed")
		gateway := read(t, f.cl, f.gateway)
		cond := apimeta.FindStatusCondition(gateway.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType))
		require.NotNil(t, cond, "the status tells of the push")
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	}

	t.Run("an error is returned once pushed, for the entity to be tried again", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		denied := patchingRegistry(func(name string) error {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "eventgatewayschemaregistries"}, name, errors.New("denied"))
		})
		f := newPushFixture(t, denied, objects(s))
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.ErrorContains(t, err, "denied")
		pushed(t, f)
	})

	t.Run("a conflict is requeued once pushed", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		conflict := patchingRegistry(func(name string) error {
			return apierrors.NewConflict(schema.GroupResource{Resource: "eventgatewayschemaregistries"}, name, errors.New("changed"))
		})
		f := newPushFixture(t, conflict, objects(s))
		res, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		assert.False(t, res.IsZero(), "it is tried again")
		pushed(t, f)
	})
}

func TestDeletionBlockedStatus(t *testing.T) {
	programmed := func(t *testing.T, cl client.Client, obj entityObject) *metav1.Condition {
		t.Helper()
		fresh, ok := obj.DeepCopyObject().(entityObject)
		require.True(t, ok)
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), fresh))
		cond := apimeta.FindStatusCondition(fresh.GetConditions(), string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
		require.NotNil(t, cond)
		return cond
	}
	objects := func(s schemaRegistryFixture, held *configurationv1alpha1.EventGatewaySchemaRegistry, policy client.Object) func(*pushFixture) []client.Object {
		return func(f *pushFixture) []client.Object {
			return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), held, policy)...)
		}
	}
	const heldBy = "being deleted, it is kept while these policies reference it: EventGatewayVirtualClusterProducePolicy validate"

	t.Run("a held entity stays programmed, with its policy and its gateway, and reports it", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		held := s.registry("orders", "gateway", 1)
		held.SetFinalizers([]string{FinalizerInUse})
		held.SetDeletionTimestamp(new(metav1.NewTime(s.at(fixtureDeletion))))
		policy := s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)
		f := newPushFixture(t, interceptor.Funcs{}, objects(s, held, policy))

		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)

		cond := programmed(t, f.cl, held)
		assert.Equal(t, metav1.ConditionTrue, cond.Status, "it is in the configuration, pushed")
		assert.Equal(t, konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked, cond.Reason, "the same reason as with Konnect")
		assert.Contains(t, cond.Message, heldBy)
		assert.True(t, ownsEntityCondition(read(t, f.cl, held), string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)),
			"the controller owns the condition it set")

		assert.Equal(t, metav1.ConditionTrue, programmed(t, f.cl, policy).Status)
		gatewayCond := apimeta.FindStatusCondition(read(t, f.cl, f.gateway).Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
		require.NotNil(t, gatewayCond)
		assert.Equal(t, metav1.ConditionTrue, gatewayCond.Status)
	})

	t.Run("with a gateway not programmed, it isn't programmed either, and reports it too", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		held := s.registry("orders", "gateway", 1)
		held.SetFinalizers([]string{FinalizerInUse})
		held.SetDeletionTimestamp(new(metav1.NewTime(s.at(fixtureDeletion))))
		policy := s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)
		f := newPushFixture(t, interceptor.Funcs{}, objects(s, held, policy))
		cfg, err := buildConfiguration(t.Context(), f.cl, f.gateway, "", nil)
		require.NoError(t, err)

		require.NoError(t, f.r.reportEntities(t.Context(), f.gateway, cfg, pushResult{}))

		cond := programmed(t, f.cl, held)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEntityDeletionBlockedReason), cond.Reason)
		assert.Contains(t, cond.Message, "which is not programmed")
		assert.Contains(t, cond.Message, heldBy)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason), programmed(t, f.cl, policy).Reason)
	})

	t.Run("an entity being deleted that no policy uses isn't reported blocked", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		held := s.registry("orders", "gateway", 1)
		held.SetFinalizers([]string{FinalizerInUse, "example.com/keep"})
		held.SetDeletionTimestamp(new(metav1.NewTime(s.at(fixtureDeletion))))
		policy := s.validateWith("validate", `{type: name, name: {name: other}}`, 3)
		f := newPushFixture(t, interceptor.Funcs{}, objects(s, held, policy))
		cfg, err := buildConfiguration(t.Context(), f.cl, f.gateway, "", nil)
		require.NoError(t, err)

		require.NoError(t, f.r.reportEntities(t.Context(), f.gateway, cfg, pushResult{}))
		assert.NotEqual(t, string(eventgatewayv1alpha1.OnPremEntityDeletionBlockedReason), programmed(t, f.cl, held).Reason)
	})

	t.Run("the same reason set by Konnect isn't taken for its own", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		byKonnect := s.registry("orders", "gateway", 1)
		byKonnect.Status.Conditions = []metav1.Condition{{
			Type: string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType), Status: metav1.ConditionFalse,
			Reason: konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked, Message: "deletion blocked: policies use it",
		}}
		assert.False(t, ownsEntityCondition(byKonnect, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)))
	})
}

// TestConsumeJSONWithSchemaRegistryIsNotLeftOut checks a policy gw-admin refuses,
// which the CRDs no longer admit but which may be stored from before, is sent as
// it is: gw-admin refusing it stops the push, which the gateway reports, instead
// of the policy being dropped without enforcing what it should.
func TestConsumeJSONWithSchemaRegistryIsNotLeftOut(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	orders := f.registry("orders", "gateway", 1)
	consume := f.consumePolicy("consume-json", `
    type: schemaValidation
    schemaValidation:
      name: consume-json
      config: {type: json, json: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}, valueValidationAction: mark}}
`, 2)

	cfg, err := buildConfiguration(t.Context(), f.objects(orders, consume), f.gateway, "", nil)
	require.NoError(t, err)
	assert.Empty(t, excludedMessages(cfg))
	_, got := policiesOf(t, cfg)
	require.Len(t, got, 1)
	assert.Equal(t, map[string]any{"id": string(orders.UID)}, at(t, got[0], "config", "schema_registry"))
}

// A parent policy is held while child policies name it: without it, they
// wouldn't be applied.
func TestParentPolicyHeldByItsChildren(t *testing.T) {
	s := newSchemaRegistryFixture(t)
	parent := s.validateWith("parent", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)
	child := s.producePolicy("child", `
    parentPolicyRef: {name: parent}
    type: modifyHeaders
    modifyHeaders:
      name: child
      config: {actions: [{op: remove, remove: {key: x-header}}]}
`, 4)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), s.registry("orders", "gateway", 1), parent, child)...)
	})
	reconcile := func(t *testing.T) {
		t.Helper()
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
	}
	pushedPolicies := func() []string {
		data, err := json.Marshal(f.pod().gateways[f.gatewayID].VirtualClusters[0].ProducePolicies)
		require.NoError(t, err)
		var policies []struct {
			Name string `json:"name"`
		}
		require.NoError(t, json.Unmarshal(data, &policies))
		names := []string{}
		for _, p := range policies {
			names = append(names, p.Name)
		}
		return names
	}

	reconcile(t)
	assert.Contains(t, read(t, f.cl, parent).GetFinalizers(), FinalizerInUse, "the parent is held")
	assert.NotContains(t, read(t, f.cl, child).GetFinalizers(), FinalizerInUse, "nothing names the child as its parent")

	require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, parent)))
	reconcile(t)
	held := read(t, f.cl, parent)
	require.False(t, held.GetDeletionTimestamp().IsZero())
	assert.ElementsMatch(t, []string{"parent", "child"}, pushedPolicies(), "the parent stays in the configuration with its child")
	cond := apimeta.FindStatusCondition(held.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, string(eventgatewayv1alpha1.OnPremEntityDeletionBlockedReason), cond.Reason)
	assert.Contains(t, cond.Message, "being deleted, it is kept while these policies reference it: EventGatewayVirtualClusterProducePolicy child")

	t.Run("a child created since can't be sent", func(t *testing.T) {
		late := s.producePolicy("late", `
    parentPolicyRef: {name: parent}
    type: modifyHeaders
    modifyHeaders:
      name: late
      config: {actions: [{op: remove, remove: {key: x-late}}]}
`, fixtureDeletion+5)
		// The fake client deletes the parent at the current time.
		late.SetCreationTimestamp(metav1.NewTime(held.GetDeletionTimestamp().Add(time.Minute)))
		late.SetResourceVersion("")
		require.NoError(t, f.cl.Create(t.Context(), late))
		cfg, err := buildConfiguration(t.Context(), f.cl, read(t, f.cl, f.gateway), "", nil)
		require.NoError(t, err)
		assert.Contains(t, excludedMessages(cfg)["EventGatewayVirtualClusterProducePolicy late"], "which is being deleted")
		require.NoError(t, f.cl.Delete(t.Context(), late))
	})

	require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, child)))
	reconcile(t)
	err := f.cl.Get(t.Context(), client.ObjectKeyFromObject(parent), &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{})
	assert.True(t, apierrors.IsNotFound(err), "released once its child is gone, the parent goes: %v", err)
	// Its deletion triggers the gateway again, which pushes without it.
	reconcile(t)
	assert.Empty(t, pushedPolicies())
}

// A policy sent as it was last, its Secret being gone, holds what it was sent
// with, which its spec may no longer name.
func TestKeptPolicyHoldsWhatItWasSentWith(t *testing.T) {
	f := newSchemaRegistryFixture(t)
	bundleA, bundleB := f.trustBundle("bundle-a", 1), f.trustBundle("bundle-b", 2)
	policy := f.tlsServer("mtls", `[{namespacedRef: {name: bundle-a}}]`, 3)

	cfg, err := buildConfiguration(t.Context(), f.objects(f.listener(), f.tlsSecret(), bundleA, bundleB, policy), f.gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, excludedMessages(cfg))
	assert.Equal(t, []string{"EventGatewayListenerPolicy mtls"}, policiesUsing(cfg, bundleA))

	// It now names bundle B, and its Secret is gone: it is sent as it was, with
	// bundle A.
	moved := f.tlsServer("mtls", `[{namespacedRef: {name: bundle-b}}]`, 3)
	moved.SetUID(policy.GetUID())
	kept, err := buildConfiguration(t.Context(), f.objects(f.listener(), bundleA, bundleB, moved), f.gateway, "", cfg.built)
	require.NoError(t, err)
	require.Empty(t, excludedMessages(kept))
	require.True(t, slices.ContainsFunc(kept.included, func(e entityStatus) bool { return e.kept && e.obj.GetName() == "mtls" }), "the policy is kept")
	assert.Equal(t, []string{"EventGatewayListenerPolicy mtls"}, policiesUsing(kept, bundleA), "the bundle it is sent with stays held")
	assert.Equal(t, []string{"EventGatewayListenerPolicy mtls"}, policiesUsing(kept, bundleB), "and so does the one its spec names")
}

// Holding an entity patches it: its status is then patched from the version
// patched, in the same reconcile, without a conflict to try again.
func TestHoldingDoesNotConflictWithTheStatus(t *testing.T) {
	s := newSchemaRegistryFixture(t)
	registry := s.registry("orders", "gateway", 1)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		return append(append(f.gwAdmin.Objects(), f.gateway),
			append(s.virtualCluster(), registry, s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3))...)
	})
	res, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.True(t, res.IsZero(), "nothing to try again: %+v", res)
	held := read(t, f.cl, registry)
	assert.Contains(t, held.GetFinalizers(), FinalizerInUse)
	cond := apimeta.FindStatusCondition(held.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
	require.NotNil(t, cond, "its status is set in the same reconcile")
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
}

// A backend cluster is held while a listener policy forwards to one of its
// virtual clusters, which can't be sent without it, as Konnect refuses to
// delete it then.
func TestBackendClusterHeldByForwardedVirtualClusters(t *testing.T) {
	s := newSchemaRegistryFixture(t)
	forward := fromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, s.at(5), `
metadata: {name: forward}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: {namespacedRef: {name: vc}}}}
`)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), s.listener(), forward)...)
	})
	reconcile := func(t *testing.T) {
		t.Helper()
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
	}
	bc := &configurationv1alpha1.EventGatewayBackendCluster{}
	bcKey := client.ObjectKey{Namespace: ns, Name: "bc"}

	reconcile(t)
	require.NoError(t, f.cl.Get(t.Context(), bcKey, bc))
	assert.Contains(t, bc.GetFinalizers(), FinalizerInUse, "the backend cluster is held")

	require.NoError(t, f.cl.Delete(t.Context(), bc))
	reconcile(t)
	require.NoError(t, f.cl.Get(t.Context(), bcKey, bc))
	require.False(t, bc.GetDeletionTimestamp().IsZero())
	assert.Len(t, f.pod().gateways[f.gatewayID].VirtualClusters, 1, "its virtual cluster stays in the configuration")
	cond := apimeta.FindStatusCondition(bc.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
	require.NotNil(t, cond)
	assert.Equal(t, string(eventgatewayv1alpha1.OnPremEntityDeletionBlockedReason), cond.Reason)
	assert.Contains(t, cond.Message, "EventGatewayListenerPolicy forward")
	assert.Empty(t, cfgExcluded(t, f), "nothing is left out")

	require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, forward)))
	reconcile(t)
	err := f.cl.Get(t.Context(), bcKey, &configurationv1alpha1.EventGatewayBackendCluster{})
	assert.True(t, apierrors.IsNotFound(err), "released once nothing forwards to it: %v", err)
}

// cfgExcluded returns the entities left out of the configuration of the
// gateway of the fixture, with why.
func cfgExcluded(t *testing.T, f *pushFixture) map[string]string {
	t.Helper()
	cfg, err := buildConfiguration(t.Context(), f.cl, read(t, f.cl, f.gateway), "", nil)
	require.NoError(t, err)
	return excludedMessages(cfg)
}
