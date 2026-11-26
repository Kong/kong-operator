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
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

// The unique values of the entities of a gateway (their name among their
// kind, the DNS label of a virtual cluster, the name of a policy among every
// policy) go to the entity sent with them, or else to the oldest one. One that
// can't have its values is left out, and stops the push only when it was sent.

// uniqueFixture builds the configuration of the objects of a gateway with a
// backend cluster bc and a virtual cluster vc, after what was sent.
type uniqueFixture struct {
	schemaRegistryFixture
}

func newUniqueFixture(t *testing.T) uniqueFixture {
	return uniqueFixture{newSchemaRegistryFixture(t)}
}

// build builds the configuration of the objects after sent, or with nothing
// known to have been sent when sent is nil.
func (f uniqueFixture) build(sent *gwadminapi.GatewayEntitySet, objs ...client.Object) configuration {
	f.t.Helper()
	cfg, err := buildConfigurationAfter(f.t.Context(), f.objects(objs...), f.gateway, "", nil, sent)
	require.NoError(f.t, err)
	return cfg
}

// sent returns the entities of the configuration of the objects, which must
// be pushable, as the last configuration that could be sent.
func (f uniqueFixture) sent(objs ...client.Object) *gwadminapi.GatewayEntitySet {
	f.t.Helper()
	cfg := f.build(nil, objs...)
	require.NoError(f.t, cfg.blocked(), "the configuration sent can be pushed")
	return &cfg.entities
}

func (f uniqueFixture) backendCluster(name, apiName string, minutes int) *configurationv1alpha1.EventGatewayBackendCluster {
	return fromYAML[configurationv1alpha1.EventGatewayBackendCluster](f.t, f.at(minutes), `
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

func (f uniqueFixture) virtualCluster(name, apiName, dnsLabel string, minutes int) *configurationv1alpha1.EventGatewayVirtualCluster {
	return fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: bc}}
  apiSpec: {name: `+apiName+`, aclMode: passthrough, authentication: [{type: anonymous}], dnsLabel: `+dnsLabel+`}
`)
}

func (f uniqueFixture) bundle(name, apiName string, minutes int) *configurationv1alpha1.EventGatewayTLSTrustBundle {
	return fromYAML[configurationv1alpha1.EventGatewayTLSTrustBundle](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: `+apiName+`
    config: {trustedCa: {type: inline, value: ca-pem}}
`)
}

// staticKey is a static key named x, reading the Secret of its name.
func (f uniqueFixture) staticKey(name string, minutes int) *configurationv1alpha1.EventGatewayStaticKey {
	return fromYAML[configurationv1alpha1.EventGatewayStaticKey](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: x
    value: {type: secretRef, secretRef: {name: `+name+`, key: key}}
`)
}

func (f uniqueFixture) keySecret(name string) *corev1.Secret {
	return &corev1.Secret{Name: name, Namespace: ns, Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")}}
}

// modifyHeaders is the apiSpec of a policy named name, of the parent policy
// parent when set.
func modifyHeaders(name, parent string) string {
	spec := `
    type: modifyHeaders
    modifyHeaders:
      name: ` + name + `
      config: {actions: [{op: remove, remove: {key: x-header}}]}
`
	if parent != "" {
		spec = `
    parentPolicyRef: {name: ` + parent + `}` + spec
	}
	return spec
}

func (f uniqueFixture) forward(name, listener, apiName string, minutes int) *configurationv1alpha1.EventGatewayListenerPolicy {
	return fromYAML[configurationv1alpha1.EventGatewayListenerPolicy](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: `+listener+`}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: `+apiName+`
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: {name: vc}}}
`)
}

func (f uniqueFixture) listenerNamed(name, apiName string, minutes int) *configurationv1alpha1.EventGatewayListener {
	return fromYAML[configurationv1alpha1.EventGatewayListener](f.t, f.at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec: {name: `+apiName+`, addresses: ["0.0.0.0"], ports: [19092]}
`)
}

