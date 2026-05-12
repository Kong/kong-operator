package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectConfig is the top-level configuration loaded from the config file.
// It groups paths and entity configs by API group-version.
type ProjectConfig struct {
	APIGroupVersions map[string]*APIGroupVersionConfig `yaml:"apiGroupVersions"`
}

// APIGroupVersionConfig holds configuration for a single API group-version.
type APIGroupVersionConfig struct {
	CommonTypes *CommonTypesConfig `yaml:"commonTypes,omitempty"`

	// GenerateGroupVersionInfo controls whether to generate a groupversion_info.go
	// file (standard Kubebuilder layout with SchemeGroupVersion and Resource helper).
	// When true, groupversion_info.go is generated instead of register.go.
	// Defaults to true when not specified.
	GenerateGroupVersionInfo *bool `yaml:"generateGroupVersionInfo,omitempty"`

	// Categories are applied via +kubebuilder:resource:categories= to every root
	// CRD type generated for this API group-version. Non-root types do not receive
	// the marker.
	Categories []string `yaml:"categories,omitempty"`

	Types []*TypeConfig `yaml:"types"`
}

// CommonTypesConfig holds configuration for common types that can be shared
// across multiple entities in the same API group-version.
type CommonTypesConfig struct {
	ObjectRef *ObjectRefConfig `yaml:"objectRef,omitempty"`
}

// ObjectRefConfig holds configuration for the common ObjectRef type, which
// can be generated locally or imported from an existing package.
type ObjectRefConfig struct {
	// Generate indicates whether to generate a common ObjectRef type for
	// referencing Kubernetes objects.
	// If false, the generator assumes that the designated API group version
	// already defines an ObjectRef type.
	// Defaults to true when not specified.
	Generate *bool `yaml:"generate,omitempty"`

	// Namespaced indicates whether the generated ObjectRef type should include a Namespace field.
	// Defaults to false when not specified.
	// Can only be set when Generate is true, and causes errors if set when Generate is false.
	Namespaced bool `yaml:"namespaced,omitempty"`

	// ImportPath is the Go import path where the ObjectRef type is defined.
	// Can't be set if Generate is true, and is required if Generate is false.
	Import *ImportConfig `yaml:"import,omitempty"`
}

type ImportConfig struct {
	// Path is the Go import path (e.g. "github.com/kong/kong-operator/v2/api/common/v1alpha1").
	Path string `yaml:"path"`
	// Alias is an optional alias to use for the imported package (e.g. "commonv1alpha1").
	Alias string `yaml:"alias,omitempty"`
}

// ReferenceConfig configures a single inter-CR reference field.
type ReferenceConfig struct {
	// Kind is the Go type name of the referenced CRD (e.g. "EventGatewayBackendCluster").
	// Used to name the resolved-ID status field: status.<lowerCamelCase(Kind)>.id.
	Kind string `yaml:"kind"`
	// Path is the dot-separated field path on the referring CR that holds the
	// reference (e.g. "spec.apiSpec.destination"). Must start with "spec.apiSpec.".
	Path string `yaml:"path"`
}

