package v1alpha1

// This file hand-translates AIGatewayDatastore into ai-deck-converter's aigw.Datastore, for
// on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment for the
// shared background and the plan to generate this some day.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWDatastore converts the AIGatewayDatastore into ai-deck-converter's aigw.Datastore. The
// client is used for the config secretRef resolution (the Redis passwords, the vector database
// credentials and the cloud authentication secrets). It carries no entity references of its own.
func (obj *AIGatewayDatastore) ToAIGWDatastore(ctx context.Context, cl client.Client) (*aigw.Datastore, error) {
	if obj.Spec.APISpec.AIGatewayDatastoreConfig == nil {
		return nil, fmt.Errorf("AIGatewayDatastore %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}

	if err := CheckCrossNamespaceSecretRefsGranted(ctx, cl, obj); err != nil {
		return nil, fmt.Errorf("AIGatewayDatastore %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// sdkOpsAPISpec resolves into a copy of the APISpec, so the caller's object is not mutated.
	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayDatastore %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}

	data, err := resolved.marshalAIGWDatastorePayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayDatastore %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var datastore aigw.Datastore
	if err := yaml.Unmarshal(data, &datastore); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayDatastore %s/%s as aigw.Datastore: %w", obj.Namespace, obj.Name, err)
	}
	if datastore.Name == "" {
		return nil, fmt.Errorf(
			"AIGatewayDatastore %s/%s: spec.apiSpec.%s.name is required: it is the name the referencing entities resolve",
			obj.Namespace, obj.Name, resolved.Type,
		)
	}
	return &datastore, nil
}

// marshalAIGWDatastorePayload builds the aigw.Datastore-shaped payload bytes. Shared by
// ToAIGWDatastore and its strict round-trip test, so the test decodes the production pipeline's
// output, not a copy of it.
//
// selectedSDKOpsPayload already does the variant selection and hoists the config type into the
// payload's `type`, and the renaming and bool normalization leave the variant's `config` in the
// plugin shape the converter substitutes verbatim into the referencing entity's plugin. What is
// left is dropping the display and Konnect-only keys aigw.Datastore has no field for.
func (spec *AIGatewayDatastoreAPISpec) marshalAIGWDatastorePayload() ([]byte, error) {
	payload, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	data, _, err := spec.selectedSDKOpsPayload(payload)
	if err != nil {
		return nil, err
	}

	var selected map[string]any
	if err := json.Unmarshal(data, &selected); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayDatastore SDK payload: %w", err)
	}
	for _, key := range []string{"description", "display_name", "labels", "managed_by"} {
		delete(selected, key)
	}
	return json.Marshal(selected)
}

// resolveDatastoreRefs resolves the AIGatewayDatastores referenced by keys (the generated
// AIGateway<Kind>RefsToAIGatewayDatastore helpers return them with the default namespace applied)
// to the datastores' entity names, and writes them into dst, which is the referencing entity's
// aigw `datastores` field.
//
// dst's element type is ai-deck-converter's DatastoreRef, which aigw/doc.go does not alias, so the
// refs are built as plain name mappings and decoded into dst.
func resolveDatastoreRefs[E any](ctx context.Context, cl client.Client, namespace string, keys []client.ObjectKey, dst *[]E) error {
	if len(keys) == 0 {
		return nil
	}
	refs := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		name, err := resolveEntityName[AIGatewayDatastore](ctx, cl, namespace, key.Namespace, key.Name)
		if err != nil {
			return err
		}
		refs = append(refs, map[string]string{"name": name})
	}
	data, err := yaml.Marshal(refs)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, dst)
}
