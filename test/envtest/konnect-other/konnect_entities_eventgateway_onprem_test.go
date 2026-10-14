package konnectother

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

const onPremParentRef = `
  parentRef:
    group: eventgateway.konghq.com
    kind: OnPremEventGateway
    name: onprem-gateway`

// objectFromYAML returns an object of type T from its manifest.
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

func newKonnectEntityReconciler[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](factory *sdkmocks.MockSDKFactory, mgr interface{ GetClient() client.Client }) *konnect.KonnectEntityReconciler[T, TEnt] {
	return konnect.NewKonnectEntityReconciler[T, TEnt](factory, logging.DevelopmentMode, mgr.GetClient(),
		konnect.WithKonnectEntitySyncPeriod[T, TEnt](consts.KonnectInfiniteSyncTime),
		konnect.WithMetricRecorder[T, TEnt](&metricsmocks.MockRecorder{}),
	)
}

// startEventGatewayReconcilers starts the Konnect reconcilers of every Event
// Gateway entity. No SDK call is expected: the mocks fail the test on any call.
func startEventGatewayReconcilers(t *testing.T, ctx context.Context) (client.Client, *corev1.Namespace) {
	t.Helper()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	envtest.StartReconcilers(ctx, t, mgr, logs,
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayBackendCluster](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayDataPlaneCertificate](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayListener](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewaySchemaRegistry](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayStaticKey](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayTLSTrustBundle](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualCluster](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayListenerPolicy](factory, mgr),
	)
	return mgr.GetClient(), ns
}

// createVisible creates the objects and waits for them to be in the manager's
// cache, which the reconcilers read from.
func createVisible(t *testing.T, ctx context.Context, cl client.Client, objects ...client.Object) {
	t.Helper()
	for _, obj := range objects {
		require.NoError(t, cl.Create(ctx, obj), "failed to create %T %s", obj, obj.GetName())
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		for _, obj := range objects {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj))
		}
	}, consts.WaitTime, consts.TickTime)
}

// konnectStatusOf returns a description of the conditions and finalizers set
// on obj, or an empty string when there are none.
func konnectStatusOf(ctx context.Context, cl client.Client, obj client.Object) string {
	if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return "failed to get: " + err.Error()
	}
	conditions := obj.(interface {
		GetConditions() []metav1.Condition
	}).GetConditions()
	if len(conditions) == 0 && len(obj.GetFinalizers()) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range conditions {
		b.WriteString(c.Type + "=" + string(c.Status) + "/" + c.Reason + " ")
	}
	return b.String() + "finalizers=" + strings.Join(obj.GetFinalizers(), ",")
}

// requireNoKonnectStatus checks that none of the objects gets a condition or a
// finalizer during d.
func requireNoKonnectStatus(t *testing.T, ctx context.Context, cl client.Client, d time.Duration, objects ...client.Object) {
	t.Helper()
	require.Never(t, func() bool {
		for _, obj := range objects {
			if s := konnectStatusOf(ctx, cl, obj); s != "" {
				t.Logf("%T %s/%s: %s", obj, obj.GetNamespace(), obj.GetName(), s)
				return true
			}
		}
		return false
	}, d, consts.TickTime, "entities attached to an OnPremEventGateway must get no Konnect condition or finalizer")
}

func onPremBackendCluster(t *testing.T, namespace, name string) *configurationv1alpha1.EventGatewayBackendCluster {
	return objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, namespace, `
metadata: {name: `+name+`}
spec:`+onPremParentRef+`
  apiSpec:
    name: `+name+`
    bootstrapServers: ["broker.example.com:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls: {enabled: Disabled}
`)
}

func virtualCluster(t *testing.T, namespace, name, backendClusterRef string) *configurationv1alpha1.EventGatewayVirtualCluster {
	return objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, namespace, `
metadata: {name: `+name+`}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: `+backendClusterRef+`}
  apiSpec:
    name: `+name+`
    dnsLabel: `+name+`
    aclMode: enforce_on_gateway
    authentication: [{type: anonymous, anonymous: {}}]
    namespace: {mode: hide_prefix, prefix: tenant_}
`)
}

func producePolicy(t *testing.T, namespace, name, virtualClusterName string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
	return objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, namespace, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: `+virtualClusterName+`}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders:
      name: add-header
      config:
        actions: [{op: set, set: {key: x-header, value: value}}]
`)
}

func onPremTLSTrustBundle(t *testing.T, namespace, name string) *configurationv1alpha1.EventGatewayTLSTrustBundle {
	return objectFromYAML[configurationv1alpha1.EventGatewayTLSTrustBundle](t, namespace, `
