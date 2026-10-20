package ops

import (
	"context"
	"fmt"
	"slices"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// eventGatewayListenersProbePageSize is the page size used to list the
// listeners of an Event Gateway when checking whether their policies use a
// TLS trust bundle being deleted.
const eventGatewayListenersProbePageSize = 100

// EventGatewayTLSTrustBundleInUseError is returned when an
// EventGatewayTLSTrustBundle is deleted while Konnect listener policies still
// reference it. Konnect allows deleting a TLS trust bundle used by listener
// policies, leaving them referencing a missing trust bundle, which fails their
// next update. The deletion is therefore blocked until those policies are
// removed or stop referencing it.
type EventGatewayTLSTrustBundleInUseError struct {
	// Users are the EventGatewayListenerPolicy objects in the trust bundle's
	// namespace (namespace/name, sorted) behind the Konnect policies using it.
	Users []string
	// OtherNamespacesUsers is the number of EventGatewayListenerPolicy objects
	// in other namespaces behind the Konnect policies using the trust bundle,
	// counted rather than named so as not to disclose them.
	OtherNamespacesUsers int
	// UnmanagedKonnectPolicies are the Konnect policies (listener/policy,
	// sorted) using the trust bundle with no EventGatewayListenerPolicy in the
	// cluster behind them.
	UnmanagedKonnectPolicies []string
}

// Error implements the error interface.
func (e EventGatewayTLSTrustBundleInUseError) Error() string {
	return "TLS trust bundle is in use, deletion blocked: " + e.usersDescription()
}

// DeletionBlockedMessage returns a user-facing message naming what still uses
// the trust bundle.
func (e EventGatewayTLSTrustBundleInUseError) DeletionBlockedMessage() string {
	return fmt.Sprintf(
		"deletion blocked: the TLS trust bundle is in use by %s; delete them or remove their references "+
			"to it and the deletion will proceed automatically",
		e.usersDescription(),
	)
}

func (e EventGatewayTLSTrustBundleInUseError) usersDescription() string {
	return describePolicyUsers("EventGatewayListenerPolicy", "Konnect listener policies", e.Users, e.OtherNamespacesUsers, e.UnmanagedKonnectPolicies)
}

// deleteEventGatewayTLSTrustBundleGuarded deletes an EventGatewayTLSTrustBundle
// from Konnect unless Konnect listener policies still reference it, in which
// case it returns an EventGatewayTLSTrustBundleInUseError so the reconciler
// keeps the finalizer and reports a DeletionBlocked condition. What blocks the
// deletion is read from Konnect, whatever references it (an
// EventGatewayListenerPolicy's namespacedRef, or a Konnect ID or name set in
// the cluster or outside it), matching the trust bundle's name as stored in
// Konnect (a rename may not have reached it yet). The deletion is retried
// periodically, and right away when an EventGatewayListenerPolicy referencing
// the trust bundle through namespacedRef is deleted (once gone, it no longer
// uses the trust bundle in Konnect).
func deleteEventGatewayTLSTrustBundleGuarded(
	ctx context.Context,
	trustBundlesSDK sdkkonnectgo.EventGatewayTLSTrustBundlesSDK,
	listenersSDK sdkkonnectgo.EventGatewayListenersSDK,
	policiesSDK sdkkonnectgo.EventGatewayListenerPoliciesSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayTLSTrustBundle,
) error {
	gatewayID, id := obj.GetGatewayID(), obj.GetKonnectID()
	if gatewayID == "" || id == "" {
		// Nothing to look up: the generated delete reports the missing gateway
		// ID (ops.Delete recovers a missing Konnect ID before calling this).
		return deleteEventGatewayTLSTrustBundle(ctx, trustBundlesSDK, obj)
	}
	resp, err := trustBundlesSDK.GetEventGatewayTLSTrustBundle(ctx, gatewayID, id)
	if err != nil {
		if ErrIsNotFound(err) {
			// Already gone from Konnect (with its gateway or out of band): the
			// generated delete treats the not found response as deleted.
			return deleteEventGatewayTLSTrustBundle(ctx, trustBundlesSDK, obj)
		}
		return fmt.Errorf("failed to get %s %s from Konnect: %w", obj.GetTypeName(), client.ObjectKeyFromObject(obj), err)
	}
	if resp == nil || resp.TLSTrustBundle == nil {
		return ErrNilResponse
	}
	konnectPolicies, err := konnectListenerPoliciesUsingTLSTrustBundle(
		ctx, listenersSDK, policiesSDK, gatewayID, id, resp.TLSTrustBundle.GetName(),
	)
	if err != nil {
		return fmt.Errorf(
			"failed to determine whether listener policies use %s %s: %w",
			obj.GetTypeName(), client.ObjectKeyFromObject(obj), err,
		)
	}
	if len(konnectPolicies) == 0 {
		return deleteEventGatewayTLSTrustBundle(ctx, trustBundlesSDK, obj)
	}

	var list configurationv1alpha1.EventGatewayListenerPolicyList
	if err := cl.List(ctx, &list); err != nil {
		return fmt.Errorf("failed to list EventGatewayListenerPolicy objects: %w", err)
	}
	managed := make(map[string]client.ObjectKey, len(list.Items))
	for i := range list.Items {
		if policyID := list.Items[i].GetKonnectID(); policyID != "" {
			managed[policyID] = client.ObjectKeyFromObject(&list.Items[i])
		}
	}
	u := classifyPolicyUsers(obj.GetNamespace(), konnectPolicies, managed)
	return EventGatewayTLSTrustBundleInUseError{
		Users:                    u.users,
		OtherNamespacesUsers:     u.otherNamespaces,
		UnmanagedKonnectPolicies: u.unmanaged,
	}
}

// konnectListenerPoliciesUsingTLSTrustBundle returns the Konnect policies of
// the Event Gateway's listeners whose client authentication references the TLS
// trust bundle, by its Konnect ID or by its Konnect name (Konnect stores
// references as written).
func konnectListenerPoliciesUsingTLSTrustBundle(
	ctx context.Context,
	listenersSDK sdkkonnectgo.EventGatewayListenersSDK,
	policiesSDK sdkkonnectgo.EventGatewayListenerPoliciesSDK,
	gatewayID, trustBundleID, trustBundleName string,
) ([]konnectPolicyUser, error) {
	var (
		users []konnectPolicyUser
		after *string
		// Cursors already requested, to detect a next-page cursor that does
		// not advance (directly or through a longer cycle).
		seenCursors = map[string]struct{}{}
	)
	for {
		resp, err := listenersSDK.ListEventGatewayListeners(ctx, sdkkonnectops.ListEventGatewayListenersRequest{
			GatewayID: gatewayID,
			PageSize:  new(int64(eventGatewayListenersProbePageSize)),
			PageAfter: after,
		})
		if err != nil {
			if ErrIsNotFound(err) {
				// The gateway is gone from Konnect: no policy uses the trust
				// bundle anymore.
				return users, nil
			}
			return nil, err
		}
		if resp == nil || resp.ListEventGatewayListenersResponse == nil {
			return nil, ErrNilResponse
		}
		for _, l := range resp.ListEventGatewayListenersResponse.GetData() {
			policies, err := listenerPoliciesTrustBundleRefs(ctx, policiesSDK, gatewayID, l.GetID())
			if err != nil {
				if ErrIsNotFound(err) {
					// The listener was deleted while listing: none of its
					// policies use the trust bundle anymore.
					continue
				}
				return nil, fmt.Errorf("listing policies of listener %s: %w", l.GetName(), err)
			}
			for _, p := range policies {
				if p.usesTrustBundle(trustBundleID, trustBundleName) {
					users = append(users, konnectPolicyUser{id: p.ID, name: p.Name, parentName: l.GetName()})
				}
			}
		}
		meta := resp.ListEventGatewayListenersResponse.GetMeta()
		page := meta.GetPage()
		if after, err = nextPageCursor(page.GetNext(), seenCursors); err != nil {
			return nil, fmt.Errorf("listing listeners of Event Gateway %s: %w", gatewayID, err)
		}
		if after == nil {
			return users, nil
		}
	}
}

// listenerPolicyTrustBundleRefs is the part of a Konnect listener policy
// needed to tell which TLS trust bundles it references.
type listenerPolicyTrustBundleRefs struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Config struct {
		ClientAuthentication *struct {
			TLSTrustBundles []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"tls_trust_bundles"`
		} `json:"client_authentication"`
	} `json:"config"`
}

func (p listenerPolicyTrustBundleRefs) usesTrustBundle(id, name string) bool {
	if p.Config.ClientAuthentication == nil {
		return false
	}
	return slices.ContainsFunc(p.Config.ClientAuthentication.TLSTrustBundles, func(ref struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}) bool {
		return (id != "" && ref.ID == id) || (name != "" && ref.Name == name)
	})
}

// listenerPoliciesTrustBundleRefs lists the policies of a Konnect listener
// with their TLS trust bundle references. The SDK models a listener policy's
// config as an empty struct, dropping it, so the references are read from the
// raw response body instead (the SDK restores it after decoding).
func listenerPoliciesTrustBundleRefs(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayListenerPoliciesSDK,
	gatewayID, listenerID string,
) ([]listenerPolicyTrustBundleRefs, error) {
	resp, err := sdk.ListEventGatewayListenerPolicies(ctx, sdkkonnectops.ListEventGatewayListenerPoliciesRequest{
		GatewayID:  gatewayID,
		ListenerID: listenerID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, ErrNilResponse
	}
	var policies []listenerPolicyTrustBundleRefs
	if err := decodeRawResponseBody(resp.RawResponse, &policies); err != nil {
		return nil, fmt.Errorf("listener policies: %w", err)
	}
	return policies, nil
}
