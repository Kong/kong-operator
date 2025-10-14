package test

import (
	"go/build"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConstructModulePath(t *testing.T) {
	tests := []struct {
		name       string
		moduleName string
		version    string
		expected   string
	}{
		{
			name:       "simple module",
			moduleName: "github.com/kong/kong-operator",
			version:    "v1.0.0",
			expected:   filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "kong", "kong-operator@v1.0.0"),
		},
		{
			name:       "simple module v2",
			moduleName: "github.com/kong/kong-operator/v2",
			version:    "v2.0.0",
			expected:   filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "kong", "kong-operator", "v2@v2.0.0"),
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
			result := ConstructModulePath(tt.moduleName, tt.version)
			require.Equal(t, tt.expected, result)
		})
	}
}
