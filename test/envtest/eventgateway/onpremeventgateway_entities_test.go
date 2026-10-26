package eventgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	kgomanager "github.com/kong/kong-operator/v2/modules/manager"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// The entities of an OnPremEventGateway pushed to its gw-admin pods: see
// onpremeventgateway_push_test.go for the push of the gateway itself and the
// fake gw-admin pods.

// provision makes the GWAdmin ready to be pushed to, with one pod.
func (g *testGWAdmin) provision(ctx context.Context) *fakeGWAdmin {
	g.t.Helper()
	g.createGWAdmin(ctx)
	g.publishAPI(ctx)
	g.issueOperatorClientCertificate(ctx)
	return g.startPod(ctx, "gw-admin-0", "10.0.0.1")
}

// backendClusterYAML returns the manifest of a backend cluster of the gateway.
func backendClusterYAML(name, apiName, gateway string) string {
	return `
metadata: {name: ` + name + `}
spec:` + onPremEventGatewayParentRef(gateway) + `
  apiSpec:
    name: ` + apiName + `
    bootstrapServers: ["kafka:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls: {enabled: Disabled}
`
}

// backendClusterNames returns the names of the backend clusters the pod
// holds for the gateway.
func backendClusterNames(ct *assert.CollectT, pod *fakeGWAdmin, gateway *eventgatewayv1alpha1.OnPremEventGateway) []string {
	g, ok, _ := pod.state(gatewayID(ct, gateway))
	if !assert.True(ct, ok, "the gw-admin pod doesn't know the gateway") {
		return nil
	}
	names := make([]string, 0, len(g.entities.BackendClusters))
	for _, bc := range g.entities.BackendClusters {
		names = append(names, bc.Name)
	}
	return names
}

