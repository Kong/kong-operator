package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sdkComponentsImportPath = "github.com/Kong/sdk-konnect-go/models/components"

func TestRequireUnionMembersMetadataField(t *testing.T) {
	t.Run("all members declare Labels", func(t *testing.T) {
		require.NoError(t, requireUnionMembersMetadataField(sdkComponentsImportPath, "CreateAIGatewayCustomPolicyRequest", false))
	})

	t.Run("member lacking the field is rejected", func(t *testing.T) {
		err := requireUnionMembersMetadataField(sdkComponentsImportPath, "CreateAIGatewayCustomPolicyRequest", true)
		require.ErrorContains(t, err, "has no Tags field")
	})
}

func TestResolveUpdateLabelsFieldPath_MultiMemberUnion(t *testing.T) {
	path, targets, err := resolveUpdateLabelsFieldPath(
		"AIGatewayCustomPolicy",
		&updateOpCallShape{
			ReqImportPath: sdkComponentsImportPath,
			ReqType:       "UpdateAIGatewayCustomPolicyRequest",
		},
		true,
		false,
	)
	require.NoError(t, err)
	assert.Empty(t, path)
	assert.Equal(t, []labelsUnionTarget{
		{
			Path:          "UpdateAIGatewayCustomPolicyInstalledRequest",
			Guard:         "req.UpdateAIGatewayCustomPolicyInstalledRequest != nil",
			ExpectedGuard: "expectedRequest.UpdateAIGatewayCustomPolicyInstalledRequest != nil",
		},
		{
			Path:          "UpdateAIGatewayCustomPolicyStreamingRequest",
			Guard:         "req.UpdateAIGatewayCustomPolicyStreamingRequest != nil",
			ExpectedGuard: "expectedRequest.UpdateAIGatewayCustomPolicyStreamingRequest != nil",
		},
	}, targets)
}

func TestResolveListItemLabelsVariantFields(t *testing.T) {
	t.Run("root-union list item returns its members", func(t *testing.T) {
		fields, err := resolveListItemLabelsVariantFields("ListAIGatewayCustomPoliciesResponse")
		require.NoError(t, err)
		assert.Equal(t, []string{"AIGatewayCustomPolicyInstalled", "AIGatewayCustomPolicyStreaming"}, fields)
	})

	t.Run("plain list item returns nil", func(t *testing.T) {
		fields, err := resolveListItemLabelsVariantFields("ListAIGatewayPoliciesResponse")
		require.NoError(t, err)
		assert.Nil(t, fields)
	})
}

func TestSDKStructFieldIsStringMap(t *testing.T) {
	t.Run("map[string]string labels", func(t *testing.T) {
		ok, err := sdkStructFieldIsStringMap("AIGatewayCustomPolicyInstalled", "Labels")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("non-map field", func(t *testing.T) {
		ok, err := sdkStructFieldIsStringMap("AIGatewayCustomPolicyInstalled", "Name")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("missing field", func(t *testing.T) {
		_, err := sdkStructFieldIsStringMap("AIGatewayCustomPolicyInstalled", "DoesNotExist")
		require.ErrorContains(t, err, "not found")
	})
}

func TestNewLabelsUnionTarget(t *testing.T) {
	testCases := []struct {
		name        string
		bodyField   string
		bodyPointer bool
		want        labelsUnionTarget
	}{
		{
			name: "unwrapped request",
			want: labelsUnionTarget{
				Path:          "Member",
				Guard:         "req.Member != nil",
				ExpectedGuard: "expectedRequest.Member != nil",
			},
		},
		{
			name:        "wrapped request with pointer body guards the body",
			bodyField:   "Body",
			bodyPointer: true,
			want: labelsUnionTarget{
				Path:          "Body.Member",
				Guard:         "req.Body != nil && req.Body.Member != nil",
				ExpectedGuard: "expectedRequest.Body != nil && expectedRequest.Body.Member != nil",
			},
		},
		{
			name:      "wrapped request with struct body does not nil-check the body",
			bodyField: "Body",
			want: labelsUnionTarget{
				Path:          "Body.Member",
				Guard:         "req.Body.Member != nil",
				ExpectedGuard: "expectedRequest.Body.Member != nil",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, newLabelsUnionTarget(tc.bodyField, tc.bodyPointer, "Member"))
		})
	}
}
