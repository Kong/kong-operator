package plugin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestTranslateGRPCRequestModifier(t *testing.T) {
	tests := []struct {
		name        string
		hf          *gatewayv1.HTTPHeaderFilter
		expected    transformerData
		expectError bool
	}{
		{
			name: "successful translation with all operations",
			hf: &gatewayv1.HTTPHeaderFilter{
				Add: []gatewayv1.HTTPHeader{
					{Name: "X-Add", Value: "add-val"},
				},
				Set: []gatewayv1.HTTPHeader{
					{Name: "X-Set", Value: "set-val"},
				},
				Remove: []string{"X-Remove"},
			},
			expected: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"X-Set:set-val"},
				},
				Append: transformerTargetSlice{
					Headers: []string{"X-Add:add-val"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"X-Set:set-val"},
					},
				},
				Remove: transformerTargetSlice{
					Headers: []string{"X-Remove"},
				},
			},
		},
		{
			name:        "nil RequestHeaderModifier",
			hf:          nil,
			expectError: true,
		},
		{
			name:        "empty RequestHeaderModifier",
			hf:          &gatewayv1.HTTPHeaderFilter{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateGRPCRequestModifier(tt.hf)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, transformerData{}, result)
				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected.Add.Headers, result.Add.Headers)
			assert.ElementsMatch(t, tt.expected.Append.Headers, result.Append.Headers)
			assert.ElementsMatch(t, tt.expected.Remove.Headers, result.Remove.Headers)
			assert.ElementsMatch(t, tt.expected.Replace.Headers, result.Replace.Headers)
		})
	}
}

func TestTranslateGRPCResponseModifier(t *testing.T) {
	tests := []struct {
		name        string
		hf          *gatewayv1.HTTPHeaderFilter
		expected    transformerData
		expectError bool
	}{
		{
			name: "successful translation with all operations",
			hf: &gatewayv1.HTTPHeaderFilter{
				Add: []gatewayv1.HTTPHeader{
					{Name: "X-Add", Value: "add-val"},
				},
				Set: []gatewayv1.HTTPHeader{
					{Name: "X-Set", Value: "set-val"},
				},
				Remove: []string{"X-Remove"},
			},
			expected: transformerData{
				Add:     transformerTargetSlice{Headers: []string{"X-Set:set-val"}},
				Append:  transformerTargetSlice{Headers: []string{"X-Add:add-val"}},
				Remove:  transformerTargetSlice{Headers: []string{"X-Remove"}},
				Replace: transformerTargetSliceReplace{transformerTargetSlice: transformerTargetSlice{Headers: []string{"X-Set:set-val"}}},
			},
		},
		{
			name:        "nil ResponseHeaderModifier",
			hf:          nil,
			expectError: true,
		},
		{
			name:        "empty ResponseHeaderModifier",
			hf:          &gatewayv1.HTTPHeaderFilter{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateGRPCResponseModifier(tt.hf)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, transformerData{}, result)
				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected.Add.Headers, result.Add.Headers)
			assert.ElementsMatch(t, tt.expected.Append.Headers, result.Append.Headers)
			assert.ElementsMatch(t, tt.expected.Remove.Headers, result.Remove.Headers)
			assert.ElementsMatch(t, tt.expected.Replace.Headers, result.Replace.Headers)
		})
	}
}

func TestTranslateGRPCFromFilter(t *testing.T) {
	tests := []struct {
		name          string
		filter        gatewayv1.GRPCRouteFilter
		expectedName  string
		expectedData  transformerData
		expectedError string
	}{
		{
			name: "RequestHeaderModifier maps to request-transformer",
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{{Name: "X-Api-Version", Value: "v2"}},
				},
			},
			expectedName: pluginRequestTransformer,
			expectedData: transformerData{Append: transformerTargetSlice{Headers: []string{"X-Api-Version:v2"}}},
		},
		{
			name: "ResponseHeaderModifier maps to response-transformer",
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{{Name: "X-Backend", Value: "echo"}},
				},
			},
			expectedName: pluginResponseTransformer,
			expectedData: transformerData{Append: transformerTargetSlice{Headers: []string{"X-Backend:echo"}}},
		},
		{
			name: "RequestMirror is not yet supported",
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterRequestMirror,
				RequestMirror: &gatewayv1.HTTPRequestMirrorFilter{
					BackendRef: gatewayv1.BackendObjectReference{
						Name: "mirror-svc",
					},
				},
			},
			expectedError: "unsupported filter type: RequestMirror",
		},
		{
			name: "unknown filter type falls into the same unsupported branch as RequestMirror",
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterType("Bogus"),
			},
			expectedError: "unsupported filter type: Bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confs, err := translateGRPCFromFilter(tt.filter)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)
			require.Len(t, confs, 1)
			assert.Equal(t, tt.expectedName, confs[0].name)

			var data transformerData
			require.NoError(t, json.Unmarshal(confs[0].config, &data))
			assert.Equal(t, tt.expectedData, data)
		})
	}
}
