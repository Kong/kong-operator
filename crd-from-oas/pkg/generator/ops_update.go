package generator

import (
	"fmt"
	"strings"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// OpsUpdateFileInfo is metadata returned from generateEntityOpsFile so the
// caller (e.g. run.go) can assemble the cross-group update dispatcher.
type OpsUpdateFileInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
	NeedsClient    bool
}

// opsUpdateFuncData holds template data for a single update<Entity> function.
type opsUpdateFuncData struct {
	Entity             string
	APIAlias           string
	UpdateSDKInterface string
	UpdateSDKMethod    string
	UpdateReqType      string
	HasTags            bool
	HasLabels          bool
	LabelsPointer      bool
	// Parents holds metadata for each parent dependency (outermost first).
	Parents []parentInfo
	// UpdateWrapped is true when the SDK call uses a request-struct (non-root).
	UpdateWrapped bool
	// UpdateFullyWrapped is true when the update.path is a fully-wrapped
	// operations.XxxRequest struct (multi-parent case). In this case the generated
	// code sets parent IDs and the entity ID on the returned request object and
	// passes it directly to the SDK, instead of constructing a manual struct literal.
	UpdateFullyWrapped bool
	// UpdateOmitsEntityID is true for parent-scoped singleton resources whose
	// PATCH path contains only parent path params and no entity-specific ID (e.g.
	// PATCH /portals/{portalId}/email-config). The SDK method takes the parent ID
	// directly instead of a separate entity ID, so neither the id local variable
	// nor an entity ID argument should be emitted.
	UpdateOmitsEntityID bool
	// ParentIDField is used only for single-parent wrapped updates (UpdateWrapped &&
	// !UpdateFullyWrapped). e.g. "PortalID".
	ParentIDField string
	// EntityIDField is the SDK request-struct field name for the entity's own ID.
	// Used for both single-parent and multi-parent wrapped updates.
	EntityIDField string
	// UpdateBodyField is used only for single-parent wrapped updates. e.g. "UpdateIdentityProvider".
	UpdateBodyField      string
	UpdateReqBodyPointer bool // true when SDK body param is a pointer
	NeedsClient          bool // true when the generated update function needs client.Client
	HasReferences        bool // true when cross-CR references or parent ref replacement need ID injection
}

// generateOpsUpdateFuncBody renders the update<Entity> function body (no file header).
func (g *Generator) generateOpsUpdateFuncBody(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*opsUpdateFuncData, error) {
	updateOp, ok := opsConfig.Ops["update"]
	if !ok || updateOp == nil {
		return nil, nil
	}
	if schema.UpdateOperationID == "" {
		return nil, fmt.Errorf("entity %q: missing OpenAPI operationId for update op (no PATCH/PUT found)", entityName)
	}
	if len(schema.UpdateTags) == 0 {
		return nil, fmt.Errorf("entity %q: missing OpenAPI tags for update op", entityName)
	}

	updateImportPath, updateReqType, err := ParseSDKTypePath(updateOp.Path)
	if err != nil {
		return nil, fmt.Errorf("entity %q: %w", entityName, err)
	}

	sdkMethod := pascalFromKebab(schema.UpdateOperationID)
	sdkInterface := pascalFromKebab(schema.UpdateTags[0]) + "SDK"
	sdkInterface, err = resolveSDKInterfaceTypeName(opsConfig, sdkInterface)
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve update SDK interface: %w", entityName, err)
	}
	hasTags, hasLabels, labelsPointer := metadataFields(schema)
	needsClient := opsConfig.RequireClient

	parents, err := g.resolveParents(entityName, schema)
	if err != nil {
		return nil, err
	}

	// Determine SDK call shape based on number of path params in the PATCH path.
	// ≥2 params → wrapped operations.XxxRequest struct.
	// 1 param → positional (entity ID only, root entity).
	wrapped := len(schema.UpdatePathParams) >= 2
	// updateFullyWrapped is true for multi-parent entities (≥3 update path params)
	// OR when update.path is itself an operations.XxxRequest wrapper type — in that
	// case To<X>() already returns the full request struct (path params + body) and
	// we set path params on it directly instead of constructing a manual struct literal.
	updateFullyWrapped := len(schema.UpdatePathParams) >= 3 ||
		strings.HasSuffix(updateImportPath, "/operations")
	// updateOmitsEntityID is true for parent-scoped singletons: the PATCH path
	// contains only parent path params (no entity-specific ID). The SDK method
	// takes the parent ID positionally; no entity ID is emitted.
	updateOmitsEntityID := !wrapped && len(parents) > 0

	var parentIDField, entityIDField, updateBodyField string
	if wrapped {
		params := schema.UpdatePathParams
		entityIDField = pathParamToFieldName(params[len(params)-1])
		if !updateFullyWrapped {
			// Single-parent wrapped: derive parent field name from second-to-last param.
			parentIDField = pathParamToFieldName(params[len(params)-2])
			updateBodyField = updateReqType
		}
	}

	// When the update path is a fully-wrapped operations.XxxRequest, the SDK
	// method always takes the struct by value — override any OAS-derived pointer
	// flag so the template emits *req (dereference) unconditionally.
	updateReqBodyPointer := schema.UpdateReqBodyPointer
	if updateFullyWrapped && strings.HasSuffix(updateImportPath, "/operations") {
		updateReqBodyPointer = false
	}

	return &opsUpdateFuncData{
		Entity:               entityName,
		APIAlias:             g.config.APIGroupPackageAlias,
		UpdateSDKInterface:   sdkInterface,
		UpdateSDKMethod:      sdkMethod,
		UpdateReqType:        updateReqType,
		HasTags:              hasTags,
		HasLabels:            hasLabels,
		LabelsPointer:        labelsPointer,
		Parents:              parents,
		UpdateWrapped:        wrapped,
		UpdateFullyWrapped:   updateFullyWrapped,
		ParentIDField:        parentIDField,
		EntityIDField:        entityIDField,
		UpdateBodyField:      updateBodyField,
		UpdateReqBodyPointer: updateReqBodyPointer,
		NeedsClient:          needsClient,
		HasReferences:        g.entityHasReferences(entityName) || g.entityHasParentRefReplacement(entityName),
		UpdateOmitsEntityID:  updateOmitsEntityID,
	}, nil
}

// GenerateOpsUpdateDispatcher emits zz_generated_ops_update.go with
// UpdateGeneratedOps[T,TEnt]. Call after all per-group generation has finished.
func GenerateOpsUpdateDispatcher(infos []*OpsUpdateFileInfo) (*GeneratedFile, error) {
	flat := make([]flatInfo, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, flatInfo{
			Entity:         info.Entity,
			APIAlias:       info.APIAlias,
			APIPackagePath: info.APIPackagePath,
			SDKGetter:      info.SDKGetter,
			NeedsClient:    info.NeedsClient,
		})
	}
	return buildDispatcherFile("zz_generated_ops_update.go", opsUpdateDispatcherTemplate, "controller/konnect/ops", flat)
}
