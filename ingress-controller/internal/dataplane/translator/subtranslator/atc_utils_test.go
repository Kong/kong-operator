package subtranslator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostMatcherFromHosts(t *testing.T) {
	testCases := []struct {
		name       string
		hosts      []string
		expression string
	}{
		{
			name:       "simple exact host",
			hosts:      []string{"a.example.com"},
			expression: `http.host == "a.example.com"`,
		},
		{
			name:       "single wildcard host",
			hosts:      []string{"*.example.com"},
			expression: `http.host =^ ".example.com"`,
		},
		{
			name:       "multiple hosts with mixture of exact and wildcard",
			hosts:      []string{"foo.com", "*.bar.com"},
			expression: `(http.host == "foo.com") || (http.host =^ ".bar.com")`,
		},
		{
			name:       "multiple hosts including invalid host",
			hosts:      []string{"foo.com", "a..bar.com"},
			expression: `http.host == "foo.com"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matcher := hostMatcherFromHosts(tc.hosts)
			require.Equal(t, tc.expression, matcher.Expression())
		})
	}
}

func TestSNIMatcherFromSNIs(t *testing.T) {
	testCases := []struct {
		name       string
		snis       []string
		expression string
	}{
		{
			name:       "single SNI",
			snis:       []string{"konghq.com"},
			expression: `tls.sni == "konghq.com"`,
		},
		{
			name:       "multiple SNIs",
			snis:       []string{"docs.konghq.com", "apis.konghq.com"},
			expression: `(tls.sni == "docs.konghq.com") || (tls.sni == "apis.konghq.com")`,
		},
		{
			name:       "multiple SNIs with wildcard SNI,",
			snis:       []string{"foo.com", "*.bar.com"},
			expression: `(tls.sni == "foo.com") || (tls.sni =^ ".bar.com")`,
		},
		{
			name:       "multiple SNIs with invalid SNI",
			snis:       []string{"foo.com", "a..bar.com"},
			expression: `tls.sni == "foo.com"`,
		},
	}

	for _, tc := range testCases {
		matcher := sniMatcherFromSNIs(tc.snis)
		require.Equal(t, tc.expression, matcher.Expression())
	}
}