// TypeConfig holds configuration for a single CRD type (identified by its OpenAPI path).
type TypeConfig struct {
	// Path is the OpenAPI path that identifies the resource (e.g. "/services").
	Path string `yaml:"path"`
	// Name overrides the generated CRD type name. When set, the entity name
	// derived from the OpenAPI path will be replaced with this value.
	// All related types (Spec, Status, List) will use this name as their base.
	Name string `yaml:"name,omitempty"`
	// CEL maps field names to their configurations, allowing additional
	// kubebuilder validation markers to be attached to specific fields.
	CEL map[string]*FieldConfig `yaml:"cel,omitempty"`
	// References lists inter-CR reference fields on this entity's spec.
	// Each entry replaces the OpenAPI-derived field with a *commonv1alpha1.ObjectRef
	// and emits a corresponding resolved-ID field on the status.
	References []ReferenceConfig `yaml:"-"`
	// Ops maps operation names (e.g. "create", "update") to SDK type configurations.
	// When set, conversion methods are generated on the entity's APISpec type.
	Ops map[string]*OpConfig `yaml:"-"`
	// OpsRequireClient indicates that generated ops for this entity require a
	// controller-runtime client to read cluster state while building SDK requests.
	OpsRequireClient bool `yaml:"-"`
	// OpsSkipGetForUID skips generation of the getForUID function for this entity.
	OpsSkipGetForUID bool `yaml:"-"`
	// OpsUseUIDTagFilter enables generated getForUID code to pass the object's
	// Kubernetes UID tag as a list query filter when the API supports it.
	OpsUseUIDTagFilter bool `yaml:"-"`
	// OpsGetForUID holds declarative matching rules for generated getForUID logic
	// when labels, UID tags, or spec.name are insufficient.
	OpsGetForUID *GetForUIDConfig `yaml:"-"`
	// OpsSDK holds SDK interface and field name for SDK factory generation.
	OpsSDK *OpSDKConfig `yaml:"-"`
	// OptionalSecretReference enables generation of a union type field on the
	// Spec that allows the user to provide sensitive data either inline in the
	// APISpec or via a Kubernetes Secret reference. When true the generated Spec
	// will include a SourceType discriminator (inline / secretRef), and a
	// SecretRef field of type NamespacedRef.
	OptionalSecretReference bool `yaml:"optionalSecretReference,omitempty"`
	// Reconciler holds configuration for reconciler code generation.
	// When set, reconciler wiring files (interface methods, watch options,
	// index files) are generated for this entity.
	Reconciler *ReconcilerConfig `yaml:"reconciler,omitempty"`
}

// ReconcilerConfig holds configuration for reconciler code generation.
type ReconcilerConfig struct {
	// IsRoot indicates this is a root entity that directly references
	// KonnectAPIAuthConfiguration. Child entities inherit auth from their parent.
	// When nil (not set in YAML), it is inferred from the OpenAPI path: true when
	// no path parameters are present (e.g. /v1/gateways), false otherwise
	// (e.g. /v1/gateways/{gatewayId}/listeners).
	IsRoot *bool `yaml:"isRoot,omitempty"`
	// ParentEntityType overrides the generated parent entity type name used for
	// child reconciler watch/index generation. When unset, the immediate parent
	// dependency name is inferred from the OpenAPI path parameter.
	ParentEntityType string `yaml:"parentEntityType,omitempty"`
	// ParentSDKFields optionally overrides the SDK request-struct field names for
	// each parent dependency (in URL order). When the list is shorter than the
	// number of dependencies, missing entries fall back to pathParamToFieldName.
	// Only needed when the Speakeasy SDK uses a shortened field name that differs
	// from the one derived from the raw path-parameter name (e.g. "ListenerID"
	// for the path param "eventGatewayListenerId").
	ParentSDKFields []string `yaml:"parentSDKFields,omitempty"`
}

// GetIsRoot returns the resolved value of IsRoot, treating nil (not explicitly
// set and inference not yet applied) as false.
func (rc *ReconcilerConfig) GetIsRoot() bool {
	return rc.IsRoot != nil && *rc.IsRoot
}

// OpConfig holds configuration for a single SDK operation.
type OpConfig struct {
	// Path is the fully qualified Go type path in the form "importpath.TypeName",
	// e.g. "github.com/Kong/sdk-konnect-go/models/components.CreatePortal".
	Path string `yaml:"path"`
}

// OpSDKConfig identifies the SDK interface and factory field name used by
// the generated SDK factory wiring for an entity.
type OpSDKConfig struct {
	// Interface is the fully qualified SDK interface path,
	// e.g. "github.com/Kong/sdk-konnect-go.EventGatewaysSDK".
	Interface string `yaml:"interface"`
	// FieldName is the field name on *sdkkonnectgo.SDK backing the interface,
	// e.g. "EventGateways".
	FieldName string `yaml:"fieldName"`
}

// GetForUIDConfig configures generated getForUID lookup logic for an entity.
type GetForUIDConfig struct {
	// MatchFields lists object/response field pairs that must all match for a
	// list response item to be considered the same entity.
	MatchFields []GetForUIDMatchField `yaml:"matchFields,omitempty"`
}

