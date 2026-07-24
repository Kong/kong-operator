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
	// SkipUpdate indicates the entity does not support update. The dispatcher
	// emits a nil return for this entity instead of calling an update function.
	SkipUpdate bool
}

type updateOpCallShape struct {
	SDKInterface     string
	SDKMethod        string
	ReqImportPath    string
	ReqQualifiedType string
	ReqMethod        string
	ReqType          string
	ReqBodyPointer   bool
	Parents          []parentInfo
	Wrapped          bool
	FullyWrapped     bool
	OmitsEntityID    bool
	ParentIDField    string
	EntityIDField    string
	BodyField        string
}

// opsUpdateFuncData holds template data for a single update<Entity> function.
type opsUpdateFuncData struct {
	Entity             string
	APIAlias           string
	UpdateSDKInterface string
	UpdateSDKMethod    string
	UpdateReqMethod    string
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
	// LabelsFieldPath is the dotted Go field path, relative to req, needed to
	// reach a .Labels/.Tags field when the update request body is (or wraps) a
	// root-level discriminated union — e.g. "SchemaRegistryUpdate.SchemaRegistryConfluentSensitiveDataAware"
	// for a fully-wrapped update whose body is itself a union. Empty when req
	// has a direct .Labels/.Tags field (the common, non-union case).
	LabelsFieldPath string
	// LabelsFieldGuard is a ready-to-emit Go boolean expression guarding every
	// pointer segment in LabelsFieldPath (e.g. "req.SchemaRegistryUpdate != nil
	// && req.SchemaRegistryUpdate.SchemaRegistryConfluentSensitiveDataAware != nil").
	// Empty when LabelsFieldPath is empty.
	LabelsFieldGuard string
	NeedsClient      bool // true when the generated update function needs client.Client
	HasReferences    bool // true when parent ref replacement needs an entity-level request builder
	// Associations lists the top-level spec association fields whose membership
	// is enforced by a hand-written helper called after the entity is updated.
	Associations []opsAssociationData
	// SupportsMirror is true when the entity opted into Origin+Mirror. The
	// generated update function then early-returns a no-op for Mirror entities.
	SupportsMirror bool
}

func qualifiedSDKTypeName(importPath, typeName string) string {
	if strings.HasSuffix(importPath, "/operations") {
		return "sdkkonnectops." + typeName
	}
	return sdkImportAlias(importPath) + "." + typeName
}

func (g *Generator) resolveUpdateOpCallShape(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*updateOpCallShape, error) {
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
	updateReqMethod, err := sdkOpsMethodNameForOp(opsConfig, "update")
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve update SDK conversion method: %w", entityName, err)
	}

	sdkMethod := pascalFromKebab(schema.UpdateOperationID)
	sdkInterface := pascalFromKebab(schema.UpdateTags[0]) + "SDK"
	sdkInterface, err = resolveSDKInterfaceTypeName(opsConfig, sdkInterface)
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve update SDK interface: %w", entityName, err)
	}

	parents, err := g.resolveParents(entityName, schema)
	if err != nil {
		return nil, err
	}

	wrapped := len(schema.UpdatePathParams) >= 2
	updateFullyWrapped := len(schema.UpdatePathParams) >= 3 ||
		strings.HasSuffix(updateImportPath, "/operations")
	updateOmitsEntityID := !wrapped && len(parents) > 0

	var parentIDField, entityIDField, updateBodyField string
	if wrapped {
		params := schema.UpdatePathParams
		entityIDField = pathParamToFieldName(params[len(params)-1])
		if !updateFullyWrapped {
			parentIDField = pathParamToFieldName(params[len(params)-2])
			updateBodyField = updateReqType
		}
	}

	updateReqBodyPointer := schema.UpdateReqBodyPointer
	if updateFullyWrapped && strings.HasSuffix(updateImportPath, "/operations") {
		updateReqBodyPointer = false
	}

	return &updateOpCallShape{
		SDKInterface:     sdkInterface,
		SDKMethod:        sdkMethod,
		ReqImportPath:    updateImportPath,
		ReqQualifiedType: qualifiedSDKTypeName(updateImportPath, updateReqType),
		ReqMethod:        updateReqMethod,
		ReqType:          updateReqType,
		ReqBodyPointer:   updateReqBodyPointer,
		Parents:          parents,
		Wrapped:          wrapped,
		FullyWrapped:     updateFullyWrapped,
		OmitsEntityID:    updateOmitsEntityID,
		ParentIDField:    parentIDField,
		EntityIDField:    entityIDField,
		BodyField:        updateBodyField,
	}, nil
}

