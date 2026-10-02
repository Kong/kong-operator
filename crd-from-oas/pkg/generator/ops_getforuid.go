package generator

import (
	"fmt"
	"slices"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// OpsGetForUIDFileInfo is metadata returned from generateEntityOpsFile so the
// caller (e.g. run.go) can assemble the cross-group getForUID dispatcher.
type OpsGetForUIDFileInfo struct {
	Entity         string
	APIAlias       string
	APIPackagePath string
	SDKGetter      string
}

// opsGetForUIDFuncData holds template data for a single get<Entity>ForUID function.
type opsGetForUIDFuncData struct {
	Entity            string
	APIAlias          string
	ListSDKInterface  string
	ListSDKMethod     string
	ListResponseField string
	// ListResponseItemsExpr is the Go expression, relative to resp, that yields
	// the iterable list items.
	ListResponseItemsExpr string
	// ListResponseNilCheck is the Go expression, relative to resp, that detects
	// a nil/invalid list payload before iterating items.
	ListResponseNilCheck string
	// Parents holds metadata for each parent dependency (outermost first).
	Parents []parentInfo
	// GetForUIDFullyWrapped is true for multi-parent entities. The SDK list
	// method takes a single request struct with all parent fields rather than
	// a single positional parentID.
	GetForUIDFullyWrapped bool
	// GetForUIDWrappedType is the SDK operations struct type name for fully-wrapped
	// list, e.g. "ListEventGatewayListenerPoliciesRequest".
	GetForUIDWrappedType string
	// ParentIDField is the SDK request struct field name for the (single) parent ID,
	// used only when GetForUIDFullyWrapped is false (single-parent case).
	// e.g. "PortalID" for an entity nested under Portal.
	ParentIDField string
	// ListCallStylePositional indicates the SDK list method takes positional
	// (pageSize *int64, pageNumber *int64) args instead of a request struct.
	ListCallStylePositional bool
	// ListPagination is how the SDK list method pages its results (one of the
	// listPagination* constants), so the generated lookup requests every page.
	ListPagination string
	// ListMetaIsPage is true when, with cursor pagination, the list response's
	// meta is the page itself (meta.next) rather than holding it (meta.page.next).
	ListMetaIsPage bool
	// ListCallPositionalWithParent is true when the SDK list method uses
	// positional args and the entity has a single parent, so the generated
	// call passes the parent ID as the first positional argument.
	ListCallPositionalWithParent bool
	// HasLabels indicates the entity's request schema declares a "labels"
	// field, so list response items are expected to expose GetLabels() and
	// the generator can match by the Kubernetes UID label.
	HasLabels bool
	// LabelsResponseVariantFields lists, when HasLabels is set and the list
	// response items are root-level discriminated unions, the union member
	// fields of a list item. The wrapper itself exposes no GetID()/GetLabels(),
	// so the generated lookup reads them from whichever member is set.
	LabelsResponseVariantFields []string
	// UseUIDTagFilter indicates the API supports filtering list requests by the
	// Kubernetes UID tag, so getForUID can avoid full scans.
	UseUIDTagFilter bool
	// MatchFields configures generated field comparisons for entities whose
	// list responses do not expose labels/tags.
	MatchFields []opsGetForUIDMatchFieldData
	// AllMatchFieldsSkipWhenUnset is true when every entry in MatchFields sets
	// SkipWhenUnset, so an object that leaves all of them unset would compare
	// nothing and match an arbitrary list entry. The template then emits an
	// upfront guard that reports not-found instead.
	AllMatchFieldsSkipWhenUnset bool
	// RootUnion configures variant-aware matching for root-union-backed specs.
	RootUnion *opsGetForUIDRootUnionData
	// HasName indicates the entity's request schema declares a "name" field,
	// used as a fallback UID-matching strategy when HasLabels is false.
	HasName bool
	// SingletonByParent is true when the entity is a singleton sub-resource whose
	// GET/UPDATE/DELETE paths are keyed solely by the parent ID.
	SingletonByParent bool
	// SingletonNoID is true when the entity is a singleton sub-resource whose
	// response has no "id" field. getForUID calls the singular GET (not a list)
	// and matches via MatchFields.
	SingletonNoID bool
}

type opsGetForUIDMatchFieldData struct {
	ObjectField   string
	ResponseField string
	// SliceMatch is true when the field is a []string slice rather than a plain
	// string/pointer, causing the template to emit matchSliceField instead of
	// matchStringField.
	SliceMatch bool
	// SkipWhenUnset is true when an empty object-side value must not block the
	// match, causing the template to emit matchOptionalStringField instead of
	// matchStringField.
	SkipWhenUnset bool
}

type opsGetForUIDRootUnionData struct {
	UnionField        string
	ResponseTypeField string
	// ResponseTypePointer is true when the SDK discriminator getter returns a
	// pointer to an enum rather than a plain string.
	ResponseTypePointer bool
	// ResponseVariantContainer is the getter path on the list entry returning
	// the SDK union container that holds per-variant payload fields.
	ResponseVariantContainer string
	Cases                    []opsGetForUIDRootUnionCaseData
}

type opsGetForUIDRootUnionCaseData struct {
	TypeValue         string
	VariantField      string
	ResponseTypeValue string
	// ResponseVariantField is the field on the SDK union container holding
	// this case's variant payload. When set, MatchFields response paths are
	// relative to that variant payload.
	ResponseVariantField string
	MatchFields          []opsGetForUIDMatchFieldData
	// AllMatchFieldsSkipWhenUnset mirrors the same field on
	// opsGetForUIDFuncData, scoped to this variant's match fields.
	AllMatchFieldsSkipWhenUnset bool
}

// generateOpsGetForUIDFuncBody renders the get<Entity>ForUID function body
// (no file header). Returns nil when:
//   - no list op is discoverable in the OpenAPI spec, or
//   - the entity is explicitly skipped via config or SkipGetForUIDEntities.
func (g *Generator) generateOpsGetForUIDFuncBody(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*opsGetForUIDFuncData, error) {
	if g.config.SkipGetForUIDEntities[entityName] {
		return nil, nil
	}
	if opsConfig != nil && opsConfig.SkipGetForUID {
		return nil, nil
	}
	if schema.ListOperationID == "" {
		return nil, nil
	}
	if len(schema.ListTags) == 0 {
		return nil, fmt.Errorf("entity %q: missing OpenAPI tags for list op (no GET found)", entityName)
	}

	listMethod := pascalFromKebab(schema.ListOperationID)
	listInterface := pascalFromKebab(schema.ListTags[0]) + "SDK"
	listInterface, err := resolveSDKInterfaceTypeName(opsConfig, listInterface)
	if err != nil {
		return nil, fmt.Errorf("entity %q: resolve list SDK interface: %w", entityName, err)
	}

	parents, err := g.resolveParents(entityName, schema)
	if err != nil {
		return nil, err
	}

	// Multi-parent entities use a fully-wrapped list request struct.
	getForUIDFullyWrapped := len(parents) >= 2

	var parentIDField, getForUIDWrappedType string
	if getForUIDFullyWrapped {
		// Wrapped type: "<PascalCaseListMethod>Request".
		getForUIDWrappedType = listMethod + "Request"
	} else if len(parents) == 1 {
		// Single-parent: derive the SDK request field name from the last path parameter.
		parentDep := schema.Dependencies[len(schema.Dependencies)-1]
		parentIDField = pathParamToFieldName(parentDep.ParamName)
	}

	// SDK codegen names the nested field on the operations response wrapper after
	// the components response type. Most entities have those names matching, e.g.
	// ListPortalsResponse → ListPortalsResponse, but some don't, e.g.
	// ListEventGatewayBackendClusters → ListBackendClustersResponse. Prefer the
	// ref name from the OpenAPI spec; fall back to the method-derived name when
	// the spec does not declare one.
	listResponseField := schema.ListSuccessResponseRef
	if listResponseField == "" {
		listResponseField = listMethod + "Response"
	}
	listResponseItemsExpr := fmt.Sprintf("resp.%s.Data", listResponseField)
	listResponseNilCheck := fmt.Sprintf("resp == nil || resp.%s == nil", listResponseField)
	if opsConfig != nil && opsConfig.GetForUID != nil &&
		opsConfig.GetForUID.ListItemsSource == config.GetForUIDListItemsSourceSlice {
		listResponseItemsExpr = fmt.Sprintf("resp.%s", listResponseField)
		listResponseNilCheck = "resp == nil"
	}

	_, hasLabels, _ := metadataFields(schema, opsConfig)
	hasName := schemaHasNameProperty(schema, opsConfig)
	matchFields := make([]opsGetForUIDMatchFieldData, 0)
	var rootUnion *opsGetForUIDRootUnionData
	if opsConfig != nil && opsConfig.GetForUID != nil {
		matchFields = make([]opsGetForUIDMatchFieldData, 0, len(opsConfig.GetForUID.MatchFields))
		for _, field := range opsConfig.GetForUID.MatchFields {
			if g.isSensitiveMatchField(entityName, field.ObjectField) {
				// The generated lookup has no client to resolve a Secret, so it
				// could not compare such a field and would match an entity with
				// any value, e.g. one created outside the operator.
				return nil, fmt.Errorf(
					"entity %q: getForUID.matchFields.objectField %q is sourced from a Secret, which the generated lookup cannot resolve; "+
						"set ops.skipGetForUID and write a lookup that resolves it",
					entityName, field.ObjectField,
				)
			}
			sliceMatch := isArrayMatchField(schema, field.ResponseField)
			if field.SkipWhenUnset && sliceMatch {
				return nil, fmt.Errorf(
					"entity %q: getForUID.matchFields.objectField %q sets skipWhenUnset, which is only supported for plain string-like fields",
					entityName, field.ObjectField,
				)
			}
			matchFields = append(matchFields, opsGetForUIDMatchFieldData{
				ObjectField:   field.ObjectField,
				ResponseField: field.ResponseField,
				SliceMatch:    sliceMatch,
				SkipWhenUnset: field.SkipWhenUnset,
			})
		}
		if opsConfig.GetForUID.RootUnion != nil {
			responseTypeField := opsConfig.GetForUID.RootUnion.ResponseTypeField
			if responseTypeField == "" {
				responseTypeField = "GetType()"
			}
			rootUnion = &opsGetForUIDRootUnionData{
				UnionField:               opsConfig.GetForUID.RootUnion.UnionField,
				ResponseTypeField:        responseTypeField,
				ResponseTypePointer:      opsConfig.GetForUID.RootUnion.ResponseTypePointer,
				ResponseVariantContainer: opsConfig.GetForUID.RootUnion.ResponseVariantContainer,
				Cases:                    make([]opsGetForUIDRootUnionCaseData, 0, len(opsConfig.GetForUID.RootUnion.Cases)),
			}
			for _, c := range opsConfig.GetForUID.RootUnion.Cases {
				caseData := opsGetForUIDRootUnionCaseData{
					TypeValue:            c.TypeValue,
					VariantField:         c.VariantField,
					ResponseTypeValue:    c.ResponseTypeValue,
					ResponseVariantField: c.ResponseVariantField,
					MatchFields:          make([]opsGetForUIDMatchFieldData, 0, len(c.MatchFields)),
				}
				for _, field := range c.MatchFields {
					caseData.MatchFields = append(caseData.MatchFields, opsGetForUIDMatchFieldData{
						ObjectField:   field.ObjectField,
						ResponseField: field.ResponseField,
						SkipWhenUnset: field.SkipWhenUnset,
					})
				}
				caseData.AllMatchFieldsSkipWhenUnset = allMatchFieldsSkipWhenUnset(caseData.MatchFields)
				rootUnion.Cases = append(rootUnion.Cases, caseData)
			}
		}
	}

	listPositional := opsConfig != nil && opsConfig.ListCallStylePositional
	useUIDTagFilter := opsConfig != nil && opsConfig.UseUIDTagFilter
	// Mirrors the branches of opsGetForUIDFuncTemplate that list entities:
	// singletons are read with a get, and with no match strategy nothing is
	// listed. The pagination is resolved either way, so that a lookup listing
	// entities never scans only the first page because this mirror is off;
	// only a lookup listing nothing may have a list method without a request
	// type (e.g. one made up for a type with no list endpoint).
	listsEntities := !isParentScopedSingleton(schema) && !isSingletonNoID(schema) &&
		(useUIDTagFilter || len(matchFields) > 0 || rootUnion != nil || hasLabels || hasName)
	listPagination, listMetaIsPage, err := resolveListPagination(listMethod, listResponseField, listPositional, len(parents))
	if err != nil {
		if listsEntities {
			return nil, fmt.Errorf("entity %q: %w", entityName, err)
		}
		listPagination, listMetaIsPage = listPaginationNone, false
	}
	if opsConfig != nil && opsConfig.GetForUID != nil &&
		opsConfig.GetForUID.ListItemsSource == config.GetForUIDListItemsSourceSlice &&
		listPagination != listPaginationNone {
		return nil, fmt.Errorf("entity %q: getForUID.listItemsSource %q is not supported for a paginated list method", entityName, config.GetForUIDListItemsSourceSlice)
	}

	var labelsResponseVariantFields []string
	usesLabelsMatch := hasLabels && !isParentScopedSingleton(schema) &&
		(opsConfig == nil || (!opsConfig.UseUIDTagFilter && len(matchFields) == 0 && rootUnion == nil))
	listItemsFromData := opsConfig == nil || opsConfig.GetForUID == nil ||
		opsConfig.GetForUID.ListItemsSource != config.GetForUIDListItemsSourceSlice
	if usesLabelsMatch && listItemsFromData {
		labelsResponseVariantFields, err = resolveListItemLabelsVariantFields(listResponseField)
		if err != nil {
			return nil, fmt.Errorf("entity %q: %w; set ops.skipGetForUID to opt out", entityName, err)
		}
	}

	return &opsGetForUIDFuncData{
		Entity:                       entityName,
		APIAlias:                     g.config.APIGroupPackageAlias,
		ListSDKInterface:             listInterface,
		ListSDKMethod:                listMethod,
		ListResponseField:            listResponseField,
		ListResponseItemsExpr:        listResponseItemsExpr,
		ListResponseNilCheck:         listResponseNilCheck,
		Parents:                      parents,
		GetForUIDFullyWrapped:        getForUIDFullyWrapped,
		GetForUIDWrappedType:         getForUIDWrappedType,
		ParentIDField:                parentIDField,
		ListCallStylePositional:      listPositional,
		ListCallPositionalWithParent: listPositional && len(parents) == 1,
		ListPagination:               listPagination,
		ListMetaIsPage:               listMetaIsPage,
		HasLabels:                    hasLabels,
		LabelsResponseVariantFields:  labelsResponseVariantFields,
		UseUIDTagFilter:              useUIDTagFilter,
		MatchFields:                  matchFields,
		AllMatchFieldsSkipWhenUnset:  allMatchFieldsSkipWhenUnset(matchFields),
		RootUnion:                    rootUnion,
		HasName:                      hasName,
		SingletonByParent:            isParentScopedSingleton(schema),
		SingletonNoID:                isSingletonNoID(schema),
	}, nil
}

// Pagination styles of Konnect list methods.
const (
	// listPaginationNone: the list method returns every item in one response.
	listPaginationNone = ""
	// listPaginationCursor: the request takes page[size] and page[after], and
	// the response's meta.page.next links to the next page.
	listPaginationCursor = "cursor"
	// listPaginationNumber: the request takes page[size] and page[number], and
	// the response's meta.page holds the total number of items.
	listPaginationNumber = "number"
)

// resolveListPagination returns how the SDK list method listMethod pages its
// results, read from its request struct (which declares the page parameters
// even when the method takes them as positional arguments) and its response
// type listResponseType, and, for cursor pagination, whether the response's
// meta is the page itself rather than holding it. It errors when the request
// and response disagree or when the generated lookup could not pass the page
// parameters, so that a lookup never silently scans only the first page.
func resolveListPagination(listMethod, listResponseType string, positional bool, parents int) (string, bool, error) {
	const (
		operationsImportPath = "github.com/Kong/sdk-konnect-go/models/operations"
		componentsImportPath = "github.com/Kong/sdk-konnect-go/models/components"
	)
	requestType := listMethod + "Request"
	if _, ok, err := sdkStructType(operationsImportPath, requestType); err != nil {
		return "", false, fmt.Errorf("inspect list request %q: %w", requestType, err)
	} else if !ok {
		if positional {
			// A positional list method may have no request struct, e.g. one
			// taking only a parent ID and a filter: it has no page parameters.
			return listPaginationNone, false, nil
		}
		return "", false, fmt.Errorf("list request type %q not found in %q", requestType, operationsImportPath)
	}
	var pagination string
	for field, style := range map[string]string{"PageAfter": listPaginationCursor, "PageNumber": listPaginationNumber} {
		has, err := sdkStructHasField(operationsImportPath, requestType, field)
		if err != nil {
			return "", false, fmt.Errorf("inspect list request %q: %w", requestType, err)
		}
		if !has {
			continue
		}
		if pagination != listPaginationNone {
			return "", false, fmt.Errorf("list request %q declares both PageAfter and PageNumber", requestType)
		}
		pagination = style
	}
	if pagination == listPaginationNone {
		return listPaginationNone, false, nil
	}

	// The response's meta holds the page (CursorMeta, PaginatedMeta), or, for
	// some cursor-paginated endpoints, is the page itself (CursorMetaPage).
	wantMeta := map[string][]string{
		listPaginationCursor: {"CursorMeta", "CursorMetaPage"},
		listPaginationNumber: {"PaginatedMeta"},
	}[pagination]
	meta, ok, err := sdkStructFieldTypeName(componentsImportPath, listResponseType, "Meta")
	if err != nil {
		return "", false, fmt.Errorf("inspect list response %q: %w", listResponseType, err)
	}
	if !ok || !slices.Contains(wantMeta, meta) {
		return "", false, fmt.Errorf("list response %q of paginated list request %q has meta %q, want one of %q", listResponseType, requestType, meta, wantMeta)
	}
	if positional && (parents > 0 || pagination != listPaginationNumber) {
		return "", false, fmt.Errorf("paginated positional list method %q is only supported with page[size] and page[number] and no parent", listMethod)
	}
	return pagination, meta == "CursorMetaPage", nil
}

// allMatchFieldsSkipWhenUnset reports whether every match field is optional
// (skipWhenUnset). Such a set degenerates into matching nothing when the object
// leaves all of the fields unset, so generated code must bail out instead of
// adopting an arbitrary list entry.
func allMatchFieldsSkipWhenUnset(fields []opsGetForUIDMatchFieldData) bool {
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if !field.SkipWhenUnset {
			return false
		}
	}
	return true
}

