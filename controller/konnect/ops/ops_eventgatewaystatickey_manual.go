package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// eventGatewayVirtualClustersProbePageSize is the page size used to list the
// virtual clusters of an Event Gateway when confirming that a failed static
// key deletion is blocked by produce policies using it.
const eventGatewayVirtualClustersProbePageSize = 100

// EventGatewayStaticKeyInUseError is returned when Konnect rejects the deletion
// of an EventGatewayStaticKey because produce policies still use it. Deletion
// is blocked until those policies are removed or stop using it.
type EventGatewayStaticKeyInUseError struct {
	// Users are the EventGatewayVirtualClusterProducePolicy objects in the
	// static key's namespace (namespace/name, sorted) behind the Konnect
	// policies using it.
	Users []string
	// OtherNamespacesUsers is the number of
	// EventGatewayVirtualClusterProducePolicy objects in other namespaces behind
	// the Konnect policies using the static key, counted rather than named so
	// as not to disclose them.
	OtherNamespacesUsers int
	// UnmanagedKonnectPolicies are the Konnect produce policies
	// (virtualCluster/policy, sorted) using the static key with no
	// EventGatewayVirtualClusterProducePolicy in the cluster behind them.
	UnmanagedKonnectPolicies []string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e EventGatewayStaticKeyInUseError) Error() string {
	return fmt.Sprintf("static key is in use by %s, deletion blocked: %v", e.usersDescription(), e.Err)
}

// Unwrap returns the underlying Konnect API error.
func (e EventGatewayStaticKeyInUseError) Unwrap() error {
	return e.Err
}

// DeletionBlockedMessage returns a user-facing message naming what still uses
// the static key.
func (e EventGatewayStaticKeyInUseError) DeletionBlockedMessage() string {
	return fmt.Sprintf(
		"deletion blocked: the static key is in use by %s; delete them or stop using the static key "+
			"and the deletion will proceed automatically",
		e.usersDescription(),
	)
}

func (e EventGatewayStaticKeyInUseError) usersDescription() string {
	return describePolicyUsers("EventGatewayVirtualClusterProducePolicy", "Konnect produce policies", e.Users, e.OtherNamespacesUsers, e.UnmanagedKonnectPolicies)
}

// updateEventGatewayStaticKey makes sure the static key still exists in
// Konnect, recreating it when it was deleted out of band. Konnect static keys
// can't be updated (and the CRD spec is immutable once created), so there is
// nothing else to enforce.
func updateEventGatewayStaticKey(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.EventGatewayStaticKeysSDK,
	obj *configurationv1alpha1.EventGatewayStaticKey,
) error {
	parentID := obj.GetGatewayID()
	if parentID == "" {
		return CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "KonnectEventGateway", Op: UpdateOp}
	}

	_, err := sdk.GetEventGatewayStaticKey(ctx, parentID, obj.GetKonnectStatus().GetKonnectID())
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, obj); errWrap != nil {
		return handleUpdateError(ctx, err, obj, func(ctx context.Context) error {
			return recreateEventGatewayStaticKey(ctx, cl, sdk, obj)
		})
	}
	return nil
}

// recreateEventGatewayStaticKey recreates a static key deleted from Konnect.
// When Konnect reports its name is taken, the key may be this object's own,
// recreated earlier with its new ID lost (e.g. the status update failed): it
// is then found by the object's UID label instead of failing forever and
// leaving it orphaned.
func recreateEventGatewayStaticKey(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.EventGatewayStaticKeysSDK,
	obj *configurationv1alpha1.EventGatewayStaticKey,
) error {
	err := createEventGatewayStaticKey(ctx, cl, sdk, obj)
	if err == nil || !isCreateNameConflict(obj, err) {
		return err
	}
	id, errGet := getEventGatewayStaticKeyForUID(ctx, sdk, obj)
	if errGet != nil {
		return fmt.Errorf("%w; looking up the static key by UID: %w", err, errGet)
	}
	obj.SetKonnectID(id)
	return nil
}

// deleteEventGatewayStaticKeyGuarded deletes an EventGatewayStaticKey, but when
// Konnect refuses the deletion, it checks whether Konnect produce policies
// still use the static key and, if so, returns an
// EventGatewayStaticKeyInUseError naming them, so the reconciler can report a
// DeletionBlocked condition. The probe avoids relying on human-readable error
// details from Konnect. The EventGatewayStaticKey controller watches produce
// policies, so the deletion is retried as soon as one referencing the static
// key through namespacedRef changes or is deleted.
func deleteEventGatewayStaticKeyGuarded(
	ctx context.Context,
	staticKeysSDK sdkkonnectgo.EventGatewayStaticKeysSDK,
	virtualClustersSDK sdkkonnectgo.EventGatewayVirtualClustersSDK,
	producePoliciesSDK sdkkonnectgo.EventGatewayVirtualClusterProducePoliciesSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayStaticKey,
) error {
	err := deleteEventGatewayStaticKey(ctx, staticKeysSDK, obj)
	if err == nil || !errorIsBadRequest(err) {
		return err
	}

	logger := ctrllog.FromContext(ctx)
	konnectPolicies, probeErr := konnectProducePoliciesUsingStaticKey(
		ctx, virtualClustersSDK, producePoliciesSDK, obj.GetGatewayID(), obj.GetKonnectID(), obj.GetKonnectName(),
	)
	if probeErr != nil {
		logger.Info("failed to determine whether produce policies using the static key blocked deletion",
			"type", obj.GetTypeName(), "id", obj.GetKonnectID(), "error", probeErr.Error(),
		)
		return err
	}
	if len(konnectPolicies) == 0 {
		return err
	}

	var list configurationv1alpha1.EventGatewayVirtualClusterProducePolicyList
	if listErr := cl.List(ctx, &list); listErr != nil {
		// Without the cluster's policies, the users can't be told apart from
		// unmanaged Konnect policies: report the failure and retry.
		return fmt.Errorf("%w; failed to list EventGatewayVirtualClusterProducePolicy objects using it: %w", err, listErr)
	}
	managed := make(map[string]client.ObjectKey, len(list.Items))
	for i := range list.Items {
		if policyID := list.Items[i].GetKonnectID(); policyID != "" {
			managed[policyID] = client.ObjectKeyFromObject(&list.Items[i])
		}
	}
	u := classifyPolicyUsers(obj.GetNamespace(), konnectPolicies, managed)
	return EventGatewayStaticKeyInUseError{
		Users:                    u.users,
		OtherNamespacesUsers:     u.otherNamespaces,
		UnmanagedKonnectPolicies: u.unmanaged,
		Err:                      err,
	}
}

