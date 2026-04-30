package generator

import (
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// entityOpsFileResult holds the outputs of generateEntityOpsFile.
type entityOpsFileResult struct {
	File           *GeneratedFile
	CreateInfo     *OpsCreateFileInfo
	UpdateInfo     *OpsUpdateFileInfo
	DeleteInfo     *OpsDeleteFileInfo
	GetForUIDInfo  *OpsGetForUIDFileInfo
	SDKFactoryInfo *SDKFactoryFileInfo
}

// generateEntityOpsFile emits a zz_generated_ops_<entity>.go file containing
// create<Entity>, update<Entity>, delete<Entity>, and get<Entity>ForUID
// functions (whichever are configured). It returns the file plus metadata for
// each dispatcher.
func (g *Generator) generateEntityOpsFile(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (entityOpsFileResult, error) {
	createData, err := g.generateOpsCreateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return entityOpsFileResult{}, fmt.Errorf("failed create op for %s: %w", entityName, err)
	}

	updateData, err := g.generateOpsUpdateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return entityOpsFileResult{}, fmt.Errorf("failed update op for %s: %w", entityName, err)
	}

	deleteData, err := g.generateOpsDeleteFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return entityOpsFileResult{}, fmt.Errorf("failed delete op for %s: %w", entityName, err)
	}

	getForUIDData, err := g.generateOpsGetForUIDFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return entityOpsFileResult{}, fmt.Errorf("failed getForUID op for %s: %w", entityName, err)
	}

	manualGetForUID := g.config.ManualGetForUIDEntities[entityName] && opsConfig != nil && opsConfig.SDK != nil
	if createData == nil && updateData == nil && deleteData == nil && getForUIDData == nil && !manualGetForUID {
		return entityOpsFileResult{}, nil
	}

	var file *GeneratedFile
	if createData != nil || updateData != nil || deleteData != nil || getForUIDData != nil {
		// Determine whether we need the sdkkonnectops import (wrapped request structs
		// are used by update and getForUID).
		needsOpsImport := (updateData != nil && updateData.UpdateWrapped) || getForUIDData != nil
		needsClientImport := (createData != nil && createData.NeedsClient) || (updateData != nil && updateData.NeedsClient)

		// Render file header.
		headerData := struct {
			APIAlias          string
			APIPackagePath    string
			NeedsOpsImport    bool
			NeedsClientImport bool
		}{
			APIAlias:          g.config.APIGroupPackageAlias,
			APIPackagePath:    g.config.APIGroupPackagePath,
			NeedsOpsImport:    needsOpsImport,
			NeedsClientImport: needsClientImport,
		}
		var content strings.Builder
		headerTmpl := template.Must(template.New("opsheader").Parse(opsPerEntityFileHeaderTemplate))
		if err := headerTmpl.Execute(&content, headerData); err != nil {
			return entityOpsFileResult{}, err
		}

		// Render create function body.
		if createData != nil {
			createTmpl := template.Must(template.New("opscreatefunc").Parse(opsCreateFuncTemplate))
			if err := createTmpl.Execute(&content, createData); err != nil {
				return entityOpsFileResult{}, err
			}
		}

		// Render update function body.
		if updateData != nil {
			updateTmpl := template.Must(template.New("opsupdatefunc").Parse(opsUpdateFuncTemplate))
			if err := updateTmpl.Execute(&content, updateData); err != nil {
				return entityOpsFileResult{}, err
			}
		}

		// Render delete function body.
		if deleteData != nil {
			deleteTmpl := template.Must(template.New("opsdeletefunc").Parse(opsDeleteFuncTemplate))
			if err := deleteTmpl.Execute(&content, deleteData); err != nil {
				return entityOpsFileResult{}, err
			}
		}

		// Render getForUID function body.
		if getForUIDData != nil {
			getForUIDTmpl := template.Must(template.New("opsgetforuidfunc").Parse(opsGetForUIDFuncTemplate))
			if err := getForUIDTmpl.Execute(&content, getForUIDData); err != nil {
				return entityOpsFileResult{}, err
			}
		}

		file = &GeneratedFile{
			Name:        "zz_generated_ops_" + EntityFilePrefix(entityName) + ".go",
			Content:     content.String(),
			RelativeDir: "controller/konnect/ops",
		}
	}

	var createInfo *OpsCreateFileInfo
	if createData != nil {
		sdkGetter := "Get" + createData.SDKInterface
		createInfo = &OpsCreateFileInfo{
			Entity:         entityName,
			APIAlias:       g.config.APIGroupPackageAlias,
			APIPackagePath: g.config.APIGroupPackagePath,
			SDKGetter:      sdkGetter,
			NeedsClient:    createData.NeedsClient,
		}
	}

	var updateInfo *OpsUpdateFileInfo
	if updateData != nil {
		sdkGetter := "Get" + updateData.UpdateSDKInterface
		updateInfo = &OpsUpdateFileInfo{
			Entity:         entityName,
			APIAlias:       g.config.APIGroupPackageAlias,
			APIPackagePath: g.config.APIGroupPackagePath,
			SDKGetter:      sdkGetter,
			NeedsClient:    updateData.NeedsClient,
		}
	}

	var deleteInfo *OpsDeleteFileInfo
	if deleteData != nil {
		sdkGetter := "Get" + deleteData.DeleteSDKInterface
		deleteInfo = &OpsDeleteFileInfo{
			Entity:         entityName,
			APIAlias:       g.config.APIGroupPackageAlias,
			APIPackagePath: g.config.APIGroupPackagePath,
			SDKGetter:      sdkGetter,
		}
	}

	var getForUIDInfo *OpsGetForUIDFileInfo
	if getForUIDData != nil {
		sdkGetter := "Get" + getForUIDData.ListSDKInterface
		getForUIDInfo = &OpsGetForUIDFileInfo{
			Entity:         entityName,
			APIAlias:       g.config.APIGroupPackageAlias,
			APIPackagePath: g.config.APIGroupPackagePath,
			SDKGetter:      sdkGetter,
		}
	} else if manualGetForUID {
		getForUIDInfo = &OpsGetForUIDFileInfo{
			Entity:         entityName,
			APIAlias:       g.config.APIGroupPackageAlias,
			APIPackagePath: g.config.APIGroupPackagePath,
			SDKGetter:      "Get" + opsConfig.SDK.FieldName + "SDK",
		}
	}

	var sdkFactoryInfo *SDKFactoryFileInfo
	if opsConfig.SDK != nil {
		importPath, typeName, err := ParseSDKTypePath(opsConfig.SDK.Interface)
		if err != nil {
			return entityOpsFileResult{}, fmt.Errorf("ops.sdk.interface for %s: %w", entityName, err)
		}
		sdkFactoryInfo = &SDKFactoryFileInfo{
			Entity:                 entityName,
			SDKInterfaceImportPath: importPath,
			SDKInterfaceTypeName:   typeName,
			SDKFieldName:           opsConfig.SDK.FieldName,
		}
	}

	return entityOpsFileResult{
		File:           file,
		CreateInfo:     createInfo,
		UpdateInfo:     updateInfo,
		DeleteInfo:     deleteInfo,
		GetForUIDInfo:  getForUIDInfo,
		SDKFactoryInfo: sdkFactoryInfo,
	}, nil
}

