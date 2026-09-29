package generator

import (
	"fmt"
	"strings"
	"text/template"

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
	// LabelsUnionTargets lists one guarded injection target per member when
	// the update request body is (or wraps) a root-level discriminated union
	// with multiple members that all declare labels/tags. When set,
	// LabelsFieldPath and LabelsFieldGuard are empty.
	LabelsUnionTargets []labelsUnionTarget
	NeedsClient        bool // true when the generated update function needs client.Client
	HasReferences      bool // true when parent ref replacement needs an entity-level request builder
	// Associations lists the top-level spec association fields whose membership
	// is enforced by a hand-written helper called after the entity is updated.
	Associations []opsAssociationData
	// SupportsMirror is true when the entity opted into Origin+Mirror. The
	// generated update function then early-returns a no-op for Mirror entities.
	SupportsMirror bool
	// RespField is the field name on the SDK update response wrapper holding
	// the updated entity (schema.UpdateSuccessResponseRef). Only set when
	// ResponseStatusFields is non-empty.
	RespField            string
	ResponseStatusFields []config.ResponseStatusFieldConfig
	// HasNestedResponseStatusFields is true when at least one ResponseStatusFields
	// entry generates a nested struct (i.e. has Fields, not a scalar RespPath).
	HasNestedResponseStatusFields bool
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

	if len(opsConfig.ResponseStatusFields) > 0 && schema.UpdateSuccessResponseRef == "" {
		return nil, fmt.Errorf("entity %q: ops.responseStatusFields requires a 2xx response ref for update op", entityName)
	}

	labelsFieldPath, labelsUnionTargets, err := resolveUpdateLabelsFieldPath(entityName, callShape, hasLabels || hasTags, hasTags)
	if err != nil {
		return nil, err
	}

	return &opsUpdateFuncData{
		APIAlias:                      g.config.APIGroupPackageAlias,
		Associations:                  associations,
		Entity:                        entityName,
		EntityIDField:                 callShape.EntityIDField,
		HasLabels:                     hasLabels,
		HasNestedResponseStatusFields: hasNestedResponseStatusFields(opsConfig.ResponseStatusFields),
		HasReferences:                 g.entityHasParentRefReplacement(entityName),
		HasTags:                       hasTags,
		LabelsFieldGuard:              labelsFieldGuardExpr("req", labelsFieldPath),
		LabelsFieldPath:               labelsFieldPath,
		LabelsPointer:                 labelsPointer,
		LabelsUnionTargets:            labelsUnionTargets,
		NeedsClient:                   needsClient,
		ParentIDField:                 callShape.ParentIDField,
		Parents:                       callShape.Parents,
		RespField:                     schema.UpdateSuccessResponseRef,
		ResponseStatusFields:          opsConfig.ResponseStatusFields,
		SupportsMirror:                g.entitySupportsMirror(entityName),
		UpdateBodyField:               callShape.BodyField,
		UpdateFullyWrapped:            callShape.FullyWrapped,
		UpdateOmitsEntityID:           callShape.OmitsEntityID,
		UpdateReqBodyPointer:          callShape.ReqBodyPointer,
		UpdateReqMethod:               callShape.ReqMethod,
		UpdateReqType:                 callShape.ReqType,
		UpdateSDKInterface:            callShape.SDKInterface,
		UpdateSDKMethod:               callShape.SDKMethod,
		UpdateWrapped:                 callShape.Wrapped,
	}, nil
}

// resolveUpdateLabelsFieldPath returns the dotted Go field path, relative to
// req, needed to reach a .Labels/.Tags field when the update request body is
// (or wraps) a root-level discriminated union. Returns "" when req has a
// direct .Labels/.Tags field (the common, non-union case) or when no
// labels/tags were detected at all. When the union has multiple members, all
// of which declare the labels/tags field, it instead returns one guarded
// target per member; a member lacking the field must opt out via
// ops.skipRootUnionMetadataFields.
func resolveUpdateLabelsFieldPath(
	entityName string,
	callShape *updateOpCallShape,
	needed bool,
	hasTags bool,
) (string, []labelsUnionTarget, error) {
	if !needed {
		return "", nil, nil
	}

	checkImportPath, checkType := callShape.ReqImportPath, callShape.ReqType
	var (
		bodyField   string
		bodyPointer bool
	)
	if callShape.FullyWrapped {
		bodyInfo, err := ParseSDKRequestBodyInfo(callShape.ReqImportPath, callShape.ReqType)
		if err != nil {
			return "", nil, fmt.Errorf("entity %q: inspect update request body: %w", entityName, err)
		}
		bodyField = bodyInfo.FieldName
		bodyPointer = bodyInfo.Pointer
		checkImportPath, checkType = "github.com/Kong/sdk-konnect-go/models/components", bodyInfo.TypeName
	}

	memberFields, err := ParseSDKUnionMemberFieldNames(checkImportPath, checkType)
	if err != nil {
		return "", nil, fmt.Errorf("entity %q: inspect update request union: %w", entityName, err)
	}
	switch len(memberFields) {
	case 0:
		return bodyField, nil, nil
	case 1:
		if bodyField == "" {
			return memberFields[0], nil, nil
		}
		return bodyField + "." + memberFields[0], nil, nil
	default:
		if err := requireUnionMembersMetadataField(checkImportPath, checkType, hasTags); err != nil {
			return "", nil, fmt.Errorf(
				"entity %q: update body %q: %w; set ops.skipRootUnionMetadataFields to opt out",
				entityName, checkType, err,
			)
		}
		targets := make([]labelsUnionTarget, 0, len(memberFields))
		for _, member := range memberFields {
			targets = append(targets, newLabelsUnionTarget(bodyField, bodyPointer, member))
		}
		return "", targets, nil
	}
}

