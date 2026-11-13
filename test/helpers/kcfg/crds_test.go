package kcfg

import (
	"go/build"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayAPIVersion(t *testing.T) {
	t.Run("defaults to the go.mod pinned version", func(t *testing.T) {
		version, err := gatewayAPIVersion()
		require.NoError(t, err)
		require.NotEmpty(t, version)
	})

	t.Run("env override takes precedence", func(t *testing.T) {
		t.Setenv(envGatewayAPIVersionOverride, "v1.2.1")
		version, err := gatewayAPIVersion()
		require.NoError(t, err)
		require.Equal(t, "v1.2.1", version)
	})

	t.Run("env override must start with 'v'", func(t *testing.T) {
		t.Setenv(envGatewayAPIVersionOverride, "1.2.1")
		_, err := gatewayAPIVersion()
		require.ErrorContains(t, err, "must start with 'v'")
	})
}

func TestGatewayAPICRDsPath(t *testing.T) {
	t.Run("defaults to the experimental channel", func(t *testing.T) {
		t.Setenv(envGatewayAPICRDsChannel, "experimental")
		require.Equal(t, GatewayAPIExperimentalCRDsPath(), GatewayAPICRDsPath())
	})

	t.Run("standard channel", func(t *testing.T) {
		t.Setenv(envGatewayAPICRDsChannel, "standard")
		require.Equal(t, GatewayAPIStandardCRDsPath(), GatewayAPICRDsPath())
	})
}

func TestConstructModulePath(t *testing.T) {
	tests := []struct {
		name       string
		moduleName string
		version    string
		expected   string
	}{
		{
			name:       "simple module",
			moduleName: "github.com/kong/example-module",
			version:    "v1.0.0",
			expected:   filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "kong", "example-module@v1.0.0"),
		},
		{
			name:       "simple module v2",
			moduleName: "github.com/kong/example-module/v2",
			version:    "v2.0.0",
			expected:   filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "kong", "example-module", "v2@v2.0.0"),
		},
		{
			name:       "module with subdomain",
			moduleName: "sigs.k8s.io/controller-runtime",
			version:    "v0.15.0",
			expected:   filepath.Join(build.Default.GOPATH, "pkg", "mod", "sigs.k8s.io", "controller-runtime@v0.15.0"),
		},
		{
			name:       "prerelease version",
			moduleName: "github.com/test/module",
			version:    "v1.0.0-alpha.1",
			expected:   filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "test", "module@v1.0.0-alpha.1"),
		},
		{
			name:       "prerelease version v2",
			moduleName: "github.com/test/module/v2",
			version:    "v2.0.0-alpha.1",
			expected:   filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "test", "module", "v2@v2.0.0-alpha.1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructModulePath(tt.moduleName, tt.version)
			require.Equal(t, tt.expected, result)
		})
	}
}
