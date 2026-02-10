# Design Document: crd-from-oas

## Overview

`crd-from-oas` is a Go code generator that converts OpenAPI 3.x specifications into
Kubernetes Custom Resource Definition (CRD) Go types with kubebuilder validation markers.
It reads API paths from an OpenAPI spec, extracts request body schemas, and produces
ready-to-use CRD type files compatible with controller-runtime and controller-gen.

## Architecture

The tool is structured as a three-stage pipeline:

```
OpenAPI Spec (YAML)
        |
        v
  +-----------+       +-------------+       +----------------+
  |  Parser   | ----> | Intermediate| ----> |   Generator    |
  | (pkg/parser)      | Representation      | (pkg/generator)|
  +-----------+       +-------------+       +----------------+
        ^                                          |
        |                                          v
  +----------+                              Generated Go files
  |  Config  |                              (api/<group>/<version>/)
  | (pkg/config)
  +----------+
```

### Packages

| Package         | Responsibility                                                                                                                                                      |
|-----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `main`          | Orchestration: reads environment variables, loads the spec, invokes the parser and generator, writes output files.                                                  |
| `pkg/parser`    | Parses OpenAPI paths, extracts request body schemas, resolves `$ref` references, and builds an intermediate representation (`ParsedSpec`).                          |
| `pkg/generator` | Converts the intermediate representation into Go source files using `text/template`, including CRD types, common types, schema types, and registration boilerplate. |
| `pkg/config`    | Loads an optional YAML configuration file that provides per-entity, per-field custom kubebuilder markers.                                                           |

## Configuration

The tool is configured entirely through environment variables:

| Variable      | Required                     | Description                                                                                          |
|---------------|------------------------------|------------------------------------------------------------------------------------------------------|
| `INPUT_FILE`  | No (default: `openapi.yaml`) | Path to the OpenAPI spec file.                                                                       |
| `OUTPUT_DIR`  | No (default: `api/`)         | Base output directory for generated files.                                                           |
| `API_GROUP`   | Yes                          | Kubernetes API group (e.g., `konnect.konghq.com`).                                                   |
| `API_VERSION` | Yes                          | API version (e.g., `v1alpha1`).                                                                      |
| `PATHS`       | Yes                          | Comma-separated list of OpenAPI paths to process (e.g., `/v3/portals,/v3/portals/{portalId}/teams`). |
| `CONFIG_FILE` | No                           | Path to a YAML file with custom field validations.                                                   |

Files are written to `<OUTPUT_DIR>/<first part of API_GROUP>/<API_VERSION>/`.

### Custom Field Configuration

An optional YAML config file allows injecting additional kubebuilder markers on
specific fields. The config is validated against the parsed schema to catch typos.

```yaml
Portal:
  name:
    _validations:
      - "+kubebuilder:validation:XValidation:rule=\"self == oldSelf\",message=\"name is immutable\""
```

## Parsing Stage

### Path-Based Discovery

The parser targets **POST operations** on the specified paths, treating them as resource
creation endpoints. For each path it:

1. Locates the path item in the OpenAPI document.
2. Extracts path parameter dependencies (e.g., `{portalId}` indicates the resource is a
   child of `Portal`).
3. Resolves the request body schema, either from a `$ref` or by deriving a name from the
   operation ID / path segments.
4. Recursively parses all properties into an intermediate `Property` tree.
5. Collects all transitively referenced component schemas for separate type generation.

### Intermediate Representation

The parser produces three key data structures:

**`Schema`** -- represents a parsed request body with its properties, dependencies, and
optional root-level `oneOf` variants.

**`Property`** -- a recursive tree node representing a single OpenAPI property. It carries
type information, validations (min/max length, pattern, enum, min/max value, default),
nullability, read-only status, and nested children (`Items` for arrays, `Properties` for
objects, `AdditionalProperties` for maps, `OneOf` for union types).

**`Dependency`** -- a parent resource reference derived from a path parameter. Includes
the parameter name, entity name, Go field name, and JSON tag.

### Reference Detection

