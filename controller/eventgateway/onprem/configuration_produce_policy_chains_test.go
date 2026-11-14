package onprem

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
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
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
)

func producePolicyChainObjects(t *testing.T) (
	*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain,
	map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, []client.Object,
) {
	t.Helper()
	gateway, _, _, objects := clusterPolicyChainObjects(t)
	objects = objects[:3]
	created := objects[1].GetCreationTimestamp().Time
	policies := map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
	for i, name := range []string{"first", "second", "root", "parse-two", "third", "parse-one"} {
		body := `
    type: modifyHeaders
    modifyHeaders:
      name: ` + name + `
      config: {actions: [{op: remove, remove: {key: x-header}}]}
`
		switch name {
		case "parse-one", "parse-two":
			body = `
    type: schemaValidation
    schemaValidation:
      name: ` + name + `
      config: {type: json, json: {valueValidationAction: reject}}
`
		case "first", "second":
			body += `    parentPolicyRef: {name: parse-one}
`
		case "third":
			body += `    parentPolicyRef: {name: parse-two}
`
		}
		policy := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created.Add(time.Duration(i)*time.Minute), `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
`+body)
		policies[name] = policy
		objects = append(objects, policy)
	}
	chain := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, created, `
metadata: {name: produce-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies:
    - {name: parse-one, kind: EventGatewayVirtualClusterProducePolicy, namespace: default}
    - {name: second}
    - {name: first}
    - {name: parse-two}
    - {name: third}
    - {name: root}
`)
	return gateway, chain, policies, objects
}

func produceIDs(policies []gwadminapi.EventGatewayProducePolicyOnPrem) []string {
	ids := make([]string, 0, len(policies))
	for _, policy := range policies {
		ids = append(ids, policy.Id.String())
	}
	return ids
}

func declaredProduceIDs(chain *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain,
	policies map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy,
) []string {
	ids := make([]string, 0, len(chain.Spec.APISpec.Policies))
	for _, ref := range chain.Spec.APISpec.Policies {
		ids = append(ids, string(policies[ref.Name].UID))
	}
	return ids
}

