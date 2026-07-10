package config

import (
	"fmt"
	"os"
	"strings"

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
		if _, _, err := ParseAPIGroupVersion(gv); err != nil {
			return nil, err
		}
		inferIsRootForGroup(agv)
		if err := agv.validate(); err != nil {
			return nil, fmt.Errorf("apiGroupVersion %q: %w", gv, err)
		}
	}

	return &cfg, nil
}

// inferIsRootForGroup sets IsRoot on every ReconcilerConfig that did not
// explicitly set it in YAML, based on whether the path has URL parameters.
func inferIsRootForGroup(agv *APIGroupVersionConfig) {
	for _, tc := range agv.Types {
		if tc.Reconciler == nil || tc.Reconciler.IsRoot != nil {
			continue
		}
		v := !strings.Contains(tc.Path, "{")
		tc.Reconciler.IsRoot = &v
	}
}
