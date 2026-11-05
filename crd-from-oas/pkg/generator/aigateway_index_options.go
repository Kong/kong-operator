package generator

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"
)

const (
	// aiGatewayIndexOptionsGroup is the API group of the AI Gateway
	// configuration entities.
	aiGatewayIndexOptionsGroup = "aiconfiguration.konghq.com"

	// AIGatewayIndexOptionsFile is the file emitted by
	// GenerateAIGatewayIndexOptionsDispatcher.
	AIGatewayIndexOptionsFile = "zz_generated_aigateway_index_options.go"

	// AIGatewayIndexOptionsDir is the directory of AIGatewayIndexOptionsFile,
	// relative to the project root. It is the package that holds the
	// per-entity Index options, so every caller of the aggregate already
	// imports it.
	AIGatewayIndexOptionsDir = "internal/utils/index"
)

// GenerateAIGatewayIndexOptionsDispatcher emits the Index options of every AI
// Gateway configuration-entity kind as one function,
// OptionsForAIGatewayEntities. BuildDocument lists every one of those kinds,
// so every cache serving it needs all of their indexes. Call after all
// per-group generation has finished.
func GenerateAIGatewayIndexOptionsDispatcher(infos []*WatchFileInfo) (*GeneratedFile, error) {
	entities := make([]string, 0, len(infos))
	for _, info := range infos {
		if info.Group != aiGatewayIndexOptionsGroup {
			continue
		}
		entities = append(entities, info.Entity)
	}
	if len(entities) == 0 {
		return nil, nil
	}
	sort.Strings(entities)

	tmpl := template.Must(template.New("aigateway-index-options").Parse(aiGatewayIndexOptionsTemplate))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, struct {
		Entities []string
	}{Entities: entities}); err != nil {
		return nil, err
	}

	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to format generated %s: %w", AIGatewayIndexOptionsFile, err)
	}

	return &GeneratedFile{
		Name:        AIGatewayIndexOptionsFile,
		Content:     string(formatted),
		RelativeDir: AIGatewayIndexOptionsDir,
	}, nil
}