func TestBuildProducePolicyChainOrder(t *testing.T) {
	gateway, chain, policies, objects := producePolicyChainObjects(t)
	cl := newFakeClient(t, append(objects, chain)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.NoError(t, cfg.blocked())
	vc := cfg.entities.VirtualClusters[0]
	require.Equal(t, declaredProduceIDs(chain, policies), produceIDs(vc.ProducePolicies))
	require.Equal(t, []orderedPolicyScope{{vc.Id, producePolicyChains.policyKind}}, cfg.orderedPolicies)
	data, err := json.Marshal(vc)
	require.NoError(t, err)
	var wire struct {
		ProducePolicies []struct {
			ID             string `json:"id"`
			ParentPolicyID string `json:"parent_policy_id"`
		} `json:"produce_policies"`
	}
	require.NoError(t, json.Unmarshal(data, &wire))
	require.Len(t, wire.ProducePolicies, len(policies), "roots and children remain in the document's flat list")
	for i, ref := range chain.Spec.APISpec.Policies {
		policy := policies[ref.Name]
		require.Equal(t, "EventGatewayVirtualClusterProducePolicy "+ns+"/"+ref.Name,
			cfg.objects[fmt.Sprintf("virtual_clusters.0.produce_policies.%d", i)])
		parent := policy.Spec.APISpec.ParentPolicyRef.Name
		if parent != "" {
			require.Equal(t, string(policies[parent].UID), wire.ProducePolicies[i].ParentPolicyID)
		} else {
			require.Empty(t, wire.ProducePolicies[i].ParentPolicyID)
		}
		require.Empty(t, policy.GetKonnectID())
		require.Empty(t, policy.GetConditions(), "previous Programmed status is not required")
	}
	require.Empty(t, chain.GetKonnectID())

	firstHash, err := deriveConfig(gateway, cfg).hash()
	require.NoError(t, err)
	chain = read(t, cl, chain)
	chain.Spec.APISpec.Policies[1], chain.Spec.APISpec.Policies[2] = chain.Spec.APISpec.Policies[2], chain.Spec.APISpec.Policies[1]
	chain.Spec.APISpec.Policies = append(slices.Clone(chain.Spec.APISpec.Policies[3:5]),
		append(slices.Clone(chain.Spec.APISpec.Policies[:3]), chain.Spec.APISpec.Policies[5])...)
	require.NoError(t, cl.Update(t.Context(), chain))
	changed, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.NoError(t, changed.blocked())
	require.Equal(t, declaredProduceIDs(chain, policies), produceIDs(changed.entities.VirtualClusters[0].ProducePolicies))
	hash, err := deriveConfig(gateway, changed).hash()
	require.NoError(t, err)
	require.NotEqual(t, firstHash, hash, "whole-group and sibling moves change the revision hash")
	require.NotEqual(t, cfg.pushedKey, changed.pushedKey)
}

func TestProducePolicyChainRejectsInvalidDeclarations(t *testing.T) {
	for _, tt := range []struct {
		name    string
		change  func(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy)
		problem string
	}{
		{name: "omitted", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = nil
		}, problem: "supplied explicitly"},
		{name: "empty nonempty scope", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}
		}, problem: "must list every produce policy"},
		{name: "incomplete", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = c.Spec.APISpec.Policies[:5]
		}, problem: "root is missing"},
		{name: "duplicate", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[1] = c.Spec.APISpec.Policies[0]
		}, problem: "duplicates policy"},
		{name: "missing", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[1].Name = "missing"
		}, problem: "does not exist"},
		{name: "wrong kind", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[1].Kind = "EventGatewayVirtualClusterConsumePolicy"
		}, problem: "must name an EventGatewayVirtualClusterProducePolicy"},
		{name: "child first", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[0], c.Spec.APISpec.Policies[1] = c.Spec.APISpec.Policies[1], c.Spec.APISpec.Policies[0]
		}, problem: "contiguous group"},
		{name: "split group", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[2], c.Spec.APISpec.Policies[5] = c.Spec.APISpec.Policies[5], c.Spec.APISpec.Policies[2]
		}, problem: "contiguous group"},
		{name: "interleaved parent groups", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[2], c.Spec.APISpec.Policies[4] = c.Spec.APISpec.Policies[4], c.Spec.APISpec.Policies[2]
		}, problem: "contiguous group"},
		{name: "wrong virtual cluster", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.EventGatewayVirtualClusterRef = namespacedRef("other-vc")
		}, problem: "different virtual cluster"},
		{name: "deleting policy", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["first"].DeletionTimestamp = new(metav1.Now())
			p["first"].Finalizers = []string{"test.example/finalizer"}
		}, problem: "being deleted"},
		{name: "invalid document policy", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["first"].UID = types.UID("invalid")
		}, problem: "not in the valid configuration"},
		{name: "wrong parent kind", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.ParentPolicyRef.Kind = "EventGatewayVirtualClusterConsumePolicy"
		}, problem: "must name an EventGatewayVirtualClusterProducePolicy"},
		{name: "wrong parent type", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.ParentPolicyRef.Name = "root"
		}, problem: "not in the valid configuration"},
		{name: "missing parent", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.ParentPolicyRef.Name = "missing"
		}, problem: "not in the valid configuration"},
		{name: "cross-parent move", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.ParentPolicyRef.Name = "parse-two"
		}, problem: "contiguous group"},
		{name: "missing configuration", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, p map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			p["root"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = nil
		}, problem: "not in the valid configuration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, chain, policies, objects := producePolicyChainObjects(t)
			tt.change(chain, policies)
			cl := newFakeClient(t, append(objects, chain)...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			require.Len(t, cfg.invalidChains, 1)
			require.Contains(t, cfg.invalidChains[0], tt.problem)
			require.Empty(t, cfg.orderedPolicies)
			ordered, message, err := policyOrder(t.Context(), &cfg, cl, chain, producePolicyChains)
			require.NoError(t, err)
			require.Nil(t, ordered, "invalid declarations cannot yield a shortened success")
			require.Contains(t, message, tt.problem)
		})
	}
}

