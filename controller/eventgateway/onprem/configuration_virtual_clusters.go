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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// The virtual clusters of a gateway are the children of its backend clusters,
// and their policies the children of the virtual clusters.

// addVirtualClusters adds the virtual clusters of the backend clusters, and
// returns them as the parents of their policies. A virtual cluster forwards to
// its backend cluster, by the id of the backend cluster in the configuration.
func (c *configuration) addVirtualClusters(ctx context.Context, cl uidResolver, backendClusters parents) (parents, error) {
	var list configurationv1alpha1.EventGatewayVirtualClusterList
	virtualClusters, backendClusterIndex, keys, err := childrenOf(ctx, c, cl, &list, &list.Items,
		index.IndexFieldEventGatewayVirtualClusterOnEventGatewayBackendClusterRef, backendClusters)
	if err != nil {
		return parents{}, err
	}
	vcIndex, err := addEntities(ctx, c, cl, virtualClusters, entityKind[*configurationv1alpha1.EventGatewayVirtualCluster, gwadminapi.EventGatewayVirtualClusterOnPrem]{
		kind: "EventGatewayVirtualCluster", what: "virtual cluster",
		request: func(_ context.Context, _ client.Client, vc *configurationv1alpha1.EventGatewayVirtualCluster) (any, error) {
			// The conversion sets the destination to the id of the backend
			// cluster it finds in the status of the virtual cluster.
			withDestination := vc.DeepCopy()
			bc := c.entities.BackendClusters[backendClusterIndex[client.ObjectKeyFromObject(vc)]]
			withDestination.SetEventGatewayBackendClusterID(bc.Id.String())
			return withDestination.ToCreateVirtualClusterRequest()
		},
		name: func(e gwadminapi.EventGatewayVirtualClusterOnPrem) string { return e.Name },
		// Clients reach a virtual cluster by its DNS label: Konnect refuses two
		// with the same one.
		unique: func(e gwadminapi.EventGatewayVirtualClusterOnPrem) []uniqueValue {
			return []uniqueValue{{scope: "DNS label", value: e.DnsLabel}}
		},
		list: in[*configurationv1alpha1.EventGatewayVirtualCluster]("virtual_clusters", &c.entities.VirtualClusters),
	})
	if err != nil {
		return parents{}, err
	}
	// gw-admin requires every list of policies.
	for i := range c.entities.VirtualClusters {
		vc := &c.entities.VirtualClusters[i]
		vc.ClusterPolicies = []gwadminapi.EventGatewayClusterPolicyOnPrem{}
		vc.ConsumePolicies = []gwadminapi.EventGatewayConsumePolicyOnPrem{}
		vc.ProducePolicies = []gwadminapi.EventGatewayProducePolicyOnPrem{}
	}
	return parents{kind: "EventGatewayVirtualCluster", keys: keys, index: vcIndex}, nil
}

