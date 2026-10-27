package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func TestPluginOrderingToSDKOrdering(t *testing.T) {
	tests := []struct {
		name        string
		ordering    *kong.PluginOrdering
		expected    *sdkkonnectcomp.Ordering
		expectedErr string
	}{
		{
			name:     "nil ordering",
			ordering: nil,
			expected: nil,
		},
		{
			name:     "empty ordering",
			ordering: &kong.PluginOrdering{},
			expected: nil,
		},
		{
			name: "empty access lists",
			ordering: &kong.PluginOrdering{
				Before: kong.PluginOrderingPhase{"access": {}},
				After:  kong.PluginOrderingPhase{"access": nil},
			},
			expected: nil,
		},
		{
			name: "before only",
			ordering: &kong.PluginOrdering{
				Before: kong.PluginOrderingPhase{"access": {"key-auth"}},
			},
			expected: &sdkkonnectcomp.Ordering{
				Before: &sdkkonnectcomp.Before{Access: []string{"key-auth"}},
			},
		},
		{
			name: "after only",
			ordering: &kong.PluginOrdering{
				After: kong.PluginOrderingPhase{"access": {"rate-limiting", "acl"}},
			},
			expected: &sdkkonnectcomp.Ordering{
				After: &sdkkonnectcomp.After{Access: []string{"rate-limiting", "acl"}},
			},
		},
		{
			name: "before and after",
			ordering: &kong.PluginOrdering{
				Before: kong.PluginOrderingPhase{"access": {"key-auth"}},
				After:  kong.PluginOrderingPhase{"access": {"rate-limiting"}},
			},
			expected: &sdkkonnectcomp.Ordering{
				Before: &sdkkonnectcomp.Before{Access: []string{"key-auth"}},
				After:  &sdkkonnectcomp.After{Access: []string{"rate-limiting"}},
			},
		},
		{
			name: "non-access phase in before",
			ordering: &kong.PluginOrdering{
				Before: kong.PluginOrderingPhase{"access": {"key-auth"}, "header_filter": {"cors"}},
			},
			expectedErr: `plugin ordering phase "header_filter" is not supported by Konnect`,
		},
		{
			name: "non-access phase in after",
			ordering: &kong.PluginOrdering{
				After: kong.PluginOrderingPhase{"rewrite": {"cors"}},
			},
			expectedErr: `plugin ordering phase "rewrite" is not supported by Konnect`,
		},
		{
			name: "empty non-access phase is ignored",
			ordering: &kong.PluginOrdering{
				Before: kong.PluginOrderingPhase{"access": {"key-auth"}, "header_filter": {}},
			},
			expected: &sdkkonnectcomp.Ordering{
				Before: &sdkkonnectcomp.Before{Access: []string{"key-auth"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pluginOrderingToSDKOrdering(tt.ordering)
			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestOrderingsEqual(t *testing.T) {
	tests := []struct {
		name     string
		expected *sdkkonnectcomp.Ordering
		actual   *sdkkonnectcomp.Ordering
		equal    bool
	}{
		{
			name:  "both nil",
			equal: true,
		},
		{
			name:   "nil and empty",
			actual: &sdkkonnectcomp.Ordering{Before: &sdkkonnectcomp.Before{Access: []string{}}},
			equal:  true,
		},
		{
			name:     "same entries in different order",
			expected: &sdkkonnectcomp.Ordering{Before: &sdkkonnectcomp.Before{Access: []string{"key-auth", "acl"}}},
			actual:   &sdkkonnectcomp.Ordering{Before: &sdkkonnectcomp.Before{Access: []string{"acl", "key-auth"}}},
			equal:    true,
		},
		{
			name:     "different entries",
			expected: &sdkkonnectcomp.Ordering{Before: &sdkkonnectcomp.Before{Access: []string{"key-auth"}}},
			actual:   &sdkkonnectcomp.Ordering{Before: &sdkkonnectcomp.Before{Access: []string{"acl"}}},
			equal:    false,
		},
		{
			name:     "set vs nil",
			expected: &sdkkonnectcomp.Ordering{After: &sdkkonnectcomp.After{Access: []string{"key-auth"}}},
			equal:    false,
		},
		{
			name:     "before vs after swapped",
			expected: &sdkkonnectcomp.Ordering{Before: &sdkkonnectcomp.Before{Access: []string{"key-auth"}}},
			actual:   &sdkkonnectcomp.Ordering{After: &sdkkonnectcomp.After{Access: []string{"key-auth"}}},
			equal:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.equal, orderingsEqual(tt.expected, tt.actual))
		})
	}
}

func TestKongPluginWithTargetsToKongPluginInput(t *testing.T) {
	binding := &configurationv1alpha1.KongPluginBinding{
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			Scope: configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane,
		},
	}

	tests := []struct {
		name             string
		ordering         *kong.PluginOrdering
		expectedOrdering *sdkkonnectcomp.Ordering
		expectedErr      string
	}{
		{
			name:             "no ordering",
			expectedOrdering: nil,
		},
		{
			name: "access ordering",
			ordering: &kong.PluginOrdering{
				Before: kong.PluginOrderingPhase{"access": {"key-auth"}},
			},
			expectedOrdering: &sdkkonnectcomp.Ordering{
				Before: &sdkkonnectcomp.Before{Access: []string{"key-auth"}},
			},
		},
		{
			name: "non-access ordering",
			ordering: &kong.PluginOrdering{
				Before: kong.PluginOrderingPhase{"header_filter": {"key-auth"}},
			},
			expectedErr: "invalid ordering in KongPlugin default/plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &configurationv1.KongPlugin{
				Name:       "plugin",
				Namespace:  "default",
				PluginName: "request-termination",
				Ordering:   tt.ordering,
			}

			got, err := kongPluginWithTargetsToKongPluginInput(binding, plugin, nil, nil)
			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "request-termination", got.Name)
			assert.Equal(t, tt.expectedOrdering, got.Ordering)
		})
	}
}