// generateOpsCreate is retained for backwards-compatible unit tests. It calls
// generateEntityOpsFile and returns only the create-specific outputs.
func (g *Generator) generateOpsCreate(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsCreateFileInfo, error) {
	res, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return res.File, res.CreateInfo, err
}

// generateOpsUpdate is a thin wrapper for unit tests — returns only update outputs.
func (g *Generator) generateOpsUpdate(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsUpdateFileInfo, error) {
	res, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return res.File, res.UpdateInfo, err
}

// generateOpsDelete is a thin wrapper for unit tests — returns only delete outputs.
func (g *Generator) generateOpsDelete(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsDeleteFileInfo, error) {
	res, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return res.File, res.DeleteInfo, err
}

// buildDispatcherFile is a shared helper that renders a dispatcher Go file.
func buildDispatcherFile(
	fileName, templateStr, relativeDir string,
	infos []flatInfo,
) (*GeneratedFile, error) {
	if len(infos) == 0 {
		return nil, nil
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Entity < infos[j].Entity })

	type dispatchCase struct {
		Entity      string
		APIAlias    string
		SDKGetter   string
		NeedsClient bool
	}

	importSet := map[string]string{}
	cases := make([]dispatchCase, 0, len(infos))
	for _, info := range infos {
		importSet[info.APIPackagePath] = info.APIAlias
		cases = append(cases, dispatchCase{
			Entity:      info.Entity,
			APIAlias:    info.APIAlias,
			SDKGetter:   info.SDKGetter,
			NeedsClient: info.NeedsClient,
		})
	}

	paths := make([]string, 0, len(importSet))
	for p := range importSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var importsBuf strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&importsBuf, "\t%s %q\n", importSet[p], p)
	}
	importsBlock := strings.TrimRight(importsBuf.String(), "\n")

	tmpl := template.Must(template.New("dispatcher").Parse(templateStr))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, struct {
		APIImportsBlock string
		Cases           []dispatchCase
	}{importsBlock, cases}); err != nil {
		return nil, err
	}

	return &GeneratedFile{
		Name:        fileName,
		Content:     buf.String(),
		RelativeDir: relativeDir,
	}, nil
}

