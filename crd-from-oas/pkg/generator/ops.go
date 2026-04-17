package generator

import (
	"fmt"
	"sort"
	"strings"
	"text/template"

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

// OpsUpdateFileInfo is metadata returned from generateEntityOpsFile so the
// caller (e.g. run.go) can assemble the cross-group update dispatcher.
type OpsUpdateFileInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
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

// opsUpdateFuncData holds template data for a single update<Entity> function.
type opsUpdateFuncData struct {
	Entity               string
	APIAlias             string
	UpdateSDKInterface   string
	UpdateSDKMethod      string
	UpdateReqType        string
	HasTags              bool
	HasLabels            bool
	LabelsPointer        bool
	ParentEntityName     string
	ParentIDGetter       string
	UpdateWrapped        bool   // true when SDK call uses a request-struct (non-root)
	ParentIDField        string // e.g. "PortalID" — only set when UpdateWrapped
	EntityIDField        string // e.g. "ID" — only set when UpdateWrapped
	UpdateBodyField      string // e.g. "UpdateIdentityProvider" — only set when UpdateWrapped
	UpdateReqBodyPointer bool   // true when SDK body param is a pointer
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

	var parentEntityName, parentIDGetter string
	rc := g.config.ReconcilerConfig[entityName]
	if rc != nil && !rc.IsRoot {
		if len(schema.Dependencies) == 0 {
			return nil, fmt.Errorf("non-root entity %q has no parent dependency in OAS path", entityName)
		}
		parentDep := schema.Dependencies[len(schema.Dependencies)-1]
		parentEntityName = parentDep.EntityName
		if rc.ParentEntityType != "" {
			parentEntityName = rc.ParentEntityType
		}
		parentIDGetter = "Get" + parentDep.EntityName + "ID"
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

	_, updateReqType, err := ParseSDKTypePath(updateOp.Path)
	if err != nil {
		return nil, fmt.Errorf("entity %q: %w", entityName, err)
	}

	sdkMethod := pascalFromKebab(schema.UpdateOperationID)
	sdkInterface := pascalFromKebab(schema.UpdateTags[0]) + "SDK"
	hasTags, hasLabels, labelsPointer := metadataFields(schema)

	var parentEntityName, parentIDGetter string
	rc := g.config.ReconcilerConfig[entityName]
	if rc != nil && !rc.IsRoot {
		if len(schema.Dependencies) == 0 {
			return nil, fmt.Errorf("non-root entity %q has no parent dependency in OAS path", entityName)
		}
		parentDep := schema.Dependencies[len(schema.Dependencies)-1]
		parentEntityName = parentDep.EntityName
		if rc.ParentEntityType != "" {
			parentEntityName = rc.ParentEntityType
		}
		parentIDGetter = "Get" + parentDep.EntityName + "ID"
	}

	// Determine SDK call shape based on number of path params in the PATCH path.
	// ≥2 params → wrapped operations.XxxRequest struct (parent ID + entity ID).
	// 1 param → positional (entity ID only, root entity).
	wrapped := len(schema.UpdatePathParams) >= 2
	var parentIDField, entityIDField, updateBodyField string
	if wrapped {
		params := schema.UpdatePathParams
		parentIDField = pathParamToFieldName(params[len(params)-2])
		entityIDField = pathParamToFieldName(params[len(params)-1])
		updateBodyField = updateReqType
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
		ParentEntityName:     parentEntityName,
		ParentIDGetter:       parentIDGetter,
		UpdateWrapped:        wrapped,
		ParentIDField:        parentIDField,
		EntityIDField:        entityIDField,
		UpdateBodyField:      updateBodyField,
		UpdateReqBodyPointer: schema.UpdateReqBodyPointer,
	}, nil
}

// generateEntityOpsFile emits a zz_generated_<entity>_ops.go file containing
// both create<Entity> and update<Entity> functions (whichever are configured).
// It returns the file plus metadata for each dispatcher.
func (g *Generator) generateEntityOpsFile(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsCreateFileInfo, *OpsUpdateFileInfo, error) {
	createData, err := g.generateOpsCreateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed create op for %s: %w", entityName, err)
	}

	updateData, err := g.generateOpsUpdateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed update op for %s: %w", entityName, err)
	}

	if createData == nil && updateData == nil {
		return nil, nil, nil, nil
	}

	// Determine whether we need the sdkkonnectops import.
	needsOpsImport := updateData != nil && updateData.UpdateWrapped

	// Render file header.
	headerData := struct {
		APIAlias       string
		APIPackagePath string
		NeedsOpsImport bool
	}{
		APIAlias:       g.config.APIGroupPackageAlias,
		APIPackagePath: g.config.APIGroupPackagePath,
		NeedsOpsImport: needsOpsImport,
	}
	var content strings.Builder
	headerTmpl := template.Must(template.New("opsheader").Parse(opsPerEntityFileHeaderTemplate))
	if err := headerTmpl.Execute(&content, headerData); err != nil {
		return nil, nil, nil, err
	}

	// Render create function body.
	if createData != nil {
		createTmpl := template.Must(template.New("opscreatefunc").Parse(opsCreateFuncTemplate))
		if err := createTmpl.Execute(&content, createData); err != nil {
			return nil, nil, nil, err
		}
	}

	// Render update function body.
	if updateData != nil {
		updateTmpl := template.Must(template.New("opsupdatefunc").Parse(opsUpdateFuncTemplate))
		if err := updateTmpl.Execute(&content, updateData); err != nil {
			return nil, nil, nil, err
		}
	}

	file := &GeneratedFile{
		Name:        "zz_generated_" + EntityFilePrefix(entityName) + "_ops.go",
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
		}
	}

	return file, createInfo, updateInfo, nil
}

