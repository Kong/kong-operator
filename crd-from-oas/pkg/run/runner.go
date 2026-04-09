package run

import (
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
)

// Runner is responsible for orchestrating the entire generation process.
// It processes each API group-version defined in the project config, parses the OpenAPI spec,
// and generates the corresponding Go types based on the parsed schemas and configurations.
type Runner struct {
	projectCfg  *config.ProjectConfig
	gvKeys      []string
	openAPI     *openapi3.T
	outputDir   string
	projectRoot string
}

// New creates new runner with the given project config, OpenAPI spec file path, and output directory.
// It loads the OpenAPI spec and prepares the runner for execution.
// The actual generation is performed in the Run method.
func New(
	projectCfg *config.ProjectConfig,
	openAPIFile string,
	outputDir string,
	opts ...Option,
) (*Runner, error) {
	gvKeys := slices.Collect(maps.Keys(projectCfg.APIGroupVersions))
	sort.Strings(gvKeys)

	// Load OpenAPI spec (shared across all group-versions)
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(openAPIFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI spec: %w", err)
	}

	r := &Runner{
		projectCfg: projectCfg,
		gvKeys:     gvKeys,
		openAPI:    doc,
		outputDir:  outputDir,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// Option is a functional option for Runner.
type Option func(*Runner)

// WithProjectRoot sets the project root directory used for writing reconciler
// files to directories outside the API types output directory.
func WithProjectRoot(root string) Option {
	return func(r *Runner) {
		r.projectRoot = root
	}
}