// isArrayMatchField reports whether the schema property matching the given Go
// field name (e.g. "AllowedIps") is an array type, so the template emits
// matchSliceField instead of matchStringField.
func isArrayMatchField(schema *parser.Schema, goName string) bool {
	if schema == nil {
		return false
	}
	for _, prop := range schema.Properties {
		if goFieldName(prop.Name) == goName && prop.Type == "array" {
			return true
		}
	}
	return false
}

// schemaHasNameProperty reports whether the request body schema declares a
// "name" string property, used as a UID-match fallback when the SDK list
// response type lacks GetLabels() / tags.
//
// When the schema is a root-level discriminated union, a "name" property
// declared inside any variant also counts — unless the entity opted out via
// ops.skipRootUnionMetadataFields. See metadataFields.
func schemaHasNameProperty(schema *parser.Schema, opsConfig *config.EntityOpsConfig) bool {
	if schema == nil {
		return false
	}
	if hasNameProperty(schema.Properties) {
		return true
	}
	if len(schema.Properties) > 0 || len(schema.OneOf) == 0 {
		return false
	}
	if opsConfig != nil && opsConfig.SkipRootUnionMetadataFields {
		return false
	}
	for _, variant := range schema.OneOf {
		if variant == nil {
			continue
		}
		if hasNameProperty(variant.Properties) {
			return true
		}
	}
	return false
}

