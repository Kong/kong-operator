package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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
	// Users are the policy objects referencing it, as "Kind namespace/name",
	// sorted.
	Users []string
	// Unblock tells how to unblock the deletion, e.g. "delete them or stop
	// forwarding to the virtual cluster".
	Unblock string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e EventGatewayEntityInUseError) Error() string {
	return fmt.Sprintf("%s is in use by %s, deletion blocked: %v", e.What, strings.Join(e.Users, ", "), e.Err)
}

// Unwrap returns the underlying Konnect API error.
func (e EventGatewayEntityInUseError) Unwrap() error {
	return e.Err
}

// DeletionBlockedMessage returns a user-facing message naming what still uses
// the entity.
func (e EventGatewayEntityInUseError) DeletionBlockedMessage() string {
	return fmt.Sprintf("deletion blocked: the %s is in use by %s; %s and the deletion will proceed automatically",
		e.What, strings.Join(e.Users, ", "), e.Unblock)
}

// referrerKind is a kind of policy object referencing other entities by
// object.
type referrerKind struct {
	// kind names the objects in messages.
	kind string
	// list returns an empty list of the objects.
	list func() client.ObjectList
	// refsTo returns the keys of the entities an object references.
	refsTo func(client.Object) []client.ObjectKey
}

// referrer returns the referrerKind of the objects T, listed by list, whose
// references refsTo returns.
func referrer[T client.Object](kind string, list func() client.ObjectList, refsTo func(T) []client.ObjectKey) referrerKind {
	return referrerKind{
		kind: kind,
		list: list,
		refsTo: func(obj client.Object) []client.ObjectKey {
			typed, ok := obj.(T)
			if !ok {
				return nil
			}
			return refsTo(typed)
		},
	}
}

// policyObjectsUsing returns the policy objects of the namespace, as "Kind
// namespace/name", sorted, that reference one of the entities: only those
// created in Konnect, which can be what Konnect refuses the deletion for.
func policyObjectsUsing(ctx context.Context, cl client.Client, namespace string, entities []client.ObjectKey, kinds ...referrerKind) ([]string, error) {
	var users []string
	for _, k := range kinds {
		list := k.list()
		if err := cl.List(ctx, list, client.InNamespace(namespace)); err != nil {
			return nil, fmt.Errorf("failed to list the %s objects using it: %w", k.kind, err)
		}
		items, err := apimeta.ExtractList(list)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			obj, ok := item.(client.Object)
			if !ok {
				continue
			}
			if withID, ok := obj.(interface{ GetKonnectID() string }); !ok || withID.GetKonnectID() == "" {
				continue
			}
			if slices.ContainsFunc(k.refsTo(obj), func(key client.ObjectKey) bool { return slices.Contains(entities, key) }) {
				users = append(users, k.kind+" "+client.ObjectKeyFromObject(obj).String())
			}
		}
	}
	slices.Sort(users)
	return users, nil
}

// inUseError returns the error of a deletion Konnect refused with err: an
// EventGatewayEntityInUseError when policy objects use the entity, or err as it
// is, e.g. for Konnect policies not managed from the cluster, or a refusal for
// another reason.
func inUseError(err error, what, unblock string, users []string) error {
	if len(users) == 0 {
		return err
	}
	return EventGatewayEntityInUseError{What: what, Users: users, Unblock: unblock, Err: err}
}

var listenerPolicyReferrers = referrer("EventGatewayListenerPolicy",
	func() client.ObjectList { return &configurationv1alpha1.EventGatewayListenerPolicyList{} },
	configurationv1alpha1.EventGatewayListenerPolicyRefsToEventGatewayVirtualCluster)

// deleteEventGatewayVirtualClusterGuarded deletes a virtual cluster. When
// Konnect refuses it, as listener policies still forward to it, and listener
// policy objects of its namespace reference it, it returns an
// EventGatewayEntityInUseError naming them.
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
	users, listErr := policyObjectsUsing(ctx, cl, obj.Namespace, []client.ObjectKey{client.ObjectKeyFromObject(obj)}, listenerPolicyReferrers)
	if listErr != nil {
		return fmt.Errorf("%w; %w", err, listErr)
	}
	return inUseError(err, "virtual cluster", "delete them or stop forwarding to the virtual cluster", users)
}

// deleteEventGatewayBackendClusterGuarded deletes a backend cluster. Konnect
// deletes its virtual clusters with it, but refuses while listener policies
// forward to one of them: when listener policy objects of its namespace do,
// it returns an EventGatewayEntityInUseError naming them.
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
	users, listErr := policyObjectsUsing(ctx, cl, obj.Namespace, virtualClusters, listenerPolicyReferrers)
	if listErr != nil {
		return fmt.Errorf("%w; %w", err, listErr)
	}
	return inUseError(err, "backend cluster", "delete them or stop forwarding to its virtual clusters", users)
}

// virtualClustersOf returns the keys of the virtual clusters of the namespace
// of the backend cluster whose parent it is.
func virtualClustersOf(ctx context.Context, cl client.Client, backendCluster *configurationv1alpha1.EventGatewayBackendCluster) ([]client.ObjectKey, error) {
	var list configurationv1alpha1.EventGatewayVirtualClusterList
	if err := cl.List(ctx, &list, client.InNamespace(backendCluster.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list the EventGatewayVirtualCluster objects of the backend cluster: %w", err)
	}
	var keys []client.ObjectKey
	for i := range list.Items {
		vc := &list.Items[i]
		ref := vc.Spec.EventGatewayBackendClusterRef.NamespacedRef
		if ref == nil || ref.Name != backendCluster.Name {
			continue
		}
		// An unset namespace is the namespace of the virtual cluster, listed in
		// the namespace of the backend cluster.
		if namespace := ptr.Deref(ref.Namespace, ""); namespace != "" && namespace != backendCluster.Namespace {
			continue
		}
		keys = append(keys, client.ObjectKeyFromObject(vc))
	}
	return keys, nil
}
