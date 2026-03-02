package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadProjectConfig loads the project configuration from a YAML file.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.APIGroupVersions == nil {
		return nil, fmt.Errorf("config file must contain apiGroupVersions")
	}

	for gv, agv := range cfg.APIGroupVersions {
		if err := agv.validate(); err != nil {
			return nil, fmt.Errorf("apiGroupVersion %q: %w", gv, err)
		}
	}

	return &cfg, nil
}