// TestOnPremEventGatewayEntities verifies that the backend clusters of an
// OnPremEventGateway are pushed to every gw-admin pod of its GWAdmin, pushed
// again when they change, and left out when they can't be sent.
func TestOnPremEventGatewayEntities(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, gwAdmin.reconciler(ctx, mgr))

	pod0 := gwAdmin.provision(ctx)
	pod1 := gwAdmin.startPod(ctx, "gw-admin-1", "10.0.0.2")
	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	require.NoError(t, cl.Create(ctx, gateway))

	pushedCondition := func(ct *assert.CollectT) *metav1.Condition {
		g := &eventgatewayv1alpha1.OnPremEventGateway{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gateway), g)) {
			return nil
		}
		cond := apimeta.FindStatusCondition(g.Status.Conditions, "ConfigurationPushed")
		assert.NotNil(ct, cond)
		return cond
	}
	revision := func(t *testing.T) int64 {
		t.Helper()
		g := &eventgatewayv1alpha1.OnPremEventGateway{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gateway), g))
		return g.Status.ConfigRevision
	}
	expectBackendClusters := func(t *testing.T, names ...string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := pushedCondition(ct); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
			for _, pod := range []*fakeGWAdmin{pod0, pod1} {
				// Ordered by creation time, to the second, then by name.
				assert.ElementsMatch(ct, names, backendClusterNames(ct, pod, gateway))
			}
		}, waitTime, tickTime)
	}

	// expectCondition waits for the condition of the backend cluster to have
	// that status and reason.
	expectCondition := func(t *testing.T, name, conditionType string, status metav1.ConditionStatus, reason string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			bc := &configurationv1alpha1.EventGatewayBackendCluster{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: name}, bc)) {
				return
			}
			cond := apimeta.FindStatusCondition(bc.Status.Conditions, conditionType)
			if assert.NotNil(ct, cond) {
				assert.Equal(ct, status, cond.Status, cond.Message)
				assert.Equal(ct, reason, cond.Reason, cond.Message)
				assert.Equal(ct, bc.Generation, cond.ObservedGeneration)
			}
		}, waitTime, tickTime)
	}
	expectProgrammed := func(t *testing.T, name string, status metav1.ConditionStatus, reason string) {
		t.Helper()
		expectCondition(t, name, "Programmed", status, reason)
	}
	// expectEntityProgrammed waits for the Programmed condition of an entity
	// of any kind.
	expectEntityProgrammed := func(t *testing.T, obj interface {
		client.Object
		GetConditions() []metav1.Condition
	}, status metav1.ConditionStatus, reason string,
	) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
				return
			}
			cond := apimeta.FindStatusCondition(obj.GetConditions(), "Programmed")
			if assert.NotNil(ct, cond, "%T %s", obj, obj.GetName()) {
				assert.Equal(ct, status, cond.Status, cond.Message)
				assert.Equal(ct, reason, cond.Reason, cond.Message)
			}
		}, waitTime, tickTime)
	}

	backendCluster := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name,
		backendClusterYAML("backend-cluster", "backend-cluster", gateway.Name))

	t.Run("a backend cluster is pushed to every gw-admin pod, with a new revision", func(t *testing.T) {
		expectBackendClusters(t)
		before := revision(t)
		require.NoError(t, cl.Create(ctx, backendCluster))
		expectBackendClusters(t, "backend-cluster")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g, _, _ := pod0.state(gatewayID(ct, gateway))
			if assert.Len(ct, g.entities.BackendClusters, 1) {
				assert.Equal(ct, string(backendCluster.UID), g.entities.BackendClusters[0].Id.String())
				assert.Equal(ct, []string{"kafka:9092"}, g.entities.BackendClusters[0].BootstrapServers)
			}
		}, waitTime, tickTime)
		require.Greater(t, revision(t), before)
		expectProgrammed(t, "backend-cluster", metav1.ConditionTrue, "Programmed")
	})

	t.Run("a change to a backend cluster is pushed", func(t *testing.T) {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backendCluster), backendCluster))
		backendCluster.Spec.APISpec.BootstrapServers = []string{"kafka-2:9092"}
		require.NoError(t, cl.Update(ctx, backendCluster))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, pod := range []*fakeGWAdmin{pod0, pod1} {
				g, _, _ := pod.state(gatewayID(ct, gateway))
				if assert.Len(ct, g.entities.BackendClusters, 1) {
					assert.Equal(ct, []string{"kafka-2:9092"}, g.entities.BackendClusters[0].BootstrapServers)
				}
			}
		}, waitTime, tickTime)
	})

	t.Run("a backend cluster that can't be sent is left out", func(t *testing.T) {
		duplicate := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name,
			backendClusterYAML("duplicate", "backend-cluster", gateway.Name))
		require.NoError(t, cl.Create(ctx, duplicate))
		other := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name,
			backendClusterYAML("other", "other", gateway.Name))
		require.NoError(t, cl.Create(ctx, other))
		// The duplicate name is left out, the other backend cluster is sent.
		expectBackendClusters(t, "backend-cluster", "other")
		expectProgrammed(t, "duplicate", metav1.ConditionFalse, "Invalid")
		expectProgrammed(t, "other", metav1.ConditionTrue, "Programmed")
	})

	t.Run("a backend cluster reads a Secret of another namespace once a KongReferenceGrant allows it", func(t *testing.T) {
		secretsNS := &corev1.Namespace{GenerateName: "secrets-"}
		require.NoError(t, cl.Create(ctx, secretsNS))
		t.Cleanup(func() { _ = cl.Delete(context.Background(), secretsNS) })
		require.NoError(t, cl.Create(ctx, &corev1.Secret{
			Name: "tls", Namespace: secretsNS.Name,
			Data: map[string][]byte{"tls.crt": []byte("client-cert"), "tls.key": []byte("client-key")},
		}))
		crossNamespace := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, `
metadata: {name: cross-namespace}
spec:`+onPremEventGatewayParentRef(gateway.Name)+`
  apiSpec:
    name: cross-namespace
    bootstrapServers: ["kafka:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls:
      enabled: Enabled
      clientIdentity:
        certificate: {type: secretRef, secretRef: {name: tls, namespace: `+secretsNS.Name+`, key: tls.crt}}
        key: {type: secretRef, secretRef: {name: tls, namespace: `+secretsNS.Name+`, key: tls.key}}
`)
		require.NoError(t, cl.Create(ctx, crossNamespace))
		expectProgrammed(t, "cross-namespace", metav1.ConditionFalse, "RefNotPermitted")
		expectBackendClusters(t, "backend-cluster", "other")

		require.NoError(t, cl.Create(ctx, &configurationv1alpha1.KongReferenceGrant{
			Name: "backend-cluster-secrets", Namespace: secretsNS.Name,
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayBackendCluster", Namespace: configurationv1alpha1.Namespace(ns.Name)}},
				To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "core", Kind: "Secret"}},
			},
		}))
		expectBackendClusters(t, "backend-cluster", "other", "cross-namespace")
		expectProgrammed(t, "cross-namespace", metav1.ConditionTrue, "Programmed")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g, _, _ := pod0.state(gatewayID(ct, gateway))
			i := slices.IndexFunc(g.entities.BackendClusters, func(bc gwadminapi.EventGatewayBackendClusterOnPrem) bool { return bc.Name == "cross-namespace" })
			if assert.GreaterOrEqual(ct, i, 0) && assert.NotNil(ct, g.entities.BackendClusters[i].Tls.ClientIdentity) {
				assert.Equal(ct, "client-cert", g.entities.BackendClusters[i].Tls.ClientIdentity.Certificate)
			}
		}, waitTime, tickTime)

		require.NoError(t, cl.Delete(ctx, crossNamespace))
		expectBackendClusters(t, "backend-cluster", "other")
	})

	t.Run("a backend cluster is pushed once its Secret exists, and again when the Secret changes", func(t *testing.T) {
		withSecret := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, `
metadata: {name: with-secret}
spec:`+onPremEventGatewayParentRef(gateway.Name)+`
  apiSpec:
    name: with-secret
    bootstrapServers: ["kafka:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls:
      enabled: Enabled
      clientIdentity:
        certificate: {type: secretRef, secretRef: {name: with-secret-tls, key: tls.crt}}
        key: {type: secretRef, secretRef: {name: with-secret-tls, key: tls.key}}
`)
		require.NoError(t, cl.Create(ctx, withSecret))
		expectProgrammed(t, "with-secret", metav1.ConditionFalse, "Invalid")
		expectBackendClusters(t, "backend-cluster", "other")

		secret := &corev1.Secret{
			Name: "with-secret-tls", Namespace: ns.Name,
			Data: map[string][]byte{"tls.crt": []byte("client-cert"), "tls.key": []byte("client-key")},
		}
		require.NoError(t, cl.Create(ctx, secret))
		expectBackendClusters(t, "backend-cluster", "other", "with-secret")
		expectProgrammed(t, "with-secret", metav1.ConditionTrue, "Programmed")

		certificate := func(ct *assert.CollectT, pod *fakeGWAdmin) string {
			g, _, _ := pod.state(gatewayID(ct, gateway))
			i := slices.IndexFunc(g.entities.BackendClusters, func(bc gwadminapi.EventGatewayBackendClusterOnPrem) bool { return bc.Name == "with-secret" })
			if !assert.GreaterOrEqual(ct, i, 0) || !assert.NotNil(ct, g.entities.BackendClusters[i].Tls.ClientIdentity) {
				return ""
			}
			return g.entities.BackendClusters[i].Tls.ClientIdentity.Certificate
		}
		secret.Data["tls.crt"] = []byte("rotated-cert")
		require.NoError(t, cl.Update(ctx, secret))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, pod := range []*fakeGWAdmin{pod0, pod1} {
				assert.Equal(ct, "rotated-cert", certificate(ct, pod))
			}
		}, waitTime, tickTime)
		expectCondition(t, "with-secret", "SecretRefValid", metav1.ConditionTrue, "Valid")

		// A Secret gone leaves the last configuration pushed.
		require.NoError(t, cl.Delete(ctx, secret))
		expectCondition(t, "with-secret", "SecretRefValid", metav1.ConditionFalse, "Invalid")
		expectProgrammed(t, "with-secret", metav1.ConditionTrue, "Programmed")
		expectBackendClusters(t, "backend-cluster", "other", "with-secret")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, pod := range []*fakeGWAdmin{pod0, pod1} {
				assert.Equal(ct, "rotated-cert", certificate(ct, pod))
			}
		}, waitTime, tickTime)

		require.NoError(t, cl.Delete(ctx, withSecret))
		expectBackendClusters(t, "backend-cluster", "other")
	})

	t.Run("a configuration a gw-admin pod rejects is reported with the object at fault, then recovered", func(t *testing.T) {
		pod1.setReject(func(e gwadminapi.GatewayEntities) string {
			if i := slices.IndexFunc(e.BackendClusters, func(bc gwadminapi.EventGatewayBackendClusterOnPrem) bool { return bc.Name == "other" }); i >= 0 {
				return fmt.Sprintf("backend_clusters.%d.bootstrap_servers", i)
			}
			return ""
		})
		other := &configurationv1alpha1.EventGatewayBackendCluster{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "other"}, other))
		other.Spec.APISpec.BootstrapServers = []string{"kafka-3:9092"}
		require.NoError(t, cl.Update(ctx, other))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := pushedCondition(ct); cond != nil {
				assert.Equal(ct, metav1.ConditionFalse, cond.Status)
				assert.Equal(ct, "ConfigurationPushFailed", cond.Reason)
				assert.Contains(ct, cond.Message, "gw-admin pod gw-admin-1")
				assert.Contains(ct, cond.Message, "EventGatewayBackendCluster "+ns.Name+"/other (backend_clusters.1.bootstrap_servers): rejected by the test")
			}
		}, waitTime, tickTime)
		expectProgrammed(t, "other", metav1.ConditionFalse, "GatewayNotProgrammed")

		pod1.setReject(nil)
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "other"}, other))
		other.Spec.APISpec.BootstrapServers = []string{"kafka-4:9092"}
		require.NoError(t, cl.Update(ctx, other))
		expectBackendClusters(t, "backend-cluster", "other")
	})

	t.Run("a deleted backend cluster is removed from gw-admin", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, backendCluster))
		// The duplicate name is sent once the oldest backend cluster of that
		// name is gone.
		expectBackendClusters(t, "backend-cluster", "other")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g, _, _ := pod0.state(gatewayID(ct, gateway))
			ids := make([]string, 0, len(g.entities.BackendClusters))
			for _, bc := range g.entities.BackendClusters {
				ids = append(ids, bc.Id.String())
			}
			assert.NotContains(ct, ids, string(backendCluster.UID))
		}, waitTime, tickTime)
	})

	t.Run("its TLS trust bundles, schema registries and static keys are pushed and programmed", func(t *testing.T) {
		require.NoError(t, cl.Create(ctx, &corev1.Secret{
			Name: "key-secret", Namespace: ns.Name,
			Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")},
		}))
		bundle := objectFromYAML[configurationv1alpha1.EventGatewayTLSTrustBundle](t, ns.Name, `
metadata: {name: bundle}
spec:`+onPremEventGatewayParentRef(gateway.Name)+`
  apiSpec:
    name: bundle
    config: {trustedCa: {type: inline, value: ca-pem}}
`)
		registry := objectFromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, ns.Name, `
metadata: {name: registry}
spec:`+onPremEventGatewayParentRef(gateway.Name)+`
  apiSpec:
    type: confluent
    confluent:
      name: registry
      config: {endpoint: "https://registry.example.com", schemaType: json}
`)
		key := objectFromYAML[configurationv1alpha1.EventGatewayStaticKey](t, ns.Name, `
metadata: {name: key}
spec:`+onPremEventGatewayParentRef(gateway.Name)+`
  apiSpec:
    name: key
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
		for _, obj := range []client.Object{bundle, registry, key} {
			require.NoError(t, cl.Create(ctx, obj))
		}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, pod := range []*fakeGWAdmin{pod0, pod1} {
				g, ok, _ := pod.state(gatewayID(ct, gateway))
				if !assert.True(ct, ok) {
					continue
				}
				if assert.Len(ct, g.entities.TlsTrustBundles, 1) {
					assert.Equal(ct, string(bundle.UID), g.entities.TlsTrustBundles[0].Id.String())
				}
				assert.Len(ct, g.entities.SchemaRegistries, 1)
				if assert.Len(ct, g.entities.StaticKeys, 1) {
					assert.Equal(ct, string(key.UID), g.entities.StaticKeys[0].Id.String())
				}
			}
		}, waitTime, tickTime)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, obj := range []interface {
				client.Object
				GetConditions() []metav1.Condition
			}{bundle, registry, key} {
				if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
					assert.True(ct, apimeta.IsStatusConditionTrue(obj.GetConditions(), "Programmed"), "%T %s", obj, obj.GetName())
				}
			}
		}, waitTime, tickTime)
	})

	t.Run("its virtual clusters are pushed with their policies, a child policy after its parent", func(t *testing.T) {
		virtualCluster := objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
metadata: {name: virtual-cluster}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: other}}
  apiSpec:
    name: virtual-cluster
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: virtual-cluster
`)
		consumePolicy := func(name, manifest string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
			return objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: virtual-cluster}}
  apiSpec:
`+manifest)
		}
		parent := consumePolicy("parent", `
    type: schemaValidation
    schemaValidation:
      name: parent
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {name: registry}, valueValidationAction: mark}}
`)
		child := consumePolicy("child", `
    type: modifyHeaders
    parentPolicyRef: {name: parent}
    modifyHeaders:
      name: child
      config:
        actions: [{op: remove, remove: {key: x-header}}]
`)
		// Created first: added once its parent is.
		for _, obj := range []client.Object{child, virtualCluster, parent} {
			require.NoError(t, cl.Create(ctx, obj))
		}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, pod := range []*fakeGWAdmin{pod0, pod1} {
				g, ok, _ := pod.state(gatewayID(ct, gateway))
				if !assert.True(ct, ok) || !assert.Len(ct, g.entities.VirtualClusters, 1) {
					continue
				}
				vc := g.entities.VirtualClusters[0]
				assert.Equal(ct, string(virtualCluster.UID), vc.Id.String())
				if assert.Len(ct, vc.ConsumePolicies, 2) {
					assert.Equal(ct, string(parent.UID), vc.ConsumePolicies[0].Id.String())
					assert.Equal(ct, string(child.UID), vc.ConsumePolicies[1].Id.String())
				}
			}
		}, waitTime, tickTime)
		expectEntityProgrammed(t, virtualCluster, metav1.ConditionTrue, "Programmed")
		expectEntityProgrammed(t, parent, metav1.ConditionTrue, "Programmed")
		expectEntityProgrammed(t, child, metav1.ConditionTrue, "Programmed")
	})

	t.Run("a virtual cluster whose backend cluster is deleted is detached", func(t *testing.T) {
		bc := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name,
			backendClusterYAML("detached-backend-cluster", "detached-backend-cluster", gateway.Name))
		vc := objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
metadata: {name: detached}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: detached-backend-cluster}}
  apiSpec:
    name: detached
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: detached
`)
		require.NoError(t, cl.Create(ctx, bc))
		require.NoError(t, cl.Create(ctx, vc))
		expectEntityProgrammed(t, vc, metav1.ConditionTrue, "Programmed")

		require.NoError(t, cl.Delete(ctx, bc))
		expectEntityProgrammed(t, vc, metav1.ConditionFalse, "Detached")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g, _, _ := pod0.state(gatewayID(ct, gateway))
			for _, pushed := range g.entities.VirtualClusters {
				assert.NotEqual(ct, string(vc.UID), pushed.Id.String(), "the virtual cluster is removed from gw-admin")
			}
		}, waitTime, tickTime)
	})

	t.Run("its listeners are pushed with their policies, forwarding to a virtual cluster by its id", func(t *testing.T) {
		listener := objectFromYAML[configurationv1alpha1.EventGatewayListener](t, ns.Name, `
metadata: {name: listener}
spec:`+onPremEventGatewayParentRef(gateway.Name)+`
  apiSpec:
    name: listener
    addresses: ["0.0.0.0"]
    ports: [19092]
`)
		forward := objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, ns.Name, `
metadata: {name: forward}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config:
        type: portMapping
        portMapping: {advertisedHost: localhost, destination: {name: virtual-cluster}}
`)
		var virtualCluster configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "virtual-cluster"}, &virtualCluster))
		require.NoError(t, cl.Create(ctx, listener))
		require.NoError(t, cl.Create(ctx, forward))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, pod := range []*fakeGWAdmin{pod0, pod1} {
				g, ok, _ := pod.state(gatewayID(ct, gateway))
				if !assert.True(ct, ok) || !assert.Len(ct, g.entities.Listeners, 1) {
					continue
				}
				l := g.entities.Listeners[0]
				assert.Equal(ct, string(listener.UID), l.Id.String())
				if assert.Len(ct, l.Policies, 1) {
					assert.Equal(ct, string(forward.UID), l.Policies[0].Id.String())
					policy, err := json.Marshal(l.Policies[0])
					if assert.NoError(ct, err) {
						var fields struct {
							Config struct {
								Destination map[string]any `json:"destination"`
							} `json:"config"`
						}
						assert.NoError(ct, json.Unmarshal(policy, &fields))
						assert.Equal(ct, map[string]any{"id": string(virtualCluster.UID)}, fields.Config.Destination)
					}
				}
			}
		}, waitTime, tickTime)
		expectEntityProgrammed(t, listener, metav1.ConditionTrue, "Programmed")
		expectEntityProgrammed(t, forward, metav1.ConditionTrue, "Programmed")
	})

	t.Run("the entities of a deleted gateway are not programmed", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, gateway))
		expectProgrammed(t, "other", metav1.ConditionFalse, "GatewayNotProgrammed")
		expectEntityProgrammed(t, &configurationv1alpha1.EventGatewayVirtualCluster{Name: "virtual-cluster", Namespace: ns.Name},
			metav1.ConditionFalse, "GatewayNotProgrammed")
		for _, name := range []string{"parent", "child"} {
			expectEntityProgrammed(t, &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{Name: name, Namespace: ns.Name},
				metav1.ConditionFalse, "GatewayNotProgrammed")
		}
		expectEntityProgrammed(t, &configurationv1alpha1.EventGatewayListener{Name: "listener", Namespace: ns.Name},
			metav1.ConditionFalse, "GatewayNotProgrammed")
		expectEntityProgrammed(t, &configurationv1alpha1.EventGatewayListenerPolicy{Name: "forward", Namespace: ns.Name},
			metav1.ConditionFalse, "GatewayNotProgrammed")
	})
}