// konnectProducePoliciesUsingStaticKey returns the Konnect produce policies of
// the Event Gateway's virtual clusters whose config references the static key
// by its Konnect ID or name.
func konnectProducePoliciesUsingStaticKey(
	ctx context.Context,
	virtualClustersSDK sdkkonnectgo.EventGatewayVirtualClustersSDK,
	producePoliciesSDK sdkkonnectgo.EventGatewayVirtualClusterProducePoliciesSDK,
	gatewayID, staticKeyID, staticKeyName string,
) ([]konnectPolicyUser, error) {
	var (
		users []konnectPolicyUser
		after *string
		// Cursors already requested, to detect a next-page cursor that does
		// not advance (directly or through a longer cycle).
		seenCursors = map[string]struct{}{}
	)
	for {
		resp, err := virtualClustersSDK.ListEventGatewayVirtualClusters(ctx, sdkkonnectops.ListEventGatewayVirtualClustersRequest{
			GatewayID: gatewayID,
			PageSize:  new(int64(eventGatewayVirtualClustersProbePageSize)),
			PageAfter: after,
		})
		if err != nil {
			if ErrIsNotFound(err) {
				// The gateway is gone from Konnect: no policy uses the static
				// key anymore.
				return users, nil
			}
			return nil, err
		}
		if resp == nil || resp.ListVirtualClustersResponse == nil {
			return nil, ErrNilResponse
		}
		for _, vc := range resp.ListVirtualClustersResponse.GetData() {
			policies, err := producePolicyConfigs(ctx, producePoliciesSDK, gatewayID, vc.GetID())
			if err != nil {
				if ErrIsNotFound(err) {
					// The virtual cluster was deleted while listing: none of
					// its policies use the static key anymore.
					continue
				}
				return nil, fmt.Errorf("listing produce policies of virtual cluster %s: %w", vc.GetName(), err)
			}
			for _, p := range policies {
				if referencesStaticKey(p.Config, staticKeyID, staticKeyName) {
					users = append(users, konnectPolicyUser{id: p.ID, name: p.Name, parentName: vc.GetName()})
				}
			}
		}
		meta := resp.ListVirtualClustersResponse.GetMeta()
		page := meta.GetPage()
		if after, err = nextPageCursor(page.GetNext(), seenCursors); err != nil {
			return nil, fmt.Errorf("listing virtual clusters of Event Gateway %s: %w", gatewayID, err)
		}
		if after == nil {
			return users, nil
		}
	}
}

// producePolicyConfig is a Konnect produce policy with its raw config.
type producePolicyConfig struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Config any    `json:"config"`
}

// producePolicyConfigs lists the produce policies of a Konnect virtual
// cluster with their config, read from the raw response body (see
// decodeRawResponseBody).
func producePolicyConfigs(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayVirtualClusterProducePoliciesSDK,
	gatewayID, virtualClusterID string,
) ([]producePolicyConfig, error) {
	resp, err := sdk.ListEventGatewayVirtualClusterProducePolicies(ctx, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest{
		GatewayID:        gatewayID,
		VirtualClusterID: virtualClusterID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, ErrNilResponse
	}
	var policies []producePolicyConfig
	if err := decodeRawResponseBody(resp.RawResponse, &policies); err != nil {
		return nil, fmt.Errorf("produce policies: %w", err)
	}
	return policies, nil
}

// referencesStaticKey reports whether a produce policy config references the
// static key: a static encryption key ({"type": "static", "key": {"id"|"name"}})
// anywhere in it, e.g. an encrypt policy's encryption_key or the
// encryption_key of one of an encrypt fields policy's fields.
func referencesStaticKey(config any, id, name string) bool {
	switch v := config.(type) {
	case map[string]any:
		if v["type"] == "static" {
			if key, ok := v["key"].(map[string]any); ok {
				if (id != "" && key["id"] == id) || (name != "" && key["name"] == name) {
					return true
				}
			}
		}
		for _, child := range v {
			if referencesStaticKey(child, id, name) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if referencesStaticKey(child, id, name) {
				return true
			}
		}
	}
	return false
}
