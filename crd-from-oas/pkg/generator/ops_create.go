package generator

import (
	"fmt"
	"strings"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// OpsCreateFileInfo is metadata returned from generateEntityOpsFile so the
// caller (e.g. run.go) can assemble the cross-group create dispatcher.
type OpsCreateFileInfo struct {
	Entity         string // PascalCase entity name, e.g. "Portal"
	APIAlias       string // Go import alias for the API package, e.g. "konnectv1alpha1"
	APIPackagePath string // Go import path for the API package
	SDKGetter      string // SDKWrapper method name, e.g. "GetPortalsSDK"
	NeedsClient    bool   // true when the generated create function needs client.Client
}

// opsCreateFuncData holds template data for a single create<Entity> function.
type opsCreateFuncData struct {
	Entity               string
	APIAlias             string
	SDKInterface         string
	SDKMethod            string
	CreateReqMethod      string
	CreateReqType        string
	CreateReqBodyPointer bool
	NeedsClient          bool
	// HasReferences is true when parent ref replacement needs an entity-level
	// request builder (instead of the APISpec-level one).
	HasReferences bool
	// HasParentRefReplacement is true when a ParentRef.ReplacesAPISpecField is
	// configured, meaning the SDK request body needs the replaced field injected
	// from the resolved parent status ID.
	HasParentRefReplacement bool
	RespField               string
	HasTags                 bool
	HasLabels               bool
	LabelsPointer           bool
	// Parents holds metadata for each parent dependency (outermost first).
	// Single-parent entities have len(Parents)==1; root entities have len==0.
	Parents []parentInfo
	// CreateFullyWrapped is true when the create.path is a fully-wrapped
	// operations.XxxRequest struct (multi-parent case). When true, the generated
	// code sets the parent ID fields on the returned request object instead of
	// passing parentID as a positional argument.
	CreateFullyWrapped bool
	// CreateBodyField is the JSON body field name on the operations request
	// wrapper. Only set when CreateFullyWrapped is true; used to target label/tag
	// injection at req.<CreateBodyField> instead of the wrapper's top level.
	CreateBodyField string
	RespIDIsPointer bool
	// SingletonNoID is true when the create response schema has no "id" field.
	// Generated code skips the SetKonnectID call entirely for these entities.
	SingletonNoID        bool
	RespRootUnion        *opsCreateRootUnionResponseData
	ResponseStatusFields []config.ResponseStatusFieldConfig
	// Associations lists the top-level spec association fields whose membership
	// is enforced by a hand-written helper called after the entity is created.
	Associations []opsAssociationData
	// SupportsMirror is true when the entity opted into Origin+Mirror. The
	// generated create function then branches on obj.Spec.Source: Mirror fetches
	// the existing Konnect entity by ID instead of creating it.
	SupportsMirror bool
	// GetSDKMethod is the SDK get-by-ID method name used by the Mirror branch,
	// derived from SDKMethod by swapping the "Create" prefix for "Get".
	GetSDKMethod string
}

// opsAssociationData is the per-association template data for the ops
// create/update func templates. The generated code calls a hand-written helper
// named enforce<Entity><GoFieldName>.
type opsAssociationData struct {
	// GoFieldName is the Go spec field name, e.g. "ConsumerGroups".
	GoFieldName string
	// SDKMethod is the SDK method the hand-written helper calls; used by the
	// generated ops-controller test to register the mock expectation.
	SDKMethod string
	// ResponseType is the SDK method's response struct name (SDKMethod+"Response").
	ResponseType string
}

type opsCreateRootUnionResponseData struct {
	VariantFieldNames []string
}

// generateOpsCreateFuncBody renders the create<Entity> function body (no file header).
func (g *Generator) generateOpsCreateFuncBody(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*opsCreateFuncData, error) {
	createOp, ok := opsConfig.Ops["create"]
	if !ok || createOp == nil {
		return nil, nil
	}
	if schema.OperationID == "" {
		return nil, fmt.Errorf("entity %q: missing OpenAPI operationId for create op", entityName)
	}
	if len(schema.Tags) == 0 {
		return nil, fmt.Errorf("entity %q: missing OpenAPI tags for create op", entityName)
	}
	if schema.SuccessResponseRef == "" {
		return nil, fmt.Errorf("entity %q: missing 2xx response ref for create op", entityName)
	}

	createReqImportPath, createReqType, err := ParseSDKTypePath(createOp.Path)
	if err != nil {
		return nil, fmt.Errorf("entity %q: %w", entityName, err)
	}
	createReqMethod, err := sdkOpsMethodNameForOp(opsConfig, "create")
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve create SDK conversion method: %w", entityName, err)
	}

	sdkMethod := pascalFromKebab(schema.OperationID)
	sdkInterface := pascalFromKebab(schema.Tags[0]) + "SDK"
	sdkInterface, err = resolveSDKInterfaceTypeName(opsConfig, sdkInterface)
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve create SDK interface: %w", entityName, err)
	}
	hasTags, hasLabels, labelsPointer := metadataFields(schema)
	associations := g.opsAssociations(entityName)
	// Association enforcement helpers need the controller-runtime client.
	needsClient := opsConfig.RequireClient || g.entityHasReferences(entityName) || len(associations) > 0

	parents, err := g.resolveParents(entityName, schema)
	if err != nil {
		return nil, err
	}

	// createFullyWrapped is true when create.path is in the operations package
	// (i.e. the full request struct with path params included), which occurs for
	// entities with multiple parent dependencies in the URL path.
	createFullyWrapped := len(parents) >= 2

	// For fully-wrapped requests the JSON body lives under a named field on the
	// operations wrapper; label/tag injection must target that field.
	var createBodyField string
	if createFullyWrapped {
		bodyInfo, err := ParseSDKRequestBodyInfo(createReqImportPath, createReqType)
		if err != nil {
			return nil, fmt.Errorf("entity %q: inspect create request body: %w", entityName, err)
		}
		createBodyField = bodyInfo.FieldName
	}

	var respRootUnion *opsCreateRootUnionResponseData
	if schema.SuccessResponseRef != "" {
		variantFieldNames, err := ParseSDKUnionMemberFieldNames(
			"github.com/Kong/sdk-konnect-go/models/components",
			schema.SuccessResponseRef,
		)
		if err != nil {
			return nil, fmt.Errorf("entity %q: inspect create response union %q: %w", entityName, schema.SuccessResponseRef, err)
		}
		if len(variantFieldNames) > 0 {
			respRootUnion = &opsCreateRootUnionResponseData{
				VariantFieldNames: variantFieldNames,
			}
		}
	}

	return &opsCreateFuncData{
		Entity:               entityName,
		APIAlias:             g.config.APIGroupPackageAlias,
		SDKInterface:         sdkInterface,
		SDKMethod:            sdkMethod,
		CreateReqMethod:      createReqMethod,
		CreateReqType:        createReqType,
		CreateReqBodyPointer: schema.CreateReqBodyPointer,
		NeedsClient:          needsClient,
		HasReferences:        g.entityHasParentRefReplacement(entityName),
		RespField:            schema.SuccessResponseRef,
		HasTags:              hasTags,
		HasLabels:            hasLabels,
		LabelsPointer:        labelsPointer,
		Parents:              parents,
		CreateFullyWrapped:   createFullyWrapped,
		CreateBodyField:      createBodyField,
		RespIDIsPointer:      schema.RespIDIsPointer,
		SingletonNoID:        isSingletonNoID(schema),
		RespRootUnion:        respRootUnion,
		ResponseStatusFields: opsConfig.ResponseStatusFields,
		Associations:         associations,
		SupportsMirror:       g.entitySupportsMirror(entityName),
		GetSDKMethod:         "Get" + strings.TrimPrefix(sdkMethod, "Create"),
	}, nil
}

// opsAssociations returns the association template data for an entity's ops
// create/update funcs, or nil when none are configured.
func (g *Generator) opsAssociations(entityName string) []opsAssociationData {
	assocs := g.entityAssociations(entityName)
	if len(assocs) == 0 {
		return nil
	}
	result := make([]opsAssociationData, len(assocs))
	for i, a := range assocs {
		result[i] = opsAssociationData{
			GoFieldName:  goFieldName(a.Name),
			SDKMethod:    a.SDKMethod,
			ResponseType: a.SDKMethod + "Response",
		}
	}
	return result
}

type flatInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
	NeedsClient    bool
	SkipUpdate     bool
}

// GenerateOpsCreateDispatcher emits zz_generated_ops_create.go with
// CreateGeneratedOps[T,TEnt]. Call after all per-group generation has finished.
func GenerateOpsCreateDispatcher(infos []*OpsCreateFileInfo) (*GeneratedFile, error) {
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
	return buildDispatcherFile("zz_generated_ops_create.go", opsCreateDispatcherTemplate, "controller/konnect/ops", flat)
}