func TestUniqueValuesOfEachKind(t *testing.T) {
	f := newUniqueFixture(t)
	for _, tc := range []struct {
		kind           string
		older, newer   client.Object
		others         []client.Object
		message        string
		olderIsInvalid bool
	}{
		{
			kind:    "EventGatewayBackendCluster",
			older:   f.backendCluster("a", "x", 10),
			newer:   f.backendCluster("b", "x", 20),
			message: `its spec.apiSpec.name "x" is already the one of EventGatewayBackendCluster default/a`,
		},
		{
			kind:    "EventGatewayTLSTrustBundle",
			older:   f.bundle("a", "x", 10),
			newer:   f.bundle("b", "x", 20),
			message: `its spec.apiSpec.name "x" is already the one of EventGatewayTLSTrustBundle default/a`,
		},
		{
			kind:    "EventGatewayStaticKey",
			older:   f.staticKey("a", 10),
			newer:   f.staticKey("b", 20),
			others:  []client.Object{f.keySecret("a"), f.keySecret("b")},
			message: `its spec.apiSpec.name "x" is already the one of EventGatewayStaticKey default/a`,
		},
		{
			kind:    "EventGatewayListener",
			older:   f.listenerNamed("a", "x", 10),
			newer:   f.listenerNamed("b", "x", 20),
			message: `its spec.apiSpec.name "x" is already the one of EventGatewayListener default/a`,
		},
		{
			kind:    "EventGatewayVirtualCluster",
			older:   f.virtualCluster("a", "x", "a", 10),
			newer:   f.virtualCluster("b", "x", "b", 20),
			message: `its spec.apiSpec.name "x" is already the one of EventGatewayVirtualCluster default/a`,
		},
		{
			kind:    "EventGatewayVirtualCluster",
			older:   f.virtualCluster("a", "a", "d", 10),
			newer:   f.virtualCluster("b", "b", "d", 20),
			message: `its spec.apiSpec.dnsLabel "d" is already the one of EventGatewayVirtualCluster default/a`,
		},
	} {
		t.Run(tc.kind+" "+tc.message, func(t *testing.T) {
			objs := slices.Concat([]client.Object{tc.older, tc.newer}, tc.others)

			t.Run("never sent, the newer is only left out", func(t *testing.T) {
				cfg := f.build(nil, objs...)
				assert.Equal(t, map[string]string{tc.kind + " b": tc.message}, excludedMessages(cfg))
				assert.Equal(t, map[string]bool{tc.kind + " b": false}, excludedBlocking(cfg))
			})

			t.Run("sent, the newer keeps it, and the older, never sent, is only left out", func(t *testing.T) {
				sent := f.sent(slices.Concat([]client.Object{tc.newer}, tc.others)...)
				cfg := f.build(sent, objs...)
				message := tc.message[:len(tc.message)-1] + "b"
				assert.Equal(t, map[string]string{tc.kind + " a": message}, excludedMessages(cfg))
				assert.Equal(t, map[string]bool{tc.kind + " a": false}, excludedBlocking(cfg))
			})

			t.Run("when what was sent isn't known, the newer stops the push", func(t *testing.T) {
				f := f
				f.gateway = f.gateway.DeepCopy()
				f.gateway.Status.ConfigRevision = 3
				cfg := f.build(nil, objs...)
				assert.Equal(t, map[string]bool{tc.kind + " b": true}, excludedBlocking(cfg))
				assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason, failureReason(cfg.blocked()))
			})
		})
	}
}

