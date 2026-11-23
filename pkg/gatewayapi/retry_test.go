package gatewayapi

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestKongRetriesForHTTPRouteRule(t *testing.T) {
	tests := []struct {
		name        string
		retry       *gatewayv1.HTTPRouteRetry
		wantRetries *int64
		wantErr     string
	}{
		{
			name: "no retry stanza leaves Kong default untouched",
		},
		{
			name:        "retry stanza defaults attempts to one",
			retry:       &gatewayv1.HTTPRouteRetry{},
			wantRetries: new(int64(1)),
		},
		{
			name:        "explicit attempts are honored",
			retry:       &gatewayv1.HTTPRouteRetry{Attempts: new(3)},
			wantRetries: new(int64(3)),
		},
		{
			name:        "zero attempts disables retries",
			retry:       &gatewayv1.HTTPRouteRetry{Attempts: new(0)},
			wantRetries: new(int64(0)),
		},
		{
			name:        "attempts at Kong maximum are accepted",
			retry:       &gatewayv1.HTTPRouteRetry{Attempts: new(int(MaxKongServiceRetries))},
			wantRetries: new(MaxKongServiceRetries),
		},
		{
			name:    "retry codes are unsupported",
			retry:   &gatewayv1.HTTPRouteRetry{Codes: []gatewayv1.HTTPRouteRetryStatusCode{500}},
			wantErr: "retry.codes is unsupported",
		},
		{
			name:    "retry backoff is unsupported",
			retry:   &gatewayv1.HTTPRouteRetry{Backoff: new(gatewayv1.Duration("100ms"))},
			wantErr: "retry.backoff is unsupported",
		},
		{
			name:    "negative attempts are rejected",
			retry:   &gatewayv1.HTTPRouteRetry{Attempts: new(-1)},
			wantErr: "retry.attempts is unsupported",
		},
		{
			name:    "attempts above Kong maximum are rejected",
			retry:   &gatewayv1.HTTPRouteRetry{Attempts: new(int(MaxKongServiceRetries + 1))},
			wantErr: "must not exceed 32767",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := KongRetriesForHTTPRouteRule(gatewayv1.HTTPRouteRule{Retry: tt.retry})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantRetries, got)
		})
	}
}

func TestValidateHTTPRouteRetriesIncludesRuleIndex(t *testing.T) {
	rules := []gatewayv1.HTTPRouteRule{
		{Retry: &gatewayv1.HTTPRouteRetry{Attempts: new(2)}},
		{Retry: &gatewayv1.HTTPRouteRetry{Codes: []gatewayv1.HTTPRouteRetryStatusCode{503}}},
	}

	err := ValidateHTTPRouteRetries(rules)
	require.ErrorContains(t, err, "rules[1].retry.codes is unsupported")
	require.True(t, IsHTTPRouteRetryUnsupported(err))
}

func TestIsHTTPRouteRetryUnsupported(t *testing.T) {
	require.False(t, IsHTTPRouteRetryUnsupported(nil))
	require.False(t, IsHTTPRouteRetryUnsupported(errors.New("other")))

	_, err := KongRetriesForHTTPRouteRule(gatewayv1.HTTPRouteRule{
		Retry: &gatewayv1.HTTPRouteRetry{Backoff: new(gatewayv1.Duration("100ms"))},
	})
	require.Error(t, err)
	require.True(t, IsHTTPRouteRetryUnsupported(err))
}