// parentInfo holds per-parent metadata used by the op generator templates.
type parentInfo struct {
	// EntityName is the Go type name of the parent entity, e.g. "KonnectEventGateway".
	// For the immediate (last) parent it may be overridden by ReconcilerConfig.ParentEntityType.
	EntityName string
	// IDGetter is the method name to fetch the parent's Konnect ID from the child object,
	// e.g. "GetGatewayID". Derived from the raw dependency EntityName (before ParentEntityType override).
	IDGetter string
	// VarName is the Go local variable name to use in generated code, e.g. "gatewayID".
	VarName string
	// SDKFieldName is the field name in the SDK operations request struct for this parent param,
	// e.g. "GatewayID" or "ListenerID". Derived from pathParamToFieldName or config override.
	SDKFieldName string
}

// resolveParents returns parent info for all parent dependencies of a non-root entity,
// ordered from outermost (first URL param) to innermost (immediate parent, last URL param).
// Returns nil for root entities.
func (g *Generator) resolveParents(entityName string, schema *parser.Schema) ([]parentInfo, error) {
	rc := g.config.ReconcilerConfig[entityName]
	if rc == nil || rc.IsRoot {
		return nil, nil
	}
	if len(schema.Dependencies) == 0 {
		return nil, fmt.Errorf("non-root entity %q has no parent dependency in OAS path", entityName)
	}

	parents := make([]parentInfo, len(schema.Dependencies))
	for i, dep := range schema.Dependencies {
		name := dep.EntityName
		// ParentEntityType overrides only the immediate (last) parent entity name.
		if i == len(schema.Dependencies)-1 && rc.ParentEntityType != "" {
			name = rc.ParentEntityType
		}

		sdkField := pathParamToFieldName(dep.ParamName)
		if i < len(rc.ParentSDKFields) && rc.ParentSDKFields[i] != "" {
			sdkField = rc.ParentSDKFields[i]
		}

		// VarName: entity-specific name for multi-parent, generic "parentID" for single-parent.
		rawName := dep.EntityName
		varName := strings.ToLower(rawName[:1]) + rawName[1:] + "ID"

		parents[i] = parentInfo{
			EntityName:   name,
			IDGetter:     "Get" + dep.EntityName + "ID",
			VarName:      varName,
			SDKFieldName: sdkField,
		}
	}
	// Single-parent entities use the generic "parentID" var name for backwards compatibility
	// with existing generated code and templates. Multi-parent entities use entity-specific names.
	if len(parents) == 1 {
		parents[0].VarName = "parentID"
	}
	return parents, nil
}

func resolveSDKInterfaceTypeName(opsConfig *config.EntityOpsConfig, fallbackTypeName string) (string, error) {
	if opsConfig == nil || opsConfig.SDK == nil || opsConfig.SDK.Interface == "" {
		return fallbackTypeName, nil
	}

	_, typeName, err := ParseSDKTypePath(opsConfig.SDK.Interface)
	if err != nil {
		return "", err
	}

	return typeName, nil
}

// pathParamToFieldName converts an OpenAPI path parameter name to a Go struct
// field name using the Speakeasy SDK codegen convention:
// "portalId" → "PortalID", "id" → "ID", "certificateId" → "CertificateID".
func pathParamToFieldName(param string) string {
	if param == "" {
		return ""
	}
	name := strings.ToUpper(param[:1]) + param[1:]
	if strings.HasSuffix(name, "Id") {
		name = name[:len(name)-2] + "ID"
	}
	return name
}

// pascalFromKebab converts a kebab-case or space-separated identifier to
// PascalCase. e.g. "create-portal" → "CreatePortal",
// "create-event-gateway" → "CreateEventGateway".
func pascalFromKebab(s string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	}) {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

// metadataFields reports whether the request body schema declares a "tags"
// array property or a "labels" object/map property, driving the optional
// metadata injection in the generated create function. labelsPointer is true
// when the labels map uses nullable string values (map[string]*string in the
// SDK), which requires the pointer-valued helper variant.
func metadataFields(schema *parser.Schema) (hasTags, hasLabels, labelsPointer bool) {
	if schema == nil {
		return false, false, false
	}
	for _, prop := range schema.Properties {
		if prop == nil {
			continue
		}
		switch prop.Name {
		case "tags":
			if prop.Type == "array" {
				hasTags = true
			}
		case "labels":
			if prop.Type == "object" || prop.AdditionalProperties != nil {
				hasLabels = true
				if prop.AdditionalProperties != nil && prop.AdditionalProperties.Nullable {
					labelsPointer = true
				}
			}
		}
	}
	return hasTags, hasLabels, labelsPointer
}
