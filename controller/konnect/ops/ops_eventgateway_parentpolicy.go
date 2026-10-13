package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// EventGatewayParentPolicyInUseError blocks deletion while child policy
// objects reference a parent whose deletion would cascade in Konnect.
type EventGatewayParentPolicyInUseError struct {
	Users []string
}

func (e EventGatewayParentPolicyInUseError) Error() string {
	return e.DeletionBlockedMessage()
}

// DeletionBlockedMessage explains how to unblock the parent's deletion.
func (e EventGatewayParentPolicyInUseError) DeletionBlockedMessage() string {
	return fmt.Sprintf("deletion blocked: child policies %s reference this parent; delete the child policy objects first and deletion will proceed automatically", strings.Join(e.Users, ", "))
}

func deleteEventGatewayVirtualClusterProducePolicyGuarded(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayVirtualClusterProducePoliciesSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy,
) error {
	var children configurationv1alpha1.EventGatewayVirtualClusterProducePolicyList
	// Include unprogrammed and terminating children: they must be removed
	// before Konnect can cascade-delete the parent's policies.
	if err := cl.List(ctx, &children, client.InNamespace(obj.Namespace)); err != nil {
		return fmt.Errorf("listing child produce policies: %w", err)
	}
	var users []string
	for i := range children.Items {
		child := &children.Items[i]
		if slices.Contains(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewayVirtualClusterProducePolicy(child), client.ObjectKeyFromObject(obj)) {
			users = append(users, client.ObjectKeyFromObject(child).String())
		}
	}
	if len(users) > 0 {
		slices.Sort(users)
		return EventGatewayParentPolicyInUseError{Users: users}
	}
	return deleteEventGatewayVirtualClusterProducePolicy(ctx, sdk, obj)
}

func deleteEventGatewayVirtualClusterConsumePolicyGuarded(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayVirtualClusterConsumePoliciesSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy,
) error {
	var children configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyList
	if err := cl.List(ctx, &children, client.InNamespace(obj.Namespace)); err != nil {
		return fmt.Errorf("listing child consume policies: %w", err)
	}
	var users []string
	for i := range children.Items {
		child := &children.Items[i]
		if slices.Contains(configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRefsToEventGatewayVirtualClusterConsumePolicy(child), client.ObjectKeyFromObject(obj)) {
			users = append(users, client.ObjectKeyFromObject(child).String())
		}
	}
	if len(users) > 0 {
		slices.Sort(users)
		return EventGatewayParentPolicyInUseError{Users: users}
	}
	return deleteEventGatewayVirtualClusterConsumePolicy(ctx, sdk, obj)
}
