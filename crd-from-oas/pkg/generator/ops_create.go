package generator

import (
	"fmt"

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
	CreateReqType        string
	CreateReqBodyPointer bool
	NeedsClient          bool
	RespField            string
	HasTags              bool
	HasLabels            bool
	LabelsPointer        bool
	// Parents holds metadata for each parent dependency (outermost first).
	// Single-parent entities have len(Parents)==1; root entities have len==0.
	Parents []parentInfo
	// CreateFullyWrapped is true when the create.path is a fully-wrapped
	// operations.XxxRequest struct (multi-parent case). When true, the generated
	// code sets the parent ID fields on the returned request object instead of
	// passing parentID as a positional argument.
	CreateFullyWrapped bool
	RespIDIsPointer    bool
	// SingletonNoID is true when the create response schema has no "id" field.
	// Generated code skips the SetKonnectID call entirely for these entities.
	SingletonNoID bool
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

	_, createReqType, err := ParseSDKTypePath(createOp.Path)
	if err != nil {
		return nil, fmt.Errorf("entity %q: %w", entityName, err)
	}

	sdkMethod := pascalFromKebab(schema.OperationID)
	sdkInterface := pascalFromKebab(schema.Tags[0]) + "SDK"
	sdkInterface, err = resolveSDKInterfaceTypeName(opsConfig, sdkInterface)
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve create SDK interface: %w", entityName, err)
	}
	hasTags, hasLabels, labelsPointer := metadataFields(schema)
	needsClient := opsConfig.RequireClient

	parents, err := g.resolveParents(entityName, schema)
	if err != nil {
		return nil, err
	}

	// createFullyWrapped is true when create.path is in the operations package
	// (i.e. the full request struct with path params included), which occurs for
	// entities with multiple parent dependencies in the URL path.
	createFullyWrapped := len(parents) >= 2

	return &opsCreateFuncData{
		Entity:               entityName,
		APIAlias:             g.config.APIGroupPackageAlias,
		SDKInterface:         sdkInterface,
		SDKMethod:            sdkMethod,
		CreateReqType:        createReqType,
		CreateReqBodyPointer: schema.CreateReqBodyPointer,
		NeedsClient:          needsClient,
		RespField:            schema.SuccessResponseRef,
		HasTags:              hasTags,
		HasLabels:            hasLabels,
		LabelsPointer:        labelsPointer,
		Parents:              parents,
		CreateFullyWrapped:   createFullyWrapped,
		RespIDIsPointer:      schema.RespIDIsPointer,
		SingletonNoID:        isSingletonNoID(schema),
	}, nil
}

type flatInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
	NeedsClient    bool
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
