package generator

import (
	"fmt"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// OpsGetForUIDFileInfo is metadata returned from generateEntityOpsFile so the
// caller (e.g. run.go) can assemble the cross-group getForUID dispatcher.
type OpsGetForUIDFileInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
}

// opsGetForUIDFuncData holds template data for a single get<Entity>ForUID function.
type opsGetForUIDFuncData struct {
	Entity            string
	APIAlias          string
	ListSDKInterface  string
	ListSDKMethod     string
	ListResponseField string
	// Parents holds metadata for each parent dependency (outermost first).
	Parents []parentInfo
	// GetForUIDFullyWrapped is true for multi-parent entities. The SDK list
	// method takes a single request struct with all parent fields rather than
	// a single positional parentID.
	GetForUIDFullyWrapped bool
	// GetForUIDWrappedType is the SDK operations struct type name for fully-wrapped
	// list, e.g. "ListEventGatewayListenerPoliciesRequest".
	GetForUIDWrappedType string
	// ParentIDField is the SDK request struct field name for the (single) parent ID,
	// used only when GetForUIDFullyWrapped is false (single-parent case).
	// e.g. "PortalID" for an entity nested under Portal.
	ParentIDField string
	// HasLabels indicates the entity's request schema declares a "labels"
	// field, so list response items are expected to expose GetLabels() and
	// the generator can match by the Kubernetes UID label.
	HasLabels bool
	// UseUIDTagFilter indicates the API supports filtering list requests by the
	// Kubernetes UID tag, so getForUID can avoid full scans.
	UseUIDTagFilter bool
	// MatchFields configures generated field comparisons for entities whose
	// list responses do not expose labels/tags.
	MatchFields []opsGetForUIDMatchFieldData
	// HasName indicates the entity's request schema declares a "name" field,
	// used as a fallback UID-matching strategy when HasLabels is false.
	HasName bool
}

type opsGetForUIDMatchFieldData struct {
	ObjectField   string
	ResponseField string
	// SliceMatch is true when the field is a []string slice rather than a plain
	// string/pointer, causing the template to emit matchSliceField instead of
	// matchStringField.
	SliceMatch bool
}

// generateOpsGetForUIDFuncBody renders the get<Entity>ForUID function body
// (no file header). Returns nil when:
//   - no list op is discoverable in the OpenAPI spec, or
//   - the entity is explicitly skipped via config or SkipGetForUIDEntities.
func (g *Generator) generateOpsGetForUIDFuncBody(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*opsGetForUIDFuncData, error) {
	if g.config.SkipGetForUIDEntities[entityName] {
		return nil, nil
	}
	if opsConfig != nil && opsConfig.SkipGetForUID {
		return nil, nil
	}
	if schema.ListOperationID == "" {
		return nil, nil
	}
	if len(schema.ListTags) == 0 {
		return nil, fmt.Errorf("entity %q: missing OpenAPI tags for list op (no GET found)", entityName)
	}

	listMethod := pascalFromKebab(schema.ListOperationID)
	listInterface := pascalFromKebab(schema.ListTags[0]) + "SDK"
	listInterface, err := resolveSDKInterfaceTypeName(opsConfig, listInterface)
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve list SDK interface: %w", entityName, err)
	}

	parents, err := g.resolveParents(entityName, schema)
	if err != nil {
		return nil, err
	}

	// Multi-parent entities use a fully-wrapped list request struct.
	getForUIDFullyWrapped := len(parents) >= 2

	var parentIDField, getForUIDWrappedType string
	if getForUIDFullyWrapped {
		// Wrapped type: "<PascalCaseListMethod>Request".
		getForUIDWrappedType = listMethod + "Request"
	} else if len(parents) == 1 {
		// Single-parent: derive the SDK request field name from the last path parameter.
		parentDep := schema.Dependencies[len(schema.Dependencies)-1]
		parentIDField = pathParamToFieldName(parentDep.ParamName)
	}

	// SDK codegen names the nested field on the operations response wrapper after
	// the components response type. Most entities have those names matching, e.g.
	// ListPortalsResponse → ListPortalsResponse, but some don't, e.g.
	// ListEventGatewayBackendClusters → ListBackendClustersResponse. Prefer the
	// ref name from the OpenAPI spec; fall back to the method-derived name when
	// the spec does not declare one.
	listResponseField := schema.ListSuccessResponseRef
	if listResponseField == "" {
		listResponseField = listMethod + "Response"
	}

	_, hasLabels, _ := metadataFields(schema)
	hasName := schemaHasNameProperty(schema)
	matchFields := make([]opsGetForUIDMatchFieldData, 0)
	if opsConfig != nil && opsConfig.GetForUID != nil {
		matchFields = make([]opsGetForUIDMatchFieldData, 0, len(opsConfig.GetForUID.MatchFields))
		for _, field := range opsConfig.GetForUID.MatchFields {
			matchFields = append(matchFields, opsGetForUIDMatchFieldData{
				ObjectField:   field.ObjectField,
				ResponseField: field.ResponseField,
				SliceMatch:    isArrayMatchField(schema, field.ResponseField),
			})
		}
	}

	return &opsGetForUIDFuncData{
		Entity:                entityName,
		APIAlias:              g.config.APIGroupPackageAlias,
		ListSDKInterface:      listInterface,
		ListSDKMethod:         listMethod,
		ListResponseField:     listResponseField,
		Parents:               parents,
		GetForUIDFullyWrapped: getForUIDFullyWrapped,
		GetForUIDWrappedType:  getForUIDWrappedType,
		ParentIDField:         parentIDField,
		HasLabels:             hasLabels,
		UseUIDTagFilter:       opsConfig != nil && opsConfig.UseUIDTagFilter,
		MatchFields:           matchFields,
		HasName:               hasName,
	}, nil
}

// isArrayMatchField reports whether the schema property matching the given Go
// field name (e.g. "AllowedIps") is an array type, so the template emits
// matchSliceField instead of matchStringField.
func isArrayMatchField(schema *parser.Schema, goName string) bool {
	if schema == nil {
		return false
	}
	for _, prop := range schema.Properties {
		if goFieldName(prop.Name) == goName && prop.Type == "array" {
			return true
		}
	}
	return false
}

// schemaHasNameProperty reports whether the request body schema declares a
// "name" string property, used as a UID-match fallback when the SDK list
// response type lacks GetLabels() / tags.
func schemaHasNameProperty(schema *parser.Schema) bool {
	if schema == nil {
		return false
	}
	for _, prop := range schema.Properties {
		if prop == nil {
			continue
		}
		if prop.Name == "name" && prop.Type == "string" {
			return true
		}
	}
	return false
}

// GenerateOpsGetForUIDDispatcher emits zz_generated_ops_getforuid.go with
// getForUID[T,TEnt] and ConflictOnCreateButNoConflifctHandlingImplementedError.
// Call after all per-group generation has finished.
func GenerateOpsGetForUIDDispatcher(infos []*OpsGetForUIDFileInfo) (*GeneratedFile, error) {
	flat := make([]flatInfo, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, flatInfo{
			Entity:         info.Entity,
			APIAlias:       info.APIAlias,
			APIPackagePath: info.APIPackagePath,
			SDKGetter:      info.SDKGetter,
		})
	}
	return buildDispatcherFile("zz_generated_ops_getforuid.go", opsGetForUIDDispatcherTemplate, "controller/konnect/ops", flat)
}
