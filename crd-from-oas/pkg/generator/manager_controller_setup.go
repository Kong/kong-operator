package generator

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"
)

// GenerateKonnectControllerSetupDispatcher emits
// modules/manager/zz_generated_konnect_controller_setup.go with
// generatedControllersForKonnectEntities. Call after all per-group generation
// has finished.
func GenerateKonnectControllerSetupDispatcher(infos []*WatchFileInfo) (*GeneratedFile, error) {
	flat := make([]flatInfo, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, flatInfo{
			Entity:         info.Entity,
			APIAlias:       info.APIAlias,
			APIPackagePath: info.APIPackagePath,
		})
	}

	return buildDispatcherFile(
		"zz_generated_konnect_controller_setup.go",
		managerControllerSetupTemplate,
		"modules/manager",
		flat,
	)
}

// GenerateKonnectIndexOptionsDispatcher emits
// modules/manager/zz_generated_konnect_index_options.go with
// generatedIndexOptionsForKonnectEntities. Call after all per-group generation
// has finished.
func GenerateKonnectIndexOptionsDispatcher(infos []*WatchFileInfo) (*GeneratedFile, error) {
	if len(infos) == 0 {
		return nil, nil
	}

	entities := make([]string, 0, len(infos))
	for _, info := range infos {
		entities = append(entities, info.Entity)
	}
	sort.Strings(entities)

	tmpl := template.Must(template.New("manager-index-options").Parse(managerIndexOptionsTemplate))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, struct {
		Entities []string
	}{Entities: entities}); err != nil {
		return nil, err
	}

	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to format generated zz_generated_konnect_index_options.go: %w", err)
	}

	return &GeneratedFile{
		Name:        "zz_generated_konnect_index_options.go",
		Content:     string(formatted),
		RelativeDir: "modules/manager",
	}, nil
}
