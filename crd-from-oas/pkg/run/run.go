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

		// Build API package path and alias for reconciler generation.
		// e.g. group "x-konnect.konghq.com" → prefix "x-konnect", alias "xkonnectv1alpha1"
		groupPrefix := strings.Split(apiGroup, ".")[0]
		apiGroupPackagePath := fmt.Sprintf("github.com/kong/kong-operator/v2/api/%s/%s", groupPrefix, apiVersion)
		apiGroupPackageAlias := strings.ReplaceAll(groupPrefix, "-", "") + apiVersion

		// Generate CRD types
		gen := generator.NewGenerator(generator.Config{
			APIGroup:             apiGroup,
			APIVersion:           apiVersion,
			GenerateStatus:       true,
			FieldConfig:          agvConfig.FieldConfig(pathToEntityName),
			OpsConfig:            agvConfig.OpsConfig(pathToEntityName),
			CommonTypes:          agvConfig.CommonTypes,
			SecretRefEntities:    agvConfig.SecretRefEntities(pathToEntityName),
			ReconcilerConfig:     agvConfig.ReconcilerConfigs(pathToEntityName),
			APIGroupPackagePath:  apiGroupPackagePath,
			APIGroupPackageAlias: apiGroupPackageAlias,
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

		if err := cleanupLegacyGeneratedFiles(dir, parsed); err != nil {
			return fmt.Errorf("failed to remove obsolete generated files in %q: %w", dir, err)
		}

		// Write generated files
		for _, file := range files {
			var filePath string
			if file.RelativeDir != "" {
				// Write to a directory relative to the project root (outputDir's parent).
				targetDir := filepath.Join(r.projectRoot, file.RelativeDir)
				if err := os.MkdirAll(targetDir, 0o755); err != nil {
					return fmt.Errorf("failed to create output directory %q: %w", targetDir, err)
				}
				filePath = filepath.Join(targetDir, file.Name)
			} else {
				filePath = filepath.Join(dir, file.Name)
			}
			if err := os.WriteFile(filePath, []byte(file.Content), 0o600); err != nil {
				return fmt.Errorf("failed to write file %q: %w", filePath, err)
			}
			logger.Info("generated file", "path", filePath)
		}
	}

	return nil
}

func cleanupLegacyGeneratedFiles(dir string, parsed *parser.ParsedSpec) error {
	for entityName := range parsed.RequestBodies {
		legacyFilePath := filepath.Join(dir, legacyGeneratedFuncsFileName(parser.GetEntityNameFromType(entityName)))
		if err := removeFileIfExists(legacyFilePath); err != nil {
			return err
		}
	}

	sharedReconcilerFuncsPath := filepath.Join(dir, "zz_generated_reconciler_funcs.go")
	if err := removeFileIfExists(sharedReconcilerFuncsPath); err != nil {
		return err
	}

	return nil
}

func legacyGeneratedFuncsFileName(entityName string) string {
	return strings.ToLower(entityName) + "_funcs.go"
}

func removeFileIfExists(filePath string) error {
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
