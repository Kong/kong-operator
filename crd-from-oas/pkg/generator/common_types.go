package generator

const objectRefTypeEnum = `// ObjectRefType is the enum type for the ObjectRef.
//
// +kubebuilder:validation:Enum=namespacedRef
type ObjectRefType string

const (
	// ObjectRefTypeNamespacedRef is the type for the namespaced ref.
	// It is used to reference an entity inside the cluster
	// using a namespaced reference.
	ObjectRefTypeNamespacedRef ObjectRefType = "namespacedRef"
)`

const objectRefType = `// ObjectRef is the schema for the ObjectRef type.
// It is used to reference an entity inside the cluster
// by its namespaced name.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? has(self.namespacedRef) : true", message="when type is namespacedRef, namespacedRef must be set"
// +kong:channels=kong-operator
type ObjectRef struct {
	// Type defines type of the object which is referenced. It can be one of:
	//
	// - namespacedRef
	//
	// +required
	Type ObjectRefType ` + "`json:\"type,omitzero\"`" + `

	// NamespacedRef is a reference to an entity inside the cluster.
	// This field is required when the Type is namespacedRef.
	//
	// +optional
	NamespacedRef *NamespacedRef ` + "`json:\"namespacedRef,omitempty\"`" + `
}`

// namespacedRefType uses Go template syntax for the conditional Namespace field.
// It is parsed as part of commonTypesTemplate.
const namespacedRefType = `// NamespacedRef is a reference to a namespaced resource.
type NamespacedRef struct {
	// Name is the name of the referred resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string ` + "`json:\"name,omitzero\"`" + `
{{- if .Namespaced}}

	// Namespace is the namespace of the referred resource.
	//
	// For namespace-scoped resources if no Namespace is provided then the
	// namespace of the parent object MUST be used.
	//
	// This field MUST not be set when referring to cluster-scoped resources.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Namespace *string ` + "`json:\"namespace,omitempty\"`" + `
{{- end}}
}`

const secretKeyRefType = `// SecretKeyRef is a reference to a key in a Secret
type SecretKeyRef struct {
	// Name is the name of the Secret
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string ` + "`json:\"name,omitzero\"`" + `

	// Key is the key within the Secret
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Key string ` + "`json:\"key,omitzero\"`" + `

	// Namespace is the namespace of the Secret
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string ` + "`json:\"namespace,omitzero\"`" + `
}`

const configMapKeyRefType = `// ConfigMapKeyRef is a reference to a key in a ConfigMap
type ConfigMapKeyRef struct {
	// Name is the name of the ConfigMap
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string ` + "`json:\"name,omitzero\"`" + `

	// Key is the key within the ConfigMap
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Key string ` + "`json:\"key,omitzero\"`" + `

	// Namespace is the namespace of the ConfigMap
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string ` + "`json:\"namespace,omitzero\"`" + `
}`

const konnectEntityStatusType = `// KonnectEntityStatus represents the status of a Konnect entity.
type KonnectEntityStatus = {{ .KonnectStatusType }}`

const sensitiveDataSourceType = `// SensitiveDataSourceType is the type of source for the sensitive data.
type SensitiveDataSourceType string

const (
	// SensitiveDataSourceTypeInline indicates that the data is provided inline in the APISpec.
	SensitiveDataSourceTypeInline SensitiveDataSourceType = "inline"
	// SensitiveDataSourceTypeSecretRef indicates that the data is sourced from a Kubernetes Secret.
	SensitiveDataSourceTypeSecretRef SensitiveDataSourceType = "secretRef"
)`

const sensitiveDataSourceStructType = `// SensitiveDataSource holds a sensitive string value that can be provided
// either inline or sourced from a Kubernetes Secret.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'inline' ? has(self.value) : has(self.secretRef)",message="value required when type=inline; secretRef required when type=secretRef"
type SensitiveDataSource struct {
	// Type indicates the source of the sensitive data: 'inline' or 'secretRef'.
	//
	// +kubebuilder:validation:Enum=inline;secretRef
	// +kubebuilder:default=inline{{range .SensitiveDataSourceTypeValidations}}
	// {{ . }}{{end}}
	Type SensitiveDataSourceType ` + "`" + `json:"type"` + "`" + `

	// Value contains the sensitive data provided inline.
	// Required when type is 'inline'.
	//
	// +optional{{range .SensitiveDataSourceValueValidations}}
	// {{ . }}{{end}}
	Value *string ` + "`" + `json:"value,omitempty"` + "`" + `

	// SecretRef is a reference to a Kubernetes Secret containing the sensitive data.
	// Required when type is 'secretRef'.
	//
	// +optional{{range .SensitiveDataSourceSecretRefValidations}}
	// {{ . }}{{end}}
	SecretRef *{{.NamespacedRefTypeName}} ` + "`" + `json:"secretRef,omitempty"` + "`" + `
}`

