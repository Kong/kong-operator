package main

import (
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/getkin/kin-openapi/openapi3"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/generator"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

type EnvConfig struct {
	InputFile  string `env:"INPUT_FILE" envDefault:"openapi.yaml"`
	OutputDir  string `env:"OUTPUT_DIR" envDefault:"api/"`
	ConfigFile string `env:"CONFIG_FILE,required"`
}

func LogOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, LogOptions()))

	var cfg EnvConfig
	if err := env.Parse(&cfg); err != nil {
		logger.Error("failed to parse environment variables", "error", err)
		os.Exit(1)
	}

	// Load project config
	projectCfg, err := config.LoadProjectConfig(cfg.ConfigFile)
	if err != nil {
		logger.Error("failed to load config file", "error", err)
		os.Exit(1)
	}

	// Load OpenAPI spec (shared across all group-versions)
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(cfg.InputFile)
	if err != nil {
		logger.Error("failed to load OpenAPI spec", "error", err)
		os.Exit(1)
	}

	gvKeys := slices.Collect(maps.Keys(projectCfg.APIGroupVersions))
	sort.Strings(gvKeys)

	for _, gvKey := range gvKeys {
		agvConfig := projectCfg.APIGroupVersions[gvKey]

		apiGroup, apiVersion, err := config.ParseAPIGroupVersion(gvKey)
		if err != nil {
			logger.Error("invalid apiGroupVersion key", "key", gvKey, "error", err)
			os.Exit(1)
		}

		paths := agvConfig.GetPaths()
		logger.Info("processing group-version", "apiGroupVersion", gvKey, "paths", paths)

		// Parse the spec for this group-version's paths
		p := parser.NewParser(doc)
		parsed, err := p.ParsePaths(paths)
		if err != nil {
			logger.Error("failed to parse OpenAPI paths", "apiGroupVersion", gvKey, "error", err)
			os.Exit(1)
		}

		logger.Info("found paths to process", "apiGroupVersion", gvKey, "count", len(parsed.RequestBodies))

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
		})

		files, err := gen.Generate(parsed)
		if err != nil {
			logger.Error("failed to generate types", "apiGroupVersion", gvKey, "error", err)
			os.Exit(1)
		}

		// Create output directory
		dir := filepath.Join(cfg.OutputDir, strings.Split(apiGroup, ".")[0], apiVersion)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Error("failed to create output directory", "error", err)
			os.Exit(1)
		}

		// Write generated files
		for _, file := range files {
			filePath := filepath.Join(dir, file.Name)
			if err := os.WriteFile(filePath, []byte(file.Content), 0o600); err != nil {
				logger.Error("failed to write file", "path", filePath, "error", err)
				os.Exit(1)
			}
			logger.Info("generated file", "path", filePath)
		}
	}

	logger.Info("done")
}
