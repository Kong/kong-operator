package onprem

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
)

func consumePolicyChainObjects(t *testing.T) (
	*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain,
	map[string]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, []client.Object,
) {
	t.Helper()
	gateway, _, _, base := producePolicyChainObjects(t)
	objects := slices.Clone(base[:3])
	created := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	specs := map[string]string{
		"parse": `type: schemaValidation
schemaValidation: {name: parse, config: {type: json, json: {valueValidationAction: skip}}}`,
		"other": `type: schemaValidation
schemaValidation: {name: other, config: {type: json, json: {valueValidationAction: skip}}}`,
		"headers": `type: modifyHeaders
parentPolicyRef: {name: parse}
modifyHeaders: {name: headers, config: {actions: [{op: set, set: {key: x-chain, value: parsed}}]}}`,
		"decrypt": `type: decryptFields
parentPolicyRef: {name: parse}
decryptFields: {name: decrypt, config: {failureMode: skip, decryptFields: {paths: {type: array, array: [{match: secret}]}}, keySources: [{type: static, static: {}}]}}`,
		"mask": `type: maskFields
parentPolicyRef: {name: parse}
maskFields: {name: mask, config: {failureMode: skip, maskFields: [{paths: {type: array, array: [{match: email}]}, strategy: {type: replace, replace: {phrase: redacted}}}]}}`,
		"transcode": `type: transcode
parentPolicyRef: {name: parse}
transcode:
  name: transcode
  config:
    outputFormat: json
    json:
      failureMode: skip
      schemaSource:
        type: reference
        reference:
          subject: orders-value
          version: latest
          schemaRefDestination: {type: none, none: {}}
          schemaRegistry: {name: orders}`,
		"skip": `type: skipRecord
parentPolicyRef: {name: other}
skipRecord: {name: skip, condition: "true"}`,
		"root": `type: decrypt
decrypt: {name: root, config: {failureMode: passthrough, partOfRecord: [value], keySources: [{type: static, static: {}}]}}`,
	}
	policies := make(map[string]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, len(specs))
	// Children are deliberately older than their parents.
	for i, name := range []string{"headers", "decrypt", "mask", "transcode", "skip", "parse", "other", "root"} {
		p := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](t, created.Add(time.Duration(i)*time.Minute), `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
`+"    "+strings.ReplaceAll(specs[name], "\n", "\n    "))
		policies[name] = p
		objects = append(objects, p)
	}
	registry := fromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, created, `
metadata: {name: orders}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    type: confluent
    confluent: {name: orders, config: {endpoint: "https://registry.example.com", schemaType: json}}
`)
	_, key, secret, _ := staticKeyObjects(t)
	objects = append(objects, registry, key, secret)
	chain := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](t, created, `
metadata: {name: consume-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies:
    - {name: other}
    - {name: skip}
    - {name: parse}
    - {name: transcode}
    - {name: mask}
    - {name: decrypt}
    - {name: headers}
    - {name: root}
`)
	return gateway, chain, policies, objects
}