const konnectEntityRefType = `// KonnectEntityRef is a reference to a Konnect entity.
type KonnectEntityRef struct {
	// ID is the unique identifier of the Konnect entity as assigned by Konnect API.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ID string ` + "`json:\"id,omitzero\"`" + `
}`

// flattenSensitiveDataHelper is a runtime helper emitted into common_types.go.
// It replaces every JSON object matching the SensitiveDataSource wire shape
// {"type": "inline"|"secretRef", "value": "<string>", ...} with just the bare
// string value, so the Konnect SDK receives a plain string instead of the
// structured CRD representation.
const flattenSensitiveDataHelper = `// flattenSensitiveData recursively replaces any SensitiveDataSource JSON
// object shape {"type": "inline|secretRef", "value": "X", ...} with the
// bare string "X", translating the CRD wire format to the Konnect SDK
// wire format which expects plain strings for sensitive fields.
func flattenSensitiveData(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = flattenSensitiveData(val)
		}
		typ, _ := x["type"].(string)
		if typ != "inline" && typ != "secretRef" {
			return x
		}
		rawVal, hasVal := x["value"]
		val, isString := rawVal.(string)
		if hasVal && isString {
			return val
		}
		return x
	case []any:
		for i, val := range x {
			x[i] = flattenSensitiveData(val)
		}
		return x
	}
	return v
}`

// flattenSDKUnionsHelper is a runtime helper used by the per-entity
// marshalSDKOpsPayload methods to bridge the wire-shape gap between the
// CRD and the Konnect SDK.
//
// CRD storage uses the nested wrapper shape that mirrors the Go struct
// layout for discriminated unions:
//
//	{"type": "X", "X": {... variant fields ...}}
//
// The Konnect SDK request types instead expect the flat shape from the
// OpenAPI spec, with the discriminator and variant fields as siblings:
//
//	{"type": "X", ... variant fields ...}
//
// flattenSDKUnions walks a JSON-decoded value tree and rewrites every
// object matching the nested pattern into the flat one. The walk is
// recursive so it also fixes unions buried inside arrays or under other
// nested properties.
const flattenSDKUnionsHelper = `// flattenSDKUnions recursively flattens nested discriminated-union shapes
// {"type": "X", "X": {...}} into the flat shape {"type": "X", ...} expected
// by the Konnect SDK request types.
func flattenSDKUnions(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = flattenSDKUnions(val)
		}
		t, ok := x["type"].(string)
		if !ok || t == "" {
			return x
		}
		inner, ok := x[t].(map[string]any)
		if !ok {
			return x
		}
		delete(x, t)
		for k, vv := range inner {
			if k == "type" {
				continue
			}
			x[k] = vv
		}
		return x
	case []any:
		for i, val := range x {
			x[i] = flattenSDKUnions(val)
		}
		return x
	}
	return v
}`

// renameKeysToSDKHelper provides camelCase → snake_case key translation so
// that the camelCase K8s wire-format JSON produced by generated CRD types can
// be consumed by Konnect SDK request structs that expect snake_case keys.
const renameKeysToSDKHelper = `
// camelToSnakeCase converts a camelCase string to snake_case.
// e.g. "bootstrapServers" → "bootstrap_servers", "defaultAPIVisibility" → "default_api_visibility"
func camelToSnakeCase(s string) string {
	var buf []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 && (s[i-1] >= 'a' && s[i-1] <= 'z' ||
				(i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z')) {
				buf = append(buf, '_')
			}
			buf = append(buf, c+('a'-'A'))
		} else {
			buf = append(buf, c)
		}
	}
	return string(buf)
}

// renameKeysToSDK converts all map keys and discriminator "type" values from
// camelCase (CRD K8s wire format) to snake_case (Konnect SDK wire format).
func renameKeysToSDK(v any) any {
	switch x := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(x))
		for k, val := range x {
			newKey := camelToSnakeCase(k)
			// Discriminator type string values must also be snake_case for the SDK.
			if k == "type" {
				if s, ok := val.(string); ok {
					val = camelToSnakeCase(s)
				}
			}
			result[newKey] = renameKeysToSDK(val)
		}
		return result
	case []any:
		for i, val := range x {
			x[i] = renameKeysToSDK(val)
		}
		return x
	}
	return v
}`
