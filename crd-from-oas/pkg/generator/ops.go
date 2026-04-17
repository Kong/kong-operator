package generator

import (
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// OpsCreateFileInfo is metadata returned from generateOpsCreate so the caller
// (e.g. run.go) can assemble the cross-group dispatcher.
type OpsCreateFileInfo struct {
	Entity         string // PascalCase entity name, e.g. "Portal"
	APIAlias       string // Go import alias for the API package, e.g. "konnectv1alpha1"
	APIPackagePath string // Go import path for the API package
	SDKGetter      string // SDKWrapper method name, e.g. "GetPortalsSDK"
}

// generateOpsCreate emits a controller/konnect/ops/zz_generated_<entity>_ops.go
// file containing a create<Entity> function for a single reconciler entity.
// The entity must have ops.create configured and a non-nil reconciler config.
func (g *Generator) generateOpsCreate(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, *OpsCreateFileInfo, error) {
	createOp, ok := opsConfig.Ops["create"]
	if !ok || createOp == nil {
		return nil, nil, nil
	}
	if schema.OperationID == "" {
		return nil, nil, fmt.Errorf("entity %q: missing OpenAPI operationId for create op", entityName)
	}
	if len(schema.Tags) == 0 {
		return nil, nil, fmt.Errorf("entity %q: missing OpenAPI tags for create op", entityName)
	}
	if schema.SuccessResponseRef == "" {
		return nil, nil, fmt.Errorf("entity %q: missing 2xx response ref for create op", entityName)
	}

	_, createReqType, err := ParseSDKTypePath(createOp.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("entity %q: %w", entityName, err)
	}

	sdkMethod := pascalFromKebab(schema.OperationID)
	sdkInterface := pascalFromKebab(schema.Tags[0]) + "SDK"
	sdkGetter := "Get" + sdkInterface

	hasTags, hasLabels, labelsPointer := metadataFields(schema)

	data := struct {
		Entity         string
		APIAlias       string
		APIPackagePath string
		SDKInterface   string
		SDKMethod      string
		CreateReqType  string
		RespField      string
		HasTags        bool
		HasLabels      bool
		LabelsPointer  bool
	}{
		Entity:         entityName,
		APIAlias:       g.config.APIGroupPackageAlias,
		APIPackagePath: g.config.APIGroupPackagePath,
		SDKInterface:   sdkInterface,
		SDKMethod:      sdkMethod,
		CreateReqType:  createReqType,
		RespField:      schema.SuccessResponseRef,
		HasTags:        hasTags,
		HasLabels:      hasLabels,
		LabelsPointer:  labelsPointer,
	}

	tmpl := template.Must(template.New("opscreate").Parse(opsCreateTemplate))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, nil, err
	}

	file := &GeneratedFile{
		Name:        "zz_generated_" + entityFilePrefix(entityName) + "_ops.go",
		Content:     buf.String(),
		RelativeDir: "controller/konnect/ops",
	}
	info := &OpsCreateFileInfo{
		Entity:         entityName,
		APIAlias:       g.config.APIGroupPackageAlias,
		APIPackagePath: g.config.APIGroupPackagePath,
		SDKGetter:      sdkGetter,
	}
	return file, info, nil
}

// GenerateOpsDispatcher emits the controller/konnect/ops/zz_generated_ops.go
// dispatcher covering the given entities. Call after all per-group generation
// has finished so cases can span multiple API groups.
func GenerateOpsDispatcher(infos []*OpsCreateFileInfo) (*GeneratedFile, error) {
	if len(infos) == 0 {
		return nil, nil
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Entity < infos[j].Entity })

	type dispatchCase struct {
		Entity    string
		APIAlias  string
		SDKGetter string
	}

	importSet := map[string]string{} // path -> alias
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

	tmpl := template.Must(template.New("opsdispatcher").Parse(opsDispatcherTemplate))
	var buf strings.Builder
	data := struct {
		APIImportsBlock string
		Cases           []dispatchCase
	}{
		APIImportsBlock: importsBlock,
		Cases:           cases,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return &GeneratedFile{
		Name:        "zz_generated_ops.go",
		Content:     buf.String(),
		RelativeDir: "controller/konnect/ops",
	}, nil
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