Properties that end with `_id` and have `format: "uuid"` are treated as **entity
references**. Instead of generating a plain `string` field, the generator produces an
`*ObjectRef` field (suffixed with `Ref`) that references a Kubernetes object by name.
This allows the controller to resolve API IDs to Kubernetes object references.

### Recursion Safety

The parser guards against circular `$ref` chains and deeply nested schemas with two
mechanisms:

- A **visited set** that tracks which schema names have already been entered during a
  single parse tree walk. Re-entering a visited schema short-circuits to a stub property.
- A **depth limit** (max 10) that stops recursion regardless of cycle detection.

## Generation Stage

### Template-Based Code Generation

The generator uses Go `text/template` with a custom function map to produce well-formed
Go source code. Key template functions include:

| Function          | Purpose                                                                                              |
|-------------------|------------------------------------------------------------------------------------------------------|
| `goType`          | Maps a `Property` to its Go type string.                                                             |
| `goFieldName`     | Converts `snake_case` names to `PascalCase`, recognizing common acronyms (ID, URL, API, UUID, etc.). |
| `jsonTag`         | Produces JSON struct tags with `omitempty`.                                                          |
| `kubebuilderTags` | Generates the full set of kubebuilder validation markers for a property.                             |
| `isRefProperty`   | Detects entity reference properties.                                                                 |
| `skipProperty`    | Filters out properties that should not appear in the CRD spec.                                       |
| `formatComment`   | Wraps description text into Go comment lines (max 76 chars per line).                                |

### Generated Files

For each processed path, the generator produces:

| File                | Contents                                                                                                                                  |
|---------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `<entity>_types.go` | Main CRD type, List type, Spec, APISpec, Status, and `init()` registration.                                                               |
| `common_types.go`   | Shared reference types: `ObjectRef`, `NamespacedObjectRef`, `SecretKeyRef`, `ConfigMapKeyRef`, `KonnectEntityStatus`, `KonnectEntityRef`. |
| `schema_types.go`   | Go struct definitions for all transitively referenced component schemas.                                                                  |
| `register.go`       | `GroupVersion`, `SchemeBuilder`, and `AddToScheme` for controller-runtime scheme registration.                                            |
| `doc.go`            | Package-level kubebuilder markers (`+kubebuilder:object:generate=true`, `+groupName`).                                                    |

### CRD Type Structure

Each entity follows Kubernetes API conventions with a Spec/Status split:

```go
type Portal struct {
    metav1.TypeMeta
    metav1.ObjectMeta
    Spec   PortalSpec
    Status PortalStatus
}

type PortalSpec struct {
    // Parent references (from path parameters)
    // e.g., PortalRef ObjectRef

    PortalAPISpec `json:",inline"`
}

type PortalAPISpec struct {
    // Fields from the OpenAPI request body schema
    // with kubebuilder validation markers
}

type PortalStatus struct {
    Conditions          []metav1.Condition
    KonnectEntityStatus `json:",inline"`
    // Parent entity ID references
    ObservedGeneration  int64
}
```

The `APISpec` is separated from `Spec` so that dependency references (parent object refs)
live at the `Spec` level while pure API fields are inlined from `APISpec`. This enables
reuse of `APISpec` independently from the Kubernetes-specific wrapper.

### Type Mapping

| OpenAPI Type                                | Go Type                                |
|---------------------------------------------|----------------------------------------|
| `string`                                    | `string`                               |
| `string` (format `uuid`, name ending `_id`) | `*ObjectRef`                           |
| `integer`                                   | `int` (or `int32`/`int64` per format)  |
| `number`                                    | `float64` (or `float32` per format)    |
| `boolean` (required)                        | `*bool`                                |
| `boolean` (optional)                        | `bool`                                 |
| `array`                                     | `[]T`                                  |
| `object` (with properties)                  | Named struct                           |
| `object` (with `additionalProperties`)      | `map[string]T`                         |
| `object` (no properties)                    | `apiextensionsv1.JSON`                 |
| `$ref`                                      | Referenced type name                   |
| `oneOf`                                     | Discriminated union struct (see below) |

### Skipped Properties