// addVirtualClusterPolicies adds the cluster, consume and produce policies of
// the virtual clusters. A policy references its schema registry or its static
// key by its id in the configuration, among the targets.
func (c *configuration) addVirtualClusterPolicies(ctx context.Context, cl uidResolver, virtualClusters parents, targets referenceTargets) error {
	var clusterPolicies configurationv1alpha1.EventGatewayVirtualClusterPolicyList
	if err := addPolicies(ctx, c, cl, virtualClusters, &clusterPolicies, &clusterPolicies.Items,
		index.IndexFieldEventGatewayVirtualClusterPolicyOnEventGatewayVirtualClusterRef, "cluster_policies",
		func(vc *gwadminapi.EventGatewayVirtualClusterOnPrem) *[]gwadminapi.EventGatewayClusterPolicyOnPrem {
			return &vc.ClusterPolicies
		},
		nil,
		entityKind[*configurationv1alpha1.EventGatewayVirtualClusterPolicy, gwadminapi.EventGatewayClusterPolicyOnPrem]{
			kind:   "EventGatewayVirtualClusterPolicy",
			unique: policyName[gwadminapi.EventGatewayClusterPolicyOnPrem],
			request: func(_ context.Context, _ client.Client, p *configurationv1alpha1.EventGatewayVirtualClusterPolicy) (any, error) {
				req, err := p.Spec.APISpec.ToCreateEventGatewayVirtualClusterClusterLevelPolicyRequest()
				if err != nil {
					return nil, err
				}
				return req.EventGatewayClusterPolicyModify, nil
			},
		}); err != nil {
		return err
	}

	var consumePolicies configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyList
	if err := addPolicies(ctx, c, cl, virtualClusters, &consumePolicies, &consumePolicies.Items,
		index.IndexFieldEventGatewayVirtualClusterConsumePolicyOnEventGatewayVirtualClusterRef, "consume_policies",
		func(vc *gwadminapi.EventGatewayVirtualClusterOnPrem) *[]gwadminapi.EventGatewayConsumePolicyOnPrem {
			return &vc.ConsumePolicies
		},
		consumeParentOf,
		entityKind[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, gwadminapi.EventGatewayConsumePolicyOnPrem]{
			kind:   "EventGatewayVirtualClusterConsumePolicy",
			unique: policyName[gwadminapi.EventGatewayConsumePolicyOnPrem],
			request: func(ctx context.Context, cl client.Client, p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) (any, error) {
				if err := checkPolicyReferences(ctx, cl, p, p.Spec.APISpec); err != nil {
					return nil, err
				}
				req, err := withUIDAsKonnectID(p).ToCreateEventGatewayVirtualClusterConsumePolicyRequest(ctx, cl)
				if err != nil {
					return nil, err
				}
				return req.EventGatewayConsumePolicyCreate, nil
			},
			adjust: targets.resolveReferences(consumePolicyReferences),
		}); err != nil {
		return err
	}

	var producePolicies configurationv1alpha1.EventGatewayVirtualClusterProducePolicyList
	return addPolicies(ctx, c, cl, virtualClusters, &producePolicies, &producePolicies.Items,
		index.IndexFieldEventGatewayVirtualClusterProducePolicyOnEventGatewayVirtualClusterRef, "produce_policies",
		func(vc *gwadminapi.EventGatewayVirtualClusterOnPrem) *[]gwadminapi.EventGatewayProducePolicyOnPrem {
			return &vc.ProducePolicies
		},
		produceParentOf,
		entityKind[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, gwadminapi.EventGatewayProducePolicyOnPrem]{
			kind:   "EventGatewayVirtualClusterProducePolicy",
			unique: policyName[gwadminapi.EventGatewayProducePolicyOnPrem],
			request: func(ctx context.Context, cl client.Client, p *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) (any, error) {
				if err := checkPolicyReferences(ctx, cl, p, p.Spec.APISpec); err != nil {
					return nil, err
				}
				req, err := withUIDAsKonnectID(p).ToCreateEventGatewayVirtualClusterProducePolicyRequest(ctx, cl)
				if err != nil {
					return nil, err
				}
				return req.EventGatewayProducePolicyCreate, nil
			},
			adjust: targets.resolveReferences(producePolicyReferences),
		})
}

// withUIDAsKonnectID returns a copy of the policy with its UID as its Konnect
// ID, its id in the configuration. The conversions, written for Konnect, only
// let a policy created in Konnect use an entity being deleted: on prem, a policy
// keeps using an entity FinalizerInUse holds, and noNewUserOfDeletedEntities
// refuses a new one. The policy itself is left as it is, as its status is
// patched from it.
func withUIDAsKonnectID[T interface {
	client.Object
	SetKonnectID(string)
}](p T) T {
	cp := p.DeepCopyObject().(T)
	cp.SetKonnectID(string(p.GetUID()))
	return cp
}

// addPolicies adds the policies of a kind of the virtual clusters, listed by
// the index field, to the list of their virtual cluster at path, e.g.
// consume_policies, that policiesOf returns, a parent policy before its
// children. parentPolicy, when set, returns the parent policy of a policy, of
// its own kind.
func addPolicies[T any, PT interface {
	*T
	entityObject
}, E any](
	ctx context.Context, c *configuration, cl client.Client, virtualClusters parents, list client.ObjectList, items *[]T,
	field, path string, policiesOf func(*gwadminapi.EventGatewayVirtualClusterOnPrem) *[]E,
	parentPolicy func(PT) (client.ObjectKey, bool), k entityKind[PT, E],
) error {
	return addChildren(ctx, c, cl, virtualClusters, list, items, field, func(i int) (string, *[]E) {
		return fmt.Sprintf("virtual_clusters.%d.%s", i, path), policiesOf(&c.entities.VirtualClusters[i])
	}, parentPolicy, k)
}

// parentPolicyFrom returns the parent policy of a policy whose kind
// references its parent policy at refsAt, ref returning the namespace and the
// name a reference names.
func parentPolicyFrom[PT client.Object, R any](refsAt func(PT) []R, ref func(R) (namespace, name string)) func(PT) (client.ObjectKey, bool) {
	return func(p PT) (client.ObjectKey, bool) {
		refs := refsAt(p)
		if len(refs) == 0 {
			return client.ObjectKey{}, false
		}
		namespace, name := ref(refs[0])
		return refKey(p, &namespace, name), true
	}
}
