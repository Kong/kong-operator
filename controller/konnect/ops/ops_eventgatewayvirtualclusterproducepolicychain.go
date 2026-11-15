package ops

import (
	"context"
	"fmt"
	"slices"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func applyEventGatewayVirtualClusterProducePolicyChain(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.EventGatewayVirtualClusterProducePoliciesSDK,
	obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain,
	op Op,
	forceUpdate bool,
) (bool, error) {
	if obj.GetGatewayID() == "" {
		return false, CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Gateway", Op: op}
	}
	if obj.GetVirtualClusterID() == "" {
		return false, CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "EventGatewayVirtualCluster", Op: op}
	}
	desired, resolved := resolvedPolicyChainIDsFromContext(ctx, obj)
	if !resolved {
		var err error
		desired, err = obj.ResolveValidatedPolicyIDs(ctx, cl)
		if err != nil {
			return false, err
		}
	}
	req := sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainRequest{
		GatewayID: obj.GetGatewayID(), VirtualClusterID: obj.GetVirtualClusterID(),
		UpdateEventGatewayPolicyChainRequest: &sdkkonnectcomp.UpdateEventGatewayPolicyChainRequest{Policies: desired},
	}
	remote, err := sdk.GetEventGatewayVirtualClusterProducePolicyChain(ctx, req.GatewayID, req.VirtualClusterID)
	if errWrap := wrapErrIfKonnectOpFailed(err, GetOp, obj); errWrap != nil {
		return false, errWrap
	}
	if remote == nil || remote.EventGatewayPolicyChainResponse == nil {
		return false, fmt.Errorf("getting produce policy chain: %w", ErrNilResponse)
	}
	if err := validateCompletePolicyChainForScope(desired, remote.EventGatewayPolicyChainResponse.Policies, "produce"); err != nil {
		return false, err
	}
	if !forceUpdate && slices.Equal(desired, remote.EventGatewayPolicyChainResponse.Policies) {
		return false, nil
	}
	resp, err := sdk.UpdateEventGatewayVirtualClusterProducePolicyChain(ctx, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, op, obj); errWrap != nil {
		return false, errWrap
	}
	if resp == nil || resp.EventGatewayPolicyChainResponse == nil {
		return false, fmt.Errorf("applying produce policy chain: %w", ErrNilResponse)
	}
	return true, validateAppliedPolicyChain(desired, resp.EventGatewayPolicyChainResponse.Policies, "produce")
}
