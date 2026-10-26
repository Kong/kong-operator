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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

func TestBuildConfigurationListeners(t *testing.T) {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
	gateway.SetUID(types.UID("6a2f1c8e-5b3d-4e7a-9c1f-2d8b4a6e0f13"))
	created := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return created.Add(time.Duration(minutes) * time.Minute) }

	bc := fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, at(0), `
metadata: {name: bc}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: bc
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    tls: {enabled: Disabled}
`)
	vc := fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, at(1), `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: bc}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	listener := func(name, apiName string, minutes int) *configurationv1alpha1.EventGatewayListener {
		return fromYAML[configurationv1alpha1.EventGatewayListener](t, at(minutes), `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: `+apiName+`
    addresses: ["0.0.0.0"]
    ports: [19092]
`)
	}
	forwardPolicy := func(name, listener, destination string, minutes int) *configurationv1alpha1.EventGatewayListenerPolicy {
		return fromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, at(minutes), `
metadata: {name: `+name+`}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: `+listener+`}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: `+name+`
      config:
        type: portMapping
        portMapping:
          advertisedHost: localhost
          destination: `+destination+`
`)
	}

	l := listener("listener", "listener", 2)
	// Left out as a duplicate: its policy is left out with it.
	lDuplicate := listener("listener-duplicate", "listener", 3)
	forward := forwardPolicy("forward", "listener", "{name: vc}", 4)
	forwardUnknown := forwardPolicy("forward-unknown", "listener", "{name: unknown}", 5)
	forwardByID := forwardPolicy("forward-by-id", "listener", "{id: 9524ec7d-36d9-465d-a8c5-83a3c9390458}", 6)
	forwardOfDuplicate := forwardPolicy("forward-of-duplicate", "listener-duplicate", "{name: vc}", 7)
	// Of another namespace, attached to a listener of the gateway: left out,
	// whatever its parent reference says.
	forwardOtherNamespace := forwardPolicy("forward-other-namespace", "listener", "{name: vc}", 8)
	forwardOtherNamespace.SetNamespace("other")
	forwardOtherNamespace.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new(ns)

	cl := newFakeClient(t, bc, vc, l, lDuplicate, forward, forwardUnknown, forwardByID, forwardOfDuplicate, forwardOtherNamespace)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)

	excluded := map[string]string{}
	for _, e := range cfg.excluded {
		excluded[reflect.TypeOf(e.obj).Elem().Name()+" "+e.obj.GetName()] = e.message
	}
	assert.Equal(t, map[string]string{
		"EventGatewayListener listener-duplicate":            `another listener of the gateway is named "listener"`,
		"EventGatewayListenerPolicy forward-unknown":         `it forwards to the virtual cluster "unknown", which isn't in the configuration of the gateway`,
		"EventGatewayListenerPolicy forward-by-id":           "it forwards to a virtual cluster by id: name a virtual cluster of the gateway instead",
		"EventGatewayListenerPolicy forward-of-duplicate":    "its EventGatewayListener " + ns + "/listener-duplicate isn't in the configuration",
		"EventGatewayListenerPolicy forward-other-namespace": "it must be in the namespace of the OnPremEventGateway, " + ns,
	}, excluded)

	got, err := json.Marshal(cfg.entities)
	require.NoError(t, err)
	var entities struct {
		Listeners []struct {
			ID       string           `json:"id"`
			Name     string           `json:"name"`
			Policies []map[string]any `json:"policies"`
		} `json:"listeners"`
	}
	require.NoError(t, json.Unmarshal(got, &entities))
	require.Len(t, entities.Listeners, 1)
	gotListener := entities.Listeners[0]
	assert.Equal(t, string(l.UID), gotListener.ID)
	require.Len(t, gotListener.Policies, 1)
	assert.Equal(t, string(forward.UID), gotListener.Policies[0]["id"])
	config, _ := gotListener.Policies[0]["config"].(map[string]any)
	assert.Equal(t, map[string]any{"id": string(vc.UID)}, config["destination"], "a virtual cluster named in the policy is forwarded to by its UID")

	assert.Equal(t, "EventGatewayListenerPolicy "+client.ObjectKeyFromObject(forward).String()+" (listeners.0.policies.0)",
		cfg.describeField("listeners.0.policies.0"))
}

