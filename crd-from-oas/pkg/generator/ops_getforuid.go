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
	ParentEntityName  string
	ParentIDGetter    string
	// ParentIDField is the SDK request struct field name for the parent ID,
	// e.g. "PortalID" for an entity nested under Portal.
	ParentIDField string
	// HasLabels indicates the entity's request schema declares a "labels"
	// field, so list response items are expected to expose GetLabels() and
	// the generator can match by the Kubernetes UID label.
	HasLabels bool
	// UseUIDTagFilter indicates the API supports filtering list requests by the
	// Kubernetes UID tag, so getForUID can avoid full scans.
	UseUIDTagFilter bool
	// HasName indicates the entity's request schema declares a "name" field,
	// used as a fallback UID-matching strategy when HasLabels is false.
	HasName bool
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

	parentEntityName, parentIDGetter, err := g.resolveParentEntity(entityName, schema)
	if err != nil {
		return nil, err
	}

	var parentIDField string
	if parentEntityName != "" && len(schema.Dependencies) > 0 {
		// Derive the SDK request field name from the last path parameter, e.g.
		// "portalId" → "PortalID" using pathParamToFieldName.
		parentDep := schema.Dependencies[len(schema.Dependencies)-1]
		parentIDField = pathParamToFieldName(parentDep.ParamName)
	}

	// Derive response field name: SDK codegen wraps list responses as
	// <ListSDKMethod>Response on the outer struct (e.g. ListPortalsResponse).
	listResponseField := listMethod + "Response"

	_, hasLabels, _ := metadataFields(schema)
	hasName := schemaHasNameProperty(schema)

	return &opsGetForUIDFuncData{
		Entity:            entityName,
		APIAlias:          g.config.APIGroupPackageAlias,
		ListSDKInterface:  listInterface,
		ListSDKMethod:     listMethod,
		ListResponseField: listResponseField,
		ParentEntityName:  parentEntityName,
		ParentIDGetter:    parentIDGetter,
		ParentIDField:     parentIDField,
		HasLabels:         hasLabels,
		UseUIDTagFilter:   opsConfig != nil && opsConfig.UseUIDTagFilter,
		HasName:           hasName,
	}, nil
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