func TestUniquePolicyNames(t *testing.T) {
	f := newUniqueFixture(t)
	produce := f.producePolicy("produce", modifyHeaders("same", ""), 3)
	consume := f.consumePolicy("consume", modifyHeaders("same", ""), 4)
	forward := f.forward("forward", "listener", "same", 5)
	listener := f.listener()

	t.Run("never sent, the oldest keeps it whatever the kinds, the others are only left out", func(t *testing.T) {
		cfg := f.build(nil, produce, consume, listener, forward)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterConsumePolicy consume": `its policy name "same" is already the one of EventGatewayVirtualClusterProducePolicy default/produce`,
			"EventGatewayListenerPolicy forward":              `its policy name "same" is already the one of EventGatewayVirtualClusterProducePolicy default/produce`,
		}, excludedMessages(cfg))
		require.NoError(t, cfg.blocked())
	})

	t.Run("the policy sent keeps it, the older ones, never sent, are only left out", func(t *testing.T) {
		cfg := f.build(f.sent(consume, listener), produce, consume, listener, forward)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterProducePolicy produce": `its policy name "same" is already the one of EventGatewayVirtualClusterConsumePolicy default/consume`,
			"EventGatewayListenerPolicy forward":              `its policy name "same" is already the one of EventGatewayVirtualClusterConsumePolicy default/consume`,
		}, excludedMessages(cfg))
		require.NoError(t, cfg.blocked())
	})

	t.Run("a policy sent renamed into a name another one runs with stops the push", func(t *testing.T) {
		renamed := consume.DeepCopy()
		renamed.Spec.APISpec.ModifyHeadersPolicyCreate.Name = "other"
		cfg := f.build(f.sent(produce, renamed), produce, consume)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterConsumePolicy consume": `its policy name "same" is already the one of EventGatewayVirtualClusterProducePolicy default/produce`,
		}, excludedMessages(cfg))
		assert.Equal(t, map[string]bool{"EventGatewayVirtualClusterConsumePolicy consume": true}, excludedBlocking(cfg),
			"left out, it would be taken off the gateway")
		assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason, failureReason(cfg.blocked()))
	})

	t.Run("the older policy renamed into the name of a newer one sent stops the push", func(t *testing.T) {
		older := produce.DeepCopy()
		older.Spec.APISpec.ModifyHeadersPolicyCreate.Name = "other"
		cfg := f.build(f.sent(older, consume), produce, consume)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterProducePolicy produce": `its policy name "same" is already the one of EventGatewayVirtualClusterConsumePolicy default/consume`,
		}, excludedMessages(cfg), "the value stays with the policy running with it, though newer")
		assert.Equal(t, map[string]bool{"EventGatewayVirtualClusterProducePolicy produce": true}, excludedBlocking(cfg))
	})

	t.Run("of two policies sent with it, the oldest keeps it", func(t *testing.T) {
		// Not a configuration that can be sent, but what was sent decides
		// nothing more than the order.
		sent := f.sent(produce)
		sent.VirtualClusters[0].ConsumePolicies = f.sent(consume).VirtualClusters[0].ConsumePolicies
		cfg := f.build(sent, produce, consume)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterConsumePolicy consume": `its policy name "same" is already the one of EventGatewayVirtualClusterProducePolicy default/produce`,
		}, excludedMessages(cfg))
		assert.Equal(t, map[string]bool{"EventGatewayVirtualClusterConsumePolicy consume": true}, excludedBlocking(cfg))
	})

	t.Run("created at the same time, the kinds break the tie", func(t *testing.T) {
		// The listener policies are converted after those of the virtual
		// clusters: the kind, not that order, decides.
		produce := f.producePolicy("p", modifyHeaders("same", ""), 3)
		forward := f.forward("p", "listener", "same", 3)
		cfg := f.build(nil, produce, listener, forward)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterProducePolicy p": `its policy name "same" is already the one of EventGatewayListenerPolicy default/p`,
		}, excludedMessages(cfg))
	})

	t.Run("a policy whose listener is left out takes no name", func(t *testing.T) {
		duplicate := f.listenerNamed("duplicate", "listener", 2)
		ofDuplicate := f.forward("of-duplicate", "duplicate", "same", 1)
		cfg := f.build(nil, listener, duplicate, ofDuplicate, produce)
		messages := excludedMessages(cfg)
		assert.NotContains(t, messages, "EventGatewayVirtualClusterProducePolicy produce", "nothing in the configuration has its name")
		assert.Contains(t, messages, "EventGatewayListenerPolicy of-duplicate")
	})

	t.Run("a policy with the name of its own parent policy takes no name", func(t *testing.T) {
		// The child is the oldest, but it can't be in: its parent can't have
		// the name while it has it. Out anyway, it keeps the name from no one.
		child := f.consumePolicy("child", modifyHeaders("same", "parent"), 1)
		parent := f.consumePolicy("parent", `
    type: schemaValidation
    schemaValidation:
      name: same
      config: {type: json, json: {valueValidationAction: mark}}
`, 2)
		other := f.consumePolicy("other", modifyHeaders("same", ""), 3)
		cfg := f.build(nil, child, parent, other)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterConsumePolicy child": `its policy name "same" is the one of its parent policy EventGatewayVirtualClusterConsumePolicy default/parent`,
			"EventGatewayVirtualClusterConsumePolicy other": `its policy name "same" is already the one of EventGatewayVirtualClusterConsumePolicy default/parent`,
		}, excludedMessages(cfg), "the parent, older than other, keeps the name")
		require.NoError(t, cfg.blocked())
	})

	t.Run("a sibling of such a policy, with a name of its own, goes in with their parent", func(t *testing.T) {
		// Only the child holds the name its parent loses: the sibling, whose
		// parent loses the name to the child too, but holds a name nobody
		// else claims, goes in once the child is out and the parent has it.
		child := f.consumePolicy("child", modifyHeaders("same", "parent"), 1)
		parent := f.consumePolicy("parent", `
    type: schemaValidation
    schemaValidation:
      name: same
      config: {type: json, json: {valueValidationAction: mark}}
`, 2)
		other := f.consumePolicy("other", modifyHeaders("unique", "parent"), 3)
		cfg := f.build(nil, child, parent, other)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterConsumePolicy child": `its policy name "same" is the one of its parent policy EventGatewayVirtualClusterConsumePolicy default/parent`,
		}, excludedMessages(cfg), "the parent and the sibling are in")
		require.NoError(t, cfg.blocked())
	})

	t.Run("a policy whose parent policy is left out takes no name, and goes along with it", func(t *testing.T) {
		parent := f.consumePolicy("parent", `
    type: schemaValidation
    schemaValidation:
      name: same
      config: {type: json, json: {valueValidationAction: mark}}
`, 6)
		// Older than its parent and than other: it would keep its name, but
		// its parent is out.
		child := f.consumePolicy("child", modifyHeaders("child-name", "parent"), 4)
		other := f.consumePolicy("other", modifyHeaders("child-name", ""), 5)
		cfg := f.build(nil, produce, parent, child, other)
		assert.Equal(t, map[string]string{
			"EventGatewayVirtualClusterConsumePolicy parent": `its policy name "same" is already the one of EventGatewayVirtualClusterProducePolicy default/produce`,
			"EventGatewayVirtualClusterConsumePolicy child":  `its parent policy default/parent isn't in the configuration`,
		}, excludedMessages(cfg), "other keeps the name of the child")
		require.NoError(t, cfg.blocked())

		t.Run("one sent stops the push when its new parent policy, never sent, can't have its name", func(t *testing.T) {
			// The child ran without a parent policy, and now names one that
			// can't have its name.
			before := child.DeepCopy()
			before.Spec.APISpec.ParentPolicyRef = configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}
			cfg := f.build(f.sent(produce, before), produce, parent, child)
			assert.Equal(t, map[string]string{
				"EventGatewayVirtualClusterConsumePolicy parent": `its policy name "same" is already the one of EventGatewayVirtualClusterProducePolicy default/produce`,
				"EventGatewayVirtualClusterConsumePolicy child":  `its parent policy default/parent isn't in the configuration`,
			}, excludedMessages(cfg))
			assert.Equal(t, map[string]bool{
				"EventGatewayVirtualClusterConsumePolicy parent": false,
				"EventGatewayVirtualClusterConsumePolicy child":  true,
			}, excludedBlocking(cfg), "left out, the child would be taken off the gateway")
			assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason, failureReason(cfg.blocked()))
		})

		t.Run("one sent stops the push when such a parent policy is out through its own parent", func(t *testing.T) {
			grandchild := f.consumePolicy("grandchild", modifyHeaders("grandchild-name", "child"), 7)
			before := grandchild.DeepCopy()
			before.Spec.APISpec.ParentPolicyRef = configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}
			cfg := f.build(f.sent(produce, before), produce, parent, child, grandchild)
			assert.Equal(t, map[string]bool{
				"EventGatewayVirtualClusterConsumePolicy parent":     false,
				"EventGatewayVirtualClusterConsumePolicy child":      false,
				"EventGatewayVirtualClusterConsumePolicy grandchild": true,
			}, excludedBlocking(cfg), "only the grandchild was sent")
		})
	})
}

