package generator

import (
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// generateEntityOpsFile emits a zz_generated_ops_<entity>.go file containing
// create<Entity>, update<Entity>, and delete<Entity> functions (whichever are
// configured). It returns the file plus metadata for each dispatcher.
func (g *Generator) generateEntityOpsFile(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsCreateFileInfo, *OpsUpdateFileInfo, *OpsDeleteFileInfo, error) {
	createData, err := g.generateOpsCreateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed create op for %s: %w", entityName, err)
	}

	updateData, err := g.generateOpsUpdateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed update op for %s: %w", entityName, err)
	}

	deleteData, err := g.generateOpsDeleteFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed delete op for %s: %w", entityName, err)
	}

	if createData == nil && updateData == nil && deleteData == nil {
		return nil, nil, nil, nil, nil
	}

	// Determine whether we need the sdkkonnectops import (wrapped request structs
	// are only used by update; delete always uses positional SDK args).
	needsOpsImport := updateData != nil && updateData.UpdateWrapped
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
		return nil, nil, nil, nil, err
	}

	// Render create function body.
	if createData != nil {
		createTmpl := template.Must(template.New("opscreatefunc").Parse(opsCreateFuncTemplate))
		if err := createTmpl.Execute(&content, createData); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	// Render update function body.
	if updateData != nil {
		updateTmpl := template.Must(template.New("opsupdatefunc").Parse(opsUpdateFuncTemplate))
		if err := updateTmpl.Execute(&content, updateData); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	// Render delete function body.
	if deleteData != nil {
		deleteTmpl := template.Must(template.New("opsdeletefunc").Parse(opsDeleteFuncTemplate))
		if err := deleteTmpl.Execute(&content, deleteData); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	file := &GeneratedFile{
		Name:        "zz_generated_ops_" + EntityFilePrefix(entityName) + ".go",
		Content:     content.String(),
		RelativeDir: "controller/konnect/ops",
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

	return file, createInfo, updateInfo, deleteInfo, nil
}

// generateOpsCreate is retained for backwards-compatible unit tests. It calls
// generateEntityOpsFile and returns only the create-specific outputs.
func (g *Generator) generateOpsCreate(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsCreateFileInfo, error) {
	file, createInfo, _, _, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return file, createInfo, err
}

// generateOpsUpdate is a thin wrapper for unit tests — returns only update outputs.
func (g *Generator) generateOpsUpdate(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsUpdateFileInfo, error) {
	file, _, updateInfo, _, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return file, updateInfo, err
}

// generateOpsDelete is a thin wrapper for unit tests — returns only delete outputs.
func (g *Generator) generateOpsDelete(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsDeleteFileInfo, error) {
	file, _, _, deleteInfo, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return file, deleteInfo, err
}

// buildDispatcherFile is a shared helper that renders a dispatcher Go file.
func buildDispatcherFile(
	fileName, templateStr string,
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
		RelativeDir: "controller/konnect/ops",
	}, nil
}

// resolveParentEntity returns the parent entity name and the ID getter method
// name for a non-root entity, deriving them from the schema's last dependency
// and the reconciler config's optional ParentEntityType override.
// Returns empty strings for root entities.
func (g *Generator) resolveParentEntity(entityName string, schema *parser.Schema) (parentEntityName, parentIDGetter string, err error) {
	rc := g.config.ReconcilerConfig[entityName]
	if rc == nil || rc.IsRoot {
		return "", "", nil
	}
	if len(schema.Dependencies) == 0 {
		return "", "", fmt.Errorf("non-root entity %q has no parent dependency in OAS path", entityName)
	}
	parentDep := schema.Dependencies[len(schema.Dependencies)-1]
	parentEntityName = parentDep.EntityName
	if rc.ParentEntityType != "" {
		parentEntityName = rc.ParentEntityType
	}
	parentIDGetter = "Get" + parentDep.EntityName + "ID"
	return parentEntityName, parentIDGetter, nil
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

func clientRequestHelperBase(entityName string) string {
	if trimmed, ok := strings.CutPrefix(entityName, "Konnect"); ok && trimmed != "" {
		return "kong" + trimmed
	}
	return toLowerCamel(entityName)
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
