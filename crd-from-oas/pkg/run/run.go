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
	// Accumulate ops metadata across all group-versions so the cross-group
	// dispatchers can be emitted once at the end.
	var allOpsCreateInfos []*generator.OpsCreateFileInfo
	var allOpsUpdateInfos []*generator.OpsUpdateFileInfo
	var allOpsDeleteInfos []*generator.OpsDeleteFileInfo
	var allOpsGetForUIDInfos []*generator.OpsGetForUIDFileInfo
	var allSDKFactoryInfos []*generator.SDKFactoryFileInfo
	var allWatchInfos []*generator.WatchFileInfo
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
		// e.g. group "konnect.konghq.com" → prefix "konnect", alias "xkonnectv1alpha1"
		groupPrefix := strings.Split(apiGroup, ".")[0]
		apiGroupPackagePath := fmt.Sprintf("github.com/kong/kong-operator/v2/api/%s/%s", groupPrefix, apiVersion)
		apiGroupPackageAlias := strings.ReplaceAll(groupPrefix, "-", "") + apiVersion

		// Determine whether to generate groupversion_info.go (default: true).
		generateGVI := true
		if agvConfig.GenerateGroupVersionInfo != nil {
			generateGVI = *agvConfig.GenerateGroupVersionInfo
		}

		// Detect which entities have a fully hand-written ops file or a
		// getForUID helper in a _manual.go file.
		opsDir := filepath.Join(r.projectRoot, "controller/konnect/ops")
		skipGetForUIDEntities := make(map[string]bool)
		manualGetForUIDEntities := make(map[string]bool)
		for _, entityName := range pathToEntityName {
			for _, candidate := range handWrittenGetForUIDFileNames(entityName) {
				if _, err := os.Stat(filepath.Join(opsDir, candidate)); err == nil {
					skipGetForUIDEntities[entityName] = true
					break
				}
			}
			for _, candidate := range handWrittenGetForUIDHelperFileNames(entityName) {
				if _, err := os.Stat(filepath.Join(opsDir, candidate)); err == nil {
					manualGetForUIDEntities[entityName] = true
					break
				}
			}
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
			SecretReferences:         agvConfig.SecretReferencesConfig(pathToEntityName),
			ReconcilerConfig:         agvConfig.ReconcilerConfigs(pathToEntityName),
			APIGroupPackagePath:      apiGroupPackagePath,
			APIGroupPackageAlias:     apiGroupPackageAlias,
			SkipGetForUIDEntities:    skipGetForUIDEntities,
			ManualGetForUIDEntities:  manualGetForUIDEntities,
			Categories:               agvConfig.Categories,
			References:               agvConfig.ReferencesConfig(pathToEntityName),
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
		opsCreateInfosKept := make([]*generator.OpsCreateFileInfo, 0, len(gen.OpsCreateInfos()))
		opsUpdateInfosKept := make([]*generator.OpsUpdateFileInfo, 0, len(gen.OpsUpdateInfos()))
		opsDeleteInfosKept := make([]*generator.OpsDeleteFileInfo, 0, len(gen.OpsDeleteInfos()))
		opsGetForUIDInfosKept := make([]*generator.OpsGetForUIDFileInfo, 0, len(gen.OpsGetForUIDInfos()))
		skippedOpsFiles := make(map[string]bool)

		// Build a set of entities with hand-written collision to skip create, update, and delete.
		collidedEntities := make(map[string]bool)
		for _, info := range gen.OpsCreateInfos() {
			for _, candidate := range handWrittenOpsFileNames(info.Entity) {
				if _, err := os.Stat(filepath.Join(opsDir, candidate)); err == nil {
					logger.Info("skipping ops generation; hand-written file present", "entity", info.Entity, "file", candidate)
					collidedEntities[info.Entity] = true
					skippedOpsFiles[generatedOpsFileName(info.Entity)] = true
					break
				}
			}
		}

		for _, info := range gen.OpsCreateInfos() {
			// Remove any stale generated ops file first so regeneration is idempotent.
			staleOpsFile := filepath.Join(opsDir, generatedOpsFileName(info.Entity))
			if err := removeFileIfExists(staleOpsFile); err != nil {
				return fmt.Errorf("failed to remove stale ops file %q: %w", staleOpsFile, err)
			}
			if collidedEntities[info.Entity] {
				continue
			}
			opsCreateInfosKept = append(opsCreateInfosKept, info)
		}

		for _, info := range gen.OpsUpdateInfos() {
			if collidedEntities[info.Entity] {
				continue
			}
			opsUpdateInfosKept = append(opsUpdateInfosKept, info)
		}

		for _, info := range gen.OpsDeleteInfos() {
			if collidedEntities[info.Entity] {
				continue
			}
			opsDeleteInfosKept = append(opsDeleteInfosKept, info)
		}

		for _, info := range gen.OpsGetForUIDInfos() {
			if collidedEntities[info.Entity] {
				continue
			}
			opsGetForUIDInfosKept = append(opsGetForUIDInfosKept, info)
		}

		allOpsCreateInfos = append(allOpsCreateInfos, opsCreateInfosKept...)
		allOpsUpdateInfos = append(allOpsUpdateInfos, opsUpdateInfosKept...)
		allOpsDeleteInfos = append(allOpsDeleteInfos, opsDeleteInfosKept...)
		allOpsGetForUIDInfos = append(allOpsGetForUIDInfos, opsGetForUIDInfosKept...)
		allSDKFactoryInfos = append(allSDKFactoryInfos, gen.SDKFactoryInfos()...)
		allWatchInfos = append(allWatchInfos, gen.WatchInfos()...)

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

	// Remove legacy single-dispatcher file if present (replaced by split files).
	legacyDispatcherPath := filepath.Join(r.projectRoot, "controller/konnect/ops", "zz_generated_ops.go")
	if err := removeFileIfExists(legacyDispatcherPath); err != nil {
		return fmt.Errorf("failed to remove legacy ops dispatcher %q: %w", legacyDispatcherPath, err)
	}

	// Emit create dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect/ops",
		"zz_generated_ops_create.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateOpsCreateDispatcher(allOpsCreateInfos)
		},
	); err != nil {
		return err
	}

	// Emit update dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect/ops",
		"zz_generated_ops_update.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateOpsUpdateDispatcher(allOpsUpdateInfos)
		},
	); err != nil {
		return err
	}

	// Emit delete dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect/ops",
		"zz_generated_ops_delete.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateOpsDeleteDispatcher(allOpsDeleteInfos)
		},
	); err != nil {
		return err
	}

	// Emit getForUID dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect/ops",
		"zz_generated_ops_getforuid.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateOpsGetForUIDDispatcher(allOpsGetForUIDInfos)
		},
	); err != nil {
		return err
	}

	// Emit watch dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect",
		"zz_generated_watch.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateWatchDispatcher(allWatchInfos)
		},
	); err != nil {
		return err
	}

	// Emit generated constraints dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect/constraints",
		"zz_generated_supported_types.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateKonnectConstraintsDispatcher(allWatchInfos)
		},
	); err != nil {
		return err
	}

	// Emit generated KonnectAPIAuth watcher registrations.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect",
		"zz_generated_konnectapiauth_watch.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateKonnectAPIAuthWatchDispatcher(allWatchInfos)
		},
	); err != nil {
		return err
	}

	// Emit manager controller setup dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"modules/manager",
		"zz_generated_konnect_controller_setup.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateKonnectControllerSetupDispatcher(allWatchInfos)
		},
	); err != nil {
		return err
	}

	// Emit manager index options dispatcher.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"modules/manager",
		"zz_generated_konnect_index_options.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateKonnectIndexOptionsDispatcher(allWatchInfos)
		},
	); err != nil {
		return err
	}

	// Emit SDK factory file.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"controller/konnect/ops/sdk",
		"zz_generated_sdkfactory.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateSDKFactoryDispatcher(allSDKFactoryInfos)
		},
	); err != nil {
		return err
	}

	// Emit mock SDK factory file.
	if err := emitDispatcherFile(
		r.projectRoot,
		logger,
		"test/mocks/sdkmocks",
		"zz_generated_sdkfactory_mock.go",
		func() (*generator.GeneratedFile, error) {
			return generator.GenerateMockSDKFactoryDispatcher(allSDKFactoryInfos)
		},
	); err != nil {
		return err
	}

	return nil
}