// An entity whose parent, never sent, is left out as it can't have a unique
// value goes along with it, and stops the push when it was sent: it would be
// taken off the gateway.
func TestUniqueValuesParentLeftOut(t *testing.T) {
	// The fixture has the backend cluster bc and its virtual cluster vc.
	f := newUniqueFixture(t)
	listener := f.listenerNamed("listener", "listener", 0)
	// movedOf is a virtual cluster of the backend cluster bc.
	movedOf := func(bc string) *configurationv1alpha1.EventGatewayVirtualCluster {
		return fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, f.at(3), `
metadata: {name: moved}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: `+bc+`}}
  apiSpec: {name: moved, aclMode: passthrough, authentication: [{type: anonymous}], dnsLabel: moved}
`)
	}
	// produceOf is a produce policy of the virtual cluster vc.
	produceOf := func(vc string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		return fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, f.at(3), `
metadata: {name: produce}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: `+vc+`}}
  apiSpec:
`+modifyHeaders("produce", ""))
	}

	for _, tc := range []struct {
		name string
		// before is the entity as sent, with others, and after the same
		// entity moved to parent, which, never sent, can't have a unique
		// value.
		before, after, parent client.Object
		others                []client.Object
		out, parentOut        string
	}{
		{
			name:   "a virtual cluster moved to a backend cluster that can't have its name",
			before: movedOf("bc"), after: movedOf("bc2"),
			parent: f.backendCluster("bc2", "bc", 2),
			out:    "EventGatewayVirtualCluster moved", parentOut: "EventGatewayBackendCluster bc2",
		},
		{
			name:   "a policy moved to a virtual cluster that can't have its DNS label",
			before: produceOf("vc"), after: produceOf("vc2"),
			parent: f.virtualCluster("vc2", "vc2", "vc", 2),
			out:    "EventGatewayVirtualClusterProducePolicy produce", parentOut: "EventGatewayVirtualCluster vc2",
		},
		{
			name:   "a listener policy moved to a listener that can't have its name",
			before: f.forward("forward", "listener", "forward", 3), after: f.forward("forward", "duplicate", "forward", 3),
			parent: f.listenerNamed("duplicate", "listener", 2), others: []client.Object{listener},
			out: "EventGatewayListenerPolicy forward", parentOut: "EventGatewayListener duplicate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.after.SetUID(tc.before.GetUID())
			objs := append(slices.Clone(tc.others), tc.parent, tc.after)
			t.Run("never sent, goes along with its parent", func(t *testing.T) {
				cfg := f.build(f.sent(), objs...)
				assert.Equal(t, map[string]bool{tc.parentOut: false, tc.out: false}, excludedBlocking(cfg))
				require.NoError(t, cfg.blocked())
			})
			t.Run("sent, stops the push", func(t *testing.T) {
				cfg := f.build(f.sent(append(slices.Clone(tc.others), tc.before)...), objs...)
				assert.Equal(t, map[string]bool{tc.parentOut: false, tc.out: true}, excludedBlocking(cfg),
					"left out, it would be taken off the gateway")
				assert.Contains(t, excludedMessages(cfg)[tc.out], "isn't in the configuration")
				assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason, failureReason(cfg.blocked()))
			})
		})
	}
}