// generateOpsUpdateFuncBody renders the update<Entity> function body (no file header).
func (g *Generator) generateOpsUpdateFuncBody(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*opsUpdateFuncData, error) {
	callShape, err := g.resolveUpdateOpCallShape(entityName, schema, opsConfig)
	if err != nil {
		return nil, err
	}
	if callShape == nil {
		return nil, nil
	}
	hasTags, hasLabels, labelsPointer := metadataFields(schema, opsConfig)
	associations := g.opsAssociations(entityName)
	// Association enforcement helpers need the controller-runtime client.
	needsClient := opsConfig.RequireClient || g.entityHasReferences(entityName) || len(associations) > 0

	labelsFieldPath, err := resolveUpdateLabelsFieldPath(entityName, callShape, hasLabels || hasTags)
	if err != nil {
		return nil, err
	}

	return &opsUpdateFuncData{
		Entity:               entityName,
		APIAlias:             g.config.APIGroupPackageAlias,
		UpdateSDKInterface:   callShape.SDKInterface,
		UpdateSDKMethod:      callShape.SDKMethod,
		UpdateReqMethod:      callShape.ReqMethod,
		UpdateReqType:        callShape.ReqType,
		HasTags:              hasTags,
		HasLabels:            hasLabels,
		LabelsPointer:        labelsPointer,
		Parents:              callShape.Parents,
		UpdateWrapped:        callShape.Wrapped,
		UpdateFullyWrapped:   callShape.FullyWrapped,
		ParentIDField:        callShape.ParentIDField,
		EntityIDField:        callShape.EntityIDField,
		UpdateBodyField:      callShape.BodyField,
		UpdateReqBodyPointer: callShape.ReqBodyPointer,
		LabelsFieldPath:      labelsFieldPath,
		LabelsFieldGuard:     labelsFieldGuardExpr("req", labelsFieldPath),
		NeedsClient:          needsClient,
		HasReferences:        g.entityHasParentRefReplacement(entityName),
		UpdateOmitsEntityID:  callShape.OmitsEntityID,
		Associations:         associations,
		SupportsMirror:       g.entitySupportsMirror(entityName),
	}, nil
}

// resolveUpdateLabelsFieldPath returns the dotted Go field path, relative to
// req, needed to reach a .Labels/.Tags field when the update request body is
// (or wraps) a root-level discriminated union. Returns "" when req has a
// direct .Labels/.Tags field (the common, non-union case) or when no
// labels/tags were detected at all. Requires the union (if any) to resolve to
// exactly one member; a request type with multiple members has no single
// field to target and must opt out via ops.skipRootUnionMetadataFields.
func resolveUpdateLabelsFieldPath(entityName string, callShape *updateOpCallShape, needed bool) (string, error) {
	if !needed {
		return "", nil
	}

	checkImportPath, checkType := callShape.ReqImportPath, callShape.ReqType
	var bodyField string
	if callShape.FullyWrapped {
		bodyInfo, err := ParseSDKRequestBodyInfo(callShape.ReqImportPath, callShape.ReqType)
		if err != nil {
			return "", fmt.Errorf("entity %q: inspect update request body: %w", entityName, err)
		}
		bodyField = bodyInfo.FieldName
		checkImportPath, checkType = "github.com/Kong/sdk-konnect-go/models/components", bodyInfo.TypeName
	}

	memberFields, err := ParseSDKUnionMemberFieldNames(checkImportPath, checkType)
	if err != nil {
		return "", fmt.Errorf("entity %q: inspect update request union: %w", entityName, err)
	}
	switch len(memberFields) {
	case 0:
		return bodyField, nil
	case 1:
		if bodyField == "" {
			return memberFields[0], nil
		}
		return bodyField + "." + memberFields[0], nil
	default:
		return "", fmt.Errorf(
			"entity %q: labels/tags detected on multi-variant union update body %q; set ops.skipRootUnionMetadataFields to opt out",
			entityName, checkType,
		)
	}
}

// labelsFieldGuardExpr builds a Go boolean expression guarding every pointer
// segment of a dotted field path (e.g. base="req", path="A.B" produces
// "req.A != nil && req.A.B != nil"). Returns "" when path is empty.
func labelsFieldGuardExpr(base, path string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(path, ".")
	guards := make([]string, 0, len(segments))
	prefix := base
	for _, seg := range segments {
		prefix = prefix + "." + seg
		guards = append(guards, prefix+" != nil")
	}
	return strings.Join(guards, " && ")
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
			SkipUpdate:     info.SkipUpdate,
		})
	}
	return buildDispatcherFile("zz_generated_ops_update.go", opsUpdateDispatcherTemplate, "controller/konnect/ops", flat)
}
