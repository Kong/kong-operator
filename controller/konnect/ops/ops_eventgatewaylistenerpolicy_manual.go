package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func getEventGatewayListenerPolicyForUID(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayListenerPoliciesSDK,
	obj *konnectv1alpha1.EventGatewayListenerPolicy,
) (string, error) {
	gatewayID := obj.GetGatewayID()
	if gatewayID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Gateway", Op: GetOp}
	}
	eventGatewayListenerID := obj.GetEventGatewayListenerID()
	if eventGatewayListenerID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "EventGatewayListener", Op: GetOp}
	}

	policyType, policyName := eventGatewayListenerPolicyIdentity(obj)
	if policyType == "" || policyName == "" {
		return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
	}

	resp, err := sdk.ListEventGatewayListenerPolicies(ctx, sdkkonnectops.ListEventGatewayListenerPoliciesRequest{
		GatewayID:  gatewayID,
		ListenerID: eventGatewayListenerID,
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
	}
	if resp == nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), ErrNilResponse)
	}

	for _, entry := range resp.GetListEventGatewayListenerPoliciesResponse() {
		if entry.GetType() != policyType {
			continue
		}
		name := entry.GetName()
		if name == nil || *name != policyName {
			continue
		}
		if entry.GetID() != "" {
			return entry.GetID(), nil
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
}

func eventGatewayListenerPolicyIdentity(
	obj *konnectv1alpha1.EventGatewayListenerPolicy,
) (policyType, policyName string) {
	cfg := obj.Spec.APISpec.EventGatewayListenerPolicyConfig
	if cfg == nil {
		return "", ""
	}

	switch cfg.Type {
	case konnectv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen:
		if cfg.EventGatewayTLSListen == nil {
			return "", ""
		}
		return "tls_server", cfg.EventGatewayTLSListen.Name
	case konnectv1alpha1.EventGatewayListenerPolicyConfigTypeForwardToVirtualClust:
		if cfg.ForwardToVirtualClust == nil {
			return "", ""
		}
		return "forward_to_virtual_cluster", cfg.ForwardToVirtualClust.Name
	default:
		return "", ""
	}
}
