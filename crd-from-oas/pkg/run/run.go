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
	// Accumulate ops create metadata across all group-versions so the
	// cross-group dispatcher can be emitted once at the end.
	var allOpsCreateInfos []*generator.OpsCreateFileInfo
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

		// Determine whether to generate groupversion_info.go (default: true).
		generateGVI := true
		if agvConfig.GenerateGroupVersionInfo != nil {
			generateGVI = *agvConfig.GenerateGroupVersionInfo
		}

		// Generate CRD types
		gen := generator.NewGenerator(generator.Config{
			APIGroup:                 apiGroup,
			APIVersion:               apiVersion,
			GenerateStatus:           true,
			GenerateGroupVersionInfo: generateGVI,
			FieldConfig:              agvConfig.FieldConfig(pathToEntityName),
			OpsConfig:                agvConfig.OpsConfig(pathToEntityName),
			CommonTypes:              agvConfig.CommonTypes,
			SecretRefEntities:        agvConfig.SecretRefEntities(pathToEntityName),
			ReconcilerConfig:         agvConfig.ReconcilerConfigs(pathToEntityName),
			APIGroupPackagePath:      apiGroupPackagePath,
			APIGroupPackageAlias:     apiGroupPackageAlias,
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

		if err := cleanupLegacyGeneratedFiles(r.projectRoot, dir, parsed); err != nil {
			return fmt.Errorf("failed to remove obsolete generated files in %q: %w", dir, err)
		}

		// Remove legacy register.go to avoid symbol conflicts with
		// groupversion_info.go (either generated or hand-written).
		if err := removeFileIfExists(filepath.Join(dir, "register.go")); err != nil {
			return fmt.Errorf("failed to remove legacy register.go in %q: %w", dir, err)
		}

		// Collect ops infos for post-loop dispatcher emission; drop infos
		// whose corresponding generated file was skipped due to a
		// hand-written ops_<entity>.go collision.
		opsInfosKept := make([]*generator.OpsCreateFileInfo, 0, len(gen.OpsCreateInfos()))
		skippedOpsFiles := make(map[string]bool)
		opsDir := filepath.Join(r.projectRoot, "controller/konnect/ops")
		for _, info := range gen.OpsCreateInfos() {
			// Remove any stale generated ops file first so regeneration is idempotent.
			staleOpsFile := filepath.Join(opsDir, generatedOpsFileName(info.Entity))
			if err := removeFileIfExists(staleOpsFile); err != nil {
				return fmt.Errorf("failed to remove stale ops file %q: %w", staleOpsFile, err)
			}
			// TODO: For now we don't do anything for types that already have hand-written ops.
			// This would require adding logic for non root objects (like KonnectEventDataPlaneCertificate).
			// We want to implement this eventually but not yet.
			collided := false
			for _, candidate := range handWrittenOpsFileNames(info.Entity) {
				handWritten := filepath.Join(opsDir, candidate)
				if _, err := os.Stat(handWritten); err == nil {
					logger.Info("skipping ops create generation; hand-written file present", "entity", info.Entity, "file", handWritten)
					skippedOpsFiles[generatedOpsFileName(info.Entity)] = true
					collided = true
					break
				}
			}
			if collided {
				continue
			}
			opsInfosKept = append(opsInfosKept, info)
		}
		allOpsCreateInfos = append(allOpsCreateInfos, opsInfosKept...)

		// Write generated files
		for _, file := range files {
			if file.RelativeDir == "controller/konnect/ops" && skippedOpsFiles[file.Name] {
				continue
			}
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

	// Emit cross-group ops dispatcher. If no entities were generated, remove
	// any stale dispatcher to keep the tree clean.
	dispatcherPath := filepath.Join(r.projectRoot, "controller/konnect/ops", "zz_generated_ops.go")
	dispatcher, err := generator.GenerateOpsDispatcher(allOpsCreateInfos)
	if err != nil {
		return fmt.Errorf("failed to generate ops dispatcher: %w", err)
	}
	if dispatcher == nil {
		if err := removeFileIfExists(dispatcherPath); err != nil {
			return fmt.Errorf("failed to remove stale ops dispatcher %q: %w", dispatcherPath, err)
		}
	} else {
		targetDir := filepath.Join(r.projectRoot, dispatcher.RelativeDir)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("failed to create output directory %q: %w", targetDir, err)
		}
		filePath := filepath.Join(targetDir, dispatcher.Name)
		if err := os.WriteFile(filePath, []byte(dispatcher.Content), 0o600); err != nil {
			return fmt.Errorf("failed to write file %q: %w", filePath, err)
		}
		logger.Info("generated file", "path", filePath)
	}

	return nil
}

// handWrittenOpsFileNames returns candidate hand-written ops file names for an
// entity. It returns both the current prefix form (ops_<EntityFilePrefix>.go)
// and the legacy unconverted-lowercase form (ops_<lower>.go) so collisions with
// either naming convention are detected.
func handWrittenOpsFileNames(entity string) []string {
	prefix := generator.EntityFilePrefix(entity)
	legacy := strings.ToLower(entity)
	if prefix == legacy {
		return []string{"ops_" + prefix + ".go"}
	}
	return []string{"ops_" + prefix + ".go", "ops_" + legacy + ".go"}
}

// generatedOpsFileName returns the generated ops file name for an entity,
// e.g. "Portal" → "zz_generated_portal_ops.go".
func generatedOpsFileName(entity string) string {
	return "zz_generated_" + generator.EntityFilePrefix(entity) + "_ops.go"
}

func cleanupLegacyGeneratedFiles(projectRoot, dir string, parsed *parser.ParsedSpec) error {
	for entityName := range parsed.RequestBodies {
		entity := parser.GetEntityNameFromType(entityName)

		legacyFilePath := filepath.Join(dir, legacyGeneratedFuncsFileName(entity))
		if err := removeFileIfExists(legacyFilePath); err != nil {
			return err
		}

		// Remove files that used the old naming convention without underscore
		// after "konnect" (e.g. konnecteventcontrolplane_types.go).
		// Only needed when the old prefix differs from the new one.
		oldPrefix := strings.ToLower(entity)
		newPrefix := oldPrefix
		if after, ok := strings.CutPrefix(oldPrefix, "konnect"); ok && after != "" {
			newPrefix = "konnect_" + after
		}
		if oldPrefix != newPrefix {
			for _, suffix := range []string{
				"_types.go",
				"_sdkops.go",
				"_sdkops_test.go",
			} {
				if err := removeFileIfExists(filepath.Join(dir, oldPrefix+suffix)); err != nil {
					return err
				}
			}
			if err := removeFileIfExists(filepath.Join(dir, "zz_generated_"+oldPrefix+"_funcs.go")); err != nil {
				return err
			}
			// Clean up old-named reconciler wiring files.
			if err := removeFileIfExists(filepath.Join(projectRoot, "controller/konnect", "zz_generated_watch_"+oldPrefix+".go")); err != nil {
				return err
			}
			for _, suffix := range []string{".go", "_test.go"} {
				if err := removeFileIfExists(filepath.Join(projectRoot, "internal/utils/index", "zz_generated_"+oldPrefix+suffix)); err != nil {
					return err
				}
			}
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