func TestBuildConsumePolicyChainOrder(t *testing.T) {
	gateway, chain, policies, objects := consumePolicyChainObjects(t)
	cl := newFakeClient(t, append(objects, chain)...)
	build := func() configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		return cfg
	}
	cfg := build()
	require.Equal(t, []uuid.UUID{uuid.MustParse(string(objects[2].GetUID()))}, cfg.orderedConsumePolicies)
	require.Empty(t, cfg.orderedPolicies)
	require.Empty(t, cfg.orderedProducePolicies)
	require.Len(t, cfg.entities.VirtualClusters[0].ConsumePolicies, len(policies))
	for i, ref := range chain.Spec.APISpec.Policies {
		p := policies[ref.Name]
		entity := cfg.entities.VirtualClusters[0].ConsumePolicies[i]
		require.Equal(t, string(p.UID), entity.Id.String())
		require.Equal(t, "EventGatewayVirtualClusterConsumePolicy "+ns+"/"+p.Name,
			cfg.objects["virtual_clusters.0.consume_policies."+strconv.Itoa(i)])
		fields, err := jsonValue(entity)
		require.NoError(t, err)
		payload := fields.(map[string]any)
		if parent := p.Spec.APISpec.ParentPolicyRef.Name; parent != "" {
			require.Equal(t, string(policies[parent].UID), payload["parent_policy_id"])
		} else {
			require.Nil(t, payload["parent_policy_id"], "root decrypt remains a root")
		}
		require.Empty(t, p.GetKonnectID())
		require.Empty(t, p.Status.Conditions, "prior Programmed status is not required")
	}

	chain = read(t, cl, chain)
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{
		{Name: "root"}, {Name: "parse"}, {Name: "headers"}, {Name: "decrypt"},
		{Name: "mask"}, {Name: "transcode"}, {Name: "other"}, {Name: "skip"},
	}
	require.NoError(t, cl.Update(t.Context(), chain))
	reordered := build()
	require.Equal(t, []string{"root", "parse", "headers", "decrypt", "mask", "transcode", "other", "skip"},
		consumeEntityNames(t, reordered.entities.VirtualClusters[0].ConsumePolicies))
	require.NotEqual(t, cfg.pushedKey, reordered.pushedKey, "sibling and whole-group moves change the comparison key")
	firstHash, err := deriveConfig(gateway, cfg).hash()
	require.NoError(t, err)
	secondHash, err := deriveConfig(gateway, reordered).hash()
	require.NoError(t, err)
	require.NotEqual(t, firstHash, secondHash)

	// Same-name policy recreation resolves the new Kubernetes identity.
	old := policies["root"]
	require.NoError(t, cl.Delete(t.Context(), old))
	recreated := old.DeepCopy()
	recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, cl.Create(t.Context(), recreated))
	reordered = build()
	require.Equal(t, string(recreated.UID), reordered.entities.VirtualClusters[0].ConsumePolicies[0].Id.String())
}

func TestBuildConsumePolicyChainValidation(t *testing.T) {
	type chainType = configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain
	type policiesType = map[string]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
	for _, tt := range []struct {
		name, problem string
		change        func(*chainType, policiesType)
	}{
		{"omitted", "supplied explicitly", func(c *chainType, _ policiesType) { c.Spec.APISpec.Policies = nil }},
		{"empty nonempty scope", "must list every consume policy", func(c *chainType, _ policiesType) {
			c.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}
		}},
		{"partial", "root is missing", func(c *chainType, _ policiesType) { c.Spec.APISpec.Policies = c.Spec.APISpec.Policies[:7] }},
		{"duplicate", "duplicates policy", func(c *chainType, _ policiesType) { c.Spec.APISpec.Policies[1] = c.Spec.APISpec.Policies[0] }},
		{"missing", "does not exist", func(c *chainType, _ policiesType) { c.Spec.APISpec.Policies[0].Name = "missing" }},
		{"wrong kind", "must name an EventGatewayVirtualClusterConsumePolicy", func(c *chainType, _ policiesType) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterProducePolicy"
		}},
		{"terminating", "being deleted", func(_ *chainType, p policiesType) {
			p["headers"].DeletionTimestamp = new(metav1.Now())
			p["headers"].Finalizers = []string{"test.example/hold"}
		}},
		{"wrong virtual cluster", "different virtual cluster", func(_ *chainType, p policiesType) {
			p["headers"].Spec.EventGatewayVirtualClusterRef = namespacedRef("other-vc")
		}},
		{"unassembled", "not in the valid configuration", func(_ *chainType, p policiesType) { p["headers"].UID = "invalid" }},
		{"child before parent", "contiguous group", func(c *chainType, _ policiesType) {
			c.Spec.APISpec.Policies[0], c.Spec.APISpec.Policies[1] = c.Spec.APISpec.Policies[1], c.Spec.APISpec.Policies[0]
		}},
		{"split group", "contiguous group", func(c *chainType, _ policiesType) {
			c.Spec.APISpec.Policies[6], c.Spec.APISpec.Policies[7] = c.Spec.APISpec.Policies[7], c.Spec.APISpec.Policies[6]
		}},
		{"cross-parent child", "contiguous group", func(_ *chainType, p policiesType) {
			p["headers"].Spec.APISpec.ParentPolicyRef.Name = "other"
		}},
		{"cross-namespace policy", "no KongReferenceGrant", func(c *chainType, _ policiesType) {
			c.Spec.APISpec.Policies[0].Namespace = "foreign"
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, chain, policies, objects := consumePolicyChainObjects(t)
			tt.change(chain, policies)
			cfg, err := buildConfiguration(t.Context(), newFakeClient(t, append(objects, chain)...), gateway, "", nil)
			require.NoError(t, err)
			require.NotEmpty(t, cfg.invalidChains)
			require.Contains(t, cfg.invalidChains[0], tt.problem)
			require.Empty(t, cfg.orderedConsumePolicies)
		})
	}
}

