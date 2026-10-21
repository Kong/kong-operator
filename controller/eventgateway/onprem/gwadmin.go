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
	"net"
	"slices"
	"strconv"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/published"
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
	labels := published.APIServiceLabels(gwAdminName)
	labels[discoveryv1.LabelManagedBy] = endpointSliceControllerName
	return labels
}

// gwAdminUnresolved tells why the GWAdmin of a gateway can't serve it yet. It
// is not retried: a change of the GWAdmin, of a KongReferenceGrant of its
// namespace or of the pods of its API Service triggers the gateway again.
type gwAdminUnresolved struct {
	reason  kcfgconsts.ConditionReason
	message string
}

// gwAdminPod is a gw-admin pod behind the API Service of a GWAdmin.
type gwAdminPod struct {
	// name is the name of the pod.
	name string
	// address is the host and port the API listener of the pod is reached on.
	address string
}

// resolveGWAdmin returns the gw-admin pods of the gateway: the pods of the
// API Service of its GWAdmin. The gateway must be allowed to use the
// GWAdmin, and the GWAdmin must have published its API and not be being
// deleted. Otherwise it returns why the GWAdmin can't serve the gateway.
func (r *Reconciler) resolveGWAdmin(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway) ([]gwAdminPod, *gwAdminUnresolved, error) {
	reason, err := gwadminref.NotAllowed(ctx, r.Client, gateway)
	if err != nil {
		return nil, nil, err
	}
	if reason != "" {
		return nil, &gwAdminUnresolved{
			reason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminRefNotPermittedReason,
			message: "The gateway may not use its GWAdmin: " + reason,
		}, nil
	}

	key := gateway.GWAdminKey()
	notReady := func(format string) *gwAdminUnresolved {
		return &gwAdminUnresolved{
			reason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason,
			message: fmt.Sprintf(format, key),
		}
	}
	var gw operatorv1alpha1.GWAdmin
	if err := r.Get(ctx, key, &gw); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &gwAdminUnresolved{
				reason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason,
				message: fmt.Sprintf("The GWAdmin %s does not exist", key),
			}, nil
		}
		return nil, nil, fmt.Errorf("failed to get the GWAdmin %s: %w", key, err)
	}
	if !gw.DeletionTimestamp.IsZero() {
		return nil, notReady("The GWAdmin %s is being deleted"), nil
	}
	if gw.Status.API == nil {
		return nil, notReady("The GWAdmin %s hasn't published its API yet"), nil
	}

	pods, err := gwAdminPods(ctx, r.Client, &gw)
	if err != nil {
		return nil, nil, err
	}
	if len(pods) == 0 {
		return nil, notReady("The GWAdmin %s has no gw-admin pod"), nil
	}
	return pods, nil, nil
}

// addressTypeOrder orders the EndpointSlices of a Service by address family:
// a pod of a dual-stack Service is reached on its IPv4 address.
var addressTypeOrder = map[discoveryv1.AddressType]int{
	discoveryv1.AddressTypeIPv4: 0,
	discoveryv1.AddressTypeIPv6: 1,
	discoveryv1.AddressTypeFQDN: 2,
}

// gwAdminPods returns the gw-admin pods behind the API Service of the
// GWAdmin, sorted by name. The Service publishes the pods before they are
// Ready: gw-admin keeps nothing across a restart, so a pod only turns Ready
// once its configuration is pushed to it, on its address. A pod that is
// shutting down is left out, and so is an endpoint that isn't a pod. A pod of
// a dual-stack Service is published by one EndpointSlice per address family:
// it is returned once, on its IPv4 address.
func gwAdminPods(ctx context.Context, cl client.Client, gw *operatorv1alpha1.GWAdmin) ([]gwAdminPod, error) {
	api := gw.Status.API
	labels := apiEndpointSliceLabels(gw.Name)
	labels[discoveryv1.LabelServiceName] = api.ServiceName
	var endpointSlices discoveryv1.EndpointSliceList
	if err := cl.List(ctx, &endpointSlices, client.InNamespace(gw.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil, fmt.Errorf("failed to list the EndpointSlices of the API Service %s/%s of the GWAdmin: %w", gw.Namespace, api.ServiceName, err)
	}
	slices.SortFunc(endpointSlices.Items, func(a, b discoveryv1.EndpointSlice) int {
		if c := addressTypeOrder[a.AddressType] - addressTypeOrder[b.AddressType]; c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	pods := map[string]gwAdminPod{}
	for _, es := range endpointSlices.Items {
		idx := slices.IndexFunc(es.Ports, func(p discoveryv1.EndpointPort) bool {
			return p.Name != nil && *p.Name == api.PortName && p.Port != nil
		})
		if idx < 0 {
			continue
		}
		port := strconv.Itoa(int(*es.Ports[idx].Port))
		for _, ep := range es.Endpoints {
			if len(ep.Addresses) == 0 ||
				(ep.Conditions.Terminating != nil && *ep.Conditions.Terminating) ||
				ep.TargetRef == nil || ep.TargetRef.Kind != "Pod" || ep.TargetRef.Name == "" {
				continue
			}
			if _, ok := pods[ep.TargetRef.Name]; ok {
				continue
			}
			pods[ep.TargetRef.Name] = gwAdminPod{name: ep.TargetRef.Name, address: net.JoinHostPort(ep.Addresses[0], port)}
		}
	}
	return slices.SortedFunc(maps.Values(pods), func(a, b gwAdminPod) int { return strings.Compare(a.name, b.name) }), nil
}