func TestProducePolicyChainOwnershipIsIndependent(t *testing.T) {
	gateway, chain, policies, objects := producePolicyChainObjects(t)
	_, clusterChain, _, clusterObjects := clusterPolicyChainObjects(t)
	cl := newFakeClient(t, append(append(objects, clusterObjects[3:]...), clusterChain, chain)...)
	build := func() configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		return cfg
	}
	cfg := build()
	require.Len(t, cfg.orderedPolicies, 2)
	parent := read(t, cl, objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster))
	require.Equal(t, policychain.ClaimValue(chain), parent.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(clusterChain), parent.Annotations[policychain.OwnerAnnotation])
	competitor := chain.DeepCopy()
	competitor.Name, competitor.UID, competitor.ResourceVersion = "competitor", types.UID(uuid.NewString()), ""
	competitor.CreationTimestamp = metav1.NewTime(chain.CreationTimestamp.Add(-time.Hour))
	competitor.Spec.APISpec.Policies[1], competitor.Spec.APISpec.Policies[2] = competitor.Spec.APISpec.Policies[2], competitor.Spec.APISpec.Policies[1]
	require.NoError(t, cl.Create(t.Context(), competitor))
	cfg = build()
	require.Equal(t, declaredProduceIDs(chain, policies), produceIDs(cfg.entities.VirtualClusters[0].ProducePolicies))
	require.Equal(t, "ConflictingChain", cfg.excluded[0].conditions[0].Reason)

	chain = read(t, cl, chain)
	chain.Finalizers = []string{"test.example/finalizer"}
	require.NoError(t, cl.Update(t.Context(), chain))
	require.NoError(t, cl.Delete(t.Context(), chain))
	cfg = build()
	require.Equal(t, declaredProduceIDs(competitor, policies), produceIDs(cfg.entities.VirtualClusters[0].ProducePolicies))
	parent = read(t, cl, parent)
	require.Equal(t, policychain.ClaimValue(competitor), parent.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(clusterChain), parent.Annotations[policychain.OwnerAnnotation])
	require.NoError(t, cl.Delete(t.Context(), competitor))
	recreated := competitor.DeepCopy()
	recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, cl.Create(t.Context(), recreated))
	build()
	parent = read(t, cl, parent)
	require.Equal(t, policychain.ClaimValue(recreated), parent.Annotations[policychain.ProduceOwnerAnnotation])
	require.NoError(t, cl.Delete(t.Context(), recreated))
	cfg = build()
	require.Len(t, cfg.entities.VirtualClusters[0].ProducePolicies, len(policies))
	require.Len(t, cfg.orderedPolicies, 1, "the cluster declaration survives Produce chain deletion")
	require.NotContains(t, read(t, cl, parent).Annotations, policychain.ProduceOwnerAnnotation)
}

func TestProducePolicyChainEmptyScope(t *testing.T) {
	gateway, chain, _, objects := producePolicyChainObjects(t)
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}
	parent := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
	parent.Annotations = map[string]string{policychain.ProduceOwnerAnnotation: "missing/" + uuid.NewString()}
	cl := newFakeClient(t, append(objects[:3], chain)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.NoError(t, cfg.blocked())
	require.Empty(t, cfg.entities.VirtualClusters[0].ProducePolicies)
	require.Len(t, cfg.orderedPolicies, 1)
	require.Equal(t, policychain.ClaimValue(chain), read(t, cl, parent).Annotations[policychain.ProduceOwnerAnnotation])
}