func TestBuildConsumePolicyChainOwnership(t *testing.T) {
	gateway, chain, _, objects := consumePolicyChainObjects(t)
	vc := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
	cl := newFakeClient(t, append(objects, chain)...)
	build := func() configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		return cfg
	}
	build()
	cluster := &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{Name: "cluster", Namespace: ns, UID: types.UID(uuid.NewString()),
		Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainSpec{EventGatewayVirtualClusterRef: namespacedRef("vc"),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainAPISpec{Policies: []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}}},
	}
	produce := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{Name: "produce", Namespace: ns, UID: types.UID(uuid.NewString()),
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainSpec{EventGatewayVirtualClusterRef: namespacedRef("vc"),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainAPISpec{Policies: []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}}},
	}
	require.NoError(t, cl.Create(t.Context(), cluster))
	require.NoError(t, cl.Create(t.Context(), produce))
	build()
	parent := read(t, cl, vc)
	require.Equal(t, policychain.ClaimValue(chain), parent.Annotations[policychain.ConsumeOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(produce), parent.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), parent.Annotations[policychain.OwnerAnnotation])

	competitor := chain.DeepCopy()
	competitor.Name, competitor.UID, competitor.ResourceVersion = "competitor", types.UID(uuid.NewString()), ""
	competitor.CreationTimestamp = metav1.NewTime(chain.CreationTimestamp.Add(-time.Hour))
	require.NoError(t, cl.Create(t.Context(), competitor))
	cfg := build()
	require.Len(t, cfg.excluded, 1)
	require.Equal(t, "ConflictingChain", cfg.excluded[0].conditions[0].Reason)
	require.Equal(t, policychain.ClaimValue(chain), read(t, cl, vc).Annotations[policychain.ConsumeOwnerAnnotation])

	chain = read(t, cl, chain)
	chain.Finalizers = []string{"test.example/hold"}
	require.NoError(t, cl.Update(t.Context(), chain))
	require.NoError(t, cl.Delete(t.Context(), chain))
	build()
	require.Equal(t, policychain.ClaimValue(competitor), read(t, cl, vc).Annotations[policychain.ConsumeOwnerAnnotation])
	require.NoError(t, cl.Delete(t.Context(), competitor))
	recreated := competitor.DeepCopy()
	recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, cl.Create(t.Context(), recreated))
	build()
	require.Equal(t, policychain.ClaimValue(recreated), read(t, cl, vc).Annotations[policychain.ConsumeOwnerAnnotation])
	require.NoError(t, cl.Delete(t.Context(), recreated))
	cfg = build()
	require.Empty(t, cfg.orderedConsumePolicies)
	require.Len(t, cfg.entities.VirtualClusters[0].ConsumePolicies, 8, "chain deletion never deletes entities")
	parent = read(t, cl, vc)
	require.NotContains(t, parent.Annotations, policychain.ConsumeOwnerAnnotation)
	require.Equal(t, policychain.ClaimValue(produce), parent.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), parent.Annotations[policychain.OwnerAnnotation])
	require.Equal(t, []string{"parse", "headers", "decrypt", "mask", "transcode", "other", "skip", "root"},
		consumeEntityNames(t, cfg.entities.VirtualClusters[0].ConsumePolicies),
		"ordinary order resumes: each parent group stays contiguous, in creation/name order")
}

func TestConsumePolicyChainUsesAssembledSnapshots(t *testing.T) {
	for _, mutation := range []string{"newer generation", "newer resource version", "same-name recreation", "nested snapshot"} {
		t.Run(mutation, func(t *testing.T) {
			gateway, chain, policies, objects := consumePolicyChainObjects(t)
			if mutation == "nested snapshot" {
				policies["headers"].Spec.APISpec.ParentPolicyRef.Name = "other"
			}
			cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if p, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy); ok && p.Name == "headers" {
						switch mutation {
						case "newer generation":
							p.Generation++
						case "newer resource version":
							p.ResourceVersion += "1"
						case "same-name recreation":
							p.UID = types.UID(uuid.NewString())
						case "nested snapshot":
							p.Spec.APISpec.ParentPolicyRef.Name = "parse"
						}
					}
					return nil
				},
			}, append(objects, chain)...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			require.NotEmpty(t, cfg.invalidChains)
			require.Empty(t, cfg.orderedConsumePolicies, "new reads cannot validate a different assembled payload")
		})
	}
}

