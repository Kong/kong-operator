package asserts

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/test/helpers"
)

// Expect404WithNoRouteFunc is used to check whether a given URL responds
// with 404 and a standard Kong no route message.
func Expect404WithNoRouteFunc(t *testing.T, ctx context.Context, url string) func() bool {
	t.Helper()

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	return func() bool {
		t.Logf("verifying connectivity to the dataplane %v", url)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Logf("failed creating request for %s: %v", url, err)
			return false
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Logf("failed issuing HTTP GET for %s: %v", url, err)
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Logf("expected 404 got %d from HTTP GET for %s: %v", resp.StatusCode, url, err)
			return false
		}
		var proxyResponse struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&proxyResponse); err != nil {
			t.Logf("failed decoding HTTP GET response from %s: %v", url, err)
			return false
		}

		const expected = "no Route matched with those values"
		if expected != proxyResponse.Message {
			t.Logf("expected %s got in HTTP GET response from %s", expected, url)
			return false
		}
		return true
	}
}