// generateOpsCreate is retained for backwards-compatible unit tests. It calls
// generateEntityOpsFile and returns only the create-specific outputs.
func (g *Generator) generateOpsCreate(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsCreateFileInfo, error) {
	file, createInfo, _, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return file, createInfo, err
}

// generateOpsUpdate is a thin wrapper for unit tests — returns only update outputs.
func (g *Generator) generateOpsUpdate(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsUpdateFileInfo, error) {
	file, _, updateInfo, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	return file, updateInfo, err
}

// buildDispatcherFile is a shared helper that renders a dispatcher Go file.
func buildDispatcherFile(
	fileName, templateStr string,
	infos []struct{ Entity, APIAlias, APIPackagePath, SDKGetter string },
) (*GeneratedFile, error) {
	if len(infos) == 0 {
		return nil, nil
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Entity < infos[j].Entity })

	type dispatchCase struct {
		Entity    string
		APIAlias  string
		SDKGetter string
	}

	importSet := map[string]string{}
	cases := make([]dispatchCase, 0, len(infos))
	for _, info := range infos {
		importSet[info.APIPackagePath] = info.APIAlias
		cases = append(cases, dispatchCase{
			Entity:    info.Entity,
			APIAlias:  info.APIAlias,
			SDKGetter: info.SDKGetter,
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

// GenerateOpsCreateDispatcher emits zz_generated_ops_create.go with
// CreateGeneratedOps[T,TEnt]. Call after all per-group generation has finished.
func GenerateOpsCreateDispatcher(infos []*OpsCreateFileInfo) (*GeneratedFile, error) {
	flat := make([]struct{ Entity, APIAlias, APIPackagePath, SDKGetter string }, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, struct{ Entity, APIAlias, APIPackagePath, SDKGetter string }{
			info.Entity, info.APIAlias, info.APIPackagePath, info.SDKGetter,
		})
	}
	return buildDispatcherFile("zz_generated_ops_create.go", opsCreateDispatcherTemplate, flat)
}

// GenerateOpsUpdateDispatcher emits zz_generated_ops_update.go with
// UpdateGeneratedOps[T,TEnt]. Call after all per-group generation has finished.
func GenerateOpsUpdateDispatcher(infos []*OpsUpdateFileInfo) (*GeneratedFile, error) {
	flat := make([]struct{ Entity, APIAlias, APIPackagePath, SDKGetter string }, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, struct{ Entity, APIAlias, APIPackagePath, SDKGetter string }{
			info.Entity, info.APIAlias, info.APIPackagePath, info.SDKGetter,
		})
	}
	return buildDispatcherFile("zz_generated_ops_update.go", opsUpdateDispatcherTemplate, flat)
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