func TestConsumePolicyChainParentIdentity(t *testing.T) {
	gateway, chain, policies, objects := consumePolicyChainObjects(t)
	cfg, err := buildConfiguration(t.Context(), newFakeClient(t, append(objects, chain)...), gateway, "", nil)
	require.NoError(t, err)
	var refs []policyRef
	var snapshots []entityWithParent
	for _, ref := range chain.Spec.APISpec.Policies {
		refs = append(refs, policyRef{name: ref.Name})
		snapshots = append(snapshots, policies[ref.Name])
	}
	entities := slices.Clone(cfg.entities.VirtualClusters[0].ConsumePolicies)
	entities[1] = editedJSON(entities[1], func(payload map[string]any) {
		payload["parent_policy_id"] = uuid.NewString()
	})
	require.Contains(t, consumePolicyOrder(chain, refs, snapshots, entities), "parent identity changed")

	refs[0].namespace = "foreign"
	require.Contains(t, consumePolicyOrder(chain, refs, snapshots, nil), "not cross-namespace")
	refs[0].namespace = ""
	chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("foreign")
	require.Contains(t, consumePolicyOrder(chain, refs, snapshots, nil), "same namespace")
}

func TestConsumePolicyChainEmptyScope(t *testing.T) {
	gateway, chain, policies, objects := consumePolicyChainObjects(t)
	cl := newFakeClient(t, objects...)
	for _, p := range policies {
		require.NoError(t, cl.Delete(t.Context(), p))
	}
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}
	require.NoError(t, cl.Create(t.Context(), chain))
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.NoError(t, cfg.blocked())
	require.Empty(t, cfg.invalidChains)
	require.Len(t, cfg.orderedConsumePolicies, 1)
	require.Empty(t, cfg.entities.VirtualClusters[0].ConsumePolicies)
}

func TestReconcileConsumePolicyChainBlocksAndRecoversLastGood(t *testing.T) {
	gateway, chain, _, objects := consumePolicyChainObjects(t)
	certPEM, keyPEM := gwadminsynctest.NewCA(t, "dp-ca").Issue("dp")
	secret := &corev1.Secret{
		Name: "dp-tls", Namespace: ns,
		Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
	cert := fromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, time.Now(), `
metadata: {name: dp-cert}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: `+gateway.Name+`}
  apiSpec:
    certificate: {type: secretRef, secretRef: {name: dp-tls, key: tls.crt}}
`)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		all := append(f.gwAdmin.Objects(), f.gateway)
		all = append(all, objects[1:]...)
		return append(all, chain, secret, cert)
	})
	reconcile := func() {
		t.Helper()
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
	}
	reconcile()
	gateway = read(t, f.cl, f.gateway)
	require.True(t, apimeta.IsStatusConditionTrue(gateway.Status.Conditions, "Programmed"))
	held := f.pod().gateways[f.gatewayID]
	certID := uuid.MustParse(string(cert.UID))
	require.Contains(t, f.pod().holds(f.gatewayID), certID)
	revision, hash := gateway.Status.ConfigRevision, gateway.Status.ConfigHash
	chain = read(t, f.cl, chain)
	chain.Spec.APISpec.Policies = chain.Spec.APISpec.Policies[:7]
	require.NoError(t, f.cl.Update(t.Context(), chain))
	reconcile()
	gateway = read(t, f.cl, f.gateway)
	require.Equal(t, revision, gateway.Status.ConfigRevision)
	require.Equal(t, hash, gateway.Status.ConfigHash)
	require.Equal(t, held, f.pod().gateways[f.gatewayID])
	require.Equal(t, "InvalidPolicyChain", apimeta.FindStatusCondition(gateway.Status.Conditions, "ConfigurationPushed").Reason)
	require.True(t, apimeta.IsStatusConditionFalse(read(t, f.cl, chain).Status.Conditions, "Programmed"))

	// An invalid chain skips only the entity write: a certificate deleted now
	// still stops being trusted.
	require.NoError(t, f.cl.Delete(t.Context(), cert))
	reconcile()
	require.NotContains(t, f.pod().holds(f.gatewayID), certID)

	// An operator restart recovers the last document from a surviving pod.
	f.r.lastConfigs.Delete(gateway.UID)
	cfg, err := buildConfiguration(t.Context(), f.cl, gateway, "", nil)
	require.NoError(t, err)
	last, err := f.r.recoverLastConfig(t.Context(), gateway, f.gatewayID, cfg)
	require.NoError(t, err)
	require.Empty(t, last.orderedProducePolicies)
	_, pushing, err := last.pushedAs(configuration{})
	require.NoError(t, err)
	same, err := sameEntitiesFor(withGWAdminDefaults(held), pushing.pushedKey, pushing.orderedPolicyLists())
	require.NoError(t, err)
	require.True(t, same)
	drifted := withGWAdminDefaults(held)
	drifted.VirtualClusters[0].ConsumePolicies = slices.Clone(drifted.VirtualClusters[0].ConsumePolicies)
	slices.Reverse(drifted.VirtualClusters[0].ConsumePolicies)
	same, err = sameEntitiesFor(drifted, pushing.pushedKey, pushing.orderedPolicyLists())
	require.NoError(t, err)
	require.False(t, same, "recovery must preserve order even when the invalid declaration supplies no marker")
	f.pod().restart()
	reconcile()
	state, registered := f.pod().gateways[f.gatewayID]
	require.True(t, registered, "an invalid chain skips only the entity write: the restarted pod is still registered")
	require.Empty(t, state.VirtualClusters, "an invalid chain must not push a shortened entity document")

	chain = read(t, f.cl, chain)
	chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: "root"})
	require.NoError(t, f.cl.Update(t.Context(), chain))
	reconcile()
	require.True(t, apimeta.IsStatusConditionTrue(read(t, f.cl, f.gateway).Status.Conditions, "Programmed"))
}