func TestProducePolicyChainAllVariants(t *testing.T) {
	for _, tt := range []struct {
		name, body, wireType string
		root                 bool
	}{
		{name: "encryptFields", wireType: "encrypt_fields", body: `
    type: encryptFields
    encryptFields:
      name: first
      config:
        failureMode: reject
        encryptFields:
        - paths: {type: array, array: [{match: "$.secret"}]}
          encryptionKey: {type: static, static: {key: {name: key}}}
`},
		{name: "maskFields", wireType: "mask_fields", body: `
    type: maskFields
    maskFields:
      name: first
      config:
        failureMode: reject
        maskFields:
        - paths: {type: array, array: [{match: "$.secret"}]}
          strategy: {type: replace, replace: {phrase: redacted}}
`},
		{name: "transcode", wireType: "transcode", body: `
    type: transcode
    transcode:
      name: first
      config:
        outputFormat: avro
        avro:
          failureMode: reject
          schemaSource: {type: inline, inline: {inline: '{"type":"record","name":"record","fields":[]}'}}
`},
		{name: "root encrypt", wireType: "encrypt", root: true, body: `
    type: encrypt
    encrypt:
      name: root
      config:
        failureMode: error
        partOfRecord: [value]
        encryptionKey: {type: static, static: {key: {name: key}}}
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, chain, policies, objects := producePolicyChainObjects(t)
			name := "first"
			if tt.root {
				name = "root"
			}
			updated := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, policies[name].CreationTimestamp.Time, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
`+tt.body)
			policies[name].Spec.APISpec = updated.Spec.APISpec
			if !tt.root {
				policies[name].Spec.APISpec.ParentPolicyRef = configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: "parse-one"}
			}
			key := fromYAML[configurationv1alpha1.EventGatewayStaticKey](t, policies[name].CreationTimestamp.Time, `
metadata: {name: key}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: key
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
			secret := &corev1.Secret{Name: "key-secret", Namespace: ns,
				Data: map[string][]byte{"key": []byte("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")}}
			cl := newFakeClient(t, append(objects, chain, key, secret)...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			require.NoError(t, cfg.blocked())
			require.Empty(t, cfg.invalidChains)
			require.Equal(t, declaredProduceIDs(chain, policies), produceIDs(cfg.entities.VirtualClusters[0].ProducePolicies))
			entity := cfg.built[policies[name].UID].(gwadminapi.EventGatewayProducePolicyOnPrem)
			wireType, err := entity.Discriminator()
			require.NoError(t, err)
			require.Equal(t, tt.wireType, wireType)
			if !tt.root {
				policy := read(t, cl, policies[name])
				policy.Spec.APISpec.ParentPolicyRef = configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}
				require.NoError(t, cl.Update(t.Context(), policy))
				cfg, err = buildConfiguration(t.Context(), cl, gateway, "", nil)
				require.NoError(t, err)
				require.NotEmpty(t, cfg.invalidChains, "parsed-record variants cannot become root policies")
			}
		})
	}
}

func TestProducePolicyChainKeepsNamespaceBoundaries(t *testing.T) {
	for _, remoteParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote parent=%t", remoteParent), func(t *testing.T) {
			gateway, chain, policies, objects := producePolicyChainObjects(t)
			grant := &configurationv1alpha1.KongReferenceGrant{
				Name: "chain-access", Namespace: ns,
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: []configurationv1alpha1.ReferenceGrantFrom{{
						Group: "configuration.konghq.com", Kind: "EventGatewayVirtualClusterProducePolicyChain", Namespace: "other",
					}},
					To: []configurationv1alpha1.ReferenceGrantTo{{Group: "configuration.konghq.com", Kind: "EventGatewayVirtualCluster"}},
				},
			}
			if remoteParent {
				chain.Namespace = "other"
				chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
			} else {
				grant.Namespace = "other"
				grant.Spec.From[0].Namespace = ns
				grant.Spec.To[0].Kind = "EventGatewayVirtualClusterProducePolicy"
				chain.Spec.APISpec.Policies[1].Namespace = "other"
				policies["second"].Namespace = "other"
				policies["second"].Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
			}
			cl := newFakeClient(t, append(objects, chain, grant)...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			require.NotEmpty(t, cfg.invalidChains)
			require.Contains(t, cfg.invalidChains[0], "same namespace")
			require.Empty(t, cfg.orderedPolicies)
			parent := read(t, cl, objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster))
			require.NotContains(t, parent.Annotations, policychain.ProduceOwnerAnnotation,
				"a grant cannot expand the Produce chain API or acquire an unsupported claim")
		})
	}
}

func TestProducePolicyChainExcludedOwner(t *testing.T) {
	for _, liveOwner := range []bool{false, true} {
		t.Run(fmt.Sprintf("live owner=%t", liveOwner), func(t *testing.T) {
			gateway, chain, _, objects := producePolicyChainObjects(t)
			parent := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
			parent.Annotations = map[string]string{policychain.ProduceOwnerAnnotation: "missing/" + uuid.NewString()}
			if liveOwner {
				parent.Annotations[policychain.ProduceOwnerAnnotation] = policychain.ClaimValue(chain)
			}
			duplicate := parent.DeepCopy()
			duplicate.Name, duplicate.UID = "older-vc", types.UID(uuid.NewString())
			duplicate.Annotations = nil
			duplicate.CreationTimestamp = metav1.NewTime(parent.CreationTimestamp.Add(-time.Hour))
			cl := newFakeClient(t, append(objects, chain, duplicate)...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			require.Equal(t, liveOwner, len(cfg.invalidChains) > 0)
			if liveOwner {
				require.Contains(t, cfg.invalidChains[0], "isn't in the configuration")
			} else {
				require.NotContains(t, read(t, cl, parent).Annotations, policychain.ProduceOwnerAnnotation)
			}
			require.Empty(t, cfg.orderedPolicies)
		})
	}
}

func TestLastGoodConfigKeepsProduceOrderScope(t *testing.T) {
	gateway, chain, _, objects := producePolicyChainObjects(t)
	cl := newFakeClient(t, append(objects, chain)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	last := lastGoodConfig{revision: 3, config: deriveConfig(gateway, cfg), orderedPolicies: cfg.orderedPolicies}
	_, pushing, err := last.pushedAs(configuration{namespace: ns})
	require.NoError(t, err)
	held := withGWAdminDefaults(cfg.entities)
	same, err := sameEntities(held, pushing.pushedKey, pushing.orderedPolicies...)
	require.NoError(t, err)
	require.True(t, same)
	held.VirtualClusters[0].ProducePolicies[1], held.VirtualClusters[0].ProducePolicies[2] = held.VirtualClusters[0].ProducePolicies[2], held.VirtualClusters[0].ProducePolicies[1]
	same, err = sameEntities(held, pushing.pushedKey, pushing.orderedPolicies...)
	require.NoError(t, err)
	require.False(t, same, "a retained configuration keeps its own scope even when the current declaration is absent")
}

func TestProducePolicyChainRejectsChangedSnapshot(t *testing.T) {
	gateway, chain, policies, objects := producePolicyChainObjects(t)
	cl := newFakeClient(t, objects...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	policy := read(t, cl, policies["first"])
	policy.Spec.APISpec.ParentPolicyRef.Name = "parse-two"
	require.NoError(t, cl.Update(t.Context(), policy))
	ordered, message, err := policyOrder(t.Context(), &cfg, cl, chain, producePolicyChains)
	require.NoError(t, err)
	require.Nil(t, ordered)
	require.Contains(t, message, "changed during configuration assembly",
		"group validation cannot validate a newer policy against an older built entity")
}

func TestReconcileProducePolicyChainLifecycle(t *testing.T) {
	_, chain, policies, objects := producePolicyChainObjects(t)
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	for _, obj := range append(objects[1:], chain) {
		require.NoError(t, f.cl.Create(t.Context(), obj))
	}
	reconcile := func() {
		t.Helper()
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		f.pod().mu.Lock()
		if held, ok := f.pod().gateways[f.gatewayID]; ok {
			f.pod().gateways[f.gatewayID] = withGWAdminDefaults(held)
		}
		f.pod().mu.Unlock()
	}
	heldIDs := func() []string {
		t.Helper()
		f.pod().mu.Lock()
		defer f.pod().mu.Unlock()
		return produceIDs(f.pod().gateways[f.gatewayID].VirtualClusters[0].ProducePolicies)
	}
	programmed := func(status metav1.ConditionStatus, reason string) {
		t.Helper()
		condition := apimeta.FindStatusCondition(read(t, f.cl, chain).Status.Conditions, "Programmed")
		require.NotNil(t, condition)
		require.Equal(t, status, condition.Status, condition.Message)
		require.Equal(t, reason, condition.Reason)
		require.Empty(t, read(t, f.cl, chain).GetKonnectID())
	}
	reconcile()
	programmed(metav1.ConditionTrue, "Programmed")
	require.Equal(t, declaredProduceIDs(chain, policies), heldIDs())
	first := read(t, f.cl, f.gateway)
	f.pod().reset()
	reconcile()
	require.NotContains(t, f.pod().recorded(), callEntities, "defaults must not cause repeated pushes")

	chain = read(t, f.cl, chain)
	chain.Generation++
	chain.Spec.APISpec.Policies[1], chain.Spec.APISpec.Policies[2] = chain.Spec.APISpec.Policies[2], chain.Spec.APISpec.Policies[1]
	require.NoError(t, f.cl.Update(t.Context(), chain))
	reconcile()
	second := read(t, f.cl, f.gateway)
	require.Greater(t, second.Status.ConfigRevision, first.Status.ConfigRevision)
	require.NotEqual(t, second.Status.ConfigHash, first.Status.ConfigHash)
	require.Equal(t, declaredProduceIDs(chain, policies), heldIDs())
	f.pod().mu.Lock()
	held := f.pod().gateways[f.gatewayID]
	held.VirtualClusters[0].ProducePolicies[1], held.VirtualClusters[0].ProducePolicies[2] = held.VirtualClusters[0].ProducePolicies[2], held.VirtualClusters[0].ProducePolicies[1]
	f.pod().gateways[f.gatewayID] = held
	f.pod().mu.Unlock()
	f.pod().reset()
	reconcile()
	require.Contains(t, f.pod().recorded(), callEntities, "independent Produce drift must be repaired")
	require.Equal(t, declaredProduceIDs(chain, policies), heldIDs())
	require.Equal(t, second.Status.ConfigRevision, read(t, f.cl, f.gateway).Status.ConfigRevision)

	valid := read(t, f.cl, chain)
	chain = valid.DeepCopy()
	chain.Spec.APISpec.Policies = chain.Spec.APISpec.Policies[:1]
	chain.Generation++
	require.NoError(t, f.cl.Update(t.Context(), chain))
	backend := read(t, f.cl, objects[1].(*configurationv1alpha1.EventGatewayBackendCluster))
	backend.Spec.APISpec.Name = "pending-change"
	require.NoError(t, f.cl.Update(t.Context(), backend))
	f.pod().reset()
	f.r.lastConfigs = sync.Map{}
	reconcile()
	programmed(metav1.ConditionFalse, "Invalid")
	require.NotContains(t, f.pod().recorded(), callEntities, "invalid owners hold the complete last-valid document after restart")
	require.Equal(t, second.Status.ConfigRevision, read(t, f.cl, f.gateway).Status.ConfigRevision)
	require.Equal(t, second.Status.ConfigHash, read(t, f.cl, f.gateway).Status.ConfigHash)
	require.Equal(t, "InvalidPolicyChain", apimeta.FindStatusCondition(read(t, f.cl, f.gateway).Status.Conditions, "Programmed").Reason)

	chain = read(t, f.cl, chain)
	chain.Spec.APISpec.Policies = valid.Spec.APISpec.Policies
	chain.Generation++
	require.NoError(t, f.cl.Update(t.Context(), chain))
	reconcile()
	programmed(metav1.ConditionTrue, "Programmed")
	require.Greater(t, read(t, f.cl, f.gateway).Status.ConfigRevision, second.Status.ConfigRevision)

	policy := read(t, f.cl, policies["first"])
	require.NoError(t, f.cl.Delete(t.Context(), policy))
	f.pod().reset()
	reconcile()
	programmed(metav1.ConditionFalse, "Invalid")
	require.NotContains(t, f.pod().recorded(), callEntities)
	policy.UID, policy.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, f.cl.Create(t.Context(), policy))
	policies["first"] = policy
	reconcile()
	programmed(metav1.ConditionTrue, "Programmed")
	require.Equal(t, declaredProduceIDs(chain, policies), heldIDs(), "policy recreation uses the new Kubernetes UID")
	require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, chain)))
	reconcile()
	require.Len(t, heldIDs(), len(policies), "ending order management does not delete policies")
}

func TestProducePolicyChainReadError(t *testing.T) {
	gateway, chain, _, objects := producePolicyChainObjects(t)
	cl := newFakeClient(t, objects...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	failing := newFakeClientWithInterceptor(t, interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy); ok {
				return fmt.Errorf("policy read unavailable")
			}
			return cl.Get(ctx, key, obj, opts...)
		},
	}, objects...)
	ordered, message, err := policyOrder(t.Context(), &cfg, failing, chain, producePolicyChains)
	require.ErrorContains(t, err, "policy read unavailable")
	require.Empty(t, message)
	require.Nil(t, ordered)
}