// A virtual cluster that can't have its DNS label has none of its unique
// values: a newer one may take its name.
func TestUniqueValuesAllOrNothing(t *testing.T) {
	f := newUniqueFixture(t)
	second := f.virtualCluster("vc2", "name", "vc", 10)
	third := f.virtualCluster("vc3", "name", "vc3", 20)
	cfg := f.build(nil, second, third)
	assert.Equal(t, map[string]string{
		"EventGatewayVirtualCluster vc2": `its spec.apiSpec.dnsLabel "vc" is already the one of EventGatewayVirtualCluster default/vc`,
	}, excludedMessages(cfg))
	require.Len(t, cfg.entities.VirtualClusters, 2)
	assert.Equal(t, "name", cfg.entities.VirtualClusters[1].Name)
}

// A static key whose Secret is gone is sent as it was last: it keeps its
// name.
func TestUniqueValuesOfKeptEntity(t *testing.T) {
	f := newUniqueFixture(t)
	key := f.staticKey("key", 10)
	sentCfg := f.build(nil, key, f.keySecret("key"))
	require.NoError(t, sentCfg.blocked())

	newer := f.staticKey("newer", 20)
	cfg, err := buildConfigurationAfter(t.Context(), f.objects(key, newer, f.keySecret("newer")), f.gateway, "", sentCfg.built, &sentCfg.entities)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"EventGatewayStaticKey newer": `its spec.apiSpec.name "x" is already the one of EventGatewayStaticKey default/key`,
	}, excludedMessages(cfg))
	assert.Equal(t, map[string]bool{"EventGatewayStaticKey newer": false}, excludedBlocking(cfg))
	require.Len(t, cfg.entities.StaticKeys, 1)
}

func TestSentEntities(t *testing.T) {
	assert.Nil(t, sentEntities(nil))
	f := newUniqueFixture(t)
	consume := f.consumePolicy("consume", modifyHeaders("same", ""), 4)
	forward := f.forward("forward", "listener", "forward", 5)
	listener := f.listener()
	sent := sentEntities(f.sent(consume, listener, forward))
	assert.Len(t, sent, 5, "the backend cluster, the virtual cluster, its policy, the listener and its policy")
	for _, obj := range []client.Object{consume, listener, forward} {
		assert.Contains(t, sent, sentID(obj), obj.GetName())
	}
}