func TestBuildConfigurationKeptListenerPolicy(t *testing.T) {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
	gateway.SetUID(types.UID("6a2f1c8e-5b3d-4e7a-9c1f-2d8b4a6e0f13"))
	created := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

	bundle := fromYAML[configurationv1alpha1.EventGatewayTLSTrustBundle](t, created, `
metadata: {name: bundle}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: bundle
    config: {trustedCa: {type: inline, value: ca-pem}}
`)
	listener := fromYAML[configurationv1alpha1.EventGatewayListener](t, created, `
metadata: {name: listener}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec: {name: listener, addresses: ["0.0.0.0"], ports: [19092]}
`)
	tlsServer := fromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, created, `
metadata: {name: tls}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: tlsServer
    tlsServer:
      name: tls
      config:
        certificates:
          - certificate: {type: secretRef, secretRef: {name: tls, key: tls.crt}}
            key: {type: secretRef, secretRef: {name: tls, key: tls.key}}
        clientAuthentication:
          mode: required
          tlsTrustBundles: [{namespacedRef: {name: bundle}}]
`)
	secret := &corev1.Secret{Name: "tls", Namespace: ns, Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")}}

	withSecret, err := buildConfiguration(t.Context(), newFakeClient(t, bundle, listener, tlsServer, secret), gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, withSecret.excluded)
	require.Len(t, withSecret.entities.Listeners, 1)
	require.Len(t, withSecret.entities.Listeners[0].Policies, 1)

	t.Run("its Secret gone, it is kept with the TLS trust bundles it references", func(t *testing.T) {
		kept, err := buildConfiguration(t.Context(), newFakeClient(t, bundle, listener, tlsServer), gateway, "", withSecret.built)
		require.NoError(t, err)
		assert.Empty(t, kept.excluded)
		assert.Equal(t, withSecret.entities.Listeners, kept.entities.Listeners)
		policy := keptStatus(t, kept, tlsServer)
		assert.True(t, policy.kept)
		assertSecretRefValid(t, policy, metav1.ConditionFalse, "the Secret "+ns+"/tls doesn't exist")
	})

	t.Run("its Secret gone, it is left out when a TLS trust bundle it references is gone too", func(t *testing.T) {
		gone, err := buildConfiguration(t.Context(), newFakeClient(t, listener, tlsServer), gateway, "", withSecret.built)
		require.NoError(t, err)
		require.Len(t, gone.excluded, 1)
		assert.Equal(t, tlsServer.UID, gone.excluded[0].obj.GetUID())
		assert.Equal(t, "it references an EventGatewayTLSTrustBundle that isn't in the configuration of the gateway", gone.excluded[0].message)
		require.Len(t, gone.entities.Listeners, 1)
		assert.Empty(t, gone.entities.Listeners[0].Policies)

		// Its Secret still gone, it is kept again once the TLS trust bundle
		// is back.
		back, err := buildConfiguration(t.Context(), newFakeClient(t, bundle, listener, tlsServer), gateway, "", gone.built)
		require.NoError(t, err)
		assert.Empty(t, back.excluded)
		assert.Equal(t, withSecret.entities.Listeners, back.entities.Listeners)
		assert.True(t, keptStatus(t, back, tlsServer).kept)
	})
}

// keptStatus returns the status of the included entity of obj.
func keptStatus(t *testing.T, cfg configuration, obj client.Object) entityStatus {
	t.Helper()
	for _, e := range cfg.included {
		if e.obj.GetUID() == obj.GetUID() {
			return e
		}
	}
	require.Failf(t, "not included", "%s isn't in the configuration", obj.GetName())
	return entityStatus{}
}