// GetForUIDMatchField configures a single equality check in generated
// getForUID logic.
type GetForUIDMatchField struct {
	// ObjectField is the Go field path relative to obj, e.g.
	// "Spec.APISpec.Certificate".
	ObjectField string `yaml:"objectField"`
	// ResponseField is the Go field path relative to the list entry, e.g.
	// "Certificate" or "GetName()".
	ResponseField string `yaml:"responseField"`
}

type typeOpsYAML struct {
	// RequireClient indicates that generated ops for this entity need a
	// controller-runtime client to fetch cluster data such as Secrets.
	RequireClient bool `yaml:"requireClient,omitempty"`
	// SkipGetForUID skips generation of the getForUID function for this entity.
	// Use when the SDK list endpoint does not support UID-label filtering, or
	// when the hand-written implementation already exists.
	SkipGetForUID bool `yaml:"skipGetForUID,omitempty"`
	// UseUIDTagFilter enables generated getForUID code to pass the object's
	// Kubernetes UID tag as a list query filter when the API supports it.
	UseUIDTagFilter bool `yaml:"useUIDTagFilter,omitempty"`
	// GetForUID holds custom field-matching configuration for generated
	// getForUID logic.
	GetForUID *GetForUIDConfig `yaml:"getForUID,omitempty"`
	// SDK holds the SDK interface and field name for SDK factory generation.
	SDK *OpSDKConfig `yaml:"sdk,omitempty"`
	// Operations maps operation names (e.g. "create", "update") to SDK type configs.
	Operations map[string]*OpConfig `yaml:",inline"`
}

type typeConfigYAML struct {
	Path                    string                  `yaml:"path"`
	Name                    string                  `yaml:"name,omitempty"`
	CEL                     map[string]*FieldConfig `yaml:"cel,omitempty"`
	References              []ReferenceConfig       `yaml:"references,omitempty"`
	Ops                     *typeOpsYAML            `yaml:"ops,omitempty"`
	OptionalSecretReference bool                    `yaml:"optionalSecretReference,omitempty"`
	Reconciler              *ReconcilerConfig       `yaml:"reconciler,omitempty"`
}

// UnmarshalYAML preserves the in-memory Ops map shape while allowing
// additional per-ops settings, such as requireClient, under the same YAML key.
func (tc *TypeConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw typeConfigYAML
	if err := value.Decode(&raw); err != nil {
		return err
	}

	*tc = TypeConfig{
		Path:                    raw.Path,
		Name:                    raw.Name,
		CEL:                     raw.CEL,
		References:              raw.References,
		OptionalSecretReference: raw.OptionalSecretReference,
		Reconciler:              raw.Reconciler,
	}

	if raw.Ops != nil {
		tc.Ops = raw.Ops.Operations
		tc.OpsRequireClient = raw.Ops.RequireClient
		tc.OpsSkipGetForUID = raw.Ops.SkipGetForUID
		tc.OpsUseUIDTagFilter = raw.Ops.UseUIDTagFilter
		tc.OpsGetForUID = raw.Ops.GetForUID
		tc.OpsSDK = raw.Ops.SDK
	}

	return nil
}

// EntityOpsConfig holds the operations configuration for a single entity type.
type EntityOpsConfig struct {
	// Ops maps operation names (e.g. "create", "update") to their SDK type configs.
	Ops map[string]*OpConfig
	// RequireClient indicates that generated ops need a controller-runtime client
	// to build their SDK requests.
	RequireClient bool
	// SkipGetForUID skips generation of the getForUID function for this entity.
	SkipGetForUID bool
	// UseUIDTagFilter enables generated getForUID code to pass the object's
	// Kubernetes UID tag as a list query filter when the API supports it.
	UseUIDTagFilter bool
	// GetForUID holds custom field-matching configuration for generated
	// getForUID logic.
	GetForUID *GetForUIDConfig
	// SDK holds SDK interface and field name for SDK factory generation.
	SDK *OpSDKConfig
}

