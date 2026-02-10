package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/kong/kong-operator/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/crd-from-oas/pkg/generator"
	"github.com/kong/kong-operator/crd-from-oas/pkg/parser"
	"github.com/samber/lo"
)

type EnvConfig struct {
	InputFile  string   `env:"INPUT_FILE" envDefault:"openapi.yaml"`
	OutputDir  string   `env:"OUTPUT_DIR" envDefault:"api/"`
	APIGroup   string   `env:"API_GROUP,required"`
	APIVersion string   `env:"API_VERSION,required"`
	Paths      []string `env:"PATHS,required" envSeparator:","`
	ConfigFile string   `env:"CONFIG_FILE"`
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

	// Load field config if provided
	fieldConfig, err := config.LoadConfig(cfg.ConfigFile)
	if err != nil {
		logger.Error("failed to load config file", "error", err)
		os.Exit(1)
	}

	// Load OpenAPI spec
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(cfg.InputFile)
	if err != nil {
		logger.Error("failed to load OpenAPI spec", "error", err)
		os.Exit(1)
	}

	// Parse the spec
	p := parser.NewParser(doc)
	parsed, err := p.ParsePaths(cfg.Paths)
	if err != nil {
		logger.Error("failed to parse OpenAPI paths", "error", err)
		os.Exit(1)
	}

	logger.Info("found paths to process", "count", len(parsed.RequestBodies))
	for name, schema := range parsed.RequestBodies {
		if len(schema.Dependencies) > 0 {
			deps := strings.Join(
				lo.Map(schema.Dependencies, func(dep *parser.Dependency, _ int) string {
					return dep.EntityName
				}),
				", ",
			)
			logger.Info("processing schema", "name", name, "depends_on", deps)
		} else {
			logger.Info("processing schema", "name", name)
		}
	}

	if len(parsed.RequestBodies) == 0 {
		logger.Error("no matching request bodies found in the OpenAPI spec")
		os.Exit(1)
	}

	// Generate CRD types
	gen := generator.NewGenerator(generator.Config{
		APIGroup:       cfg.APIGroup,
		APIVersion:     cfg.APIVersion,
		GenerateStatus: true,
		FieldConfig:    fieldConfig,
	})

	files, err := gen.Generate(parsed)
	if err != nil {
		logger.Error("failed to generate types", "error", err)
		os.Exit(1)
	}

	// Create output directory
	dir := filepath.Join(cfg.OutputDir, strings.Split(cfg.APIGroup, ".")[0], cfg.APIVersion)
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

	logger.Info("done")
}