metadata: {name: `+name+`}
spec:`+onPremParentRef+`
  apiSpec:
    name: `+name+`
    config:
      trustedCa: {type: inline, value: ca-pem-data}
`)
}

// TestEventGatewayEntitiesOnPremEventGateway verifies that the Konnect
// reconcilers leave alone the event gateway entities attached to an
// OnPremEventGateway, and their children: they make no Konnect call and set no
// status condition or finalizer.
func TestEventGatewayEntitiesOnPremEventGateway(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cl, ns := startEventGatewayReconcilers(t, ctx)

	t.Log("Creating a virtual cluster whose backend cluster does not exist, to check the reconcilers run")
	control := deploy.EventGatewayVirtualCluster(t, ctx, client.NewNamespacedClient(cl, ns.Name),
		&configurationv1alpha1.EventGatewayBackendCluster{Name: "missing-backend-cluster"},
	)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(control), control)) {
			return
		}
		assert.NotEmpty(ct, control.Status.Conditions, "the Konnect reconciler must report the missing backend cluster")
	}, consts.WaitTime, consts.TickTime)

	t.Log("Creating every root entity attached to an OnPremEventGateway")
	backendCluster := onPremBackendCluster(t, ns.Name, "backend")
	listener := objectFromYAML[configurationv1alpha1.EventGatewayListener](t, ns.Name, `
metadata: {name: listener}
spec:`+onPremParentRef+`
  apiSpec: {name: listener, addresses: ["0.0.0.0"], ports: [9092]}
`)
	roots := []client.Object{
		backendCluster,
		listener,
		objectFromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, ns.Name, `
metadata: {name: schema-registry}
spec:`+onPremParentRef+`
  apiSpec:
    type: confluent
    confluent:
      name: schema-registry
      config: {endpoint: "https://schema-registry.example.com", schemaType: json}
`),
		onPremTLSTrustBundle(t, ns.Name, "trust-bundle"),
		objectFromYAML[configurationv1alpha1.EventGatewayStaticKey](t, ns.Name, `
metadata: {name: static-key}
spec:`+onPremParentRef+`
  apiSpec:
    name: static-key
    value: {type: inline, value: key-value}
`),
		objectFromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, ns.Name, `
metadata: {name: dataplane-certificate}
spec:`+onPremParentRef+`
  apiSpec:
    certificate: {type: inline, value: certificate-pem-data}
`),
	}
	// Parents are created first: a child reconciled before its parent exists
	// legitimately reports the missing parent.
	createVisible(t, ctx, cl, roots...)

	t.Log("Creating children under them, including one in another namespace")
	otherNamespace := &corev1.Namespace{GenerateName: "other-"}
	require.NoError(t, cl.Create(ctx, otherNamespace))
	// A cross-namespace reference to the backend cluster, without a
	// KongReferenceGrant: the Konnect reconciler must not report the missing
	// grant, it doesn't manage this entity.
	crossNamespaceVirtualCluster := virtualCluster(t, otherNamespace.Name, "cross-namespace-virtual-cluster",
		`{name: `+backendCluster.Name+`, namespace: `+ns.Name+`}`)
	vc := virtualCluster(t, ns.Name, "virtual-cluster", `{name: `+backendCluster.Name+`}`)
	listenerPolicy := objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, ns.Name, `
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
`)
	createVisible(t, ctx, cl, vc, crossNamespaceVirtualCluster, listenerPolicy)
	policy := producePolicy(t, ns.Name, "produce-policy", vc.Name)
	createVisible(t, ctx, cl, policy)

	objects := slices.Concat(roots, []client.Object{vc, crossNamespaceVirtualCluster, listenerPolicy, policy})
	requireNoKonnectStatus(t, ctx, cl, 5*time.Second, objects...)
}

// TestEventGatewayChildrenCreatedBeforeTheirOnPremAncestor verifies that the
// conditions the Konnect reconciler sets on children whose parent chain is
// not complete yet are removed once it turns out to end at an
// OnPremEventGateway.
func TestEventGatewayChildrenCreatedBeforeTheirOnPremAncestor(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cl, ns := startEventGatewayReconcilers(t, ctx)

	t.Log("Creating a virtual cluster and a produce policy before their backend cluster")
	vc := virtualCluster(t, ns.Name, "virtual-cluster", `{name: backend}`)
	createVisible(t, ctx, cl, vc)
	policy := producePolicy(t, ns.Name, "produce-policy", vc.Name)
	createVisible(t, ctx, cl, policy)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NotEmpty(ct, konnectStatusOf(ctx, cl, vc), "the virtual cluster must report its missing backend cluster")
		assert.NotEmpty(ct, konnectStatusOf(ctx, cl, policy), "the policy must report its virtual cluster as not programmed")
	}, consts.WaitTime, consts.TickTime)

	t.Log("Creating the backend cluster, attached to an OnPremEventGateway")
	backendCluster := onPremBackendCluster(t, ns.Name, "backend")
	createVisible(t, ctx, cl, backendCluster)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Empty(ct, konnectStatusOf(ctx, cl, vc))
		assert.Empty(ct, konnectStatusOf(ctx, cl, policy))
	}, consts.WaitTime, consts.TickTime, "the Konnect conditions must be removed")
	requireNoKonnectStatus(t, ctx, cl, 3*time.Second, backendCluster, vc, policy)
}

// TestEventGatewayKonnectPolicyReferencingOnPremEntity verifies that a Konnect
// entity referencing an entity attached to an OnPremEventGateway, which never
// gets a Konnect ID, reports the reference as invalid instead of waiting for
// it, and is not created in Konnect. The referenced entity, enqueued into the
// Konnect reconciler by the reverse watch of the referencing one, is left
// alone and causes no reconciliation error.
func TestEventGatewayKonnectPolicyReferencingOnPremEntity(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	// No SDK call is expected: the mocks fail the test on any call. The
	// listener reconciler is not started: the listener's status is set by hand.
	factory := sdkmocks.NewMockSDKFactory(t)
	envtest.StartReconcilers(ctx, t, mgr, logs,
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayListenerPolicy](factory, mgr),
		newKonnectEntityReconciler[configurationv1alpha1.EventGatewayTLSTrustBundle](factory, mgr),
	)
	cl := mgr.GetClient()
	clientNamespaced := client.NewNamespacedClient(cl, ns.Name)

	t.Log("Creating a programmed KonnectEventGateway and a programmed listener under it")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	eventGateway := deploy.KonnectEventGateway(t, ctx, clientNamespaced, apiAuth)
	const gatewayID = "gateway-12345"
	envtest.UpdateKonnectEventGatewayStatusWithProgrammed(t, ctx, clientNamespaced, eventGateway, gatewayID)

	listener := objectFromYAML[configurationv1alpha1.EventGatewayListener](t, ns.Name, `