// labelsUnionTarget is a single labels/tags injection target inside a
// multi-member root-level discriminated union request body.
type labelsUnionTarget struct {
	// Path is the dotted Go field path, relative to the request, of the
	// union member carrying the .Labels/.Tags field.
	Path string
	// Guard is a Go boolean expression (rooted at "req") guarding every
	// pointer segment of Path, as used by the generated ops.
	Guard string
	// ExpectedGuard is Guard rooted at "expectedRequest", as used by the
	// generated ops tests.
	ExpectedGuard string
}

// labelsUnionInjectData is the input of the "labelsUnionInject" sub-template
// (see labelsUnionInjectTemplate).
type labelsUnionInjectData struct {
	// Root is the request variable to inject through: "req" in generated
	// ops, "expectedRequest" in generated ops tests.
	Root          string
	Targets       []labelsUnionTarget
	HasTags       bool
	LabelsPointer bool
}

// labelsUnionInjectFuncs exposes the constructor of labelsUnionInjectData to
// templates that invoke the "labelsUnionInject" sub-template.
var labelsUnionInjectFuncs = template.FuncMap{
	"labelsUnionInject": func(root string, targets []labelsUnionTarget, hasTags, labelsPointer bool) labelsUnionInjectData {
		return labelsUnionInjectData{Root: root, Targets: targets, HasTags: hasTags, LabelsPointer: labelsPointer}
	},
}

// parseWithLabelsUnionInject parses tmplText along with the shared
// "labelsUnionInject" sub-template.
func parseWithLabelsUnionInject(name, tmplText string) *template.Template {
	tmpl := template.Must(template.New(name).Funcs(labelsUnionInjectFuncs).Parse(tmplText))
	return template.Must(tmpl.Parse(labelsUnionInjectTemplate))
}

// newLabelsUnionTarget builds the injection target for the union member
// field member, nested under the request body field bodyField when the
// request is fully wrapped (bodyField is empty otherwise). The guards check
// every pointer segment: the body field only when it is a pointer (a nil
// comparison on a struct-typed body would not compile), and always the
// member, as only the selected one is set at runtime.
func newLabelsUnionTarget(bodyField string, bodyPointer bool, member string) labelsUnionTarget {
	path := member
	var pointerSegments []string
	if bodyField != "" {
		path = bodyField + "." + member
		if bodyPointer {
			pointerSegments = append(pointerSegments, bodyField)
		}
	}
	pointerSegments = append(pointerSegments, path)

	guard := func(base string) string {
		guards := make([]string, 0, len(pointerSegments))
		for _, segment := range pointerSegments {
			guards = append(guards, base+"."+segment+" != nil")
		}
		return strings.Join(guards, " && ")
	}
	return labelsUnionTarget{
		Path:          path,
		Guard:         guard("req"),
		ExpectedGuard: guard("expectedRequest"),
	}
}

// requireUnionMembersMetadataField verifies that every union member type of
// the SDK request type declares the metadata field that label/tag injection
// targets (Tags when hasTags, Labels otherwise).
func requireUnionMembersMetadataField(importPath, typeName string, hasTags bool) error {
	fieldName := "Labels"
	if hasTags {
		fieldName = "Tags"
	}
	memberTypes, err := ParseSDKUnionMemberTypeNames(importPath, typeName)
	if err != nil {
		return fmt.Errorf("inspect union member types: %w", err)
	}
	for _, memberType := range memberTypes {
		ok, err := sdkStructHasField(importPath, memberType, fieldName)
		if err != nil {
			return fmt.Errorf("inspect union member %q: %w", memberType, err)
		}
		if !ok {
			return fmt.Errorf("union member %q has no %s field", memberType, fieldName)
		}
	}
	return nil
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
