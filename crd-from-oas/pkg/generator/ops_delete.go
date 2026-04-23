package generator

import (
	"fmt"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// OpsDeleteFileInfo is metadata returned from generateEntityOpsFile so the
// caller (e.g. run.go) can assemble the cross-group delete dispatcher.
type OpsDeleteFileInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
}

// opsDeleteFuncData holds template data for a single delete<Entity> function.
type opsDeleteFuncData struct {
	Entity             string
	APIAlias           string
	DeleteSDKInterface string
	DeleteSDKMethod    string
	ParentEntityName   string
	ParentIDGetter     string
	// DeleteNilArgs holds one entry per optional query parameter on the DELETE
	// operation. The SDK codegen promotes query params to positional args before
	// the variadic opts; we pass nil for each since they are all optional.
	DeleteNilArgs []struct{}
}

// generateOpsDeleteFuncBody renders the delete<Entity> function body (no file header).
func (g *Generator) generateOpsDeleteFuncBody(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*opsDeleteFuncData, error) {
	deleteOp, ok := opsConfig.Ops["delete"]
	if !ok || deleteOp == nil {
		return nil, nil
	}
	if schema.DeleteOperationID == "" {
		return nil, fmt.Errorf("entity %q: missing OpenAPI operationId for delete op (no DELETE found)", entityName)
	}
	if len(schema.DeleteTags) == 0 {
		return nil, fmt.Errorf("entity %q: missing OpenAPI tags for delete op", entityName)
	}

	sdkMethod := pascalFromKebab(schema.DeleteOperationID)
	sdkInterface := pascalFromKebab(schema.DeleteTags[0]) + "SDK"

	parentEntityName, parentIDGetter, err := g.resolveParentEntity(entityName, schema)
	if err != nil {
		return nil, err
	}

	nilArgs := make([]struct{}, schema.DeleteQueryParamCount)

	return &opsDeleteFuncData{
		Entity:             entityName,
		APIAlias:           g.config.APIGroupPackageAlias,
		DeleteSDKInterface: sdkInterface,
		DeleteSDKMethod:    sdkMethod,
		ParentEntityName:   parentEntityName,
		ParentIDGetter:     parentIDGetter,
		DeleteNilArgs:      nilArgs,
	}, nil
}

// GenerateOpsDeleteDispatcher emits zz_generated_ops_delete.go with
// DeleteGeneratedOps[T,TEnt]. Call after all per-group generation has finished.
func GenerateOpsDeleteDispatcher(infos []*OpsDeleteFileInfo) (*GeneratedFile, error) {
	flat := make([]flatInfo, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, flatInfo{
			Entity:         info.Entity,
			APIAlias:       info.APIAlias,
			APIPackagePath: info.APIPackagePath,
			SDKGetter:      info.SDKGetter,
		})
	}
	return buildDispatcherFile("zz_generated_ops_delete.go", opsDeleteDispatcherTemplate, "controller/konnect/ops", flat)
}
