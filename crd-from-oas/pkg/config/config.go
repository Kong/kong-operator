package config

import (
	"fmt"
	"os"
	"sort"
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

// EntityGVKConfig identifies a generated API type by Kind and optional API group.
// When Group is omitted, the current API group-version being generated is used.
type EntityGVKConfig struct {
	Kind  string `yaml:"kind"`
	Group string `yaml:"group,omitempty"`
}

// ReferenceConfig declares a spec field that holds references to other CRs
// in this API group, where the Konnect API accepts the referenced entity's
// Konnect ID (or, in the future, Konnect name). This is deliberately NOT a
// general type-override facility: the generator derives the ref struct, CRD
// validation, SDK request resolution, and watch/index wiring from it.
type ReferenceConfig struct {
	// Path is the dot-separated field path on the referring CR that holds the
	// references (e.g. "spec.apiSpec.policies" or the nested
	// "spec.apiSpec.access.acls.allow.allow"). Must start with "spec.apiSpec." and
	// resolve to an array field, possibly through nested objects and oneOf/anyOf
	// union variants.
	Path string `yaml:"path"`
	// Kinds lists the CRD kinds (in this API group) the field may reference.
	// With a single kind, the generated ref struct's kind field is optional and
	// defaulted; with multiple kinds it is required with an enum of Kinds.
	Kinds []string `yaml:"kinds"`
	// ResolvesTo selects what is pushed to Konnect for each reference:
	// "id" (the referenced CR's Konnect ID) or "name" (its Konnect name).
	ResolvesTo string `yaml:"resolvesTo"`
	// RefTypeName names the generated ref struct. Required when Kinds has more
	// than one entry (the name cannot be derived); must be empty otherwise
	// (single-kind names derive as "<Kind>Ref").
	RefTypeName string `yaml:"refTypeName,omitempty"`
}

// TypeName returns the Go type name of the generated ref struct.
func (rc ReferenceConfig) TypeName() string {
	if rc.RefTypeName != "" {
		return rc.RefTypeName
	}
	if len(rc.Kinds) == 1 {
		return rc.Kinds[0] + "Ref"
	}
	return ""
}

// AssociationConfig declares a spec-level field holding references to other CRs
// in this API group that the entity is associated with. Unlike ReferenceConfig
// (which replaces an OpenAPI-derived field inside spec.apiSpec and injects the
// resolved value into the SDK create/update body), an association is a new
// top-level spec field (sibling of apiSpec) whose membership is enforced in
// Konnect by a dedicated, hand-written ops helper.
//
// The generator emits the spec field, the referenced ref struct(s), and a call
// site to the helper named by convention (enforce<Entity><GoFieldName>). The
// enforcement SDK call itself lives in the hand-written helper.
type AssociationConfig struct {
	// Name is the lowerCamelCase JSON name of the top-level spec field, e.g.
	// "consumerGroups". The Go field name is derived from it.
	Name string `yaml:"name"`
	// Kinds lists the CRD kinds (in this API group) the field may reference.
	// A single kind emits a name-only ref struct (no kind field on the CRD);
	// multiple kinds are not yet supported.
	Kinds []string `yaml:"kinds"`
	// SDKMethod is the SDK method the hand-written enforcement helper calls to
	// push the membership set to Konnect (e.g.
	// "UpdateAiGatewayConsumerGroupsForConsumer"). The generator does not emit
	// the call itself — the helper (enforce<Entity><GoFieldName>) does — but it
	// needs the name to wire the generated ops-controller test's mock expectation.
	SDKMethod string `yaml:"sdkMethod"`
}

// RefTypeName returns the Go type name of the generated ref struct for a
// single-kind association (e.g. "AIGatewayConsumerGroupRef").
func (a AssociationConfig) RefTypeName() string {
	return a.Kinds[0] + "Ref"
}

// SourceConfig declares whether a generated root CRD supports being a Mirror of
// an existing Konnect entity in addition to the default Origin behavior. When
// SupportsMirror is true the generator emits Source/Mirror spec fields, spec-level
// CEL, an optional-pointer APISpec, GetSource/GetMirror accessors, and a Mirror
// short-circuit in the generated create/update/delete ops.
type SourceConfig struct {
	// SupportsMirror enables Origin+Mirror. When false (or when the source block
	// is omitted entirely) the entity stays Origin-only.
	SupportsMirror bool `yaml:"supportsMirror,omitempty"`
}

// SecretReferenceConfig configures a single sensitive field that can be provided
// inline or sourced from a Kubernetes Secret.
//
// The Go type backing the inline value is inferred from the OAS field at Path,
// not configured here:
//   - When the field resolves to a string (OAS booleans included, since this
//     generator represents them as Go string), it becomes the shared
//     SensitiveDataSource type (Value *string) — unchanged regardless of this change.
//   - Otherwise (e.g. an arbitrary JSON object, an integer, a typed map), the
//     generator emits a dedicated per-field type instead (named from the
//     entity and the field's Go selector, e.g. "AIGatewayPolicyConfigDataSource")
//     whose Value field matches the OAS field's type. Resolving such a field's
//     secretRef at runtime requires a hand-written valueFromSecretRef method on
//     the generated dedicated type, added to secretref_manual.go in the target
//     API package — the generator only emits the call site, not the method.
//   - A leaf that is itself a oneOf union or an object with nested properties
//     (inline or via $ref) is rejected at generation time: there's no single
//     unambiguous value to wrap in inline-vs-secretRef.
type SecretReferenceConfig struct {
	// Path is the dot-separated field path within the spec
	// (e.g. "spec.apiSpec.tls.clientIdentity.certificate").
	// Must start with "spec.apiSpec.".
	// When the resolved OAS leaf is itself an array of strings (e.g.
	// "clientSecret: []string"), the generator detects this from the schema and
	// emits a list of secret sources ([]SensitiveDataSource, one per element)
	// instead of a single one — no separate array notation is needed in Path.
	Path string `yaml:"path"`
	// Type is the Kubernetes resource type that holds the sensitive data.
	// Currently only "Secret" is supported.
	Type string `yaml:"type"`
}

// TypeConfig holds configuration for a single CRD type (identified by its OpenAPI path).
type TypeConfig struct {
	// Path is the OpenAPI path that identifies the resource (e.g. "/services").
	Path string `yaml:"path"`
	// Name overrides the generated CRD type name. When set, the entity name
	// derived from the OpenAPI path will be replaced with this value.
	// All related types (Spec, Status, List) will use this name as their base.
	Name string `yaml:"name,omitempty"`
	// SchemaFieldOmissions maps generated schema type names to JSON field names
	// that should be omitted when emitting schema_types.go for this API.
	SchemaFieldOmissions map[string][]string `yaml:"schemaFieldOmissions,omitempty"`
	// CEL maps field names to their configurations, allowing additional
	// kubebuilder validation markers to be attached to specific fields.
	CEL map[string]*FieldConfig `yaml:"cel,omitempty"`
	// References lists inter-CR reference fields on this entity's spec.
	// Each entry replaces the OpenAPI-derived field with a *commonv1alpha1.ObjectRef
	// and emits a corresponding resolved-ID field on the status.
	References []ReferenceConfig `yaml:"-"`
	// Associations lists top-level spec fields (siblings of apiSpec) holding
	// references to other CRs whose membership is enforced in Konnect by a
	// hand-written ops helper. See AssociationConfig.
	Associations []AssociationConfig `yaml:"associations,omitempty"`
	// Ops maps operation names (e.g. "create", "update") to SDK type configurations.
	// When set, conversion methods are generated on the entity's APISpec type.
	Ops map[string]*OpConfig `yaml:"-"`
	// OpsRequireClient indicates that generated ops for this entity require a
	// controller-runtime client to read cluster state while building SDK requests.
	OpsRequireClient bool `yaml:"-"`
	// OpsSkipGetForUID skips generation of the getForUID function for this entity.
	OpsSkipGetForUID bool `yaml:"-"`
	// OpsSkipRootUnionMetadataFields opts an entity out of labels/tags/name
	// detection inside root-union (discriminated-union) request-body variants.
	OpsSkipRootUnionMetadataFields bool `yaml:"-"`
	// OpsUseUIDTagFilter enables generated getForUID code to pass the object's
	// Kubernetes UID tag as a list query filter when the API supports it.
	OpsUseUIDTagFilter bool `yaml:"-"`
	// OpsGetForUID holds declarative matching rules for generated getForUID logic
	// when labels, UID tags, or spec.name are insufficient.
	OpsGetForUID *GetForUIDConfig `yaml:"-"`
	// OpsSDK holds SDK interface and field name for SDK factory generation.
	OpsSDK *OpSDKConfig `yaml:"-"`
	// OpsListCallStylePositional indicates that the SDK list method for this
	// entity uses positional (pageSize *int64, pageNumber *int64) arguments
	// instead of a request struct. The generator emits nil, nil for both.
	OpsListCallStylePositional bool `yaml:"-"`
	// OpsResponseStatusFields configures fields to copy from the SDK create
	// response into the CRD status struct.
	OpsResponseStatusFields []ResponseStatusFieldConfig `yaml:"-"`
	// SecretReferences lists sensitive field paths whose values can be provided
	// either inline or sourced from a Kubernetes Secret. Each entry causes the
	// OAS-derived field at Path to become a union struct supporting inline and
	// secretRef variants at runtime — the shared SensitiveDataSource type for
	// string fields, or a dedicated generated type for non-string fields (see
	// SecretReferenceConfig's doc comment).
	SecretReferences []SecretReferenceConfig `yaml:"secretReferences,omitempty"`
	// Reconciler holds configuration for reconciler code generation.
	// When set, reconciler wiring files (interface methods, watch options,
	// index files) are generated for this entity.
	Reconciler *ReconcilerConfig `yaml:"reconciler,omitempty"`
	// Source declares Origin/Mirror support for this (root) entity. Nil => Origin-only.
	Source *SourceConfig `yaml:"-"`
	// OneOfVariantNamesFromTitle lists generated union wrapper type names (e.g.
	// "AIGatewayModelRouteConfigModel") for which anonymous oneOf variants (no
	// $ref) are named after their OAS `title` instead of the generic
	// Variant1/Variant2/... fallback. A titled variant whose schema has
	// exactly one nested property is additionally flattened onto the wrapper
	// field directly, avoiding a doubled JSON key like
	// route.model.pathAliases.pathAliases. Declared under whichever entity's
	// config block is convenient — the names are matched against the
	// generated union type regardless of which entity or shared schema it
	// belongs to. Opt-in only: unlisted unions keep the existing generic
	// naming and shape.
	OneOfVariantNamesFromTitle []string `yaml:"oneOfVariantNamesFromTitle,omitempty"`
}

// ParentRefConfig overrides the spec field for the parent reference when the
// OpenAPI-derived parent dependency should be replaced with a higher-level
// parent entity. When set, the OpenAPI-derived Spec.<GatewayRef> field is
// suppressed and a new top-level Spec.<FieldName> ObjectRef field is emitted.
type ParentRefConfig struct {
	// FieldName is the lowerCamelCase JSON name for the top-level spec ref
	// field, e.g. "eventGatewayBackendClusterRef".
	FieldName string `yaml:"fieldName"`
	// ReplacesAPISpecField is the JSON name of the apiSpec property to suppress
	// in favour of the new top-level field, e.g. "destination".
	ReplacesAPISpecField string `yaml:"replacesAPISpecField"`
}

// ReconcilerConfig holds configuration for reconciler code generation.
type ReconcilerConfig struct {
	// IsRoot indicates this is a root entity that directly references
	// KonnectAPIAuthConfiguration. Child entities inherit auth from their parent.
	// When nil (not set in YAML), it is inferred from the OpenAPI path: true when
	// no path parameters are present (e.g. /v1/gateways), false otherwise
	// (e.g. /v1/gateways/{gatewayId}/listeners).
	IsRoot *bool `yaml:"isRoot,omitempty"`
	// ParentEntityGVK overrides the generated parent entity type used for child
	// reconciler watch/index generation and parent GVK helpers. When unset, the
	// immediate parent dependency name and current API group are inferred from the
	// OpenAPI path parameter.
	ParentEntityGVK *EntityGVKConfig `yaml:"parentEntityGVK,omitempty"`
	// ParentEntityType is the deprecated kind-only predecessor of
	// ParentEntityGVK. Prefer ParentEntityGVK for new config.
	ParentEntityType string `yaml:"parentEntityType,omitempty"`
	// ParentRef, when set, replaces the OpenAPI-derived parent spec field with a
	// new top-level ObjectRef field and changes what GetParentRef / SetParentID
	// operate on. Requires a parent entity kind override when the immediate parent
	// kind should differ from the OpenAPI-derived dependency name.
	ParentRef *ParentRefConfig `yaml:"parentRef,omitempty"`
	// AncestorEntityGVKs lists the ancestor entity kinds and optional API groups
	// for each OpenAPI-derived path-parameter dependency in URL order
	// (outermost first). Only the Kind values are used by current ancestor-ID
	// helpers, which remain keyed by kind for compatibility.
	AncestorEntityGVKs []EntityGVKConfig `yaml:"ancestorEntityGVKs,omitempty"`
	// AncestorEntityTypes is the deprecated kind-only predecessor of
	// AncestorEntityGVKs. Prefer AncestorEntityGVKs for new config.
	//
	// E.g. for a path /event-gateways/{gatewayId}/virtual-clusters with
	// parentRef overriding the immediate parent to EventGatewayBackendCluster,
	// set to ["KonnectEventGateway"] so the gateway ancestry is preserved.
	AncestorEntityTypes []string `yaml:"ancestorEntityTypes,omitempty"`
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

// ParentEntityKind returns the configured parent Kind override, if any.
func (rc *ReconcilerConfig) ParentEntityKind() string {
	if rc == nil {
		return ""
	}
	if rc.ParentEntityGVK != nil && rc.ParentEntityGVK.Kind != "" {
		return rc.ParentEntityGVK.Kind
	}
	return rc.ParentEntityType
}

// ParentEntityGroup returns the configured parent API group override, or the
// current API group when no override is provided.
func (rc *ReconcilerConfig) ParentEntityGroup(currentGroup string) string {
	if rc == nil || rc.ParentEntityGVK == nil || rc.ParentEntityGVK.Group == "" {
		return currentGroup
	}
	return rc.ParentEntityGVK.Group
}

// AncestorEntityKinds returns the configured ancestor Kind values in URL order.
func (rc *ReconcilerConfig) AncestorEntityKinds() []string {
	if rc == nil {
		return nil
	}
	if len(rc.AncestorEntityGVKs) > 0 {
		kinds := make([]string, 0, len(rc.AncestorEntityGVKs))
		for _, gvk := range rc.AncestorEntityGVKs {
			if gvk.Kind == "" {
				continue
			}
			kinds = append(kinds, gvk.Kind)
		}
		if len(kinds) > 0 {
			return kinds
		}
	}
	return rc.AncestorEntityTypes
}

// OpConfig holds configuration for a single SDK operation.
type OpConfig struct {
	// Path is the fully qualified Go type path in the form "importpath.TypeName",
	// e.g. "github.com/Kong/sdk-konnect-go/models/components.CreatePortal".
	// Required for create/update ops.
	Path string `yaml:"path,omitempty"`
	// AsPUT makes generated delete ops call the configured update/PUT SDK method
	// with an empty request body instead of requiring an OpenAPI DELETE operation.
	AsPUT bool `yaml:"asPUT,omitempty"`
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
	// ListItemsSource controls how generated getForUID code iterates the SDK list
	// response payload. The default ("data") expects resp.<field>.Data; "slice"
	// expects resp.<field> itself to be the item slice.
	ListItemsSource GetForUIDListItemsSource `yaml:"listItemsSource,omitempty"`
	// MatchFields lists object/response field pairs that must all match for a
	// list response item to be considered the same entity.
	MatchFields []GetForUIDMatchField `yaml:"matchFields,omitempty"`
	// RootUnion configures variant-aware matching for entities whose identity is
	// derived from a root-union-backed spec field.
	RootUnion *GetForUIDRootUnionConfig `yaml:"rootUnion,omitempty"`
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

// GetForUIDListItemsSource controls how list response items are extracted in
// generated getForUID code.
type GetForUIDListItemsSource string

const (
	// GetForUIDListItemsSourceData expects resp.<field>.Data to hold the items.
	GetForUIDListItemsSourceData GetForUIDListItemsSource = "data"
	// GetForUIDListItemsSourceSlice expects resp.<field> itself to be the items slice.
	GetForUIDListItemsSourceSlice GetForUIDListItemsSource = "slice"
)

// GetForUIDRootUnionConfig configures variant-aware matching for a root union.
type GetForUIDRootUnionConfig struct {
	// UnionField is the Go field path, relative to obj, that references the root
	// union container (for example "Spec.APISpec.EventGatewayListenerPolicyConfig").
	UnionField string `yaml:"unionField"`
	// ResponseTypeField is the Go field or getter path, relative to the list
	// entry, that returns the SDK discriminator value. Defaults to "GetType()".
	ResponseTypeField string `yaml:"responseTypeField,omitempty"`
	// Cases enumerates the supported CRD union variants.
	Cases []GetForUIDRootUnionCase `yaml:"cases,omitempty"`
}

// GetForUIDRootUnionCase configures matching for a single root-union variant.
type GetForUIDRootUnionCase struct {
	// TypeValue is the CRD union discriminator value (for example "tlsServer").
	TypeValue string `yaml:"typeValue"`
	// VariantField is the Go field on the union container that holds the selected
	// variant payload (for example "EventGatewayTLSListen").
	VariantField string `yaml:"variantField"`
	// ResponseTypeValue is the SDK discriminator value expected on the list entry
	// (for example "tls_server").
	ResponseTypeValue string `yaml:"responseTypeValue"`
	// MatchFields lists the fields, relative to the selected variant payload and
	// the list entry respectively, that must match.
	MatchFields []GetForUIDMatchField `yaml:"matchFields,omitempty"`
}

// ResponseStatusFieldConfig configures one field to copy from the SDK create
// response into the CRD status struct. It also causes a new Go struct type
// to be emitted for the field's type.
type ResponseStatusFieldConfig struct {
	// StatusField is the Go field name on the status struct, e.g. "Endpoints".
	// It also becomes the suffix of the generated struct type name: {EntityName}{StatusField}.
	StatusField string `yaml:"statusField"`
	// StatusJSON is the json tag for the status field, e.g. "endpoints".
	StatusJSON string `yaml:"statusJSON"`
	// Fields lists the scalar sub-fields of the new status struct.
	Fields []ResponseStatusSubField `yaml:"fields"`
}

// ResponseStatusSubField is one scalar field in a response-derived status struct.
type ResponseStatusSubField struct {
	// Name is the Go field name, e.g. "Configuration".
	Name string `yaml:"name"`
	// JSON is the json tag, e.g. "configuration".
	JSON string `yaml:"json"`
	// RespPath is the Go path on the response entity (e.g. "Endpoints.Configuration")
	// relative to resp.{RespField} (where RespField comes from schema.SuccessResponseRef).
	RespPath string `yaml:"respPath"`
}

type typeOpsYAML struct {
	// RequireClient indicates that generated ops for this entity need a
	// controller-runtime client to fetch cluster data such as Secrets.
	RequireClient bool `yaml:"requireClient,omitempty"`
	// SkipGetForUID skips generation of the getForUID function for this entity.
	// Use when the SDK list endpoint does not support UID-label filtering, or
	// when the hand-written implementation already exists.
	SkipGetForUID bool `yaml:"skipGetForUID,omitempty"`
	// SkipRootUnionMetadataFields opts an entity out of labels/tags/name
	// detection inside root-union (discriminated-union) request-body variants.
	// Defaults to false (detection enabled). Set true to preserve pre-existing
	// generated output for entities not yet reviewed for this behavior.
	SkipRootUnionMetadataFields bool `yaml:"skipRootUnionMetadataFields,omitempty"`
	// UseUIDTagFilter enables generated getForUID code to pass the object's
	// Kubernetes UID tag as a list query filter when the API supports it.
	UseUIDTagFilter bool `yaml:"useUIDTagFilter,omitempty"`
	// ListCallStylePositional indicates that the SDK list method uses positional
	// (pageSize *int64, pageNumber *int64) args instead of a request struct.
	ListCallStylePositional bool `yaml:"listCallStylePositional,omitempty"`
	// GetForUID holds custom field-matching configuration for generated
	// getForUID logic.
	GetForUID *GetForUIDConfig `yaml:"getForUID,omitempty"`
	// SDK holds the SDK interface and field name for SDK factory generation.
	SDK *OpSDKConfig `yaml:"sdk,omitempty"`
	// ResponseStatusFields configures fields to copy from the SDK create response
	// into the CRD status struct.
	ResponseStatusFields []ResponseStatusFieldConfig `yaml:"responseStatusFields,omitempty"`
	// Operations maps operation names (e.g. "create", "update") to SDK type configs.
	Operations map[string]*OpConfig `yaml:",inline"`
}

type typeConfigYAML struct {
	Path                 string                  `yaml:"path"`
	Name                 string                  `yaml:"name,omitempty"`
	SchemaFieldOmissions map[string][]string     `yaml:"schemaFieldOmissions,omitempty"`
	CEL                  map[string]*FieldConfig `yaml:"cel,omitempty"`
	References           []ReferenceConfig       `yaml:"references,omitempty"`
	Associations         []AssociationConfig     `yaml:"associations,omitempty"`
	SecretReferences     []SecretReferenceConfig `yaml:"secretReferences,omitempty"`
	Ops                  *typeOpsYAML            `yaml:"ops,omitempty"`
	Reconciler           *ReconcilerConfig       `yaml:"reconciler,omitempty"`
	Source               *SourceConfig           `yaml:"source,omitempty"`
	// OneOfVariantNamesFromTitle lists generated union wrapper type names opted
	// into title-based anonymous variant naming. See TypeConfig's doc comment.
	OneOfVariantNamesFromTitle []string `yaml:"oneOfVariantNamesFromTitle,omitempty"`
}

// UnmarshalYAML preserves the in-memory Ops map shape while allowing
// additional per-ops settings, such as requireClient, under the same YAML key.
func (tc *TypeConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw typeConfigYAML
	if err := value.Decode(&raw); err != nil {
		return err
	}

	*tc = TypeConfig{
		Path:                       raw.Path,
		Name:                       raw.Name,
		SchemaFieldOmissions:       raw.SchemaFieldOmissions,
		CEL:                        raw.CEL,
		References:                 raw.References,
		Associations:               raw.Associations,
		SecretReferences:           raw.SecretReferences,
		Reconciler:                 raw.Reconciler,
		Source:                     raw.Source,
		OneOfVariantNamesFromTitle: raw.OneOfVariantNamesFromTitle,
	}

	if raw.Ops != nil {
		tc.Ops = raw.Ops.Operations
		tc.OpsRequireClient = raw.Ops.RequireClient
		tc.OpsSkipGetForUID = raw.Ops.SkipGetForUID
		tc.OpsSkipRootUnionMetadataFields = raw.Ops.SkipRootUnionMetadataFields
		tc.OpsUseUIDTagFilter = raw.Ops.UseUIDTagFilter
		tc.OpsListCallStylePositional = raw.Ops.ListCallStylePositional
		tc.OpsGetForUID = raw.Ops.GetForUID
		tc.OpsSDK = raw.Ops.SDK
		tc.OpsResponseStatusFields = raw.Ops.ResponseStatusFields
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
	// SkipRootUnionMetadataFields opts an entity out of labels/tags/name
	// detection inside root-union (discriminated-union) request-body variants.
	SkipRootUnionMetadataFields bool
	// UseUIDTagFilter enables generated getForUID code to pass the object's
	// Kubernetes UID tag as a list query filter when the API supports it.
	UseUIDTagFilter bool
	// ListCallStylePositional indicates that the SDK list method uses positional
	// (pageSize *int64, pageNumber *int64) args instead of a request struct.
	ListCallStylePositional bool
	// GetForUID holds custom field-matching configuration for generated
	// getForUID logic.
	GetForUID *GetForUIDConfig
	// SDK holds SDK interface and field name for SDK factory generation.
	SDK *OpSDKConfig
	// ResponseStatusFields configures fields to copy from the SDK create response
	// into the CRD status struct.
	ResponseStatusFields []ResponseStatusFieldConfig
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

// SecretRefEntities returns the set of entity names that have at least one
// SecretReference configured, using the provided pathToEntityName mapping.
func (c *APIGroupVersionConfig) SecretRefEntities(pathToEntityName map[string]string) map[string]bool {
	result := make(map[string]bool)
	for _, tc := range c.Types {
		if len(tc.SecretReferences) == 0 {
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

// SecretReferencesConfig builds a mapping from entity name to secret reference configs using the
// provided pathToEntityName mapping (built after parsing the OpenAPI spec).
func (c *APIGroupVersionConfig) SecretReferencesConfig(pathToEntityName map[string]string) map[string][]SecretReferenceConfig {
	result := make(map[string][]SecretReferenceConfig)
	for _, tc := range c.Types {
		if len(tc.SecretReferences) == 0 {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		result[entityName] = tc.SecretReferences
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

// SourceConfigs builds a mapping from entity name to source config using the
// provided pathToEntityName mapping (built after parsing the OpenAPI spec).
func (c *APIGroupVersionConfig) SourceConfigs(pathToEntityName map[string]string) map[string]*SourceConfig {
	result := make(map[string]*SourceConfig)
	for _, tc := range c.Types {
		if tc.Source == nil {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		result[entityName] = tc.Source
	}
	return result
}

// OneOfVariantNamesFromTitleUnions builds the set of generated union wrapper
// type names opted into naming anonymous oneOf variants after their OAS title,
// aggregated across every entity's configuration.
func (c *APIGroupVersionConfig) OneOfVariantNamesFromTitleUnions() map[string]bool {
	result := make(map[string]bool)
	for _, tc := range c.Types {
		for _, unionTypeName := range tc.OneOfVariantNamesFromTitle {
			if unionTypeName == "" {
				continue
			}
			result[unionTypeName] = true
		}
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
		requireClient := tc.OpsRequireClient || len(tc.SecretReferences) > 0
		result[entityName] = &EntityOpsConfig{
			Ops:                         tc.Ops,
			RequireClient:               requireClient,
			SkipGetForUID:               tc.OpsSkipGetForUID,
			SkipRootUnionMetadataFields: tc.OpsSkipRootUnionMetadataFields,
			UseUIDTagFilter:             tc.OpsUseUIDTagFilter,
			ListCallStylePositional:     tc.OpsListCallStylePositional,
			GetForUID:                   tc.OpsGetForUID,
			SDK:                         tc.OpsSDK,
			ResponseStatusFields:        tc.OpsResponseStatusFields,
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

// AssociationsConfig builds a mapping from entity name to association configs using the
// provided pathToEntityName mapping (built after parsing the OpenAPI spec).
func (c *APIGroupVersionConfig) AssociationsConfig(pathToEntityName map[string]string) map[string][]AssociationConfig {
	result := make(map[string][]AssociationConfig)
	for _, tc := range c.Types {
		if len(tc.Associations) == 0 {
			continue
		}
		entityName, ok := pathToEntityName[tc.Path]
		if !ok {
			continue
		}
		result[entityName] = tc.Associations
	}
	return result
}

// SchemaFieldOmissionsConfig returns a normalized schema-type field omission set.
func (c *APIGroupVersionConfig) SchemaFieldOmissionsConfig() map[string]map[string]bool {
	if c == nil || len(c.Types) == 0 {
		return nil
	}
	result := make(map[string]map[string]bool)
	for _, tc := range c.Types {
		if tc == nil || len(tc.SchemaFieldOmissions) == 0 {
			continue
		}
		for typeName, fields := range tc.SchemaFieldOmissions {
			if typeName == "" || len(fields) == 0 {
				continue
			}
			fieldSet := result[typeName]
			if fieldSet == nil {
				fieldSet = make(map[string]bool, len(fields))
				result[typeName] = fieldSet
			}
			for _, fieldName := range fields {
				if fieldName == "" {
					continue
				}
				fieldSet[fieldName] = true
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (c *APIGroupVersionConfig) validate() error {
	if c.CommonTypes != nil && c.CommonTypes.ObjectRef != nil {
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
	}

	for _, tc := range c.Types {
		if err := tc.validate(); err != nil {
			return fmt.Errorf("type %q: %w", tc.Path, err)
		}
	}

	if err := c.validateReferenceTypeConsistency(); err != nil {
		return err
	}

	return nil
}

// validateReferenceTypeConsistency ensures that every generated ref struct type
// name (see ReferenceConfig.TypeName) is declared consistently across all types
// in the API group-version: two references that share a type name must agree on
// their Kinds (order-insensitive) and ResolvesTo.
func (c *APIGroupVersionConfig) validateReferenceTypeConsistency() error {
	seen := make(map[string]ReferenceConfig)
	for _, tc := range c.Types {
		for _, ref := range tc.References {
			name := ref.TypeName()
			if name == "" {
				continue
			}
			prev, ok := seen[name]
			if !ok {
				seen[name] = ref
				continue
			}
			if !equalKinds(prev.Kinds, ref.Kinds) || prev.ResolvesTo != ref.ResolvesTo {
				return fmt.Errorf("reference type %q declared with conflicting kinds or resolvesTo", name)
			}
		}
	}
	return nil
}

// equalKinds reports whether two kind lists contain the same elements,
// ignoring order.
func equalKinds(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func (tc *TypeConfig) validate() error {
	for _, ref := range tc.References {
		if !strings.HasPrefix(ref.Path, "spec.apiSpec.") {
			return fmt.Errorf("reference %q: path must start with \"spec.apiSpec.\"", ref.Path)
		}
		if len(ref.Kinds) == 0 {
			return fmt.Errorf("reference %q: kinds must not be empty", ref.Path)
		}
		switch ref.ResolvesTo {
		case "id", "name":
		default:
			return fmt.Errorf("reference %q: resolvesTo must be \"id\" or \"name\", got %q", ref.Path, ref.ResolvesTo)
		}
		if len(ref.Kinds) > 1 && ref.RefTypeName == "" {
			return fmt.Errorf("reference %q: refTypeName is required when multiple kinds are configured", ref.Path)
		}
	}
	seenAssocNames := make(map[string]bool)
	for i, a := range tc.Associations {
		if a.Name == "" {
			return fmt.Errorf("associations[%d].name must not be empty", i)
		}
		if len(a.Kinds) == 0 {
			return fmt.Errorf("associations[%d]: kinds must not be empty", i)
		}
		if len(a.Kinds) > 1 {
			return fmt.Errorf("associations[%d]: multi-kind associations are not yet supported", i)
		}
		if a.SDKMethod == "" {
			return fmt.Errorf("associations[%d].sdkMethod must not be empty", i)
		}
		if seenAssocNames[a.Name] {
			return fmt.Errorf("associations[%d]: duplicate name %q", i, a.Name)
		}
		seenAssocNames[a.Name] = true
	}
	seenSecretPaths := make(map[string]bool)
	for i, sr := range tc.SecretReferences {
		if !strings.HasPrefix(sr.Path, "spec.apiSpec.") {
			return fmt.Errorf("secretReferences[%d].path must start with \"spec.apiSpec.\", got %q", i, sr.Path)
		}
		if sr.Type != "Secret" {
			return fmt.Errorf("secretReferences[%d].type %q is not supported; only \"Secret\" is currently allowed", i, sr.Type)
		}
		if seenSecretPaths[sr.Path] {
			return fmt.Errorf("secretReferences[%d]: duplicate path %q", i, sr.Path)
		}
		seenSecretPaths[sr.Path] = true
	}
	if deleteOp, ok := tc.Ops["delete"]; ok && deleteOp != nil && deleteOp.AsPUT {
		updateOp, ok := tc.Ops["update"]
		if !ok || updateOp == nil || updateOp.Path == "" {
			return fmt.Errorf("ops.delete.asPUT requires ops.update.path")
		}
	}
	if tc.OpsSkipGetForUID && tc.OpsGetForUID != nil {
		return fmt.Errorf("ops.skipGetForUID and ops.getForUID are mutually exclusive")
	}
	if tc.OpsGetForUID != nil {
		if tc.OpsGetForUID.ListItemsSource != "" &&
			tc.OpsGetForUID.ListItemsSource != GetForUIDListItemsSourceData &&
			tc.OpsGetForUID.ListItemsSource != GetForUIDListItemsSourceSlice {
			return fmt.Errorf("ops.getForUID.listItemsSource must be one of %q or %q", GetForUIDListItemsSourceData, GetForUIDListItemsSourceSlice)
		}
		if len(tc.OpsGetForUID.MatchFields) > 0 && tc.OpsGetForUID.RootUnion != nil {
			return fmt.Errorf("ops.getForUID.matchFields and ops.getForUID.rootUnion are mutually exclusive")
		}
		if tc.OpsGetForUID.RootUnion != nil {
			if tc.OpsGetForUID.RootUnion.UnionField == "" {
				return fmt.Errorf("ops.getForUID.rootUnion.unionField is required")
			}
			if len(tc.OpsGetForUID.RootUnion.Cases) == 0 {
				return fmt.Errorf("ops.getForUID.rootUnion.cases is required when ops.getForUID.rootUnion is set")
			}
			for i, c := range tc.OpsGetForUID.RootUnion.Cases {
				if c.TypeValue == "" {
					return fmt.Errorf("ops.getForUID.rootUnion.cases[%d].typeValue is required", i)
				}
				if c.VariantField == "" {
					return fmt.Errorf("ops.getForUID.rootUnion.cases[%d].variantField is required", i)
				}
				if c.ResponseTypeValue == "" {
					return fmt.Errorf("ops.getForUID.rootUnion.cases[%d].responseTypeValue is required", i)
				}
				if err := validateGetForUIDMatchFields(
					fmt.Sprintf("ops.getForUID.rootUnion.cases[%d].matchFields", i),
					c.MatchFields,
				); err != nil {
					return err
				}
			}
		} else if err := validateGetForUIDMatchFields("ops.getForUID.matchFields", tc.OpsGetForUID.MatchFields); err != nil {
			return err
		}
	}
	if tc.Source != nil && tc.Source.SupportsMirror && strings.Contains(tc.Path, "{") {
		return fmt.Errorf("source.supportsMirror is only supported on root entities (path %q has path parameters)", tc.Path)
	}
	return nil
}

func validateGetForUIDMatchFields(prefix string, fields []GetForUIDMatchField) error {
	if len(fields) == 0 {
		return fmt.Errorf("%s is required", prefix)
	}
	for i, field := range fields {
		if field.ObjectField == "" {
			return fmt.Errorf("%s[%d].objectField is required", prefix, i)
		}
		if field.ResponseField == "" {
			return fmt.Errorf("%s[%d].responseField is required", prefix, i)
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