func TestConsumePolicyChainComparisonAndLastGoodScopes(t *testing.T) {
	gateway, chain, _, objects := consumePolicyChainObjects(t)
	cfg, err := buildConfiguration(t.Context(), newFakeClient(t, append(objects, chain)...), gateway, "", nil)
	require.NoError(t, err)
	ids := cfg.orderedConsumePolicies
	require.Len(t, ids, 1)
	held := withGWAdminDefaults(cfg.entities)
	held.VirtualClusters[0].ConsumePolicies = slices.Clone(held.VirtualClusters[0].ConsumePolicies)
	slices.Reverse(held.VirtualClusters[0].ConsumePolicies)
	for _, scope := range []orderedPolicyLists{
		{},
		{clusterAndListener: ids},
		{produce: ids},
		{consume: ids},
	} {
		key, err := comparisonKeyFor(withGWAdminDefaults(cfg.entities), scope)
		require.NoError(t, err)
		same, err := sameEntitiesFor(held, key, scope)
		require.NoError(t, err)
		require.Equal(t, len(scope.consume) == 0, same, "only a consume marker makes consume order significant")
	}
	entities := cfg.entities
	entities.VirtualClusters = slices.Clone(entities.VirtualClusters)
	unmanaged := entities.VirtualClusters[0]
	unmanaged.Id = uuid.New()
	unmanaged.ConsumePolicies = slices.Clone(unmanaged.ConsumePolicies)
	entities.VirtualClusters = append(entities.VirtualClusters, unmanaged)
	key, err := comparisonKeyFor(entities, orderedPolicyLists{consume: ids})
	require.NoError(t, err)
	slices.Reverse(unmanaged.ConsumePolicies)
	same, err := sameEntitiesFor(entities, key, orderedPolicyLists{consume: ids})
	require.NoError(t, err)
	require.True(t, same, "a consume chain does not make an unchained virtual cluster's consume order significant")

	last := &lastGoodConfig{config: deriveConfig(gateway, cfg), orderedConsumePolicies: slices.Clone(ids)}
	_, pushing, err := last.pushedAs(configuration{})
	require.NoError(t, err)
	require.Equal(t, ids, pushing.orderedConsumePolicies)
	require.Empty(t, pushing.orderedProducePolicies)
	require.Empty(t, pushing.orderedPolicies)
	same, err = sameEntitiesFor(held, pushing.pushedKey, pushing.orderedPolicyLists())
	require.NoError(t, err)
	require.False(t, same, "a remembered last-good document retains independent consume drift detection")
}

func consumeEntityNames(t *testing.T, entities []gwadminapi.EventGatewayConsumePolicyOnPrem) []string {
	t.Helper()
	names := make([]string, 0, len(entities))
	for _, entity := range entities {
		data, err := json.Marshal(entity)
		require.NoError(t, err)
		var named struct{ Name string }
		require.NoError(t, json.Unmarshal(data, &named))
		names = append(names, named.Name)
	}
	return names
}
