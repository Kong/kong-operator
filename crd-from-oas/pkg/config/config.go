package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// FieldConfig holds configuration for a single field
type FieldConfig struct {
	// Validations are additional kubebuilder markers to add to the field
	Validations []string `yaml:"_validations,omitempty"`
}

// EntityConfig holds configuration for a single entity (CRD type)
type EntityConfig struct {
	// Fields maps field names to their configurations
	Fields map[string]*FieldConfig `yaml:",inline"`
}

// Config holds the complete configuration for CRD generation
type Config struct {
	// Entities maps entity names to their configurations
	Entities map[string]*EntityConfig `yaml:",inline"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return &Config{Entities: make(map[string]*EntityConfig)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg.Entities); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.Entities == nil {
		cfg.Entities = make(map[string]*EntityConfig)
	}

	return &cfg, nil
}

// GetFieldValidations returns the additional validations for a field
func (c *Config) GetFieldValidations(entityName, fieldName string) []string {
	if c == nil || c.Entities == nil {
		return nil
	}

	entityCfg, ok := c.Entities[entityName]
	if !ok || entityCfg == nil || entityCfg.Fields == nil {
		return nil
	}

	fieldCfg, ok := entityCfg.Fields[fieldName]
	if !ok || fieldCfg == nil {
		return nil
	}

	return fieldCfg.Validations
}

// ValidateAgainstSchema validates that all fields in the config exist in the schema
func (c *Config) ValidateAgainstSchema(entityName string, validFields map[string]bool) error {
	if c == nil || c.Entities == nil {
		return nil
	}

	entityCfg, ok := c.Entities[entityName]
	if !ok || entityCfg == nil || entityCfg.Fields == nil {
		return nil
	}

	for fieldName := range entityCfg.Fields {
		if !validFields[fieldName] {
			return fmt.Errorf("field %q in config does not exist in entity %q", fieldName, entityName)
		}
	}

	return nil
}
