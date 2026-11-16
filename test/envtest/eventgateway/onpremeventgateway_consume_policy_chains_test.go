package eventgateway

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
	kgomanager "github.com/kong/kong-operator/v2/modules/manager"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

type consumeChainFixture struct {
	t        *testing.T
	cfg      *rest.Config
	cl       client.WithWatch
	admin    *testGWAdmin
	pod      *fakeGWAdmin
	gateway  *eventgatewayv1alpha1.OnPremEventGateway
	vc       *configurationv1alpha1.EventGatewayVirtualCluster
	chain    *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain
	policies map[string]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
}

func newConsumeChainFixture(t *testing.T) *consumeChainFixture {
	t.Helper()
	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	cl, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	f := &consumeChainFixture{t: t, cfg: cfg, cl: cl, policies: map[string]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{}}
	f.admin = newTestGWAdmin(t, cl, ns.Name)
	f.pod = f.admin.provision(ctx)
	f.gateway = objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	backend := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, backendClusterYAML("backend", "backend", "gateway"))
	f.vc = objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	f.chain = objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](t, ns.Name, `
metadata: {name: consume-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: parse}, {name: mask}, {name: headers}, {name: other}, {name: skip}, {name: root}]
`)
	for _, obj := range []client.Object{f.gateway, backend, f.vc, f.chain} {
		require.NoError(t, cl.Create(ctx, obj))
	}
	return f
}

// start runs only the on-prem controller, without periodic resync or Konnect
// controllers, and allows a full stop before starting a fresh reconciler.
func (f *consumeChainFixture) start() func() {
	f.t.Helper()
	ctx, cancel := context.WithCancel(f.t.Context())
	mgr, logs := newManagerWithoutResync(f.t, ctx, f.cfg)
	require.NoError(f.t, kgomanager.SetupCacheIndexes(ctx, mgr, kgomanager.Config{OnPremEventGatewayControllerEnabled: true}))
	require.NoError(f.t, f.admin.reconciler(ctx, mgr).SetupWithManager(ctx, mgr))
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	stop := sync.OnceFunc(func() {
		cancel()
		require.NoError(f.t, <-done)
		envtest.DumpLogsIfTestFailed(f.t, logs)
	})
	f.t.Cleanup(stop)
	require.True(f.t, mgr.GetCache().WaitForCacheSync(ctx))
	return stop
}

