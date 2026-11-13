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
	"reflect"
	"slices"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// Konnect refuses two policies of a gateway with the same name, whatever
// their kind or their virtual cluster or listener. The kinds are added one
// after the other, so the name of a policy is reserved beforehand for the
// oldest policy of the gateway naming it: a newer one of another kind doesn't
// take it from a policy pushed already (see claim).

// policyNameCandidate is a policy of the gateway, with its name.
type policyNameCandidate struct {
	obj  client.Object
	kind string
	name string
}

// reservePolicyNames reserves each policy name for the oldest policy of the
// gateway, of the virtual clusters or of the listeners attached to it, naming
// it. Only the policies that belong to the gateway count: those of another
// namespace no KongReferenceGrant lets reference their parent are only left
// out (see parentNotGranted), and must not take a name.
func (c *configuration) reservePolicyNames(
	ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway, virtualClusters parents,
) error {
	var candidates []policyNameCandidate
	// Only the virtual clusters and listeners in the configuration: the
	// policies of the others are only left out.
	vcKeys := slices.Collect(maps.Keys(virtualClusters.index))
	add := func(list client.ObjectList, kind, field string, parentKeys []client.ObjectKey) error {
		for _, parent := range parentKeys {
			if err := cl.List(ctx, list, client.MatchingFields{field: parent.String()}); err != nil {
				return fmt.Errorf("failed to list the %ss of %s: %w", kind, parent, err)
			}
			items, err := extractObjects(list)
			if err != nil {
				return err
			}
			for _, obj := range items {
				// A policy being deleted that is held keeps its name: it stays
				// in the configuration (see live).
				if !isLive(obj) {
					continue
				}
				if message, err := c.parentNotGranted(ctx, cl, obj, kind); err != nil {
					return err
				} else if message != "" {
					continue
				}
				if name := apiSpecPolicyName(obj); name != "" {
					candidates = append(candidates, policyNameCandidate{obj: obj, kind: kind, name: name})
				}
			}
		}
		return nil
	}
	if err := add(&configurationv1alpha1.EventGatewayVirtualClusterPolicyList{}, "EventGatewayVirtualClusterPolicy",
		index.IndexFieldEventGatewayVirtualClusterPolicyOnEventGatewayVirtualClusterRef, vcKeys); err != nil {
		return err
	}
	if err := add(&configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyList{}, "EventGatewayVirtualClusterConsumePolicy",
		index.IndexFieldEventGatewayVirtualClusterConsumePolicyOnEventGatewayVirtualClusterRef, vcKeys); err != nil {
		return err
	}
	if err := add(&configurationv1alpha1.EventGatewayVirtualClusterProducePolicyList{}, "EventGatewayVirtualClusterProducePolicy",
		index.IndexFieldEventGatewayVirtualClusterProducePolicyOnEventGatewayVirtualClusterRef, vcKeys); err != nil {
		return err
	}
	var listeners configurationv1alpha1.EventGatewayListenerList
	if err := cl.List(ctx, &listeners, client.MatchingFields{
		index.IndexFieldEventGatewayListenerOnOnPremEventGatewayRef: client.ObjectKeyFromObject(gateway).String(),
	}); err != nil {
		return fmt.Errorf("failed to list the EventGatewayListeners of the gateway: %w", err)
	}
	listenerKeys := make([]client.ObjectKey, 0, len(listeners.Items))
	for i := range listeners.Items {
		l := &listeners.Items[i]
		if !l.DeletionTimestamp.IsZero() {
			continue
		}
		if message, err := c.parentNotGranted(ctx, cl, l, "EventGatewayListener"); err != nil {
			return err
		} else if message != "" {
			continue
		}
		listenerKeys = append(listenerKeys, client.ObjectKeyFromObject(l))
	}
	if err := add(&configurationv1alpha1.EventGatewayListenerPolicyList{}, "EventGatewayListenerPolicy",
		index.IndexFieldEventGatewayListenerPolicyOnEventGatewayListenerRef, listenerKeys); err != nil {
		return err
	}

	slices.SortFunc(candidates, func(a, b policyNameCandidate) int {
		if n := byCreation(a.obj, b.obj); n != 0 {
			return n
		}
		if a.kind < b.kind {
			return -1
		}
		if a.kind > b.kind {
			return 1
		}
		return 0
	})
	c.reserved = map[string]string{}
	for _, p := range candidates {
		if _, ok := c.reserved[p.name]; !ok {
			c.reserved[p.name] = p.kind + " " + client.ObjectKeyFromObject(p.obj).String()
		}
	}
	return nil
}

// extractObjects returns the objects of a list.
func extractObjects(list client.ObjectList) ([]client.Object, error) {
	items := reflect.ValueOf(list).Elem().FieldByName("Items")
	if !items.IsValid() {
		return nil, fmt.Errorf("%T has no items", list)
	}
	objs := make([]client.Object, 0, items.Len())
	for i := range items.Len() {
		obj, ok := reflect.TypeAssert[client.Object](items.Index(i).Addr())
		if !ok {
			return nil, fmt.Errorf("%T holds no objects", list)
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

// apiSpecPolicyName returns the name of the policy, set in the member of its
// type, or "".
func apiSpecPolicyName(obj client.Object) string {
	fields, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return ""
	}
	spec, _ := fields["spec"].(map[string]any)
	apiSpec, _ := spec["apiSpec"].(map[string]any)
	policyType, _ := apiSpec["type"].(string)
	member, _ := apiSpec[policyType].(map[string]any)
	name, _ := member["name"].(string)
	return name
}
