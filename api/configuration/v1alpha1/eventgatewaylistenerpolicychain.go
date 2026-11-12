package v1alpha1

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveValidatedPolicyIDs resolves the complete declared order from one snapshot
// of each policy. Any invalid reference rejects the entire result.
func (obj *EventGatewayListenerPolicyChain) ResolveValidatedPolicyIDs(ctx context.Context, cl client.Client) ([]string, error) {
	return resolveChainPolicyIDs(ctx, cl, obj.Namespace, obj.Spec.EventGatewayListenerRef, "spec.eventGatewayListenerRef",
		"EventGatewayListener", obj.GetEventGatewayListenerID(), "EventGatewayListenerPolicy", obj.GetGatewayID(),
		chainPolicyRefs(obj.Spec.APISpec.Policies, func(r EventGatewayListenerPolicyRef) chainPolicyRef { return chainPolicyRef(r) }),
		func(p *EventGatewayListenerPolicy) string { return p.GetEventGatewayListenerID() })
}
