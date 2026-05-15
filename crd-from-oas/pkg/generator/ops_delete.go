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
	DeleteAsUpdate     bool
	// Parents holds metadata for each parent dependency (outermost first).
	Parents []parentInfo
	// DeleteFullyWrapped is true for multi-parent entities. The SDK delete method
	// takes a single operations.DeleteXxxRequest struct rather than positional args.
	DeleteFullyWrapped bool
	// DeleteWrappedType is the SDK operations struct type name for fully-wrapped
	// delete, e.g. "DeleteEventGatewayListenerPolicyRequest".
	DeleteWrappedType string
	// DeleteEntityIDField is the SDK request field name for the entity's own ID,
	// e.g. "PolicyID". Used only when DeleteFullyWrapped is true.
	DeleteEntityIDField string
	// DeleteNilArgs holds one entry per optional query parameter on the DELETE
	// operation. The SDK codegen promotes query params to positional args before
	// the variadic opts; we pass nil for each since they are all optional.
	// Only used when DeleteFullyWrapped is false.
	DeleteNilArgs []struct{}
	// DeleteOmitsEntityID is true for parent-scoped singleton resources whose
	// DELETE path contains only parent path params and no entity-specific ID (e.g.
	// DELETE /portals/{portalId}/email-config). The SDK method takes the parent ID
	// directly; no entity ID local variable or argument is emitted.
	DeleteOmitsEntityID bool
	// DeletePutReqImportPath is the Go import path for the SDK type used when
	// delete is implemented via the update/PUT operation.
	DeletePutReqImportPath string
	// DeletePutReqQualifiedType is the fully qualified SDK type used for the empty
	// request body (or fully wrapped request) when delete is implemented via PUT.
	DeletePutReqQualifiedType string
	// DeletePutWrapped mirrors the update op's wrapped-request call shape.
	DeletePutWrapped bool
	// DeletePutFullyWrapped mirrors the update op's fully-wrapped request shape.
	DeletePutFullyWrapped bool
	// DeletePutOmitsEntityID mirrors the update op's parent-scoped singleton shape.
	DeletePutOmitsEntityID bool
	// DeletePutParentIDField is the parent ID field on the synthetic operations
	// request struct for single-parent wrapped PUT calls.
	DeletePutParentIDField string
	// DeletePutEntityIDField is the entity ID field on wrapped PUT calls.
	DeletePutEntityIDField string
	// DeletePutBodyField is the request body field on the synthetic operations
	// request struct for single-parent wrapped PUT calls.
	DeletePutBodyField string
	// DeletePutReqBodyPointer indicates whether the update/PUT SDK method expects
	// the request body by pointer.
	DeletePutReqBodyPointer bool
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
	if deleteOp.AsPUT {
		callShape, err := g.resolveUpdateOpCallShape(entityName, schema, opsConfig)
		if err != nil {
			return nil, err
		}
		if callShape == nil {
			return nil, fmt.Errorf("entity %q: ops.delete.asPUT requires update op", entityName)
		}
		return &opsDeleteFuncData{
			Entity:                    entityName,
			APIAlias:                  g.config.APIGroupPackageAlias,
			DeleteSDKInterface:        callShape.SDKInterface,
			DeleteSDKMethod:           callShape.SDKMethod,
			DeleteAsUpdate:            true,
			Parents:                   callShape.Parents,
			DeletePutReqImportPath:    callShape.ReqImportPath,
			DeletePutReqQualifiedType: callShape.ReqQualifiedType,
			DeletePutWrapped:          callShape.Wrapped,
			DeletePutFullyWrapped:     callShape.FullyWrapped,
			DeletePutOmitsEntityID:    callShape.OmitsEntityID,
			DeletePutParentIDField:    callShape.ParentIDField,
			DeletePutEntityIDField:    callShape.EntityIDField,
			DeletePutBodyField:        callShape.BodyField,
			DeletePutReqBodyPointer:   callShape.ReqBodyPointer,
		}, nil
	}
	if schema.DeleteOperationID == "" {
		return nil, fmt.Errorf("entity %q: missing OpenAPI operationId for delete op (no DELETE found)", entityName)
	}
	if len(schema.DeleteTags) == 0 {
		return nil, fmt.Errorf("entity %q: missing OpenAPI tags for delete op", entityName)
	}

	sdkMethod := pascalFromKebab(schema.DeleteOperationID)
	sdkInterface := pascalFromKebab(schema.DeleteTags[0]) + "SDK"
	sdkInterface, err := resolveSDKInterfaceTypeName(opsConfig, sdkInterface)
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve delete SDK interface: %w", entityName, err)
	}

	parents, err := g.resolveParents(entityName, schema)
	if err != nil {
		return nil, err
	}

	// Multi-parent entities use a fully-wrapped delete request struct.
	deleteFullyWrapped := len(parents) >= 2
	// deleteOmitsEntityID is true for parent-scoped singletons: the DELETE path
	// contains only parent path params (no entity-specific ID). The SDK method
	// takes the parent ID positionally; no entity ID is emitted.
	deleteOmitsEntityID := !deleteFullyWrapped && len(parents) > 0 && len(schema.DeletePathParams) <= len(parents)

	var deleteWrappedType, deleteEntityIDField string
	if deleteFullyWrapped {
		// The wrapped request type name follows the SDK convention:
		// "<PascalCaseDeleteMethod>Request", e.g. "DeleteEventGatewayListenerPolicyRequest".
		deleteWrappedType = sdkMethod + "Request"
		// Entity ID field is derived from the last delete path param.
		if len(schema.DeletePathParams) > 0 {
			deleteEntityIDField = pathParamToFieldName(schema.DeletePathParams[len(schema.DeletePathParams)-1])
		}
	}

	nilArgs := make([]struct{}, schema.DeleteQueryParamCount)

	return &opsDeleteFuncData{
		Entity:              entityName,
		APIAlias:            g.config.APIGroupPackageAlias,
		DeleteSDKInterface:  sdkInterface,
		DeleteSDKMethod:     sdkMethod,
		Parents:             parents,
		DeleteFullyWrapped:  deleteFullyWrapped,
		DeleteWrappedType:   deleteWrappedType,
		DeleteEntityIDField: deleteEntityIDField,
		DeleteNilArgs:       nilArgs,
		DeleteOmitsEntityID: deleteOmitsEntityID,
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
