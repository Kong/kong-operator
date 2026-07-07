package manager

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestIsSSAProviderNeeded(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "neither KEG DataPlane nor MCPServer enabled",
			cfg:  Config{},
			want: false,
		},
		{
			name: "KEG DataPlane controller enabled",
			cfg:  Config{KEGDataPlaneControllerEnabled: true},
			want: true,
		},
		{
			name: "MCPServer feature gate enabled",
			cfg:  Config{FeatureGates: FeatureGates{FeatureGateMCPServer: {}}},
			want: true,
		},
		{
			name: "both enabled",
			cfg: Config{
				KEGDataPlaneControllerEnabled: true,
				FeatureGates:                  FeatureGates{FeatureGateMCPServer: {}},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsSSAProviderNeeded(tc.cfg))
		})
	}
}

// TestBuildSSAProvider_error checks that buildSSAProvider surfaces errors
// from building the initial TypeConverter. rest.Config.Host here points at a
// port nothing listens on, so the API calls buildSSAProvider makes (fetching
// built-in schemas, listing CRDs) fail with a connection error,no real
// cluster needed to exercise this path.
func TestBuildSSAProvider_error(t *testing.T) {
	mgr, err := ctrl.NewManager(&rest.Config{Host: "http://127.0.0.1:0"}, ctrlmgr.Options{Scheme: managerscheme.Get()})
	require.NoError(t, err)

	_, err = buildSSAProvider(t.Context(), logr.Discard(), mgr)
	require.Error(t, err, "buildSSAProvider must fail when it cannot reach the API server")
	assert.Contains(t, err.Error(), "failed to build initial SSA TypeConverter")
}
