package generator

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"
)

type sdkFactoryCase struct {
	GetterName          string
	Alias               string
	TypeName            string
	FieldName           string
	Entity              string
	MockFieldName       string
	MockTypeName        string
	MockConstructorName string
}

type sdkFactoryTemplateData struct {
	APIImportsBlock string
	Cases           []sdkFactoryCase
}

// SDKFactoryFileInfo captures what is needed to emit one Get<X>SDK method on
// the shared sdkWrapper and one entry in the GeneratedSDK interface.
type SDKFactoryFileInfo struct {
	Entity                 string // e.g. "KonnectEventGateway"
	SDKInterfaceImportPath string // e.g. "github.com/Kong/sdk-konnect-go"
	SDKInterfaceTypeName   string // e.g. "EventGatewaysSDK"
	SDKFieldName           string // e.g. "EventGateways"
}

// GenerateSDKFactoryDispatcher renders controller/konnect/ops/sdk/zz_generated_sdkfactory.go.
func GenerateSDKFactoryDispatcher(infos []*SDKFactoryFileInfo) (*GeneratedFile, error) {
	data := buildSDKFactoryTemplateData(infos)
	if data == nil {
		return nil, nil
	}

	return renderSDKFactoryDispatcher(
		"sdkfactory",
		sdkFactoryTemplate,
		"zz_generated_sdkfactory.go",
		"controller/konnect/ops/sdk",
		data,
	)
}

// GenerateMockSDKFactoryDispatcher renders test/mocks/sdkmocks/zz_generated_sdkfactory_mock.go.
func GenerateMockSDKFactoryDispatcher(infos []*SDKFactoryFileInfo) (*GeneratedFile, error) {
	data := buildSDKFactoryTemplateData(infos)
	if data == nil {
		return nil, nil
	}

	return renderSDKFactoryDispatcher(
		"sdkfactory-mock",
		mockSDKFactoryTemplate,
		"zz_generated_sdkfactory_mock.go",
		"test/mocks/sdkmocks",
		data,
	)
}

func buildSDKFactoryTemplateData(infos []*SDKFactoryFileInfo) *sdkFactoryTemplateData {
	if len(infos) == 0 {
		return nil
	}

	sorted := make([]*SDKFactoryFileInfo, len(infos))
	copy(sorted, infos)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Entity < sorted[j].Entity })

	// Deduplicate imports: alias derived by stripping hyphens from the last path segment.
	importSet := map[string]string{}
	for _, info := range sorted {
		if _, ok := importSet[info.SDKInterfaceImportPath]; !ok {
			importSet[info.SDKInterfaceImportPath] = sdkFactoryImportAlias(info.SDKInterfaceImportPath)
		}
	}
	importPaths := make([]string, 0, len(importSet))
	for p := range importSet {
		importPaths = append(importPaths, p)
	}
	sort.Strings(importPaths)

	var importsBuf strings.Builder
	for _, p := range importPaths {
		fmt.Fprintf(&importsBuf, "\t%s %q\n", importSet[p], p)
	}
	importsBlock := strings.TrimRight(importsBuf.String(), "\n")

	// Multiple entities can share a single SDK interface (e.g. AIGatewayConsumer
	// and AIGatewayConsumerCredential both use AIGatewayConsumersSDK). The factory
	// emits one getter/interface entry/mock field per SDK interface, so deduplicate
	// cases by getter name to avoid duplicate method and field declarations.
	cases := make([]sdkFactoryCase, 0, len(sorted))
	seenGetters := make(map[string]bool, len(sorted))
	for _, info := range sorted {
		alias := importSet[info.SDKInterfaceImportPath]
		getterName := "Get" + strings.TrimSuffix(info.SDKInterfaceTypeName, "SDK") + "SDK"
		if seenGetters[getterName] {
			continue
		}
		seenGetters[getterName] = true
		cases = append(cases, sdkFactoryCase{
			GetterName:          getterName,
			Alias:               alias,
			TypeName:            info.SDKInterfaceTypeName,
			FieldName:           info.SDKFieldName,
			Entity:              info.Entity,
			MockFieldName:       mockSDKFieldName(info.SDKFieldName),
			MockTypeName:        mockSDKTypeName(info.SDKInterfaceTypeName),
			MockConstructorName: mockSDKConstructorName(info.SDKInterfaceTypeName),
		})
	}

	return &sdkFactoryTemplateData{
		APIImportsBlock: importsBlock,
		Cases:           cases,
	}
}

func renderSDKFactoryDispatcher(
	templateName string,
	templateStr string,
	fileName string,
	relativeDir string,
	data *sdkFactoryTemplateData,
) (*GeneratedFile, error) {
	tmpl := template.Must(template.New(templateName).Parse(templateStr))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to format generated %s: %w", fileName, err)
	}

	return &GeneratedFile{
		Name:        fileName,
		Content:     string(formatted),
		RelativeDir: relativeDir,
	}, nil
}

// sdkFactoryImportAlias derives a deterministic alias from an import path by
// stripping hyphens from the last path segment.
// e.g. "github.com/Kong/sdk-konnect-go" → "sdkkonnectgo".
func sdkFactoryImportAlias(importPath string) string {
	parts := strings.Split(importPath, "/")
	last := parts[len(parts)-1]
	return strings.ReplaceAll(last, "-", "")
}

func mockSDKFieldName(fieldName string) string {
	return fieldName + "SDK"
}

func mockSDKTypeName(typeName string) string {
	return "Mock" + typeName
}

func mockSDKConstructorName(typeName string) string {
	return "NewMock" + typeName
}
