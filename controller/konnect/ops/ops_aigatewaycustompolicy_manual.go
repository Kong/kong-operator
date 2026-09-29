package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/internal/utils/konnectpagination"
)

// aiGatewayPoliciesProbePageSize is the page size used to list the Konnect
// policies of an AI Gateway when confirming that a failed custom policy
// deletion is blocked by policies using it.
const aiGatewayPoliciesProbePageSize = 100

// AIGatewayCustomPolicyInUseError is returned when Konnect rejects the deletion
// of an AIGatewayCustomPolicy because policies still use it (their type is the
// custom policy's Konnect name). Deletion is blocked until those policies are
// removed or stop using it.
type AIGatewayCustomPolicyInUseError struct {
	// CustomPolicyName is the Konnect name of the custom policy.
	CustomPolicyName string
	// Users are the AIGatewayPolicy objects in the cluster (namespace/name)
	// behind the Konnect policies using the custom policy.
	Users []string
	// KonnectPolicies are the names of the Konnect policies using the custom
	// policy, including ones not managed from this cluster.
	KonnectPolicies []string
	// UnmanagedKonnectPolicies are the names of the KonnectPolicies with no
	// AIGatewayPolicy in the cluster behind them.
	UnmanagedKonnectPolicies []string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e AIGatewayCustomPolicyInUseError) Error() string {
	return fmt.Sprintf(
		"custom policy %s is in use by policies %s, deletion blocked: %v",
		e.CustomPolicyName, strings.Join(e.KonnectPolicies, ", "), e.Err,
	)
}

// Unwrap returns the underlying Konnect API error.
func (e AIGatewayCustomPolicyInUseError) Unwrap() error {
	return e.Err
}

// DeletionBlockedMessage returns a user-facing message naming what still uses
// the custom policy.
func (e AIGatewayCustomPolicyInUseError) DeletionBlockedMessage() string {
	switch {
	case len(e.Users) > 0 && len(e.UnmanagedKonnectPolicies) > 0:
		return fmt.Sprintf(
			"deletion blocked: the custom policy is in use by AIGatewayPolicy %s, and by Konnect policies %s, "+
				"which are not managed from this cluster; delete them or stop using the custom policy and "+
				"the deletion will proceed automatically",
			strings.Join(e.Users, ", "), strings.Join(e.UnmanagedKonnectPolicies, ", "),
		)
	case len(e.Users) > 0:
		return fmt.Sprintf(
			"deletion blocked: the custom policy is in use by AIGatewayPolicy %s; "+
				"delete them or stop using the custom policy and the deletion will proceed automatically",
			strings.Join(e.Users, ", "),
		)
	case len(e.UnmanagedKonnectPolicies) == 0:
		// The lookup of the AIGatewayPolicy objects in the cluster failed: do
		// not claim which side manages the Konnect policies.
		return fmt.Sprintf(
			"deletion blocked: the custom policy is in use by Konnect policies %s; "+
				"delete them or stop using the custom policy and the deletion will proceed automatically",
			strings.Join(e.KonnectPolicies, ", "),
		)
	default:
		return fmt.Sprintf(
			"deletion blocked: the custom policy is in use by Konnect policies %s, which are not managed "+
				"from this cluster; delete them in Konnect and the deletion will proceed automatically",
			strings.Join(e.UnmanagedKonnectPolicies, ", "),
		)
	}
}

// deleteAIGatewayCustomPolicyGuarded deletes an AIGatewayCustomPolicy, but when
// Konnect refuses the deletion, it checks whether Konnect policies still use
// the custom policy and, if so, returns an AIGatewayCustomPolicyInUseError
// naming them (and the AIGatewayPolicy objects in the cluster behind them), so
// the reconciler can report a DeletionBlocked condition. The probe avoids
// relying on human-readable error details from Konnect.
func deleteAIGatewayCustomPolicyGuarded(
	ctx context.Context,
	customPoliciesSDK sdkkonnectgo.AIGatewayCustomPoliciesSDK,
	policiesSDK sdkkonnectgo.AIGatewayPoliciesSDK,
	cl client.Client,
	obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy,
) error {
	err := deleteAIGatewayCustomPolicy(ctx, customPoliciesSDK, obj)
	if err == nil || !errorIsBadRequest(err) {
		return err
	}

	logger := ctrllog.FromContext(ctx)
	name := obj.GetKonnectName()
	konnectPolicies, listErr := konnectPoliciesUsingCustomPolicy(ctx, policiesSDK, obj.GetGatewayID(), name)
	if listErr != nil {
		logger.Info("failed to determine whether policies using the custom policy blocked deletion",
			"type", obj.GetTypeName(), "id", obj.GetKonnectStatus().GetKonnectID(), "error", listErr.Error(),
		)
		return err
	}
	if len(konnectPolicies) == 0 {
		return err
	}

	inUse := AIGatewayCustomPolicyInUseError{CustomPolicyName: name, Err: err}
	for _, p := range konnectPolicies {
		inUse.KonnectPolicies = append(inUse.KonnectPolicies, p.GetName())
	}
	users, managedIDs, usersErr := aiGatewayPoliciesBehindKonnectPolicies(ctx, cl, obj, konnectPolicies)
	if usersErr != nil {
		// Still report the blockage, naming the Konnect policies only.
		logger.Info("failed to list AIGatewayPolicy objects using the custom policy",
			"type", obj.GetTypeName(), "error", usersErr.Error(),
		)
		return inUse
	}
	inUse.Users = users
	for _, p := range konnectPolicies {
		if !managedIDs[p.GetID()] {
			inUse.UnmanagedKonnectPolicies = append(inUse.UnmanagedKonnectPolicies, p.GetName())
		}
	}
	return inUse
}