// NameOverrides returns a mapping from OpenAPI path to the custom CRD type name
// for types that have a Name override configured.
func (c *APIGroupVersionConfig) NameOverrides() map[string]string {
	overrides := make(map[string]string)
	for _, tc := range c.Types {
		if tc.Name != "" {
			overrides[tc.Path] = tc.Name
		}
	}
	return overrides
}

// GetPaths returns the list of OpenAPI paths from the types configuration.
func (c *APIGroupVersionConfig) GetPaths() []string {
	paths := make([]string, 0, len(c.Types))
	for _, tc := range c.Types {
		paths = append(paths, tc.Path)
	}
	return paths
}

// FieldConfig builds a *Config suitable for the generator's FieldConfig parameter.
// It maps CEL validations from per-path config to per-entity config using the provided
// pathToEntityName mapping (built after parsing the OpenAPI spec).
func (c *APIGroupVersionConfig) FieldConfig(pathToEntityName map[string]string) *Config {
	entities := make(map[string]*EntityConfig)
	for _, tc := range c.Types {
		if tc.CEL == nil {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		entities[entityName] = &EntityConfig{Fields: tc.CEL}
	}
	return &Config{Entities: entities}
}

// SecretRefEntities returns the set of entity names that have
// OptionalSecretReference enabled, using the provided pathToEntityName mapping.
func (c *APIGroupVersionConfig) SecretRefEntities(pathToEntityName map[string]string) map[string]bool {
	result := make(map[string]bool)
	for _, tc := range c.Types {
		if !tc.OptionalSecretReference {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		result[entityName] = true
	}
	return result
}

// ReconcilerConfigs builds a mapping from entity name to reconciler config using the provided
// pathToEntityName mapping (built after parsing the OpenAPI spec).
func (c *APIGroupVersionConfig) ReconcilerConfigs(pathToEntityName map[string]string) map[string]*ReconcilerConfig {
	result := make(map[string]*ReconcilerConfig)
	for _, tc := range c.Types {
		if tc.Reconciler == nil {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		result[entityName] = tc.Reconciler
	}
	return result
}

// OpsConfig builds a mapping from entity name to operations config using the provided
// pathToEntityName mapping (built after parsing the OpenAPI spec).
func (c *APIGroupVersionConfig) OpsConfig(pathToEntityName map[string]string) map[string]*EntityOpsConfig {
	result := make(map[string]*EntityOpsConfig)
	for _, tc := range c.Types {
		if tc.Ops == nil {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		requireClient := tc.OpsRequireClient || tc.OptionalSecretReference
		result[entityName] = &EntityOpsConfig{
			Ops:             tc.Ops,
			RequireClient:   requireClient,
			SkipGetForUID:   tc.OpsSkipGetForUID,
			UseUIDTagFilter: tc.OpsUseUIDTagFilter,
			GetForUID:       tc.OpsGetForUID,
			SDK:             tc.OpsSDK,
		}
	}
	return result
}

// ReferencesConfig builds a mapping from entity name to reference configs using the provided
// pathToEntityName mapping (built after parsing the OpenAPI spec).
func (c *APIGroupVersionConfig) ReferencesConfig(pathToEntityName map[string]string) map[string][]ReferenceConfig {
	result := make(map[string][]ReferenceConfig)
	for _, tc := range c.Types {
		if len(tc.References) == 0 {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		result[entityName] = tc.References
	}
	return result
}

func (c *APIGroupVersionConfig) validate() error {
	if c.CommonTypes == nil || c.CommonTypes.ObjectRef == nil {
		for _, tc := range c.Types {
			if err := tc.validate(); err != nil {
				return fmt.Errorf("type %q: %w", tc.Path, err)
			}
		}
		return nil
	}
	ref := c.CommonTypes.ObjectRef

	// Default Generate to true when ObjectRef is present but Generate is not explicitly set.
	if ref.Generate == nil && ref.Import == nil {
		ref.Generate = new(true)
	}

	// Only flag mutual exclusion when generate is explicitly set to true.
	if ref.Generate != nil && *ref.Generate && ref.Import != nil {
		return fmt.Errorf("commonTypes.objectRef: generate and import are mutually exclusive")
	}
	// Require import when generate is explicitly set to false.
	if ref.Generate != nil && !*ref.Generate && ref.Import == nil {
		return fmt.Errorf("commonTypes.objectRef: import is required when generate is false")
	}
	if ref.Import != nil && ref.Import.Path == "" {
		return fmt.Errorf("commonTypes.objectRef.import: path is required")
	}
	for _, tc := range c.Types {
		if err := tc.validate(); err != nil {
			return fmt.Errorf("type %q: %w", tc.Path, err)
		}
	}
	return nil
}

func (tc *TypeConfig) validate() error {
	seenRefKinds := make(map[string]bool)
	for i, ref := range tc.References {
		if ref.Kind == "" {
			return fmt.Errorf("references[%d].kind is required", i)
		}
		if !strings.HasPrefix(ref.Path, "spec.apiSpec.") {
			return fmt.Errorf("references[%d].path must start with \"spec.apiSpec.\", got %q", i, ref.Path)
		}
		if seenRefKinds[ref.Kind] {
			return fmt.Errorf("references[%d]: duplicate kind %q (each kind must appear at most once per type)", i, ref.Kind)
		}
		seenRefKinds[ref.Kind] = true
	}
	if tc.OpsSkipGetForUID && tc.OpsGetForUID != nil {
		return fmt.Errorf("ops.skipGetForUID and ops.getForUID are mutually exclusive")
	}
	if tc.OpsGetForUID != nil {
		if len(tc.OpsGetForUID.MatchFields) == 0 {
			return fmt.Errorf("ops.getForUID.matchFields is required when ops.getForUID is set")
		}
		for i, field := range tc.OpsGetForUID.MatchFields {
			if field.ObjectField == "" {
				return fmt.Errorf("ops.getForUID.matchFields[%d].objectField is required", i)
			}
			if field.ResponseField == "" {
				return fmt.Errorf("ops.getForUID.matchFields[%d].responseField is required", i)
			}
		}
	}
	return nil
}

// ParseAPIGroupVersion splits a "group/version" string into its group and version parts.
func ParseAPIGroupVersion(gv string) (group, version string, err error) {
	parts := strings.SplitN(gv, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid apiGroupVersion %q: must be in format 'group/version'", gv)
	}
	return parts[0], parts[1], nil
}

// FieldConfig holds configuration for a single field, optionally with nested sub-field configs.
// Sub-fields are decoded from the same YAML map via the inline tag so that deeply nested
// paths like `cel.tls.client_identity.certificate._validations` work transparently.
type FieldConfig struct {
	// Validations are additional kubebuilder markers to add to the field.
	Validations []string `yaml:"_validations,omitempty"`
	// Fields maps child field names to their own FieldConfig, allowing
	// multi-segment CEL paths that traverse referenced schema types.
	Fields map[string]*FieldConfig `yaml:",inline"`
}

// Sub returns the child FieldConfig for the given name, or nil if none exists.
func (fc *FieldConfig) Sub(name string) *FieldConfig {
	if fc == nil {
		return nil
	}
	return fc.Fields[name]
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

// GetFieldConfig returns the FieldConfig reached by walking the given path
// (one or more field name segments) starting from the entity's top-level fields.
// Returns nil if any segment is missing.
func (c *Config) GetFieldConfig(entityName string, path ...string) *FieldConfig {
	if c == nil || c.Entities == nil || len(path) == 0 {
		return nil
	}
	entityCfg, ok := c.Entities[entityName]
	if !ok || entityCfg == nil || entityCfg.Fields == nil {
		return nil
	}
	fc, ok := entityCfg.Fields[path[0]]
	if !ok || fc == nil {
		return nil
	}
	for _, segment := range path[1:] {
		fc = fc.Sub(segment)
		if fc == nil {
			return nil
		}
	}
	return fc
}

// GetFieldValidations returns the additional validations for a field.
func (c *Config) GetFieldValidations(entityName, fieldName string) []string {
	fc := c.GetFieldConfig(entityName, fieldName)
	if fc == nil {
		return nil
	}
	return fc.Validations
}

