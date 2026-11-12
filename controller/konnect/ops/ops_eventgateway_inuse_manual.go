package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// EventGatewayEntityInUseError is returned when Konnect refuses to delete an
// Event Gateway entity that policy objects of the cluster, created in Konnect,
// still reference by object, e.g. a virtual cluster listener policies forward
// to. Deletion is blocked until those policies are deleted or stop referencing
// it: the entity's controller watches them, so the deletion is retried once
// one changes.
type EventGatewayEntityInUseError struct {
	// What names the entity in messages, e.g. "virtual cluster".
	What string
	// Users are the policy objects of the entity's namespace referencing it,
	// as "Kind namespace/name", sorted.
	Users []string
	// OtherNamespacesUsers is the number of policy objects of other namespaces
	// referencing it, counted rather than named so as not to disclose them.
	OtherNamespacesUsers int
	// Unblock tells how to unblock the deletion, e.g. "delete them or stop
	// forwarding to the virtual cluster".
	Unblock string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e EventGatewayEntityInUseError) Error() string {
	return fmt.Sprintf("%s is in use by %s, deletion blocked: %v", e.What, e.users(), e.Err)
}

// Unwrap returns the underlying Konnect API error.
func (e EventGatewayEntityInUseError) Unwrap() error {
	return e.Err
}

// DeletionBlockedMessage returns a user-facing message naming what still uses
// the entity.
func (e EventGatewayEntityInUseError) DeletionBlockedMessage() string {
	return fmt.Sprintf("deletion blocked: the %s is in use by %s; %s and the deletion will proceed automatically",
		e.What, e.users(), e.Unblock)
}

// users describes the policy objects using the entity.
func (e EventGatewayEntityInUseError) users() string {
	parts := slices.Clone(e.Users)
	if e.OtherNamespacesUsers > 0 {
		parts = append(parts, fmt.Sprintf("%d policy objects in other namespaces", e.OtherNamespacesUsers))
	}
	return strings.Join(parts, ", ")
}

// referrerKind is a kind of policy object referencing other entities by
// object.
type referrerKind struct {
	// kind names the objects in messages.
	kind string
	// list returns an empty list of the objects.
	list func() client.ObjectList
	// field is the index of the keys of the entities an object references.
	field string
}

// policyObjectsUsing returns the policy objects that reference one of the
// entities: only those created in Konnect, which can be what Konnect refuses
// the deletion for. Those of the namespace are named, as "Kind
// namespace/name", sorted, and those of other namespaces counted.
func policyObjectsUsing(
	ctx context.Context, cl client.Client, namespace string, entities []client.ObjectKey, kinds ...referrerKind,
) (users []string, otherNamespaces int, err error) {
	for _, k := range kinds {
		seen := map[client.ObjectKey]struct{}{}
		for _, entity := range entities {
			list := k.list()
			if err := cl.List(ctx, list, client.MatchingFields{k.field: entity.String()}); err != nil {
				return nil, 0, fmt.Errorf("failed to list the %s objects using it: %w", k.kind, err)
			}
			items, err := apimeta.ExtractList(list)
			if err != nil {
				return nil, 0, err
			}
			for _, item := range items {
				obj, ok := item.(client.Object)
				if !ok {
					continue
				}
				if withID, ok := obj.(interface{ GetKonnectID() string }); !ok || withID.GetKonnectID() == "" {
					continue
				}
				key := client.ObjectKeyFromObject(obj)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				if key.Namespace == namespace {
					users = append(users, k.kind+" "+key.String())
				} else {
					otherNamespaces++
				}
			}
		}
	}
	slices.Sort(users)
	return users, otherNamespaces, nil
}

// inUseError returns the error of a deletion Konnect refused with err: an
// EventGatewayEntityInUseError when policy objects use the entity, or err as it
// is, e.g. for Konnect policies not managed from the cluster, or a refusal for
// another reason.
func inUseError(err error, what, unblock string, users []string, otherNamespaces int) error {
	if len(users) == 0 && otherNamespaces == 0 {
		return err
	}
	return EventGatewayEntityInUseError{What: what, Users: users, OtherNamespacesUsers: otherNamespaces, Unblock: unblock, Err: err}
}

var listenerPolicyReferrers = referrerKind{
	kind:  "EventGatewayListenerPolicy",
	list:  func() client.ObjectList { return &configurationv1alpha1.EventGatewayListenerPolicyList{} },
	field: index.IndexFieldEventGatewayListenerPolicyOnEventGatewayVirtualClusterRef,
}

// deleteEventGatewayVirtualClusterGuarded deletes a virtual cluster. When
// Konnect refuses it, as listener policies still forward to it, and listener
// policy objects reference it, it returns an EventGatewayEntityInUseError
// naming them.
func deleteEventGatewayVirtualClusterGuarded(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayVirtualClustersSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayVirtualCluster,
) error {
	err := deleteEventGatewayVirtualCluster(ctx, sdk, obj)
	if err == nil || !errorIsBadRequest(err) {
		return err
	}
	users, otherNamespaces, listErr := policyObjectsUsing(ctx, cl, obj.Namespace, []client.ObjectKey{client.ObjectKeyFromObject(obj)}, listenerPolicyReferrers)
	if listErr != nil {
		return fmt.Errorf("%w; %w", err, listErr)
	}
	return inUseError(err, "virtual cluster", "delete them or stop forwarding to the virtual cluster", users, otherNamespaces)
}

// deleteEventGatewayBackendClusterGuarded deletes a backend cluster. Konnect
// deletes its virtual clusters with it, but refuses while listener policies
// forward to one of them: when listener policy objects do, it returns an
// EventGatewayEntityInUseError naming them.
func deleteEventGatewayBackendClusterGuarded(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayBackendClustersSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayBackendCluster,
) error {
	err := deleteEventGatewayBackendCluster(ctx, sdk, obj)
	if err == nil || !errorIsBadRequest(err) {
		return err
	}
	virtualClusters, listErr := virtualClustersOf(ctx, cl, obj)
	if listErr != nil {
		return fmt.Errorf("%w; %w", err, listErr)
	}
	if len(virtualClusters) == 0 {
		return err
	}
	users, otherNamespaces, listErr := policyObjectsUsing(ctx, cl, obj.Namespace, virtualClusters, listenerPolicyReferrers)
	if listErr != nil {
		return fmt.Errorf("%w; %w", err, listErr)
	}
	return inUseError(err, "backend cluster", "delete them or stop forwarding to its virtual clusters", users, otherNamespaces)
}

// virtualClustersOf returns the keys of the virtual clusters, of any
// namespace, whose parent the backend cluster is.
func virtualClustersOf(ctx context.Context, cl client.Client, backendCluster *configurationv1alpha1.EventGatewayBackendCluster) ([]client.ObjectKey, error) {
	var list configurationv1alpha1.EventGatewayVirtualClusterList
	if err := cl.List(ctx, &list, client.MatchingFields{
		index.IndexFieldEventGatewayVirtualClusterOnEventGatewayBackendClusterRef: client.ObjectKeyFromObject(backendCluster).String(),
	}); err != nil {
		return nil, fmt.Errorf("failed to list the EventGatewayVirtualCluster objects of the backend cluster: %w", err)
	}
	keys := make([]client.ObjectKey, 0, len(list.Items))
	for i := range list.Items {
		keys = append(keys, client.ObjectKeyFromObject(&list.Items[i]))
	}
	return keys, nil
}
