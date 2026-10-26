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

package gwadminsync

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
)

// endpointSliceControllerName is the value of discoveryv1.LabelManagedBy on
// the EndpointSlices that the EndpointSlice controller of Kubernetes manages.
const endpointSliceControllerName = "endpointslice-controller.k8s.io"

// apiEndpointSliceLabels returns the labels of the EndpointSlices of the API
// Service of the named GWAdmin: those the EndpointSlice controller copies
// from the Service, and its own. An EndpointSlice someone else manages is
// left out.
func apiEndpointSliceLabels(gwAdminName string) map[string]string {
	labels := gwadminref.APIServiceLabels(gwAdminName)
	labels[discoveryv1.LabelManagedBy] = endpointSliceControllerName
	return labels
}

// apiPodsRunning reports whether the API Service of the GWAdmin publishes
// gw-admin pods. Its EndpointSlices are found by the labels the GWAdmin
// controller sets on the API Service, which they carry too: neither the
// GWAdmin nor its status is needed.
func apiPodsRunning(ctx context.Context, cl client.Client, key client.ObjectKey) (bool, error) {
	var endpointSlices discoveryv1.EndpointSliceList
	if err := cl.List(ctx, &endpointSlices,
		client.InNamespace(key.Namespace),
		client.MatchingLabels(apiEndpointSliceLabels(key.Name)),
	); err != nil {
		return false, fmt.Errorf("failed to list the EndpointSlices of the API Service of the GWAdmin %s: %w", key, err)
	}
	for _, es := range endpointSlices.Items {
		if slices.ContainsFunc(es.Endpoints, func(ep discoveryv1.Endpoint) bool { return len(ep.Addresses) > 0 }) {
			return true, nil
		}
	}
	return false, nil
}

// addressTypeOrder orders the EndpointSlices of a Service by address family:
// a pod of a dual-stack Service is reached on its IPv4 address.
var addressTypeOrder = map[discoveryv1.AddressType]int{
	discoveryv1.AddressTypeIPv4: 0,
	discoveryv1.AddressTypeIPv6: 1,
	discoveryv1.AddressTypeFQDN: 2,
}

// gwAdminPods returns the gw-admin pods behind the API Service of the
// GWAdmin, sorted by name: those that aren't terminating, and the
// terminating ones still serving. The Service publishes the pods before they
// are Ready: gw-admin keeps nothing across a restart, so a pod only turns
// Ready once provisioned, on its address. An endpoint that isn't a pod is
// left out. A pod of a dual-stack Service is published by one EndpointSlice
// per address family: it is returned once, on its IPv4 address.
func gwAdminPods(ctx context.Context, cl client.Client, gw *operatorv1alpha1.GWAdmin) (pods, terminating []Pod, err error) {
	api := gw.Status.API
	labels := apiEndpointSliceLabels(gw.Name)
	labels[discoveryv1.LabelServiceName] = api.ServiceName
	var endpointSlices discoveryv1.EndpointSliceList
	if err := cl.List(ctx, &endpointSlices, client.InNamespace(gw.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil, nil, fmt.Errorf("failed to list the EndpointSlices of the API Service %s/%s of the GWAdmin: %w", gw.Namespace, api.ServiceName, err)
	}
	slices.SortFunc(endpointSlices.Items, func(a, b discoveryv1.EndpointSlice) int {
		if c := addressTypeOrder[a.AddressType] - addressTypeOrder[b.AddressType]; c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	seen := map[string]struct{}{}
	for _, es := range endpointSlices.Items {
		idx := slices.IndexFunc(es.Ports, func(p discoveryv1.EndpointPort) bool {
			return p.Name != nil && *p.Name == api.PortName && p.Port != nil
		})
		if idx < 0 {
			continue
		}
		port := strconv.Itoa(int(*es.Ports[idx].Port))
		for _, ep := range es.Endpoints {
			if len(ep.Addresses) == 0 || ep.TargetRef == nil || ep.TargetRef.Kind != "Pod" || ep.TargetRef.Name == "" {
				continue
			}
			if _, ok := seen[ep.TargetRef.Name]; ok {
				continue
			}
			seen[ep.TargetRef.Name] = struct{}{}
			// The endpoints of the API Service are all Ready, as it publishes
			// the pods not Ready: whether a pod is told by serving. One
			// unknown falls back to ready, which falls back to true, as the
			// API of EndpointSlices tells.
			serving := lo.FromPtrOr(ep.Conditions.Serving, lo.FromPtrOr(ep.Conditions.Ready, true))
			pod := Pod{Name: ep.TargetRef.Name, UID: ep.TargetRef.UID, Address: net.JoinHostPort(ep.Addresses[0], port), Serving: serving}
			switch {
			case !lo.FromPtr(ep.Conditions.Terminating):
				pods = append(pods, pod)
			case serving:
				terminating = append(terminating, pod)
			}
		}
	}
	byName := func(a, b Pod) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(pods, byName)
	slices.SortFunc(terminating, byName)
	return pods, terminating, nil
}
