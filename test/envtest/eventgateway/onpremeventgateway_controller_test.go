package eventgateway

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	egonprem "github.com/kong/kong-operator/v2/controller/eventgateway/onprem"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
)

// objectFromYAML returns an object of type T, in the namespace, from its
// manifest.
func objectFromYAML[T any, TPtr interface {
	*T
	client.Object
}](t *testing.T, namespace, manifest string) TPtr {
	t.Helper()
	var obj T
	require.NoError(t, yaml.Unmarshal([]byte(manifest), &obj))
	ptr := TPtr(&obj)
	ptr.SetNamespace(namespace)
	return ptr
}

// onPremEventGatewayParentRef is the parentRef of a root entity attached to
// the named OnPremEventGateway.
func onPremEventGatewayParentRef(gateway string) string {
	return `
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: ` + gateway + `}`
}

// onPremEventGatewayReconciledMessage is logged at the end of each reconcile
// of an OnPremEventGateway, with the gateway's name.
const onPremEventGatewayReconciledMessage = "reconciliation complete for OnPremEventGateway resource"

// TestOnPremEventGatewayReconciler verifies that an OnPremEventGateway reports
// a status, sets it back when someone else changes it, and can be deleted,
// and that creating, changing or deleting any Event Gateway entity, or
// changing its parent reference, reconciles the gateway it belongs to, and
// only that one.
func TestOnPremEventGatewayReconciler(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	// Like envtest.NewManager, without its short resync period: a resync reconciles
	// every gateway, which would hide which gateways a change reconciled.
	_, logger, logs := envtest.CreateTestLogger(ctx)
	mgr, err := ctrl.NewManager(cfg, manager.Options{
		Logger:     logger,
		Scheme:     scheme.Get(),
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{RecoverPanic: new(false), SkipNameValidation: new(true)},
	})
	require.NoError(t, err)

	envtest.StartReconcilers(ctx, t, mgr, logs,
		&egonprem.Reconciler{
			Client: mgr.GetClient(),
		},
	)

	cl := mgr.GetClient()

	newGateway := func(name string) *eventgatewayv1alpha1.OnPremEventGateway {
		return objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  gwAdminRef: {name: gw-admin}
`)
	}
	gatewayA := newGateway("gateway-a")
	gatewayB := newGateway("gateway-b")
	gateways := []*eventgatewayv1alpha1.OnPremEventGateway{gatewayA, gatewayB}

	reportsStatus := func(ct *assert.CollectT, gateway *eventgatewayv1alpha1.OnPremEventGateway) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)) {
			return
		}
		assert.Equal(ct, gateway.Generation, gateway.Status.ObservedGeneration)
		cond := apimeta.FindStatusCondition(gateway.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
		if assert.NotNil(ct, cond) {
			// The gateways of this test have no GWAdmin.
			assert.Equal(ct, "False", string(cond.Status))
			assert.Equal(ct, string(eventgatewayv1alpha1.DependenciesNotReadyReason), cond.Reason)
			assert.Equal(ct, gateway.Generation, cond.ObservedGeneration)
		}
	}

	t.Log("Creating two OnPremEventGateways and expecting them to report their status")
	for _, gateway := range gateways {
		require.NoError(t, cl.Create(ctx, gateway))
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		for _, gateway := range gateways {
			reportsStatus(ct, gateway)
		}
	}, waitTime, tickTime)

	// reconciles returns, for each gateway, how many times it was reconciled.
	reconciles := func() map[string]int {
		counts := make(map[string]int, len(gateways))
		for _, entry := range logs.All() {
			if entry.Message != onPremEventGatewayReconciledMessage {
				continue
			}
			ref := fmt.Sprint(entry.ContextMap()["OnPremEventGateway"])
			for _, gateway := range gateways {
				if strings.Contains(ref, gateway.Name) {
					counts[gateway.Name]++
				}
			}
		}
		return counts
	}
	// waitForNoReconcile waits until no gateway has been reconciled for a
	// second, e.g. after the reconcile triggered by a gateway's own status
	// write. testify checks the condition once before the first tick, so the
	// samples must be a second apart: taken back to back they are always equal
	// and the wait would return at once. It assumes one step of the
	// status-write chain takes well under a second.
	waitForNoReconcile := func(t *testing.T) {
		t.Helper()
		var last map[string]int
		require.Eventually(t, func() bool {
			counts := reconciles()
			quiet := last != nil && maps.Equal(counts, last)
			last = counts
			return quiet
		}, waitTime, time.Second)
	}

	// expectReconciled applies change and expects it to reconcile the given
	// gateways, and no other one.
	expectReconciled := func(t *testing.T, change func(), reconciled ...*eventgatewayv1alpha1.OnPremEventGateway) {
		t.Helper()
		waitForNoReconcile(t)
		before := reconciles()

		change()

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			after := reconciles()
			for _, gateway := range reconciled {
				assert.Greater(ct, after[gateway.Name], before[gateway.Name], "gateway %s must be reconciled", gateway.Name)
			}
		}, waitTime, tickTime)
		// asserts.Never, not testify's require.Never: the latter runs the
		// condition in a goroutine that it can leave running after it returns,
		// which races with the next subtest writing the gateways read here.
		asserts.Never(t, func(context.Context) bool {
			after := reconciles()
			for _, gateway := range gateways {
				if !slices.Contains(reconciled, gateway) && after[gateway.Name] != before[gateway.Name] {
					t.Logf("gateway %s must not be reconciled", gateway.Name)
					return true
				}
			}
			return false
		}, time.Second, tickTime, "a gateway other than the expected ones must not be reconciled")
	}
	create := func(t *testing.T, obj client.Object) func() {
		return func() { require.NoError(t, cl.Create(ctx, obj)) }
	}

	backendCluster := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, `
metadata: {name: backend-cluster}
spec:`+onPremEventGatewayParentRef(gatewayA.Name)+`
  apiSpec:
    name: backend-cluster
    bootstrapServers: ["broker.example.com:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls: {enabled: Disabled}
`)
	listener := objectFromYAML[configurationv1alpha1.EventGatewayListener](t, ns.Name, `
metadata: {name: listener}
spec:`+onPremEventGatewayParentRef(gatewayA.Name)+`
  apiSpec: {name: listener, addresses: ["0.0.0.0"], ports: [9092]}
`)
	virtualCluster := objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
metadata: {name: virtual-cluster}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend-cluster}}
  apiSpec:
    name: virtual-cluster
    dnsLabel: virtual-cluster
    aclMode: enforce_on_gateway
    authentication: [{type: anonymous, anonymous: {}}]
    namespace: {mode: hide_prefix, prefix: tenant_}
`)
	const modifyHeaders = `
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: virtual-cluster}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders:
      name: add-header
      config:
        actions: [{op: set, set: {key: x-header, value: value}}]
`
	producePolicy := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, ns.Name,
		"metadata: {name: produce-policy}"+modifyHeaders)

	t.Run("each entity kind reconciles its gateway when created", func(t *testing.T) {
		for _, obj := range []client.Object{
			backendCluster,
			listener,
			objectFromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, ns.Name, `
metadata: {name: schema-registry}
spec:`+onPremEventGatewayParentRef(gatewayA.Name)+`
  apiSpec:
    type: confluent
    confluent:
      name: schema-registry
      config: {endpoint: "https://schema-registry.example.com", schemaType: json}
`),
			objectFromYAML[configurationv1alpha1.EventGatewayTLSTrustBundle](t, ns.Name, `
metadata: {name: trust-bundle}
spec:`+onPremEventGatewayParentRef(gatewayA.Name)+`
  apiSpec:
    name: trust-bundle
    config:
      trustedCa: {type: inline, value: ca-pem-data}
`),
			objectFromYAML[configurationv1alpha1.EventGatewayStaticKey](t, ns.Name, `
metadata: {name: static-key}
spec:`+onPremEventGatewayParentRef(gatewayA.Name)+`
  apiSpec:
    name: static-key
    value: {type: inline, value: key-value}
`),
			objectFromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, ns.Name, `
metadata: {name: dataplane-certificate}
spec:`+onPremEventGatewayParentRef(gatewayA.Name)+`
  apiSpec:
    certificate: {type: inline, value: certificate-pem-data}
`),
			objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, ns.Name, `
metadata: {name: listener-policy}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: tlsServer
    tlsServer:
      name: tls-policy
      config:
        certificates:
          - certificate: {type: inline, value: cert-pem-data}
            key: {type: inline, value: key-pem-data}
`),
			virtualCluster,
			objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, ns.Name, `
metadata: {name: virtual-cluster-policy}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: virtual-cluster}}
  apiSpec:
    type: acls
    acls:
      name: acls
      config:
        rules:
          - action: allow
            resourceType: topic
            operations: [{name: read}]
            resourceNames:
              type: stat
              stat: [{match: orders.*}]
`),
			objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](t, ns.Name,
				"metadata: {name: consume-policy}"+modifyHeaders),
			producePolicy,
		} {
			t.Logf("Creating %T %s", obj, obj.GetName())
			expectReconciled(t, create(t, obj), gatewayA)
		}
	})

	// programmedOf returns the Programmed condition of the gateway, read from
	// the API server.
	programmedOf := func(t *testing.T, gateway *eventgatewayv1alpha1.OnPremEventGateway) metav1.Condition {
		t.Helper()
		g := &eventgatewayv1alpha1.OnPremEventGateway{}
		require.NoError(t, mgr.GetAPIReader().Get(ctx, client.ObjectKeyFromObject(gateway), g))
		cond := apimeta.FindStatusCondition(g.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
		require.NotNil(t, cond)
		return *cond
	}

	t.Run("a change to an entity reconciles its gateway", func(t *testing.T) {
		// Set more than 10s ago, when the gateway was created: a reconcile
		// that changes nothing must keep it.
		before := programmedOf(t, gatewayA)
		expectReconciled(t, func() {
			require.NoError(t, cl.Patch(ctx, producePolicy, client.RawPatch(types.MergePatchType, []byte(
				`{"spec":{"apiSpec":{"modifyHeaders":{"config":{"actions":[{"op":"set","set":{"key":"x-header","value":"another-value"}}]}}}}}`,
			))))
		}, gatewayA)
		require.Equal(t, before, programmedOf(t, gatewayA), "the Programmed condition must not change")
	})

	// The CRDs allow changing the parent reference only while the entity is
	// not Programmed, which the listener is not.
	t.Run("an entity moved to another gateway reconciles both gateways", func(t *testing.T) {
		expectReconciled(t, func() {
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(listener), listener))
			listener.Spec.ParentRef.Name = gatewayB.Name
			require.NoError(t, cl.Update(ctx, listener))
		}, gatewayA, gatewayB)
	})

	t.Run("an entity of a KonnectEventGateway reconciles no gateway", func(t *testing.T) {
		// The deprecated spec.gatewayRef only references a KonnectEventGateway,
		// even when an OnPremEventGateway has the same name.
		expectReconciled(t, create(t, objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, `
metadata: {name: deprecated-gateway-ref-backend-cluster}
spec:
  gatewayRef: {type: namespacedRef, namespacedRef: {name: `+gatewayA.Name+`}}
  apiSpec:
    name: deprecated-gateway-ref-backend-cluster
    bootstrapServers: ["broker.example.com:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls: {enabled: Disabled}
`)))
		expectReconciled(t, create(t, objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, `
metadata: {name: konnect-backend-cluster}
spec:
  parentRef: {group: konnect.konghq.com, kind: KonnectEventGateway, name: konnect-gateway}
  apiSpec:
    name: konnect-backend-cluster
    bootstrapServers: ["broker.example.com:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls: {enabled: Disabled}
`)))
	})

	t.Run("a status changed by someone else is set back", func(t *testing.T) {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gatewayA), gatewayA))
		gatewayA.Status = eventgatewayv1alpha1.OnPremEventGatewayStatus{
			ObservedGeneration: gatewayA.Generation + 10,
			ServerURL:          "https://set-by-someone-else.example.com",
			Conditions: []metav1.Condition{
				{
					Type: string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType), Status: metav1.ConditionTrue,
					Reason: "Tampered", Message: "set by someone else", LastTransitionTime: metav1.Now(),
				},
				{
					Type: "SetBySomeoneElse", Status: metav1.ConditionTrue,
					Reason: "SomeoneElse", Message: "not set by the controller", LastTransitionTime: metav1.Now(),
				},
			},
		}
		require.NoError(t, cl.Status().Update(ctx, gatewayA))
		// Read from the API server: the cache may still have the previous
		// status, so only a read there shows it was set back.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			gateway := &eventgatewayv1alpha1.OnPremEventGateway{}
			if !assert.NoError(ct, mgr.GetAPIReader().Get(ctx, client.ObjectKeyFromObject(gatewayA), gateway)) {
				return
			}
			assert.Equal(ct, gateway.Generation, gateway.Status.ObservedGeneration)
			cond := apimeta.FindStatusCondition(gateway.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
			if assert.NotNil(ct, cond) {
				assert.Equal(ct, metav1.ConditionFalse, cond.Status)
				assert.Equal(ct, string(eventgatewayv1alpha1.DependenciesNotReadyReason), cond.Reason)
				assert.Equal(ct, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotResolvedMessage, cond.Message)
			}
			resolved := apimeta.FindStatusCondition(gateway.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedType))
			if assert.NotNil(ct, resolved) {
				assert.Equal(ct, metav1.ConditionFalse, resolved.Status)
				assert.Equal(ct, string(eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason), resolved.Reason)
				assert.Contains(ct, resolved.Message, "does not exist")
			}
			// The controller owns the whole status: what it doesn't set is
			// removed.
			assert.Nil(ct, apimeta.FindStatusCondition(gateway.Status.Conditions, "SetBySomeoneElse"))
			assert.Len(ct, gateway.Status.Conditions, 2)
			assert.Empty(ct, gateway.Status.ServerURL)
		}, waitTime, tickTime)
	})

	t.Run("deleting an entity reconciles its gateway", func(t *testing.T) {
		expectReconciled(t, func() {
			require.NoError(t, cl.Delete(ctx, producePolicy))
		}, gatewayA)
		expectReconciled(t, func() {
			require.NoError(t, cl.Delete(ctx, listener))
		}, gatewayB)
	})

	backendClusterB := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, `
metadata: {name: backend-cluster-b}
spec:`+onPremEventGatewayParentRef(gatewayB.Name)+`
  apiSpec:
    name: backend-cluster-b
    bootstrapServers: ["broker.example.com:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls: {enabled: Disabled}
`)
	t.Run("a child moved to a parent of another gateway reconciles both gateways", func(t *testing.T) {
		expectReconciled(t, create(t, backendClusterB), gatewayB)
		// The CRDs allow changing the parent reference of a child only when it
		// has status conditions and is not Programmed: their rule fails to
		// evaluate on an object without status. Write the status the gateway
		// reconcile will write on an entity it can't push. A status write on
		// an entity reconciles no gateway.
		expectReconciled(t, func() {
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(virtualCluster), virtualCluster))
			virtualCluster.Status.Conditions = []metav1.Condition{{
				Type: "Programmed", Status: metav1.ConditionFalse,
				Reason: "Pending", Message: "not pushed", LastTransitionTime: metav1.Now(),
			}}
			require.NoError(t, cl.Status().Update(ctx, virtualCluster))
		})
		expectReconciled(t, func() {
			require.NoError(t, cl.Patch(ctx, virtualCluster, client.RawPatch(types.MergePatchType, []byte(
				`{"spec":{"eventGatewayBackendClusterRef":{"namespacedRef":{"name":"`+backendClusterB.Name+`"}}}}`,
			))))
		}, gatewayA, gatewayB)
	})

	t.Run("a parent deleted and created again under another gateway moves its children", func(t *testing.T) {
		expectReconciled(t, func() {
			require.NoError(t, cl.Delete(ctx, backendClusterB))
		}, gatewayB)
		recreated := backendClusterB.DeepCopy()
		recreated.ResourceVersion = ""
		recreated.UID = ""
		recreated.Spec.ParentRef.Name = gatewayA.Name
		expectReconciled(t, create(t, recreated), gatewayA)
		expectReconciled(t, func() {
			require.NoError(t, cl.Patch(ctx, virtualCluster, client.RawPatch(types.MergePatchType, []byte(
				`{"spec":{"apiSpec":{"dnsLabel":"virtual-cluster-2"}}}`,
			))))
		}, gatewayA)
	})

	t.Run("a change to an entity's metadata only reconciles no gateway", func(t *testing.T) {
		expectReconciled(t, func() {
			require.NoError(t, cl.Patch(ctx, backendCluster, client.RawPatch(types.MergePatchType, []byte(
				`{"metadata":{"labels":{"example.com/label":"value"}}}`,
			))))
		})
	})

	t.Run("an entity in another namespace reconciles the gateway of its parent", func(t *testing.T) {
		otherNamespace := &corev1.Namespace{GenerateName: "other-"}
		require.NoError(t, cl.Create(ctx, otherNamespace))
		crossNamespace := objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, otherNamespace.Name, `
metadata: {name: cross-namespace-virtual-cluster}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend-cluster, namespace: `+ns.Name+`}}
  apiSpec:
    name: cross-namespace-virtual-cluster
    dnsLabel: cross-namespace-virtual-cluster
    aclMode: enforce_on_gateway
    authentication: [{type: anonymous, anonymous: {}}]
    namespace: {mode: hide_prefix, prefix: tenant_}
`)
		expectReconciled(t, create(t, crossNamespace), gatewayA)
	})

	t.Run("an entity with a finalizer reconciles its gateway when its deletion starts and ends", func(t *testing.T) {
		registry := &configurationv1alpha1.EventGatewaySchemaRegistry{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "schema-registry"}, registry))
		expectReconciled(t, func() {
			require.NoError(t, cl.Patch(ctx, registry, client.RawPatch(types.MergePatchType, []byte(
				`{"metadata":{"finalizers":["example.com/hold"]}}`,
			))))
		})
		expectReconciled(t, func() {
			require.NoError(t, cl.Delete(ctx, registry))
		}, gatewayA)
		expectReconciled(t, func() {
			require.NoError(t, cl.Patch(ctx, registry, client.RawPatch(types.MergePatchType, []byte(
				`{"metadata":{"finalizers":null}}`,
			))))
		}, gatewayA)
	})

	t.Run("a gateway can be deleted", func(t *testing.T) {
		for _, gateway := range gateways {
			require.NoError(t, cl.Delete(ctx, gateway))
		}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, gateway := range gateways {
				assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)))
			}
		}, waitTime, tickTime)
	})
}
