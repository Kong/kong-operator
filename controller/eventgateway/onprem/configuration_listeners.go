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
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// The listeners of a gateway are attached to the gateway itself, and their
// policies are their children. A forwarding policy names a virtual cluster of
// the gateway, and is sent with the id of the virtual cluster.

// addListeners adds the listeners of the gateway, with their policies, which
// reference the TLS trust bundles and the virtual clusters among the targets.
func (c *configuration) addListeners(ctx context.Context, cl uidResolver, gateway *eventgatewayv1alpha1.OnPremEventGateway, targets referenceTargets) error {
	var listeners configurationv1alpha1.EventGatewayListenerList
	parentsOfPolicies, err := addAttached(ctx, c, cl, gateway, &listeners, &listeners.Items, index.IndexFieldEventGatewayListenerOnOnPremEventGatewayRef,
		entityKind[*configurationv1alpha1.EventGatewayListener, gwadminapi.EventGatewayListenerOnPrem]{
			kind: "EventGatewayListener", what: "listener",
			request: func(_ context.Context, _ client.Client, l *configurationv1alpha1.EventGatewayListener) (any, error) {
				return l.Spec.APISpec.ToCreateEventGatewayListenerRequest()
			},
			name: func(e gwadminapi.EventGatewayListenerOnPrem) string { return e.Name },
			list: in[*configurationv1alpha1.EventGatewayListener]("listeners", &c.entities.Listeners),
		})
	if err != nil {
		return err
	}
	// gw-admin requires the list of policies.
	for i := range c.entities.Listeners {
		c.entities.Listeners[i].Policies = []gwadminapi.EventGatewayListenerPolicyOnPrem{}
	}

	var policies configurationv1alpha1.EventGatewayListenerPolicyList
	return addChildren(ctx, c, cl, parentsOfPolicies, &policies, &policies.Items, index.IndexFieldEventGatewayListenerPolicyOnEventGatewayListenerRef,
		func(i int) (string, *[]gwadminapi.EventGatewayListenerPolicyOnPrem) {
			return fmt.Sprintf("listeners.%d.policies", i), &c.entities.Listeners[i].Policies
		},
		nil,
		entityKind[*configurationv1alpha1.EventGatewayListenerPolicy, gwadminapi.EventGatewayListenerPolicyOnPrem]{
			kind: "EventGatewayListenerPolicy",
			request: func(ctx context.Context, cl client.Client, p *configurationv1alpha1.EventGatewayListenerPolicy) (any, error) {
				req, err := p.ToCreateEventGatewayListenerPolicyRequest(ctx, cl)
				if err != nil {
					return nil, err
				}
				return req.EventGatewayListenerPolicyCreate, nil
			},
			adjust: targets.resolveReferences(listenerPolicyReferences),
		})
}