Certain properties are excluded from the generated Spec because they are managed
server-side:

- Read-only properties (`readOnly: true`)
- The `id` field
- Timestamp fields: `created_at`, `updated_at`

### Kubebuilder Validation Markers

The generator automatically produces validation markers based on the OpenAPI schema:

| Constraint                        | Marker                                               |
|-----------------------------------|------------------------------------------------------|
| Required (non-nullable)           | `+required`                                          |
| Optional or nullable              | `+optional`                                          |
| String min length                 | `+kubebuilder:validation:MinLength=N`                |
| String max length                 | `+kubebuilder:validation:MaxLength=N` (default: 256) |
| Required string (no explicit min) | `+kubebuilder:validation:MinLength=1`                |
| Regex pattern                     | `` +kubebuilder:validation:Pattern=`regex` ``        |
| Numeric minimum                   | `+kubebuilder:validation:Minimum=N`                  |
| Numeric maximum                   | `+kubebuilder:validation:Maximum=N`                  |
| Enum values                       | `+kubebuilder:validation:Enum=a;b;c`                 |
| Default value                     | `+kubebuilder:default=value`                         |

Smart defaults are applied: all strings get a `MaxLength=256` if none is specified, and
required strings get `MinLength=1`. Custom markers from the config file are appended
after the auto-generated ones.

## oneOf / Union Types

OpenAPI `oneOf` constructs (at both root-level and property-level) are converted into
**discriminated union structs**:

```go
type IdentityProviderRequestConfig struct {
    Type IdentityProviderRequestConfigType `json:"type,omitempty"`
    OIDC *ConfigureOIDCIdentityProviderConfig `json:"oidc,omitempty"`
    SAML *ConfigureSAMLIdentityProviderConfig `json:"saml,omitempty"`
}

type IdentityProviderRequestConfigType string

const (
    IdentityProviderRequestConfigTypeOIDC IdentityProviderRequestConfigType = "OIDC"
    IdentityProviderRequestConfigTypeSAML IdentityProviderRequestConfigType = "SAML"
)
```

A `Type` discriminator field selects which variant is active. Each variant is an optional
pointer field.

### Variant Name Extraction

Variant names are cleaned automatically by finding the longest common prefix and suffix
across all variant reference names and extracting the unique middle part. Additional
cleanup removes common prefixes (`Configure`, `Create`, `Update`) and suffixes (`Config`,
`Configuration`, `Provider`, `Request`, `IdentityProvider`).

Example: `["ConfigureOIDCIdentityProviderConfig", "ConfigureSAMLIdentityProviderConfig"]`
produces variant names `["OIDC", "SAML"]`.

## Dependency Handling

Path parameters encode parent-child relationships between resources. For a path like
`/v3/portals/{portalId}/teams/{teamId}/members`, the parser extracts two dependencies:
`Portal` and `Team`.

These become:
- **Spec fields**: `PortalRef ObjectRef` and `TeamRef ObjectRef` -- required references
  to parent Kubernetes objects.
- **Status fields**: `PortalID *KonnectEntityRef` and `TeamID *KonnectEntityRef` --
  the resolved Konnect API IDs of the parent entities.

Parameter names are converted to entity names by stripping `Id`/`ID`/`_id` suffixes and
title-casing the result.

## Comment Wrapping

Description text from the OpenAPI spec is formatted into Go comments. Long descriptions
are wrapped intelligently:

1. Text is first split on sentence boundaries (`. ` followed by more text).
2. Each sentence is then wrapped at word boundaries to fit within a 76-character line
   width (accounting for the `// ` prefix to stay within 80 columns total).

## Testing Strategy

Each package has comprehensive unit tests:

- **Parser tests**: path parsing, dependency extraction, all property types, references,
  validations, nested objects, cycle detection, and depth limiting.
- **Generator tests**: variant name extraction, common prefix/suffix finding, union type
  generation.
- **Tags tests**: marker generation for all constraint types, custom config integration,
  config validation errors.
- **Wrap tests**: sentence splitting, long line wrapping, multi-sentence handling.