// hasNameProperty reports whether props declares a "name" string property.
func hasNameProperty(props []*parser.Property) bool {
	for _, prop := range props {
		if prop == nil {
			continue
		}
		if prop.Name == "name" && prop.Type == "string" {
			return true
		}
	}
	return false
}

// GenerateOpsGetForUIDDispatcher emits zz_generated_ops_getforuid.go with
// getForUID[T,TEnt] and ConflictOnCreateButNoConflifctHandlingImplementedError.
// Call after all per-group generation has finished.
func GenerateOpsGetForUIDDispatcher(infos []*OpsGetForUIDFileInfo) (*GeneratedFile, error) {
	flat := make([]flatInfo, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, flatInfo{
			Entity:         info.Entity,
			APIAlias:       info.APIAlias,
			APIPackagePath: info.APIPackagePath,
			SDKGetter:      info.SDKGetter,
		})
	}
	return buildDispatcherFile("zz_generated_ops_getforuid.go", opsGetForUIDDispatcherTemplate, "controller/konnect/ops", flat)
}

// resolveListItemLabelsVariantFields returns the union member fields of the
// list response item type (the element type of listResponseType's Data
// field) when that item is a root-level discriminated union, or nil when it
// is a plain struct. Every member must declare an ID field and a
// map[string]string Labels field, as the generated lookup reads them from
// whichever member is set.
func resolveListItemLabelsVariantFields(listResponseType string) ([]string, error) {
	const importPath = "github.com/Kong/sdk-konnect-go/models/components"
	itemType, ok, err := sdkSliceFieldElemTypeName(importPath, listResponseType, "Data")
	if err != nil {
		return nil, fmt.Errorf("inspect list response %q: %w", listResponseType, err)
	}
	if !ok {
		return nil, nil
	}
	memberFields, err := ParseSDKUnionMemberFieldNames(importPath, itemType)
	if err != nil {
		return nil, fmt.Errorf("inspect list item union %q: %w", itemType, err)
	}
	if len(memberFields) == 0 {
		return nil, nil
	}
	memberTypes, err := ParseSDKUnionMemberTypeNames(importPath, itemType)
	if err != nil {
		return nil, fmt.Errorf("inspect list item union %q member types: %w", itemType, err)
	}
	for _, memberType := range memberTypes {
		for _, field := range []string{"ID", "Labels"} {
			has, err := sdkStructHasField(importPath, memberType, field)
			if err != nil {
				return nil, fmt.Errorf("inspect list item union member %q: %w", memberType, err)
			}
			if !has {
				return nil, fmt.Errorf("list item union member %q has no %s field", memberType, field)
			}
		}
		// The generated lookup reads labels into a map[string]string; other
		// shapes (e.g. map[string]*string) would not compile.
		isStringMap, err := sdkStructFieldIsStringMap(memberType, "Labels")
		if err != nil {
			return nil, fmt.Errorf("inspect list item union member %q: %w", memberType, err)
		}
		if !isStringMap {
			return nil, fmt.Errorf("list item union member %q Labels field is not a map[string]string", memberType)
		}
	}
	return memberFields, nil
}