func (f *consumeChainFixture) createPolicies() {
	f.t.Helper()
	specs := map[string]string{
		"parse": `type: schemaValidation
    schemaValidation: {name: parse, config: {type: json, json: {valueValidationAction: skip}}}`,
		"other": `type: schemaValidation
    schemaValidation: {name: other, config: {type: json, json: {valueValidationAction: skip}}}`,
		"headers": `type: modifyHeaders
    parentPolicyRef: {name: parse}
    modifyHeaders: {name: headers, config: {actions: [{op: remove, remove: {key: x-chain}}]}}`,
		"mask": `type: maskFields
    parentPolicyRef: {name: parse}
    maskFields:
      name: mask
      config:
        failureMode: skip
        maskFields:
        - paths: {type: array, array: [{match: email}]}
          strategy: {type: replace, replace: {phrase: redacted}}`,
		"skip": `type: skipRecord
    parentPolicyRef: {name: other}
    skipRecord: {name: skip, condition: "true"}`,
		"root": `type: modifyHeaders
    modifyHeaders: {name: root, config: {actions: [{op: remove, remove: {key: x-root}}]}}`,
	}
	// Children precede their parents in creation order.
	for _, name := range []string{"headers", "mask", "skip", "root", "parse", "other"} {
		p := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](f.t, f.vc.Namespace, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    `+specs[name])
		require.NoError(f.t, f.cl.Create(f.t.Context(), p))
		require.Empty(f.t, p.GetKonnectID())
		require.Empty(f.t, p.Status.Conditions, "no prior Konnect readiness is supplied")
		f.policies[name] = p
	}
}

func (f *consumeChainFixture) condition(obj interface {
	client.Object
	GetConditions() []metav1.Condition
}, typ string, status metav1.ConditionStatus, reason string) {
	f.t.Helper()
	require.EventuallyWithT(f.t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, f.cl.Get(f.t.Context(), client.ObjectKeyFromObject(obj), obj)) {
			return
		}
		got := apimeta.FindStatusCondition(obj.GetConditions(), typ)
		if assert.NotNil(ct, got) {
			assert.Equal(ct, status, got.Status, got.Message)
			assert.Equal(ct, reason, got.Reason, got.Message)
			assert.Equal(ct, obj.GetGeneration(), got.ObservedGeneration)
		}
	}, waitTime, tickTime)
}

// conditionMessage waits for the Programmed condition of the object to become
// False with reason Invalid and a message containing the text. An invalid
// chain is reconciled again, so an intermediate message, e.g. while the
// configuration snapshot catches up, isn't the final one.
func (f *consumeChainFixture) conditionMessage(obj interface {
	client.Object
	GetConditions() []metav1.Condition
}, message string) {
	f.t.Helper()
	require.EventuallyWithT(f.t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, f.cl.Get(f.t.Context(), client.ObjectKeyFromObject(obj), obj)) {
			return
		}
		got := apimeta.FindStatusCondition(obj.GetConditions(), "Programmed")
		if assert.NotNil(ct, got) {
			assert.Equal(ct, metav1.ConditionFalse, got.Status, got.Message)
			assert.Equal(ct, "Invalid", got.Reason, got.Message)
			assert.Equal(ct, obj.GetGeneration(), got.ObservedGeneration)
			assert.Contains(ct, got.Message, message)
		}
	}, waitTime, tickTime)
}

func (f *consumeChainFixture) order(pod *fakeGWAdmin, names ...string) fakeGateway {
	f.t.Helper()
	var held fakeGateway
	require.EventuallyWithT(f.t, func(ct *assert.CollectT) {
		var ok bool
		held, ok, _ = pod.state(gatewayID(ct, f.gateway))
		if !assert.True(ct, ok) || !assert.Len(ct, held.entities.VirtualClusters, 1) {
			return
		}
		policies := held.entities.VirtualClusters[0].ConsumePolicies
		got := make([]string, 0, len(policies))
		for _, entity := range policies {
			got = append(got, entity.Id.String())
			raw, err := json.Marshal(entity)
			if !assert.NoError(ct, err) {
				continue
			}
			var fields struct {
				Name   string `json:"name"`
				Parent string `json:"parent_policy_id"`
			}
			if !assert.NoError(ct, json.Unmarshal(raw, &fields)) {
				continue
			}
			wantParent := ""
			var p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
			for _, candidate := range f.policies {
				if string(candidate.UID) == entity.Id.String() {
					p = candidate
					break
				}
			}
			if assert.NotNil(ct, p) {
				if parent := p.Spec.APISpec.ParentPolicyRef.Name; parent != "" {
					wantParent = string(f.policies[parent].UID)
				}
			}
			assert.Equal(ct, wantParent, fields.Parent, "parent_policy_id of %s", fields.Name)
		}
		want := make([]string, 0, len(names))
		for _, name := range names {
			want = append(want, string(f.policies[name].UID))
		}
		assert.Equal(ct, want, got, "the flat consume_policies array is ordered, not a set")
	}, waitTime, tickTime)
	return held
}

func (f *consumeChainFixture) editChain(names ...string) {
	f.t.Helper()
	require.NoError(f.t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := f.cl.Get(f.t.Context(), client.ObjectKeyFromObject(f.chain), f.chain); err != nil {
			return err
		}
		f.chain.Spec.APISpec.Policies = make([]configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef, 0, len(names))
		for _, name := range names {
			f.chain.Spec.APISpec.Policies = append(f.chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: name})
		}
		return f.cl.Update(f.t.Context(), f.chain)
	}))
}

func (f *consumeChainFixture) editPolicy(name string, edit func(*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy)) {
	f.t.Helper()
	p := f.policies[name]
	require.NoError(f.t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := f.cl.Get(f.t.Context(), client.ObjectKeyFromObject(p), p); err != nil {
			return err
		}
		edit(p)
		return f.cl.Update(f.t.Context(), p)
	}))
}

func (f *consumeChainFixture) defaultOrder() []string {
	policies := make([]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, 0, len(f.policies))
	for _, p := range f.policies {
		policies = append(policies, p)
	}
	slices.SortFunc(policies, func(a, b *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) int {
		return cmp.Or(a.CreationTimestamp.Compare(b.CreationTimestamp.Time), cmp.Compare(a.Name, b.Name))
	})
	remaining := policies
	added := map[string]bool{}
	names := make([]string, 0, len(remaining))
	// A root opens its group: its children follow it before the next root, as
	// gw-admin requires a parent's children to stay contiguous.
	var addChildren func(parent string)
	addChildren = func(parent string) {
		for {
			i := slices.IndexFunc(remaining, func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) bool {
				return p.Spec.APISpec.ParentPolicyRef.Name == parent
			})
			if i < 0 {
				return
			}
			p := remaining[i]
			remaining = slices.Delete(remaining, i, i+1)
			names = append(names, p.Name)
			added[p.Name] = true
			addChildren(p.Name)
		}
	}
	for len(remaining) > 0 {
		i := slices.IndexFunc(remaining, func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) bool {
			parent := p.Spec.APISpec.ParentPolicyRef.Name
			return parent == "" || added[parent]
		})
		require.GreaterOrEqual(f.t, i, 0, "policies must form an acyclic parent group")
		p := remaining[i]
		remaining = slices.Delete(remaining, i, i+1)
		names = append(names, p.Name)
		added[p.Name] = true
		addChildren(p.Name)
	}
	return names
}

func TestOnPremEventGatewayConsumePolicyChainLifecycle(t *testing.T) {
	t.Parallel()
	f := newConsumeChainFixture(t)
	f.start()
	f.conditionMessage(f.chain, "does not exist")
	f.editChain()
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	f.order(f.pod)
	f.createPolicies()
	f.condition(f.chain, "Programmed", metav1.ConditionFalse, "Invalid")
	f.editChain("parse", "mask", "headers", "other", "skip", "root")
	f.condition(f.chain, "PolicyChainOwnerValid", metav1.ConditionTrue, "Owner")
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	first := f.order(f.pod, "parse", "mask", "headers", "other", "skip", "root")
	require.Empty(t, f.chain.GetKonnectID(), "a chain has no independent remote ID")
	f.condition(f.gateway, "Programmed", metav1.ConditionTrue, "Programmed")
	hash := f.gateway.Status.ConfigHash

	f.editChain("root", "other", "skip", "parse", "headers", "mask")
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	second := f.order(f.pod, "root", "other", "skip", "parse", "headers", "mask")
	require.Greater(t, second.version.DataVersion, first.version.DataVersion)
	f.condition(f.gateway, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NotEqual(t, hash, f.gateway.Status.ConfigHash, "whole-group and sibling moves change the config hash")

	f.editPolicy("headers", func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) {
		p.Spec.APISpec.ModifyHeadersPolicyCreate.Name = "changed-headers"
	})
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		g, ok, _ := f.pod.state(gatewayID(ct, f.gateway))
		if !assert.True(ct, ok) || !assert.Len(ct, g.entities.VirtualClusters, 1) || !assert.Len(ct, g.entities.VirtualClusters[0].ConsumePolicies, 6) {
			return
		}
		entity, err := g.entities.VirtualClusters[0].ConsumePolicies[4].AsEventGatewayModifyHeadersPolicyCreate()
		if assert.NoError(ct, err) && assert.NotNil(ct, entity.Name) {
			assert.Equal(ct, "changed-headers", *entity.Name, "policy changes propagate without resync")
		}
	}, waitTime, tickTime)
	// Drift changes only sibling order, not membership or policy contents.
	writes := f.pod.writeCount()
	f.pod.mu.Lock()
	held := f.pod.gateways[gatewayID(t, f.gateway)]
	version := held.version
	entities := held.entities
	entities.VirtualClusters = slices.Clone(entities.VirtualClusters)
	policies := slices.Clone(entities.VirtualClusters[0].ConsumePolicies)
	policies[4], policies[5] = policies[5], policies[4]
	entities.VirtualClusters[0].ConsumePolicies = policies
	held.entities = entities
	f.pod.mu.Unlock()
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.gateway), f.gateway); err != nil {
			return err
		}
		old := f.gateway.DeepCopy()
		f.gateway.Annotations = map[string]string{"test.konghq.com/reconcile": "drift"}
		return f.cl.Patch(t.Context(), f.gateway, client.MergeFrom(old))
	}))
	repaired := f.order(f.pod, "root", "other", "skip", "parse", "headers", "mask")
	require.Equal(t, version, repaired.version, "drift repair does not change the desired revision")
	require.Greater(t, f.pod.writeCount(), writes, "same-set order drift must be repaired")

	for _, name := range []string{"mask", "parse"} {
		t.Run("recreate "+name, func(t *testing.T) {
			f := *f
			f.t = t
			if name == "parse" {
				// Referenced parents are protected while a running document
				// still uses them. Move their children before recreating one.
				for _, child := range []string{"headers", "mask"} {
					f.editPolicy(child, func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) {
						p.Spec.APISpec.ParentPolicyRef.Name = "other"
					})
				}
				f.editChain("root", "parse", "other", "headers", "mask", "skip")
				f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
				f.order(f.pod, "root", "parse", "other", "headers", "mask", "skip")
			}
			old := f.policies[name]
			require.NoError(t, f.cl.Delete(t.Context(), old))
			require.Eventually(t, func() bool {
				return apierrors.IsNotFound(f.cl.Get(t.Context(), client.ObjectKeyFromObject(old), &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{}))
			}, waitTime, tickTime)
			f.condition(f.chain, "Programmed", metav1.ConditionFalse, "Invalid")
			recreated := old.DeepCopy()
			recreated.UID, recreated.ResourceVersion, recreated.Generation = "", "", 0
			recreated.CreationTimestamp = metav1.Time{}
			recreated.ManagedFields, recreated.Status.Conditions = nil, nil
			require.NoError(t, f.cl.Create(t.Context(), recreated))
			require.NotEqual(t, old.UID, recreated.UID)
			f.policies[name] = recreated
			f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
			if name == "parse" {
				for _, child := range []string{"headers", "mask"} {
					f.editPolicy(child, func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) {
						p.Spec.APISpec.ParentPolicyRef.Name = "parse"
					})
				}
				f.editChain("root", "other", "skip", "parse", "headers", "mask")
				f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
			}
			f.order(f.pod, "root", "other", "skip", "parse", "headers", "mask")
		})
	}

	require.NoError(t, f.cl.Delete(t.Context(), f.chain))
	f.condition(f.gateway, "Programmed", metav1.ConditionTrue, "Programmed")
	f.order(f.pod, f.defaultOrder()...)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if assert.NoError(ct, f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.vc), f.vc)) {
			assert.NotContains(ct, f.vc.Annotations, policychain.ConsumeOwnerAnnotation)
		}
	}, waitTime, tickTime)
	for _, p := range f.policies {
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(p), p), "chain deletion preserves policy resources")
		require.Empty(t, p.GetKonnectID(), "on-prem programming never writes remote policy IDs")
	}
}

func TestOnPremEventGatewayConsumePolicyChainInvalidAndRestart(t *testing.T) {
	t.Parallel()
	f := newConsumeChainFixture(t)
	stop := f.start()
	f.createPolicies()
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	good := f.order(f.pod, "parse", "mask", "headers", "other", "skip", "root")

	for _, tt := range []struct {
		name, message string
		order         []string
	}{
		{"incomplete", "root is missing", []string{"parse", "mask", "headers", "other", "skip"}},
		{"empty nonempty scope", "must list every consume policy", []string{}},
		{"missing", "does not exist", []string{"missing", "mask", "headers", "other", "skip", "root"}},
		{"duplicate", "duplicates policy", []string{"parse", "mask", "mask", "other", "skip", "root"}},
		{"split group", "contiguous group", []string{"parse", "mask", "other", "skip", "headers", "root"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := *f
			f.t = t
			f.editChain(tt.order...)
			f.conditionMessage(f.chain, tt.message)
			f.condition(f.gateway, "ConfigurationPushed", metav1.ConditionFalse, "InvalidPolicyChain")
			held, ok, _ := f.pod.state(gatewayID(t, f.gateway))
			require.True(t, ok)
			require.Equal(t, good.entities, held.entities, "an invalid chain retains the whole last-good document")
			require.Equal(t, good.version, held.version)
			f.editChain("parse", "mask", "headers", "other", "skip", "root")
			f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
		})
	}
	for _, invalid := range []string{"omitted policies", "wrong kind"} {
		t.Run(invalid, func(t *testing.T) {
			obj := f.chain.DeepCopy()
			if invalid == "omitted policies" {
				obj.Spec.APISpec.Policies = nil
			} else {
				obj.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterProducePolicy"
			}
			err := f.cl.Update(t.Context(), obj)
			require.True(t, apierrors.IsInvalid(err), "admission must reject %s: %v", invalid, err)
		})
	}

	// A referenced policy moving to another schema group invalidates the
	// declaration until that whole group is made contiguous again.
	f.editPolicy("headers", func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) {
		p.Spec.APISpec.ParentPolicyRef.Name = "other"
	})
	f.conditionMessage(f.chain, "contiguous group")
	f.editChain("parse", "mask", "other", "headers", "skip", "root")
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	f.order(f.pod, "parse", "mask", "other", "headers", "skip", "root")

	// Programmed virtual-cluster references are immutable at admission.
	otherVC := f.vc.DeepCopy()
	otherVC.Name, otherVC.UID, otherVC.ResourceVersion = "other-vc", "", ""
	otherVC.Spec.APISpec.Name, otherVC.Spec.APISpec.DNSLabel = "other-vc", "other-vc"
	otherVC.Annotations, otherVC.ManagedFields = nil, nil
	require.NoError(t, f.cl.Create(t.Context(), otherVC))
	require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.policies["root"]), f.policies["root"]))
	root := f.policies["root"].DeepCopy()
	root.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = otherVC.Name
	require.True(t, apierrors.IsInvalid(f.cl.Update(t.Context(), root)))
	root.Name, root.UID, root.ResourceVersion = "foreign-root", "", ""
	root.ManagedFields = nil
	root.Spec.APISpec.ModifyHeadersPolicyCreate.Name = "foreign-root"
	require.NoError(t, f.cl.Create(t.Context(), root))
	f.editChain("parse", "mask", "other", "headers", "skip", "foreign-root")
	f.conditionMessage(f.chain, "different virtual cluster")
	f.editChain("parse", "mask", "other", "headers", "skip", "root")
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, f.cl.Delete(t.Context(), root))
	require.NoError(t, f.cl.Delete(t.Context(), otherVC))
	good = f.order(f.pod, "parse", "mask", "other", "headers", "skip", "root")

	f.editPolicy("mask", func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) {
		p.Finalizers = append(p.Finalizers, "test.konghq.com/hold")
	})
	require.NoError(t, f.cl.Delete(t.Context(), f.policies["mask"]))
	f.conditionMessage(f.chain, "being deleted")
	f.condition(f.gateway, "Programmed", metav1.ConditionFalse, "InvalidPolicyChain")
	held, ok, _ := f.pod.state(gatewayID(t, f.gateway))
	require.True(t, ok)
	require.Equal(t, good.entities, held.entities)
	f.editPolicy("root", func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) {
		p.Spec.APISpec.ModifyHeadersPolicyCreate.Name = "pending-root"
	})

	// Restart the operator with only the surviving pod as last-good storage. A
	// pod started while the chain is invalid is still registered, but receives
	// no entity document until the whole declaration is valid again.
	stop()
	f.start()
	f.editChain("parse", "mask", "other", "headers", "skip")
	f.condition(f.chain, "Programmed", metav1.ConditionFalse, "Invalid")
	f.condition(f.gateway, "Programmed", metav1.ConditionFalse, "InvalidPolicyChain")
	fresh := f.admin.startPod(t.Context(), "gw-admin-new", "10.0.0.2")
	id := gatewayID(t, f.gateway)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		g, ok, expected := fresh.state(id)
		if assert.True(ct, ok, "the restarted pod must be registered") {
			assert.Empty(ct, g.entities.VirtualClusters, "an invalid chain must not be pushed as a partial document")
		}
		assert.Contains(ct, expected, id)
	}, waitTime, tickTime)
	writes := fresh.writeCount()
	require.Never(t, func() bool {
		g, _, _ := fresh.state(id)
		return len(g.entities.VirtualClusters) != 0 || fresh.writeCount() != writes
	}, time.Second, tickTime, "invalid ownership must not push entities to the new pod")
	held, ok, _ = f.pod.state(id)
	require.True(t, ok)
	require.Equal(t, good.entities, held.entities)
	require.Equal(t, good.version, held.version)

	old := f.policies["mask"].DeepCopy()
	f.editPolicy("mask", func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) { p.Finalizers = nil })
	require.Eventually(t, func() bool {
		return apierrors.IsNotFound(f.cl.Get(t.Context(), client.ObjectKeyFromObject(old), &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{}))
	}, waitTime, tickTime)
	old.UID, old.ResourceVersion, old.Generation = "", "", 0
	old.CreationTimestamp, old.DeletionTimestamp = metav1.Time{}, nil
	old.Finalizers, old.ManagedFields, old.Status.Conditions = nil, nil, nil
	require.NoError(t, f.cl.Create(t.Context(), old))
	f.policies["mask"] = old
	f.editChain("parse", "mask", "other", "headers", "skip", "root")
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	f.condition(f.gateway, "Programmed", metav1.ConditionTrue, "Programmed")
	for _, pod := range []*fakeGWAdmin{f.pod, fresh} {
		g := f.order(pod, "parse", "mask", "other", "headers", "skip", "root")
		root, err := g.entities.VirtualClusters[0].ConsumePolicies[5].AsEventGatewayModifyHeadersPolicyCreate()
		require.NoError(t, err)
		require.NotNil(t, root.Name)
		require.Equal(t, "pending-root", *root.Name, "recovery applies pending changes only with the complete valid chain")
	}
}

func TestOnPremEventGatewayConsumePolicyChainOwnership(t *testing.T) {
	t.Parallel()
	f := newConsumeChainFixture(t)
	f.createPolicies()
	cluster := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, f.vc.Namespace, `
metadata: {name: cluster-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec: {policies: []}
`)
	produce := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, f.vc.Namespace, `
metadata: {name: produce-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec: {policies: []}
`)
	listener := objectFromYAML[configurationv1alpha1.EventGatewayListener](t, f.vc.Namespace, `
metadata: {name: listener}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: listener
    addresses: ["0.0.0.0"]
    ports: [19092]
`)
	listenerChain := objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicyChain](t, f.vc.Namespace, `
metadata: {name: listener-chain}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec: {policies: []}
`)
	for _, obj := range []client.Object{cluster, produce, listener, listenerChain} {
		require.NoError(t, f.cl.Create(t.Context(), obj))
	}
	// A UID-bound stale claim cannot stop the actual chain acquiring ownership.
	f.vc.Annotations = map[string]string{policychain.ConsumeOwnerAnnotation: "consume-chain/" + uuid.NewString()}
	require.NoError(t, f.cl.Update(t.Context(), f.vc))
	f.start()
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	f.condition(cluster, "Programmed", metav1.ConditionTrue, "Programmed")
	f.condition(produce, "Programmed", metav1.ConditionTrue, "Programmed")
	f.condition(listenerChain, "Programmed", metav1.ConditionTrue, "Programmed")
	f.order(f.pod, "parse", "mask", "headers", "other", "skip", "root")
	require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.vc), f.vc))
	require.Equal(t, policychain.ClaimValue(f.chain), f.vc.Annotations[policychain.ConsumeOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), f.vc.Annotations[policychain.OwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(produce), f.vc.Annotations[policychain.ProduceOwnerAnnotation])

	competitor := f.chain.DeepCopy()
	competitor.Name, competitor.UID, competitor.ResourceVersion = "competitor", "", ""
	competitor.ManagedFields = nil
	slices.Reverse(competitor.Spec.APISpec.Policies)
	require.NoError(t, f.cl.Create(t.Context(), competitor))
	f.condition(competitor, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	f.condition(competitor, "Programmed", metav1.ConditionFalse, "Invalid")
	f.condition(f.chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.chain), f.chain); err != nil {
			return err
		}
		f.chain.Finalizers = []string{"test.konghq.com/hold"}
		return f.cl.Update(t.Context(), f.chain)
	}))
	require.NoError(t, f.cl.Delete(t.Context(), f.chain))
	f.condition(competitor, "PolicyChainOwnerValid", metav1.ConditionTrue, "Owner")
	f.condition(competitor, "Programmed", metav1.ConditionFalse, "Invalid")
	f.condition(f.gateway, "Programmed", metav1.ConditionFalse, "InvalidPolicyChain")
	f.chain = competitor
	f.editChain("root", "other", "skip", "parse", "headers", "mask")
	f.condition(competitor, "Programmed", metav1.ConditionTrue, "Programmed")
	f.order(f.pod, "root", "other", "skip", "parse", "headers", "mask")
	require.NoError(t, f.cl.Delete(t.Context(), competitor))
	f.condition(f.gateway, "Programmed", metav1.ConditionTrue, "Programmed")
	f.order(f.pod, f.defaultOrder()...)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if assert.NoError(ct, f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.vc), f.vc)) {
			assert.NotContains(ct, f.vc.Annotations, policychain.ConsumeOwnerAnnotation)
			assert.Equal(ct, policychain.ClaimValue(cluster), f.vc.Annotations[policychain.OwnerAnnotation])
			assert.Equal(ct, policychain.ClaimValue(produce), f.vc.Annotations[policychain.ProduceOwnerAnnotation])
		}
	}, waitTime, tickTime)
	f.condition(cluster, "Programmed", metav1.ConditionTrue, "Programmed")
	f.condition(produce, "Programmed", metav1.ConditionTrue, "Programmed")
	f.condition(listenerChain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(listener), listener))
	require.Equal(t, policychain.ClaimValue(listenerChain), listener.Annotations[policychain.ListenerOwnerAnnotation])
}

func TestOnPremEventGatewayConsumePolicyChainClaimCAS(t *testing.T) {
	t.Parallel()
	f := newConsumeChainFixture(t)
	competitor := f.chain.DeepCopy()
	competitor.Name, competitor.UID, competitor.ResourceVersion = "competitor", "", ""
	require.NoError(t, f.cl.Create(t.Context(), competitor))
	// Both claimants read the same API-server resourceVersion before either
	// optimistic-lock patch is sent.
	ready := make(chan struct{}, 2)
	proceed := make(chan struct{})
	cl := interceptor.NewClient(f.cl, interceptor.Funcs{
		Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			ready <- struct{}{}
			select {
			case <-proceed:
				return cl.Patch(ctx, obj, patch, opts...)
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	type outcome struct {
		chain *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain
		err   error
	}
	results := make(chan outcome, 2)
	for _, chain := range []*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{f.chain, competitor} {
		go func() {
			_, err := policychain.Claim(t.Context(), cl, chain)
			results <- outcome{chain: chain, err: err}
		}()
	}
	require.Eventually(t, func() bool { return len(ready) == 2 }, waitTime, tickTime)
	close(proceed)
	first, second := <-results, <-results
	if first.err != nil {
		first, second = second, first
	}
	require.NoError(t, first.err)
	require.True(t, apierrors.IsConflict(second.err), "the losing claim must conflict, not overwrite: %v", second.err)
	owner, err := policychain.Claim(t.Context(), f.cl, second.chain)
	require.NoError(t, err)
	require.Equal(t, first.chain.Name, owner)
	require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.vc), f.vc))
	require.Equal(t, policychain.ClaimValue(first.chain), f.vc.Annotations[policychain.ConsumeOwnerAnnotation])
}
