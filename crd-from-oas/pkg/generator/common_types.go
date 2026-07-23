package generator

// sensitiveDataSourceValueMaxLength defines the maximum length for string values
// in the SensitiveDataSource struct.
// When needed this can be considered to be made configurable via config.yaml,
// but for now it's a constant since it applies uniformly to all string fields in the SensitiveDataSource struct.
const sensitiveDataSourceValueMaxLength = 4096

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
	// +optional
	// +kubebuilder:validation:MaxLength={{ .SensitiveDataSourceValueMaxLength }}
	Value *string ` + "`" + `json:"value,omitempty"` + "`" + `

	// SecretRef is a reference to a Kubernetes Secret containing the sensitive data.
	// Required when type is 'secretRef'.
	//
	// +optional{{range .SensitiveDataSourceSecretRefValidations}}
	// {{ . }}{{end}}
	SecretRef *SensitiveDataSecretRef ` + "`" + `json:"secretRef,omitempty"` + "`" + `
}`

// dedicatedSensitiveDataSourceStructType is a per-field variant of
// sensitiveDataSourceStructType, emitted into an entity's own generated types
// file (not common_types.go) for secret reference leaves whose OAS type isn't
// string. It shares the SensitiveDataSourceType enum and SensitiveDataSecretRef
// type with the common SensitiveDataSource, differing only in the Value field's
// type. Rendered via [fmt.Sprintf] with (name, valueGoType, valueTypeMarker) —
// plain string substitution rather than text/template, since callers build
// this per-field (potentially many times per file) and shouldn't have to
// thread template execution errors through generateSchemaTypes/generateCRDType.
// valueTypeMarker is an optional extra kubebuilder marker line (with trailing
// newline and indentation, or "") inserted just above the Value field — see
// sensitiveValueTypeMarker.
const dedicatedSensitiveDataSourceStructType = `// %[1]s holds a sensitive value that can be provided either inline or
// sourced from a Kubernetes Secret.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'inline' ? has(self.value) : has(self.secretRef)",message="value required when type=inline; secretRef required when type=secretRef"
type %[1]s struct {
	// Type indicates the source of the sensitive data: 'inline' or 'secretRef'.
	//
	// +kubebuilder:validation:Enum=inline;secretRef
	// +kubebuilder:default=inline
	Type SensitiveDataSourceType ` + "`" + `json:"type"` + "`" + `

	// Value contains the sensitive data provided inline.
	// Required when type is 'inline'.
	//
	// +optional
%[3]s	Value *%[2]s ` + "`" + `json:"value,omitempty"` + "`" + `

	// SecretRef is a reference to a Kubernetes Secret containing the sensitive data.
	// Required when type is 'secretRef'.
	//
	// +optional
	SecretRef *SensitiveDataSecretRef ` + "`" + `json:"secretRef,omitempty"` + "`" + `
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
// It replaces every JSON object matching the SensitiveDataSource (or dedicated
// per-field DataSource) wire shape {"type": "inline"|"secretRef", "value": X, ...}
// with the bare value X, so the Konnect SDK receives a plain value instead of
// the structured CRD representation. X may be a string, number, boolean,
// object, or array — the shape check only inspects "type", not "value"'s kind.
const flattenSensitiveDataHelper = `// flattenSensitiveData recursively replaces any SensitiveDataSource (or
// dedicated per-field DataSource) JSON object shape
// {"type": "inline|secretRef", "value": X, ...} with the bare value X,
// translating the CRD wire format to the Konnect SDK wire format which
// expects plain values (of whatever type X is) for sensitive fields.
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
		if rawVal, hasVal := x["value"]; hasVal {
			return rawVal
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
// object matching the nested pattern into the SDK wire shape. Object-valued
// members become flat sibling fields, while scalar and array members become
// the bare selected payload. The walk is recursive so it also fixes unions
// buried inside arrays or under other nested properties.
const flattenSDKUnionsHelper = `// flattenSDKUnions recursively flattens nested discriminated-union shapes.
// Object-valued members are rewritten from {"<disc>": "X", "X": {...}}
// to {"<disc>": "X", ...}, while scalar and array members are rewritten to
// the bare selected payload. Both forms match the Konnect SDK request types.
func flattenSDKUnions(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = flattenSDKUnions(val)
		}
		_, discriminatorValue, inner, ok := nestedSDKUnionMember(x)
		if !ok {
			return x
		}
		innerMap, ok := inner.(map[string]any)
		if !ok {
			if len(x) == 2 {
				return inner
			}
			return x
		}
		delete(x, discriminatorValue)
		for k, vv := range innerMap {
			if _, isString := vv.(string); isString && x[k] == vv {
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
}

func nestedSDKUnionMember(object map[string]any) (string, string, any, bool) {
	preferred := []string{"type", "op", "kind", "mode"}
	for _, key := range preferred {
		if value, inner, ok := nestedSDKUnionMemberForKey(object, key); ok {
			return key, value, inner, true
		}
	}
	for key := range object {
		if value, inner, ok := nestedSDKUnionMemberForKey(object, key); ok {
			return key, value, inner, true
		}
	}
	return "", "", nil, false
}

func nestedSDKUnionMemberForKey(object map[string]any, key string) (string, any, bool) {
	discriminatorValue, ok := object[key].(string)
	if !ok || discriminatorValue == "" {
		return "", nil, false
	}
	// A discriminator must point at a *different* sibling member. A field whose
	// value names itself (e.g. {"certificate":"certificate"}) is plain data, not
	// a union wrapper.
	if discriminatorValue == key {
		return "", nil, false
	}
	inner, ok := object[discriminatorValue]
	if !ok || inner == nil {
		return "", nil, false
	}
	return discriminatorValue, inner, true
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

// renameKeysToSDK converts all map keys and common discriminator values from
// camelCase (CRD K8s wire format) to snake_case (Konnect SDK wire format).
func isSDKDiscriminatorKey(key string) bool {
	switch key {
	case "type", "op", "kind", "mode", "aclAttributeType":
		return true
	default:
		return false
	}
}

func renameKeysToSDK(v any) any {
	switch x := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(x))
		for k, val := range x {
			newKey := camelToSnakeCase(k)
			// Discriminator string values must also be snake_case for the SDK.
			if isSDKDiscriminatorKey(k) {
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

// injectSDKOpsConstFieldsHelper re-injects const discriminators (e.g. a nested
// auth type="basic") that were stripped from the CRD structs because their
// schema is also used as a discriminated-union member, but are still required
// by the standalone ($ref) Konnect SDK request types. It runs after
// renameKeysToSDK, so paths use snake_case segments; "[]" descends into every
// array element and "{}" into every map value.
const injectSDKOpsConstFieldsHelper = `// sdkOpsConstField describes a const discriminator to inject at a payload path.
type sdkOpsConstField struct {
	Path  []string
	Key   string
	Value string
}

// injectSDKOpsConstFields sets each const discriminator into the payload, only
// when the key is absent, so user- or union-provided values are never overridden.
func injectSDKOpsConstFields(payload map[string]any, fields []sdkOpsConstField) {
	for _, f := range fields {
		setSDKOpsConstAtPath(payload, f.Path, f.Key, f.Value)
	}
}

func setSDKOpsConstAtPath(v any, path []string, key, value string) {
	if len(path) == 0 {
		if m, ok := v.(map[string]any); ok {
			if _, exists := m[key]; !exists {
				m[key] = value
			}
		}
		return
	}
	switch segment := path[0]; segment {
	case "[]":
		if items, ok := v.([]any); ok {
			for _, item := range items {
				setSDKOpsConstAtPath(item, path[1:], key, value)
			}
		}
	case "{}":
		if object, ok := v.(map[string]any); ok {
			for _, item := range object {
				setSDKOpsConstAtPath(item, path[1:], key, value)
			}
		}
	default:
		if object, ok := v.(map[string]any); ok {
			if child, ok := object[segment]; ok {
				setSDKOpsConstAtPath(child, path[1:], key, value)
			}
		}
	}
}`

// unwrapSDKOpsUnionFieldsHelper rewrites a property-level oneOf's payload
// from the CRD's synthesized discriminated shape {"type": "X", "X": <value>}
// to the bare selected-member shape {"X": <value>}. This is needed for oneOf
// properties that have no OAS discriminator: Speakeasy generates those as a
// non-discriminated SDK union (no "type" key on the wire, members tried in
// order, each kept under its own key regardless of whether its value is an
// object, array, or scalar) — a shape flattenSDKUnions' generic heuristic
// cannot produce, since it can't distinguish "no discriminator, single-key
// member" from "no discriminator, genuinely bare scalar" by JSON shape alone.
// It runs after renameKeysToSDK (so paths and the "type" value use
// snake_case) and before flattenSDKUnions; "[]" descends into every array
// element and "{}" into every map value.
const unwrapSDKOpsUnionFieldsHelper = `// sdkOpsUnionUnwrapField locates a property-level oneOf that has no OAS
// discriminator, so its Konnect SDK type expects the non-discriminated wire
// shape {"<member>": <value>} instead of the CRD's synthesized
// {"type": "<member>", "<member>": <value>}.
type sdkOpsUnionUnwrapField struct {
	Path []string
}

// unwrapSDKOpsUnionFields rewrites each payload path from the CRD's
// discriminated shape to the bare selected-member shape, for oneOf
// properties with no OAS discriminator. Runs after renameKeysToSDK (paths use
// snake_case segments) and before flattenSDKUnions; "[]" descends into every
// array element and "{}" into every map value.
func unwrapSDKOpsUnionFields(payload map[string]any, fields []sdkOpsUnionUnwrapField) {
	for _, f := range fields {
		unwrapSDKOpsUnionAtPath(payload, f.Path)
	}
}

func unwrapSDKOpsUnionAtPath(v any, path []string) {
	if len(path) == 0 {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		typ, ok := m["type"].(string)
		if !ok {
			return
		}
		member, ok := m[typ]
		if !ok {
			return
		}
		for k := range m {
			delete(m, k)
		}
		m[typ] = member
		return
	}
	switch segment := path[0]; segment {
	case "[]":
		if items, ok := v.([]any); ok {
			for _, item := range items {
				unwrapSDKOpsUnionAtPath(item, path[1:])
			}
		}
	case "{}":
		if object, ok := v.(map[string]any); ok {
			for _, item := range object {
				unwrapSDKOpsUnionAtPath(item, path[1:])
			}
		}
	default:
		if object, ok := v.(map[string]any); ok {
			if child, ok := object[segment]; ok {
				unwrapSDKOpsUnionAtPath(child, path[1:])
			}
		}
	}
}`

// assignSDKOpsUnionMembersHelper works around a Konnect SDK bug: for a
// non-discriminated union whose members are all optional, single-property
// objects (the same shape unwrapSDKOpsUnionFields targets), the SDK's
// generated UnmarshalJSON tries each member in declaration order and accepts
// the first one that unmarshals without error. Since every member's sole
// field is optional, the first member always "succeeds" vacuously — so
// json.Unmarshal into the parent SDK struct silently discards the real value
// (e.g. AIGatewayModelRouteConfig.Model always resolves to an empty Body,
// regardless of input). assignSDKOpsUnionMembers reassigns the affected field
// directly from its already-correct bare sub-JSON after that unmarshal,
// bypassing the union's broken UnmarshalJSON entirely.
const assignSDKOpsUnionMembersHelper = `// assignSDKOpsUnionMembers reassigns each non-discriminated union field
// listed in fields directly on the already-unmarshaled SDK struct target,
// using its bare sub-JSON from data. This works around Konnect SDK unions
// whose UnmarshalJSON can't distinguish between optional, single-property
// members and always resolves to the first one. variantJSONName is the
// snake_case selected-variant key (fields[i].Path[0]) that data corresponds
// to; fields not rooted at variantJSONName are skipped.
func assignSDKOpsUnionMembers(target any, data []byte, fields []sdkOpsUnionUnwrapField, variantJSONName string) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("assignSDKOpsUnionMembers: target must be a non-nil pointer")
	}
	for _, field := range fields {
		if len(field.Path) < 2 || field.Path[0] != variantJSONName {
			continue
		}
		if err := assignSDKOpsUnionMemberAtPath(any(raw), v.Elem(), field.Path[1:]); err != nil {
			return err
		}
	}
	return nil
}

// assignSDKOpsUnionMemberAtPath walks rawValue and target in lockstep,
// following path (snake_case JSON field names; "[]" descends into every
// slice element). It stops descending into maps ("{}"): map values obtained
// via reflection are not addressable, so a union field nested under a map
// can't be reassigned this way; no field generated so far needs it.
func assignSDKOpsUnionMemberAtPath(rawValue any, target reflect.Value, path []string) error {
	if len(path) == 0 {
		subMap, ok := rawValue.(map[string]any)
		if !ok || len(subMap) == 0 {
			return nil
		}
		return assignSDKOpsUnionMember(target, subMap)
	}

	if path[0] == "[]" {
		rawItems, ok := rawValue.([]any)
		if !ok {
			return nil
		}
		sliceTarget := reflectIndirect(target)
		if sliceTarget.Kind() != reflect.Slice {
			return nil
		}
		for i := 0; i < sliceTarget.Len() && i < len(rawItems); i++ {
			if err := assignSDKOpsUnionMemberAtPath(rawItems[i], sliceTarget.Index(i), path[1:]); err != nil {
				return err
			}
		}
		return nil
	}

	rawMap, ok := rawValue.(map[string]any)
	if !ok {
		return nil
	}
	rawChild, ok := rawMap[path[0]]
	if !ok || rawChild == nil {
		return nil
	}
	structTarget := reflectIndirect(target)
	if structTarget.Kind() != reflect.Struct {
		return nil
	}
	fieldVal := sdkStructFieldByJSONName(structTarget, path[0])
	if !fieldVal.IsValid() {
		return nil
	}
	return assignSDKOpsUnionMemberAtPath(rawChild, fieldVal, path[1:])
}

// assignSDKOpsUnionMember rebuilds the single-property member that matches
// subMap's one key onto the union struct held at target (a pointer field,
// allocated if nil), and sets the union's sibling Type field to the matching
// member's Go field name, mirroring the SDK's own Create<Union>... constructors.
func assignSDKOpsUnionMember(target reflect.Value, subMap map[string]any) error {
	if len(subMap) != 1 {
		return nil
	}
	var memberKey string
	for k := range subMap {
		memberKey = k
	}

	unionVal := reflectIndirect(target)
	if unionVal.Kind() != reflect.Struct {
		return nil
	}
	unionType := unionVal.Type()

	matchIndex := -1
	for i := 0; i < unionType.NumField(); i++ {
		field := unionType.Field(i)
		if field.Tag.Get("union") != "member" {
			continue
		}
		memberType := field.Type
		if memberType.Kind() == reflect.Ptr {
			memberType = memberType.Elem()
		}
		if memberType.Kind() == reflect.Struct && sdkStructHasJSONField(memberType, memberKey) {
			matchIndex = i
			break
		}
	}
	if matchIndex < 0 {
		return fmt.Errorf("assignSDKOpsUnionMembers: no union member matches key %q", memberKey)
	}

	// The SDK's own UnmarshalJSON runs first and, for these ambiguous unions,
	// may have already populated a *different* member (see package doc).
	// Clear every member field before setting the matched one, or the SDK's
	// declaration-order MarshalJSON would still prefer the stale one.
	for i := 0; i < unionType.NumField(); i++ {
		field := unionType.Field(i)
		if field.Tag.Get("union") == "member" {
			unionVal.Field(i).SetZero()
		}
	}

	matchField := unionType.Field(matchIndex)
	memberType := matchField.Type.Elem()
	subData, err := json.Marshal(subMap)
	if err != nil {
		return err
	}
	memberPtr := reflect.New(memberType)
	if err := json.Unmarshal(subData, memberPtr.Interface()); err != nil {
		return err
	}
	unionVal.Field(matchIndex).Set(memberPtr)
	if typeField := unionVal.FieldByName("Type"); typeField.IsValid() && typeField.Kind() == reflect.String {
		typeField.SetString(matchField.Name)
	}
	return nil
}

// reflectIndirect dereferences a pointer, allocating a zero value first if it
// is nil, and returns v unchanged otherwise.
func reflectIndirect(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			if !v.CanSet() {
				return reflect.Value{}
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		return v.Elem()
	}
	return v
}

// sdkStructFieldByJSONName returns the field of structVal whose json tag's
// name matches jsonName, or the zero Value if none matches.
func sdkStructFieldByJSONName(structVal reflect.Value, jsonName string) reflect.Value {
	structType := structVal.Type()
	for i := 0; i < structType.NumField(); i++ {
		if sdkJSONFieldName(structType.Field(i)) == jsonName {
			return structVal.Field(i)
		}
	}
	return reflect.Value{}
}

// sdkStructHasJSONField reports whether structType has a field whose json
// tag's name matches jsonName.
func sdkStructHasJSONField(structType reflect.Type, jsonName string) bool {
	for i := 0; i < structType.NumField(); i++ {
		if sdkJSONFieldName(structType.Field(i)) == jsonName {
			return true
		}
	}
	return false
}

// sdkJSONFieldName extracts the name portion of a struct field's json tag,
// falling back to the Go field name when the tag is absent or "-".
func sdkJSONFieldName(field reflect.StructField) string {
	name := field.Tag.Get("json")
	if idx := strings.IndexByte(name, ','); idx >= 0 {
		name = name[:idx]
	}
	if name == "" || name == "-" {
		return field.Name
	}
	return name
}`
