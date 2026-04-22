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
}

// opsCreateFuncData holds template data for a single create<Entity> function.
type opsCreateFuncData struct {
	Entity           string
	APIAlias         string
	SDKInterface     string
	SDKMethod        string
	CreateReqType    string
	RespField        string
	HasTags          bool
	HasLabels        bool
	LabelsPointer    bool
	ParentEntityName string
	ParentIDGetter   string
	RespIDIsPointer  bool
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
	hasTags, hasLabels, labelsPointer := metadataFields(schema)

	parentEntityName, parentIDGetter, err := g.resolveParentEntity(entityName, schema)
	if err != nil {
		return nil, err
	}

	return &opsCreateFuncData{
		Entity:           entityName,
		APIAlias:         g.config.APIGroupPackageAlias,
		SDKInterface:     sdkInterface,
		SDKMethod:        sdkMethod,
		CreateReqType:    createReqType,
		RespField:        schema.SuccessResponseRef,
		HasTags:          hasTags,
		HasLabels:        hasLabels,
		LabelsPointer:    labelsPointer,
		ParentEntityName: parentEntityName,
		ParentIDGetter:   parentIDGetter,
		RespIDIsPointer:  schema.RespIDIsPointer,
	}, nil
}

type flatInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
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
		})
	}
	return buildDispatcherFile("zz_generated_ops_create.go", opsCreateDispatcherTemplate, flat)
}