// TestOnPremEventGatewayPushWithoutKonnectControllers verifies that the
// OnPremEventGateway controller finds the entities of a gateway when the
// Konnect controllers, which also register their indexes, are disabled.
func TestOnPremEventGatewayPushWithoutKonnectControllers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	require.NoError(t, kgomanager.SetupCacheIndexes(ctx, mgr, kgomanager.Config{OnPremEventGatewayControllerEnabled: true}))

	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	require.NoError(t, gwAdmin.reconciler(ctx, mgr).SetupWithManager(ctx, mgr))
	var wg sync.WaitGroup
	wg.Go(func() { assert.NoError(t, mgr.Start(ctx)) })
	t.Cleanup(func() {
		wg.Wait()
		envtest.DumpLogsIfTestFailed(t, logs)
	})
	require.True(t, mgr.GetCache().WaitForCacheSync(ctx))

	pod := gwAdmin.provision(ctx)
	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	require.NoError(t, cl.Create(ctx, gateway))
	backendCluster := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name,
		backendClusterYAML("backend-cluster", "backend-cluster", gateway.Name))
	require.NoError(t, cl.Create(ctx, backendCluster))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Equal(ct, []string{"backend-cluster"}, backendClusterNames(ct, pod, gateway))
	}, waitTime, tickTime)
}