metadata: {name: konnect-listener}
spec:
  gatewayRef: {type: namespacedRef, namespacedRef: {name: `+eventGateway.Name+`}}
  apiSpec: {name: konnect-listener, addresses: ["0.0.0.0"], ports: [9092]}
`)
	require.NoError(t, cl.Create(ctx, listener))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(listener), listener)) {
			return
		}
		listener.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(listener.GetGeneration())}
		listener.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
			ID:        "listener-12345",
			ServerURL: sdkmocks.SDKServerURL,
			OrgID:     "org-id",
		}
		listener.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: gatewayID}
		assert.NoError(ct, cl.Status().Update(ctx, listener))
	}, consts.WaitTime, consts.TickTime)

	t.Log("Creating a TLS trust bundle attached to an OnPremEventGateway, and a listener policy referencing it")
	trustBundle := onPremTLSTrustBundle(t, ns.Name, "onprem-trust-bundle")
	createVisible(t, ctx, cl, trustBundle)
	policy := objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, ns.Name, `
metadata: {name: listener-policy}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: konnect-listener}}
  apiSpec:
    type: tlsServer
    tlsServer:
      name: tls-policy
      config:
        certificates:
          - certificate: {type: inline, value: cert-pem-data}
            key: {type: inline, value: key-pem-data}
        clientAuthentication:
          mode: required
          tlsTrustBundles:
            - namespacedRef: {name: onprem-trust-bundle}
`)
	require.NoError(t, cl.Create(ctx, policy))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(policy), policy)) {
			return
		}
		cond := meta.FindStatusCondition(policy.Status.Conditions, configurationv1alpha1.KonnectReferencesResolvedConditionType)
		if !assert.NotNil(ct, cond, "KonnectReferencesResolved condition not set yet") {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, configurationv1alpha1.KonnectReferencesResolvedReasonInvalid, cond.Reason)
		assert.Contains(ct, cond.Message, "EventGatewayTLSTrustBundle "+ns.Name+"/onprem-trust-bundle is not attached to a Konnect gateway")
		assert.Empty(ct, policy.GetKonnectID(), "the policy must not be created in Konnect")
	}, consts.WaitTime, consts.TickTime)

	t.Log("Checking the referenced trust bundle is left alone")
	requireNoKonnectStatus(t, ctx, cl, 3*time.Second, trustBundle)
	for _, entry := range logs.All() {
		if entry.Message != "Reconciler error" {
			continue
		}
		fields := entry.ContextMap()
		assert.NotEqual(t, "EventGatewayTLSTrustBundle", fields["controller"], "unexpected reconciliation error: %v", fields["error"])
	}
}
