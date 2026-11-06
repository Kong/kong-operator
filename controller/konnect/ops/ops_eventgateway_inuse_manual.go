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

// EventGatewayEntityInUseError is returned when Konnect refuses to delete an
// Event Gateway entity that policy objects of the cluster still reference by
// object, e.g. a schema registry. Deletion is blocked
// until those policies are deleted or stop referencing it: the entity's
// controller watches them, so the deletion is retried once one changes.
type EventGatewayEntityInUseError struct {
	// What names the entity in messages, e.g. "schema registry".
	What string
	// Users are the policy objects referencing it, as "Kind namespace/name",
	// sorted.
	Users []string
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
	return fmt.Sprintf(
		"deletion blocked: the %s is in use by %s; delete them or stop using the %s and the deletion will proceed automatically",
		e.What, strings.Join(e.Users, ", "), e.What,
	)
}

// deleteEventGatewaySchemaRegistryGuarded deletes a schema registry. When
// Konnect refuses it, as policies still use the registry, and policy objects of
// its namespace reference it, it returns an EventGatewayEntityInUseError naming
// them. Otherwise Konnect's error is returned as it is, e.g. for Konnect
// policies not managed from the cluster.
func deleteEventGatewaySchemaRegistryGuarded(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaySchemaRegistriesSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewaySchemaRegistry,
) error {
	err := deleteEventGatewaySchemaRegistry(ctx, sdk, obj)
	if err == nil || !errorIsBadRequest(err) {
		return err
	}
	key := client.ObjectKeyFromObject(obj)
	var users []string
	var produce configurationv1alpha1.EventGatewayVirtualClusterProducePolicyList
	if listErr := cl.List(ctx, &produce, client.InNamespace(obj.Namespace)); listErr != nil {
		return fmt.Errorf("%w; failed to list the EventGatewayVirtualClusterProducePolicy objects using it: %w", err, listErr)
	}
	for i := range produce.Items {
		if slices.Contains(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewaySchemaRegistry(&produce.Items[i]), key) {
			users = append(users, "EventGatewayVirtualClusterProducePolicy "+client.ObjectKeyFromObject(&produce.Items[i]).String())
		}
	}
	var consume configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyList
	if listErr := cl.List(ctx, &consume, client.InNamespace(obj.Namespace)); listErr != nil {
		return fmt.Errorf("%w; failed to list the EventGatewayVirtualClusterConsumePolicy objects using it: %w", err, listErr)
	}
	for i := range consume.Items {
		if slices.Contains(configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRefsToEventGatewaySchemaRegistry(&consume.Items[i]), key) {
			users = append(users, "EventGatewayVirtualClusterConsumePolicy "+client.ObjectKeyFromObject(&consume.Items[i]).String())
		}
	}
	if len(users) == 0 {
		return err
	}
	slices.Sort(users)
	return EventGatewayEntityInUseError{What: "schema registry", Users: users, Err: err}
}