// konnectPoliciesUsingCustomPolicy returns the Konnect policies of the AI
// Gateway whose type is the custom policy's Konnect name.
func konnectPoliciesUsingCustomPolicy(
	ctx context.Context,
	sdk sdkkonnectgo.AIGatewayPoliciesSDK,
	gatewayID string,
	customPolicyName string,
) ([]sdkkonnectcomp.AIGatewayPolicy, error) {
	var (
		policies []sdkkonnectcomp.AIGatewayPolicy
		after    *string
		// Cursors already requested, to detect a next-page cursor that does
		// not advance (directly or through a longer cycle).
		seenCursors = map[string]struct{}{}
	)
	for {
		resp, err := sdk.ListAiGatewayPolicies(ctx, sdkkonnectops.ListAiGatewayPoliciesRequest{
			GatewayID: gatewayID,
			PageSize:  new(int64(aiGatewayPoliciesProbePageSize)),
			PageAfter: after,
		})
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.ListAIGatewayPoliciesResponse == nil {
			return nil, ErrNilResponse
		}
		for _, p := range resp.ListAIGatewayPoliciesResponse.GetData() {
			if p.GetType() == customPolicyName {
				policies = append(policies, p)
			}
		}
		meta := resp.ListAIGatewayPoliciesResponse.GetMeta()
		page := meta.GetPage()
		next := page.GetNext()
		if next == nil || *next == "" {
			return policies, nil
		}
		cursor, err := konnectpagination.PageAfterCursorFromNextPageURL(*next)
		if err != nil {
			return nil, err
		}
		if _, ok := seenCursors[cursor]; ok {
			return nil, fmt.Errorf("next page cursor %q repeated while listing policies of AI Gateway %s", cursor, gatewayID)
		}
		seenCursors[cursor] = struct{}{}
		after = &cursor
	}
}

// aiGatewayPoliciesBehindKonnectPolicies returns the AIGatewayPolicy objects
// (namespace/name, sorted) behind the given Konnect policies using the custom
// policy, and the Konnect IDs of those policies. Candidates are the objects
// using the custom policy through customPolicyRef, or through type set to its
// Konnect name on the same AI Gateway, both looked up through indexes. Only
// those whose Konnect ID is one of the Konnect policies' are kept: a candidate
// not (yet) created in Konnect, or on another AI Gateway, does not block the
// deletion.
func aiGatewayPoliciesBehindKonnectPolicies(
	ctx context.Context,
	cl client.Client,
	obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy,
	konnectPolicies []sdkkonnectcomp.AIGatewayPolicy,
) ([]string, map[string]bool, error) {
	blocking := make(map[string]bool, len(konnectPolicies))
	for _, p := range konnectPolicies {
		if id := p.GetID(); id != "" {
			blocking[id] = true
		}
	}

	selectors := []client.MatchingFields{
		{index.IndexFieldAIGatewayPolicyOnAIGatewayCustomPolicyRef: client.ObjectKeyFromObject(obj).String()},
	}
	if gatewayID, name := obj.GetGatewayID(), obj.GetKonnectName(); gatewayID != "" && name != "" {
		selectors = append(selectors, client.MatchingFields{
			index.IndexFieldAIGatewayPolicyOnType: gatewayID + "/" + name,
		})
	}

	var users []string
	managedIDs := make(map[string]bool)
	for _, selector := range selectors {
		var list aiconfigurationv1alpha1.AIGatewayPolicyList
		if err := cl.List(ctx, &list, selector); err != nil {
			return nil, nil, err
		}
		for i := range list.Items {
			id := list.Items[i].GetKonnectID()
			if !blocking[id] {
				continue
			}
			managedIDs[id] = true
			if key := client.ObjectKeyFromObject(&list.Items[i]).String(); !slices.Contains(users, key) {
				users = append(users, key)
			}
		}
	}
	slices.Sort(users)
	return users, managedIDs, nil
}
