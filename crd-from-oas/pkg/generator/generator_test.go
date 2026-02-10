package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractVariantNames(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty input",
			input:    []string{},
			expected: nil,
		},
		{
			name:     "single variant - removes common prefixes/suffixes",
			input:    []string{"CreateDcrProviderRequestAuth0"},
			expected: []string{"DcrProviderRequestAuth0"}, // Request not removed since it's not at the end
		},
		{
			name:     "identity provider variants - OIDC and SAML",
			input:    []string{"ConfigureOIDCIdentityProviderConfig", "SAMLIdentityProviderConfig"},
			expected: []string{"OIDC", "SAML"},
		},
		{
			name:     "dcr provider variants - multiple providers",
			input:    []string{"CreateDcrProviderRequestAuth0", "CreateDcrProviderRequestAzureAd", "CreateDcrProviderRequestCurity", "CreateDcrProviderRequestOkta", "CreateDcrProviderRequestHttp"},
			expected: []string{"Auth0", "AzureAd", "Curity", "Okta", "Http"},
		},
		{
			name:     "common prefix only",
			input:    []string{"ConfigTypeA", "ConfigTypeB"},
			expected: []string{"A", "B"},
		},
		{
			name:     "common suffix only",
			input:    []string{"AConfig", "BConfig"},
			expected: []string{"A", "B"},
		},
		{
			name:     "no common prefix or suffix - common suffix 'a' is too short",
			input:    []string{"Alpha", "Beta"},
			expected: []string{"Alph", "Bet"}, // common suffix is "a" so it gets trimmed
		},
		{
			name:     "variants with Configure prefix",
			input:    []string{"ConfigureAuth", "ConfigureSAML"},
			expected: []string{"Auth", "SAML"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractVariantNames(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{
			name:     "identical strings",
			a:        "hello",
			b:        "hello",
			expected: "hello",
		},
		{
			name:     "common prefix",
			a:        "CreateDcrProviderRequestAuth0",
			b:        "CreateDcrProviderRequestAzureAd",
			expected: "CreateDcrProviderRequestA",
		},
		{
			name:     "no common prefix",
			a:        "alpha",
			b:        "beta",
			expected: "",
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: "",
		},
		{
			name:     "one empty string",
			a:        "hello",
			b:        "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := commonPrefix(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCommonSuffix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{
			name:     "identical strings",
			a:        "hello",
			b:        "hello",
			expected: "hello",
		},
		{
			name:     "common suffix",
			a:        "ConfigureOIDCIdentityProviderConfig",
			b:        "SAMLIdentityProviderConfig",
			expected: "IdentityProviderConfig",
		},
		{
			name:     "no common suffix",
			a:        "alpha",
			b:        "beta",
			expected: "a",
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: "",
		},
		{
			name:     "one empty string",
			a:        "hello",
			b:        "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := commonSuffix(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCleanSingleVariantName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes Config suffix",
			input:    "SomethingConfig",
			expected: "Something",
		},
		{
			name:     "removes Configuration suffix",
			input:    "SomethingConfiguration",
			expected: "Something",
		},
		{
			name:     "removes Provider suffix",
			input:    "SomethingProvider",
			expected: "Something",
		},
		{
			name:     "removes Request suffix",
			input:    "SomethingRequest",
			expected: "Something",
		},
		{
			name:     "removes Configure prefix",
			input:    "ConfigureSomething",
			expected: "Something",
		},
		{
			name:     "removes Create prefix",
			input:    "CreateSomething",
			expected: "Something",
		},
		{
			name:     "removes Update prefix",
			input:    "UpdateSomething",
			expected: "Something",
		},
		{
			name:     "removes multiple prefixes/suffixes",
			input:    "CreateSomethingRequest",
			expected: "Something",
		},
		{
			name:     "no prefixes or suffixes to remove",
			input:    "Something",
			expected: "Something",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := cleanSingleVariantName(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