// emitDispatcherFile writes a generated dispatcher file, or removes the stale
// one when the generator returns nil (no entities).
// relativeDir is relative to projectRoot (e.g. "controller/konnect/ops").
// staleFileName is the file name to remove when the generator returns nil.
func emitDispatcherFile(
	projectRoot string,
	logger *slog.Logger,
	relativeDir string,
	staleFileName string,
	generate func() (*generator.GeneratedFile, error),
) error {
	file, err := generate()
	if err != nil {
		return fmt.Errorf("failed to generate dispatcher %s/%s: %w", relativeDir, staleFileName, err)
	}
	targetDir := filepath.Join(projectRoot, relativeDir)
	stale := filepath.Join(targetDir, staleFileName)
	if file == nil {
		if err := removeFileIfExists(stale); err != nil {
			return fmt.Errorf("failed to remove stale dispatcher %q: %w", stale, err)
		}
		return nil
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory %q: %w", targetDir, err)
	}
	filePath := filepath.Join(targetDir, file.Name)
	if err := os.WriteFile(filePath, []byte(file.Content), 0o600); err != nil {
		return fmt.Errorf("failed to write file %q: %w", filePath, err)
	}
	logger.Info("generated file", "path", filePath)
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

// handWrittenGetForUIDFileNames returns candidate hand-written file names for
// entities whose getForUID function is implemented manually (i.e. in a
// fully hand-written ops file, not a _manual.go helper file). This is the
// same set as handWrittenOpsFileNames — entities with a _manual.go helper
// file (which only contains SDK request builders) use the ops config's
// skipGetForUID flag instead.
func handWrittenGetForUIDFileNames(entity string) []string {
	return handWrittenOpsFileNames(entity)
}

// handWrittenGetForUIDHelperFileNames returns candidate hand-written helper file
// names for entities whose getForUID function lives in an ops_*_manual.go file.
func handWrittenGetForUIDHelperFileNames(entity string) []string {
	prefix := generator.EntityFilePrefix(entity)
	legacy := strings.ToLower(entity)
	if prefix == legacy {
		return []string{"ops_" + prefix + "_manual.go"}
	}
	return []string{"ops_" + prefix + "_manual.go", "ops_" + legacy + "_manual.go"}
}

// generatedOpsFileName returns the generated ops file name for an entity,
// e.g. "Portal" → "zz_generated_ops_portal.go".
func generatedOpsFileName(entity string) string {
	return "zz_generated_ops_" + generator.EntityFilePrefix(entity) + ".go"
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
	reconcilerConditionsPath := filepath.Join(dir, "zz_generated_reconciler_conditions.go")
	if err := removeFileIfExists(reconcilerConditionsPath); err != nil {
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
