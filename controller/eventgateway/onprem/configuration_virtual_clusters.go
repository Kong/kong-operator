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
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// The virtual clusters of a gateway are the children of its backend clusters,
// and their policies the children of the virtual clusters. A child is in the
// configuration only when its parent is.

// parents are the objects of a kind other entities are the children of.
type parents struct {
	kind string
	// keys are every parent of the gateway, in the configuration or not.
	keys []client.ObjectKey
	// index is the index of each parent in the configuration in its list.
	index map[client.ObjectKey]int
}

// childrenOf lists into list, whose items are items, the objects whose
// parent, by the index field, is one of the parents. It returns those whose
// parent is in the configuration, with the index of their parent, and the
// keys of every object not being deleted: the others are left out.
func childrenOf[T any, PT interface {
	*T
	entityObject
}](
	ctx context.Context, c *configuration, cl client.Client, list client.ObjectList, items *[]T, field string, p parents,
) ([]PT, map[client.ObjectKey]int, []client.ObjectKey, error) {
	var (
		children    []PT
		keys        []client.ObjectKey
		parentIndex = map[client.ObjectKey]int{}
	)
	for _, parent := range p.keys {
		if err := cl.List(ctx, list, client.MatchingFields{field: parent.String()}); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list %T by %s: %w", list, field, err)
		}
		for _, child := range live[T, PT](*items) {
			key := client.ObjectKeyFromObject(child)
			keys = append(keys, key)
			i, ok := p.index[parent]
			if !ok {
				c.exclude(entityStatus{obj: child}, eventgatewayv1alpha1.OnPremEntityInvalidReason,
					fmt.Sprintf("its %s %s isn't in the configuration", p.kind, parent))
				continue
			}
			parentIndex[key] = i
			children = append(children, child)
		}
	}
	return children, parentIndex, keys, nil
}

// addVirtualClusters adds the virtual clusters of the backend clusters, with
// their policies. A virtual cluster forwards to its backend cluster, by the id
// of the backend cluster in the configuration.
func (c *configuration) addVirtualClusters(ctx context.Context, cl uidResolver, backendClusters parents) error {
	var list configurationv1alpha1.EventGatewayVirtualClusterList
	virtualClusters, backendClusterIndex, keys, err := childrenOf(ctx, c, cl, &list, &list.Items,
		index.IndexFieldEventGatewayVirtualClusterOnEventGatewayBackendClusterRef, backendClusters)
	if err != nil {
		return err
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
		list: in[*configurationv1alpha1.EventGatewayVirtualCluster]("virtual_clusters", &c.entities.VirtualClusters),
	})
	if err != nil {
		return err
	}
	// gw-admin requires every list of policies.
	for i := range c.entities.VirtualClusters {
		vc := &c.entities.VirtualClusters[i]
		vc.ClusterPolicies = []gwadminapi.EventGatewayClusterPolicyOnPrem{}
		vc.ConsumePolicies = []gwadminapi.EventGatewayConsumePolicyOnPrem{}
		vc.ProducePolicies = []gwadminapi.EventGatewayProducePolicyOnPrem{}
	}
	return c.addVirtualClusterPolicies(ctx, cl, parents{kind: "EventGatewayVirtualCluster", keys: keys, index: vcIndex})
}

// addVirtualClusterPolicies adds the cluster, consume and produce policies of
// the virtual clusters. A policy references its schema registry or its static
// key by its id in the configuration.
func (c *configuration) addVirtualClusterPolicies(ctx context.Context, cl uidResolver, virtualClusters parents) error {
	targets := referenceTargetsOf(c, cl.read)

	var clusterPolicies configurationv1alpha1.EventGatewayVirtualClusterPolicyList
	if err := addPolicies(ctx, c, cl, virtualClusters, &clusterPolicies, &clusterPolicies.Items,
		index.IndexFieldEventGatewayVirtualClusterPolicyOnEventGatewayVirtualClusterRef, "cluster_policies",
		func(vc *gwadminapi.EventGatewayVirtualClusterOnPrem) *[]gwadminapi.EventGatewayClusterPolicyOnPrem {
			return &vc.ClusterPolicies
		},
		nil,
		entityKind[*configurationv1alpha1.EventGatewayVirtualClusterPolicy, gwadminapi.EventGatewayClusterPolicyOnPrem]{
			kind: "EventGatewayVirtualClusterPolicy",
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
		parentPolicyFrom(configurationv1alpha1.RefsAtEventGatewayVirtualClusterConsumePolicyParentPolicyRef,
			func(r configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef) (string, string) {
				return r.Namespace, r.Name
			}),
		entityKind[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, gwadminapi.EventGatewayConsumePolicyOnPrem]{
			kind: "EventGatewayVirtualClusterConsumePolicy",
			request: func(ctx context.Context, cl client.Client, p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) (any, error) {
				req, err := p.ToCreateEventGatewayVirtualClusterConsumePolicyRequest(ctx, cl)
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
		parentPolicyFrom(configurationv1alpha1.RefsAtEventGatewayVirtualClusterProducePolicyParentPolicyRef,
			func(r configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef) (string, string) {
				return r.Namespace, r.Name
			}),
		entityKind[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, gwadminapi.EventGatewayProducePolicyOnPrem]{
			kind: "EventGatewayVirtualClusterProducePolicy",
			request: func(ctx context.Context, cl client.Client, p *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) (any, error) {
				req, err := p.ToCreateEventGatewayVirtualClusterProducePolicyRequest(ctx, cl)
				if err != nil {
					return nil, err
				}
				return req.EventGatewayProducePolicyCreate, nil
			},
			adjust: targets.resolveReferences(producePolicyReferences),
		})
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
	policies, vcIndex, _, err := childrenOf[T, PT](ctx, c, cl, list, items, field, virtualClusters)
	if err != nil {
		return err
	}
	k.list = func(p PT) (string, *[]E) {
		i := vcIndex[client.ObjectKeyFromObject(p)]
		return fmt.Sprintf("virtual_clusters.%d.%s", i, path), policiesOf(&c.entities.VirtualClusters[i])
	}
	return addParentsFirst[PT, E](ctx, c, cl, policies, parentPolicy, k)
}

// addParentsFirst adds the objects, a parent, of their own kind, before its
// children: those without a parent first, then their children, and so on.
// One whose parent isn't added is left out. parentOf, when set, returns the
// parent of an object.
func addParentsFirst[PT entityObject, E any](
	ctx context.Context, c *configuration, cl client.Client, objs []PT, parentOf func(PT) (client.ObjectKey, bool), k entityKind[PT, E],
) error {
	added := map[client.ObjectKey]int{}
	for pending := objs; len(pending) > 0; {
		var ready, waiting []PT
		for _, obj := range pending {
			if parentOf != nil {
				if parent, ok := parentOf(obj); ok {
					if _, ok := added[parent]; !ok {
						waiting = append(waiting, obj)
						continue
					}
				}
			}
			ready = append(ready, obj)
		}
		if len(ready) == 0 {
			for _, obj := range waiting {
				parent, _ := parentOf(obj)
				c.exclude(entityStatus{obj: obj}, eventgatewayv1alpha1.OnPremEntityInvalidReason,
					fmt.Sprintf("its parent policy %s isn't in the configuration", parent))
			}
			return nil
		}
		index, err := addEntities(ctx, c, cl, ready, k)
		if err != nil {
			return err
		}
		maps.Copy(added, index)
		pending = waiting
	}
	return nil
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
