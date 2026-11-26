package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// EventGatewayParentPolicyInUseError blocks deletion while child policy
// objects reference a parent whose deletion would cascade in Konnect.
type EventGatewayParentPolicyInUseError struct {
	// Users are the child policy objects in the namespace of the parent
	// (namespace/name, sorted).
	Users []string
	// OtherNamespacesUsers is the number of child policy objects in other
	// namespaces, counted rather than named so as not to disclose them.
	OtherNamespacesUsers int
}

func (e EventGatewayParentPolicyInUseError) Error() string {
	return e.DeletionBlockedMessage()
}

// DeletionBlockedMessage explains how to unblock the parent's deletion.
func (e EventGatewayParentPolicyInUseError) DeletionBlockedMessage() string {
	parts := slices.Clone(e.Users)
	if e.OtherNamespacesUsers > 0 {
		parts = append(parts, fmt.Sprintf("%d in other namespaces", e.OtherNamespacesUsers))
	}
	return fmt.Sprintf("deletion blocked: child policies %s reference this parent; delete the child policy objects first and deletion will proceed automatically", strings.Join(parts, ", "))
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
	if err := cl.List(ctx, &children, client.MatchingFields{
		index.IndexFieldEventGatewayVirtualClusterProducePolicyOnEventGatewayVirtualClusterProducePolicyRef: client.ObjectKeyFromObject(obj).String(),
	}); err != nil {
		return fmt.Errorf("listing child produce policies: %w", err)
	}
	var (
		users           []string
		otherNamespaces int
	)
	for i := range children.Items {
		child := &children.Items[i]
		if child.Namespace != obj.Namespace {
			holds, err := otherNamespaceChildHolds(ctx, cl, child, child.GetKonnectID(), obj, "EventGatewayVirtualClusterProducePolicy")
			if err != nil {
				return err
			}
			if holds {
				otherNamespaces++
			}
			continue
		}
		users = append(users, client.ObjectKeyFromObject(child).String())
	}
	if len(users) > 0 || otherNamespaces > 0 {
		// The spec may have changed since programming. Only the actual
		// Konnect policy type tells us whether deletion can cascade.
		resp, err := sdk.GetEventGatewayVirtualClusterProducePolicy(ctx, sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyRequest{
			GatewayID: obj.GetGatewayID(), VirtualClusterID: obj.GetVirtualClusterID(), PolicyID: obj.GetKonnectID(),
		})
		if err != nil {
			return handleDeleteError(ctx, wrapErrIfKonnectOpFailed(err, GetOp, obj), obj)
		}
		if resp == nil || resp.EventGatewayPolicy == nil || resp.EventGatewayPolicy.Type == "" {
			return fmt.Errorf("failed getting %s: %w", obj.GetTypeName(), ErrNilResponse)
		}
		if resp.EventGatewayPolicy.Type != "schema_validation" {
			return deleteEventGatewayVirtualClusterProducePolicy(ctx, sdk, obj)
		}
		slices.Sort(users)
		return EventGatewayParentPolicyInUseError{Users: users, OtherNamespacesUsers: otherNamespaces}
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
	if err := cl.List(ctx, &children, client.MatchingFields{
		index.IndexFieldEventGatewayVirtualClusterConsumePolicyOnEventGatewayVirtualClusterConsumePolicyRef: client.ObjectKeyFromObject(obj).String(),
	}); err != nil {
		return fmt.Errorf("listing child consume policies: %w", err)
	}
	var (
		users           []string
		otherNamespaces int
	)
	for i := range children.Items {
		child := &children.Items[i]
		if child.Namespace != obj.Namespace {
			holds, err := otherNamespaceChildHolds(ctx, cl, child, child.GetKonnectID(), obj, "EventGatewayVirtualClusterConsumePolicy")
			if err != nil {
				return err
			}
			if holds {
				otherNamespaces++
			}
			continue
		}
		users = append(users, client.ObjectKeyFromObject(child).String())
	}
	if len(users) > 0 || otherNamespaces > 0 {
		resp, err := sdk.GetEventGatewayVirtualClusterConsumePolicy(ctx, sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyRequest{
			GatewayID: obj.GetGatewayID(), VirtualClusterID: obj.GetVirtualClusterID(), PolicyID: obj.GetKonnectID(),
		})
		if err != nil {
			return handleDeleteError(ctx, wrapErrIfKonnectOpFailed(err, GetOp, obj), obj)
		}
		if resp == nil || resp.EventGatewayPolicy == nil || resp.EventGatewayPolicy.Type == "" {
			return fmt.Errorf("failed getting %s: %w", obj.GetTypeName(), ErrNilResponse)
		}
		if resp.EventGatewayPolicy.Type != "schema_validation" {
			return deleteEventGatewayVirtualClusterConsumePolicy(ctx, sdk, obj)
		}
		slices.Sort(users)
		return EventGatewayParentPolicyInUseError{Users: users, OtherNamespacesUsers: otherNamespaces}
	}
	return deleteEventGatewayVirtualClusterConsumePolicy(ctx, sdk, obj)
}

// otherNamespaceChildHolds tells whether a child policy of another namespace
// than its parent holds back the deletion of the parent: it is in Konnect,
// under the parent, or a KongReferenceGrant lets it reference the parent, so
// that it may soon be. One no KongReferenceGrant lets in, and not in Konnect,
// doesn't: anyone could otherwise block the deletion of a policy of another
// namespace.
func otherNamespaceChildHolds(ctx context.Context, cl client.Client, child client.Object, konnectID string, parent client.Object, kind string) (bool, error) {
	if konnectID != "" {
		return true, nil
	}
	// A child policy references a parent policy of its own kind.
	gvk := metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(kind))
	err := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, child.GetNamespace(), parent.GetNamespace(), parent.GetName(), gvk, gvk)
	if crossnamespace.IsReferenceNotGranted(err) {
		return false, nil
	}
	return err == nil, err
}
