package run

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/generator"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// Run is responsible for orchestrating the entire generation process:
// parsing the OpenAPI spec, applying configurations, generating code, and writing files.
func (r *Runner) Run(
	ctx context.Context,
	logger *slog.Logger,
) error {
	for _, gvKey := range r.gvKeys {
		agvConfig := r.projectCfg.APIGroupVersions[gvKey]

		apiGroup, apiVersion, err := config.ParseAPIGroupVersion(gvKey)
		if err != nil {
			return fmt.Errorf("invalid APIGroupVersion key %q: %w", gvKey, err)
		}

		paths := agvConfig.GetPaths()
		logger.Info("processing group-version", "apiGroupVersion", gvKey, "paths", paths)

		// Parse the spec for this group-version's paths
		p := parser.NewParser(r.openAPI)
		parsed, err := p.ParsePaths(paths)
		if err != nil {
			return fmt.Errorf("failed to parse OpenAPI paths for apiGroupVersion %q: %w", gvKey, err)
		}

		logger.Info("found paths to process", "apiGroupVersion", gvKey, "count", len(parsed.RequestBodies))

		// Apply name overrides: rename request body entries so the generator
		// uses the custom CRD type name instead of the one derived from the
		// OpenAPI schema.
		nameOverrides := agvConfig.NameOverrides()
		for name, schema := range parsed.RequestBodies {
			override, ok := nameOverrides[schema.SourcePath]
			if !ok {
				continue
			}
			delete(parsed.RequestBodies, name)
			// Prefix with "Create" so GetEntityNameFromType strips it to
			// yield the override name.
			newKey := "Create" + override
			schema.Name = newKey
			parsed.RequestBodies[newKey] = schema
			logger.Info("applied name override", "path", schema.SourcePath, "from", parser.GetEntityNameFromType(name), "to", override)
		}

		// Build path → entity name mapping for field config resolution
		pathToEntityName := make(map[string]string)
		for name, schema := range parsed.RequestBodies {
			entityName := parser.GetEntityNameFromType(name)
			pathToEntityName[schema.SourcePath] = entityName
			if len(schema.Dependencies) > 0 {
				deps := make([]string, 0, len(schema.Dependencies))
				for _, dep := range schema.Dependencies {
					deps = append(deps, dep.EntityName)
				}
				logger.Info("processing schema", "name", name, "depends_on", strings.Join(deps, ", "))
			} else {
				logger.Info("processing schema", "name", name)
			}
		}

		if len(parsed.RequestBodies) == 0 {
			logger.Warn("no matching request bodies found", "apiGroupVersion", gvKey)
			continue
		}

		// Generate CRD types
		gen := generator.NewGenerator(generator.Config{
			APIGroup:       apiGroup,
			APIVersion:     apiVersion,
			GenerateStatus: true,
			FieldConfig:    agvConfig.FieldConfig(pathToEntityName),
			OpsConfig:      agvConfig.OpsConfig(pathToEntityName),
			CommonTypes:    agvConfig.CommonTypes,
		})

		files, err := gen.Generate(parsed)
		if err != nil {
			return fmt.Errorf("failed to generate types for apiGroupVersion %q: %w", gvKey, err)
		}

		// Create output directory
		dir := filepath.Join(r.outputDir, strings.Split(apiGroup, ".")[0], apiVersion)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create output directory %q: %w", dir, err)
		}

		// Write generated files
		for _, file := range files {
			filePath := filepath.Join(dir, file.Name)
			if err := os.WriteFile(filePath, []byte(file.Content), 0o600); err != nil {
				return fmt.Errorf("failed to write file %q: %w", filePath, err)
			}
			logger.Info("generated file", "path", filePath)
		}
	}

	return nil
}
