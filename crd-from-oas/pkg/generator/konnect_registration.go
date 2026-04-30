package generator

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
)

func buildImportBlock(infos []*WatchFileInfo) string {
	importSet := map[string]string{}
	for _, info := range infos {
		importSet[info.APIPackagePath] = info.APIAlias
	}

	paths := make([]string, 0, len(importSet))
	for path := range importSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var imports strings.Builder
	for _, path := range paths {
		fmt.Fprintf(&imports, "\t%s %q\n", importSet[path], path)
	}

	return imports.String()
}

// GenerateKonnectConstraintsDispatcher emits
// controller/konnect/constraints/zz_generated_supported_types.go with the union
// of generated Konnect entity types.
func GenerateKonnectConstraintsDispatcher(infos []*WatchFileInfo) (*GeneratedFile, error) {
	if len(infos) == 0 {
		return nil, nil
	}

	sorted := append([]*WatchFileInfo(nil), infos...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Entity < sorted[j].Entity
	})

	var buf strings.Builder
	buf.WriteString(sharedGeneratedFilePreamble)
	buf.WriteString("\n\npackage constraints\n\nimport (\n")
	buf.WriteString(buildImportBlock(sorted))
	buf.WriteString(")\n\n")
	buf.WriteString("// SupportedGeneratedKonnectEntityType is the generated subset of\n")
	buf.WriteString("// constraints.SupportedKonnectEntityType.\n")
	buf.WriteString("type SupportedGeneratedKonnectEntityType interface {\n")
	for i, info := range sorted {
		suffix := "\n"
		if i < len(sorted)-1 {
			suffix = " |\n"
		}
		fmt.Fprintf(&buf, "\t%s.%s%s", info.APIAlias, info.Entity, suffix)
	}
	buf.WriteString("}\n")

	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to format generated zz_generated_supported_types.go: %w", err)
	}

	return &GeneratedFile{
		Name:        "zz_generated_supported_types.go",
		Content:     string(formatted),
		RelativeDir: "controller/konnect/constraints",
	}, nil
}

// GenerateKonnectAPIAuthWatchDispatcher emits
// controller/konnect/zz_generated_konnectapiauth_watch.go with generated root
// entities that directly reference KonnectAPIAuthConfiguration.
func GenerateKonnectAPIAuthWatchDispatcher(infos []*WatchFileInfo) (*GeneratedFile, error) {
	rootInfos := make([]*WatchFileInfo, 0, len(infos))
	for _, info := range infos {
		if info.IsRoot {
			rootInfos = append(rootInfos, info)
		}
	}
	if len(rootInfos) == 0 {
		return nil, nil
	}

	sort.Slice(rootInfos, func(i, j int) bool {
		return rootInfos[i].Entity < rootInfos[j].Entity
	})

	var buf strings.Builder
	buf.WriteString(sharedGeneratedFilePreamble)
	buf.WriteString("\n\npackage konnect\n\nimport (\n")
	buf.WriteString("\t\"sigs.k8s.io/controller-runtime/pkg/client\"\n\n")
	buf.WriteString(buildImportBlock(rootInfos))
	buf.WriteString("\t\"github.com/kong/kong-operator/v2/controller/konnect/constraints\"\n")
	buf.WriteString("\t\"github.com/kong/kong-operator/v2/internal/utils/index\"\n")
	buf.WriteString(")\n\n")
	buf.WriteString("var generatedKonnectAPIAuthReferencingTypes = []constraints.EntityWithKonnectAPIAuthConfigurationRef{\n")
	for _, info := range rootInfos {
		fmt.Fprintf(&buf, "\t&%s.%s{},\n", info.APIAlias, info.Entity)
	}
	buf.WriteString("}\n\n")
	buf.WriteString("var generatedKonnectAPIAuthReferencingTypeListsWithIndexes = map[client.ObjectList]string{\n")
	for _, info := range rootInfos {
		fmt.Fprintf(&buf, "\t&%s.%sList{}: index.IndexField%sOnAPIAuthConfiguration,\n", info.APIAlias, info.Entity, info.Entity)
	}
	buf.WriteString("}\n")

	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to format generated zz_generated_konnectapiauth_watch.go: %w", err)
	}

	return &GeneratedFile{
		Name:        "zz_generated_konnectapiauth_watch.go",
		Content:     string(formatted),
		RelativeDir: "controller/konnect",
	}, nil
}
