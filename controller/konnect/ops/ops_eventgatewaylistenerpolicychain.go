package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// applyEventGatewayListenerPolicyChain only reorders a complete remote policy set.
// GET failures must not be interpreted as an empty scope.
func applyEventGatewayListenerPolicyChain(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.EventGatewayListenerPoliciesSDK,
	obj *configurationv1alpha1.EventGatewayListenerPolicyChain,
	op Op,
) error {
	if obj.GetGatewayID() == "" {
		return CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Gateway", Op: op}
	}
	if obj.GetEventGatewayListenerID() == "" {
		return CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "EventGatewayListener", Op: op}
	}
	desired, resolved := resolvedPolicyChainIDsFromContext(ctx, obj)
	if !resolved {
		var err error
		desired, err = obj.ResolveValidatedPolicyIDs(ctx, cl)
		if err != nil {
			return err
		}
	}
	req := sdkkonnectops.UpdateEventGatewayListenerPolicyChainRequest{
		GatewayID: obj.GetGatewayID(), ListenerID: obj.GetEventGatewayListenerID(),
		UpdateEventGatewayPolicyChainRequest: &sdkkonnectcomp.UpdateEventGatewayPolicyChainRequest{Policies: desired},
	}
	remote, err := sdk.GetEventGatewayListenerPolicyChain(ctx, req.GatewayID, req.ListenerID)
	if errWrap := wrapErrIfKonnectOpFailed(err, GetOp, obj); errWrap != nil {
		return errWrap
	}
	if remote == nil || remote.EventGatewayPolicyChainResponse == nil {
		return fmt.Errorf("getting listener policy chain: %w", ErrNilResponse)
	}
	if err := validateCompletePolicyChainForScope(desired, remote.EventGatewayPolicyChainResponse.Policies, "listener"); err != nil {
		return err
	}
	resp, err := sdk.UpdateEventGatewayListenerPolicyChain(ctx, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, op, obj); errWrap != nil {
		return errWrap
	}
	if resp == nil || resp.EventGatewayPolicyChainResponse == nil {
		return fmt.Errorf("applying listener policy chain: %w", ErrNilResponse)
	}
	return validateAppliedPolicyChain(desired, resp.EventGatewayPolicyChainResponse.Policies, "listener")
}
