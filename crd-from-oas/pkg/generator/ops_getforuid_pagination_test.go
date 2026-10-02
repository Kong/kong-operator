package generator

import (
	"go/format"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveListPagination(t *testing.T) {
	testCases := []struct {
		name               string
		listMethod         string
		listResponseType   string
		positional         bool
		parents            int
		expectedPagination string
		expectedMetaIsPage bool
		expectedErr        string
	}{
		{
			name:               "cursor",
			listMethod:         "ListEventGateways",
			listResponseType:   "ListEventGatewaysResponse",
			expectedPagination: listPaginationCursor,
		},
		{
			name:               "cursor with a meta that is the page itself",
			listMethod:         "ListPortalIPAllowList",
			listResponseType:   "PortalSourceIPRestrictionPaginatedResponse",
			parents:            1,
			expectedPagination: listPaginationCursor,
			expectedMetaIsPage: true,
		},
		{
			name:               "page number",
			listMethod:         "ListPortals",
			listResponseType:   "ListPortalsResponse",
			expectedPagination: listPaginationNumber,
		},
		{
			name:               "page number, positional",
			listMethod:         "ListAiGateways",
			listResponseType:   "ListAIGatewaysResponse",
			positional:         true,
			expectedPagination: listPaginationNumber,
		},
		{
			name:               "not paginated",
			listMethod:         "ListEventGatewayListenerPolicies",
			listResponseType:   "ListEventGatewayListenerPoliciesResponse",
			parents:            2,
			expectedPagination: listPaginationNone,
		},
		{
			name:               "positional without a request struct",
			listMethod:         "ListNoSuchEntities",
			listResponseType:   "ListNoSuchEntitiesResponse",
			positional:         true,
			expectedPagination: listPaginationNone,
		},
		{
			name:             "request struct not found",
			listMethod:       "ListNoSuchEntities",
			listResponseType: "ListNoSuchEntitiesResponse",
			expectedErr:      "not found",
		},
		{
			name:             "response meta does not match the request's pagination",
			listMethod:       "ListEventGateways",
			listResponseType: "ListPortalsResponse",
			expectedErr:      `has meta "PaginatedMeta"`,
		},
		{
			name:             "positional with cursor pagination",
			listMethod:       "ListEventGateways",
			listResponseType: "ListEventGatewaysResponse",
			positional:       true,
			expectedErr:      "only supported with page[size] and page[number]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pagination, metaIsPage, err := resolveListPagination(tc.listMethod, tc.listResponseType, tc.positional, tc.parents)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedPagination, pagination)
			assert.Equal(t, tc.expectedMetaIsPage, metaIsPage)
		})
	}
}

// TestOpsGetForUIDFuncTemplate_Pagination checks that the generated lookup
// requests every page of a paginated list, and only the one response of a
// list that is not paginated.
func TestOpsGetForUIDFuncTemplate_Pagination(t *testing.T) {
	base := func() opsGetForUIDFuncData {
		return opsGetForUIDFuncData{
			Entity:                "Widget",
			APIAlias:              "konnectv1alpha1",
			ListSDKInterface:      "WidgetsSDK",
			ListSDKMethod:         "ListWidgets",
			ListResponseField:     "ListWidgetsResponse",
			ListResponseItemsExpr: "resp.ListWidgetsResponse.Data",
			ListResponseNilCheck:  "resp == nil || resp.ListWidgetsResponse == nil",
			HasLabels:             true,
		}
	}

	testCases := []struct {
		name        string
		data        func() opsGetForUIDFuncData
		contains    []string
		notContains []string
	}{
		{
			name: "cursor",
			data: func() opsGetForUIDFuncData {
				d := base()
				d.ListPagination = listPaginationCursor
				return d
			},
			contains: []string{
				"PageSize:  new(listPageSize),",
				"PageAfter: pageAfter,",
				"page := meta.GetPage()",
				"nextPageCursor(page.GetNext(), seenCursors)",
				"if pageAfter == nil {",
			},
		},
		{
			name: "cursor with a meta that is the page itself",
			data: func() opsGetForUIDFuncData {
				d := base()
				d.ListPagination = listPaginationCursor
				d.ListMetaIsPage = true
				return d
			},
			contains:    []string{"nextPageCursor(meta.GetNext(), seenCursors)"},
			notContains: []string{"meta.GetPage()"},
		},
		{
			name: "page number",
			data: func() opsGetForUIDFuncData {
				d := base()
				d.ListPagination = listPaginationNumber
				return d
			},
			contains: []string{
				"for pageNumber := int64(1); ; pageNumber++ {",
				"PageNumber: new(pageNumber),",
				"hasNextNumberedPage(pageNumber, meta.GetPage(), len(resp.ListWidgetsResponse.Data))",
			},
		},
		{
			name: "page number, positional",
			data: func() opsGetForUIDFuncData {
				d := base()
				d.ListPagination = listPaginationNumber
				d.ListCallStylePositional = true
				return d
			},
			contains: []string{"sdk.ListWidgets(ctx, new(listPageSize), new(pageNumber))"},
		},
		{
			name: "match fields, cursor",
			data: func() opsGetForUIDFuncData {
				d := base()
				d.HasLabels = false
				d.MatchFields = []opsGetForUIDMatchFieldData{{ObjectField: "Spec.APISpec.Name", ResponseField: "Name"}}
				d.ListPagination = listPaginationCursor
				return d
			},
			contains: []string{"PageAfter: pageAfter,", "nextPageCursor(page.GetNext(), seenCursors)"},
		},
		{
			name: "not paginated",
			data: base,
			contains: []string{
				"sdk.ListWidgets(ctx, sdkkonnectops.ListWidgetsRequest{})",
			},
			notContains: []string{"PageSize", "pageAfter", "pageNumber", "for {"},
		},
	}

	tmpl := template.Must(template.New("opsgetforuidfunc").Parse(opsGetForUIDFuncTemplate))
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			require.NoError(t, tmpl.Execute(&out, tc.data()))
			content := out.String()

			formatted, err := format.Source([]byte("package ops\n" + content))
			require.NoError(t, err, content)
			for _, s := range tc.contains {
				assert.Contains(t, string(formatted), s)
			}
			for _, s := range tc.notContains {
				assert.NotContains(t, string(formatted), s)
			}
			assert.NotContains(t, string(formatted), "only the first page")
		})
	}
}
