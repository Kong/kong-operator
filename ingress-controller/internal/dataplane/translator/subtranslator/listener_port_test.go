package subtranslator

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
)

func TestListenerPortMatcher(t *testing.T) {
	require.Empty(t, listenerPortMatcher(nil).Expression())
	require.Equal(t,
		`http.headers.x_kong_listener_port == "80"`,
		listenerPortMatcher([]int32{80}).Expression(),
	)
	require.Equal(t,
		`(http.headers.x_kong_listener_port == "80") || (http.headers.x_kong_listener_port == "8080")`,
		listenerPortMatcher([]int32{80, 8080}).Expression(),
	)
}

func TestExpressionUsesListenerPort(t *testing.T) {
	require.True(t, ExpressionUsesListenerPort(`(http.path == "/") && (http.headers.x_kong_listener_port == "80")`))
	require.False(t, ExpressionUsesListenerPort(`http.path == "/"`))
}

func TestListenerPortPlugin(t *testing.T) {
	t.Run("no global pre-function plugin yields a new one", func(t *testing.T) {
		existing := []kongstate.Plugin{
			// Not global.
			{Name: new("pre-function"), Route: &kong.Route{ID: new("r")}},
			// Global, but a different plugin.
			{Name: new("cors")},
		}
		plugin, i := ListenerPortPlugin(existing)
		require.Equal(t, -1, i)
		require.Equal(t, "pre-function", *plugin.Name)
		require.Nil(t, plugin.Route)
		require.Equal(t, []any{listenerPortRewriteCode}, plugin.Config["rewrite"])
		require.Equal(t, []any{listenerPortAccessCode}, plugin.Config["access"])
	})

	t.Run("existing global pre-function plugin gets the code prepended", func(t *testing.T) {
		existing := []kongstate.Plugin{
			{Name: new("cors")},
			{
				Name: new("pre-function"),
				Config: kong.Configuration{
					"rewrite":       []any{"user_rewrite()"},
					"header_filter": []any{"user_header_filter()"},
				}},
		}
		plugin, i := ListenerPortPlugin(existing)
		require.Equal(t, 1, i)
		require.Equal(t, []any{listenerPortRewriteCode, "user_rewrite()"}, plugin.Config["rewrite"])
		require.Equal(t, []any{listenerPortAccessCode}, plugin.Config["access"])
		require.Equal(t, []any{"user_header_filter()"}, plugin.Config["header_filter"])
		// The original plugin is left intact.
		require.Equal(t, []any{"user_rewrite()"}, existing[1].Config["rewrite"])
		require.NotContains(t, existing[1].Config, "access")
	})
}
