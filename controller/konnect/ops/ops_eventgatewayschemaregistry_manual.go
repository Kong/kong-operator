package ops

import (
	"context"
	"fmt"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// EventGatewaySchemaRegistryInUseError is returned when Konnect rejects the
// deletion of an EventGatewaySchemaRegistry because produce or consume
// policies still use it. Deletion is blocked until those policies are removed
// or stop using it: the EventGatewaySchemaRegistry controller watches the
// policy objects, so the deletion is retried once one of them changes.
type EventGatewaySchemaRegistryInUseError struct {
	// ProduceUsers and ConsumeUsers are the policy objects in the schema
	// registry's namespace (namespace/name, sorted) behind the Konnect
	// policies using it.
	ProduceUsers []string
	ConsumeUsers []string
	// OtherNamespacesProduceUsers and OtherNamespacesConsumeUsers count the
	// policy objects in other namespaces behind the Konnect policies using the
	// schema registry, rather than naming them, so as not to disclose them.
	OtherNamespacesProduceUsers int
	OtherNamespacesConsumeUsers int
	// UnmanagedProducePolicies and UnmanagedConsumePolicies are the Konnect
	// policies (virtualCluster/policy, sorted) using the schema registry with
	// no policy object in the cluster behind them.
	UnmanagedProducePolicies []string
	UnmanagedConsumePolicies []string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e EventGatewaySchemaRegistryInUseError) Error() string {
	return fmt.Sprintf("schema registry is in use by %s, deletion blocked: %v", e.usersDescription(), e.Err)
}

// Unwrap returns the underlying Konnect API error.
func (e EventGatewaySchemaRegistryInUseError) Unwrap() error {
	return e.Err
}

// DeletionBlockedMessage returns a user-facing message naming what still uses
// the schema registry.
func (e EventGatewaySchemaRegistryInUseError) DeletionBlockedMessage() string {
	return fmt.Sprintf(
		"deletion blocked: the schema registry is in use by %s; delete them or stop using the schema registry "+
			"and the deletion will proceed automatically",
		e.usersDescription(),
	)
}

func (e EventGatewaySchemaRegistryInUseError) usersDescription() string {
	var parts []string
	if d := describePolicyUsers("EventGatewayVirtualClusterProducePolicy", "Konnect produce policies",
		e.ProduceUsers, e.OtherNamespacesProduceUsers, e.UnmanagedProducePolicies); d != "" {
		parts = append(parts, d)
	}
	if d := describePolicyUsers("EventGatewayVirtualClusterConsumePolicy", "Konnect consume policies",
		e.ConsumeUsers, e.OtherNamespacesConsumeUsers, e.UnmanagedConsumePolicies); d != "" {
		parts = append(parts, d)
	}
	return strings.Join(parts, ", and by ")
}

// deleteEventGatewaySchemaRegistryGuarded deletes an EventGatewaySchemaRegistry,
// but when Konnect refuses the deletion, it checks whether Konnect produce or
// consume policies still use the schema registry and, if so, returns an
// EventGatewaySchemaRegistryInUseError naming them, so the reconciler can
// report a DeletionBlocked condition. The policies are matched on the Konnect
// ID of the schema registry and on its name as stored in Konnect (a rename may
// not have reached it yet). The probe avoids relying on
// human-readable error details from Konnect: any other refusal, or a failed
// probe, returns Konnect's error as it is.
func deleteEventGatewaySchemaRegistryGuarded(
	ctx context.Context,
	schemaRegistriesSDK sdkkonnectgo.EventGatewaySchemaRegistriesSDK,
	virtualClustersSDK sdkkonnectgo.EventGatewayVirtualClustersSDK,
	producePoliciesSDK sdkkonnectgo.EventGatewayVirtualClusterProducePoliciesSDK,
	consumePoliciesSDK sdkkonnectgo.EventGatewayVirtualClusterConsumePoliciesSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewaySchemaRegistry,
) error {
	err := deleteEventGatewaySchemaRegistry(ctx, schemaRegistriesSDK, obj)
	if err == nil || !errorIsBadRequest(err) {
		return err
	}

	logger := ctrllog.FromContext(ctx)
	// A policy referencing the schema registry by name holds the name stored in
	// Konnect, which a rename in the spec may not have reached yet.
	name, probeErr := konnectSchemaRegistryName(ctx, schemaRegistriesSDK, obj.GetGatewayID(), obj.GetKonnectID())
	var produce, consume []konnectPolicyUser
	if probeErr == nil {
		produce, consume, probeErr = konnectPoliciesUsingSchemaRegistry(
			ctx, virtualClustersSDK, producePoliciesSDK, consumePoliciesSDK, obj.GetGatewayID(), obj.GetKonnectID(), name,
		)
	}
	if probeErr != nil {
		logger.Info("failed to determine whether policies using the schema registry blocked deletion",
			"type", obj.GetTypeName(), "id", obj.GetKonnectID(), "error", probeErr.Error(),
		)
		return err
	}
	if len(produce) == 0 && len(consume) == 0 {
		return err
	}

	// Without the cluster's policies, the users can't be told apart from
	// unmanaged Konnect policies: report the failure and retry.
	var produceList configurationv1alpha1.EventGatewayVirtualClusterProducePolicyList
	if listErr := cl.List(ctx, &produceList); listErr != nil {
		return fmt.Errorf("%w; failed to list EventGatewayVirtualClusterProducePolicy objects using it: %w", err, listErr)
	}
	var consumeList configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyList
	if listErr := cl.List(ctx, &consumeList); listErr != nil {
		return fmt.Errorf("%w; failed to list EventGatewayVirtualClusterConsumePolicy objects using it: %w", err, listErr)
	}
	p := classifyPolicyUsers(obj.GetNamespace(), produce, managedPolicies(produceList.Items))
	c := classifyPolicyUsers(obj.GetNamespace(), consume, managedPolicies(consumeList.Items))
	return EventGatewaySchemaRegistryInUseError{
		ProduceUsers:                p.users,
		ConsumeUsers:                c.users,
		OtherNamespacesProduceUsers: p.otherNamespaces,
		OtherNamespacesConsumeUsers: c.otherNamespaces,
		UnmanagedProducePolicies:    p.unmanaged,
		UnmanagedConsumePolicies:    c.unmanaged,
		Err:                         err,
	}
}

// managedPolicies maps the Konnect ID of each policy object created in Konnect
// to the object.
func managedPolicies[T any, PT interface {
	*T
	client.Object
	GetKonnectID() string
}](items []T) map[string]client.ObjectKey {
	managed := make(map[string]client.ObjectKey, len(items))
	for i := range items {
		obj := PT(&items[i])
		if id := obj.GetKonnectID(); id != "" {
			managed[id] = client.ObjectKeyFromObject(obj)
		}
	}
	return managed
}

// konnectSchemaRegistryName returns the name of the schema registry as stored
// in Konnect.
func konnectSchemaRegistryName(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaySchemaRegistriesSDK,
	gatewayID, registryID string,
) (string, error) {
	resp, err := sdk.GetEventGatewaySchemaRegistry(ctx, gatewayID, registryID)
	if err != nil {
		return "", fmt.Errorf("getting the schema registry: %w", err)
	}
	if resp == nil || resp.SchemaRegistry == nil {
		return "", ErrNilResponse
	}
	return resp.SchemaRegistry.GetName(), nil
}

// konnectPoliciesUsingSchemaRegistry returns the Konnect produce and consume
// policies of the Event Gateway's virtual clusters whose config references the
// schema registry by its Konnect ID or name.
func konnectPoliciesUsingSchemaRegistry(
	ctx context.Context,
	virtualClustersSDK sdkkonnectgo.EventGatewayVirtualClustersSDK,
	producePoliciesSDK sdkkonnectgo.EventGatewayVirtualClusterProducePoliciesSDK,
	consumePoliciesSDK sdkkonnectgo.EventGatewayVirtualClusterConsumePoliciesSDK,
	gatewayID, registryID, registryName string,
) (produce, consume []konnectPolicyUser, err error) {
	var (
		after *string
		// Cursors already requested, to detect a next-page cursor that does
		// not advance (directly or through a longer cycle).
		seenCursors = map[string]struct{}{}
	)
	using := func(policies []policyConfig, vcName string) []konnectPolicyUser {
		var users []konnectPolicyUser
		for _, p := range policies {
			if referencesSchemaRegistry(p.Config, registryID, registryName) {
				users = append(users, konnectPolicyUser{id: p.ID, name: p.Name, parentName: vcName})
			}
		}
		return users
	}
	for {
		resp, err := virtualClustersSDK.ListEventGatewayVirtualClusters(ctx, sdkkonnectops.ListEventGatewayVirtualClustersRequest{
			GatewayID: gatewayID,
			PageSize:  new(int64(eventGatewayVirtualClustersProbePageSize)),
			PageAfter: after,
		})
		if err != nil {
			if ErrIsNotFound(err) {
				// The gateway is gone from Konnect: no policy uses the schema
				// registry anymore.
				return produce, consume, nil
			}
			return nil, nil, err
		}
		if resp == nil || resp.ListVirtualClustersResponse == nil {
			return nil, nil, ErrNilResponse
		}
		for _, vc := range resp.ListVirtualClustersResponse.GetData() {
			producePolicies, err := producePolicyConfigs(ctx, producePoliciesSDK, gatewayID, vc.GetID())
			if err != nil && !ErrIsNotFound(err) {
				return nil, nil, fmt.Errorf("listing produce policies of virtual cluster %s: %w", vc.GetName(), err)
			}
			consumePolicies, err := consumePolicyConfigs(ctx, consumePoliciesSDK, gatewayID, vc.GetID())
			if err != nil && !ErrIsNotFound(err) {
				return nil, nil, fmt.Errorf("listing consume policies of virtual cluster %s: %w", vc.GetName(), err)
			}
			// A virtual cluster deleted while listing has no policies using
			// the schema registry anymore.
			produce = append(produce, using(producePolicies, vc.GetName())...)
			consume = append(consume, using(consumePolicies, vc.GetName())...)
		}
		meta := resp.ListVirtualClustersResponse.GetMeta()
		page := meta.GetPage()
		if after, err = nextPageCursor(page.GetNext(), seenCursors); err != nil {
			return nil, nil, fmt.Errorf("listing virtual clusters of Event Gateway %s: %w", gatewayID, err)
		}
		if after == nil {
			return produce, consume, nil
		}
	}
}

// consumePolicyConfigs lists the consume policies of a Konnect virtual cluster
// with their config, read from the raw response body (see
// decodeRawResponseBody).
func consumePolicyConfigs(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayVirtualClusterConsumePoliciesSDK,
	gatewayID, virtualClusterID string,
) ([]policyConfig, error) {
	resp, err := sdk.ListEventGatewayVirtualClusterConsumePolicies(ctx, sdkkonnectops.ListEventGatewayVirtualClusterConsumePoliciesRequest{
		GatewayID:        gatewayID,
		VirtualClusterID: virtualClusterID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, ErrNilResponse
	}
	var policies []policyConfig
	if err := decodeRawResponseBody(resp.RawResponse, &policies); err != nil {
		return nil, fmt.Errorf("consume policies: %w", err)
	}
	return policies, nil
}

// referencesSchemaRegistry reports whether a policy config references the
// schema registry: a schema_registry reference ({"id"} or {"name"}) anywhere
// in it, e.g. the one of a schema validation policy or of a transcode
// policy's schema source.
func referencesSchemaRegistry(config any, id, name string) bool {
	switch v := config.(type) {
	case map[string]any:
		if ref, ok := v["schema_registry"].(map[string]any); ok {
			if (id != "" && ref["id"] == id) || (name != "" && ref["name"] == name) {
				return true
			}
		}
		for _, child := range v {
			if referencesSchemaRegistry(child, id, name) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if referencesSchemaRegistry(child, id, name) {
				return true
			}
		}
	}
	return false
}
