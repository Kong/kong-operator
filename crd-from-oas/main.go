package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/caarlos0/env/v11"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/run"
)

type EnvConfig struct {
	InputFile  string `env:"INPUT_FILE" envDefault:"openapi.yaml"`
	OutputDir  string `env:"OUTPUT_DIR" envDefault:"api/"`
	ConfigFile string `env:"CONFIG_FILE,required"`
}

func main() {
	logger := GetLogger()

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

	runner, err := run.New(projectCfg, cfg.InputFile, cfg.OutputDir)
	if err != nil {
		logger.Error("failed to initialize runner", "error", err)
		os.Exit(1)
	}

	if err = runner.Run(context.Background(), logger); err != nil {
		logger.Error("failed to run generator", "error", err)
		os.Exit(1)
	}

	logger.Info("done")
}

func LogOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
}

func GetLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, LogOptions()))
}
