package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProjectConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v3/portals
        cel:
          name:
            _validations:
              - "+kubebuilder:validation:MinLength=1"
        ops:
          create:
            path: github.com/Kong/sdk-konnect-go/models/components.CreatePortal
          update:
            path: github.com/Kong/sdk-konnect-go/models/components.UpdatePortal
      - path: /v3/portals/{portalId}/teams
  gateway.konghq.com/v1beta1:
    types:
      - path: /v3/gateways
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)
		require.Len(t, cfg.APIGroupVersions, 2)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect)
		require.Len(t, konnect.Types, 2)
		assert.Equal(t, "/v3/portals", konnect.Types[0].Path)
		require.NotNil(t, konnect.Types[0].CEL)
		assert.Contains(t, konnect.Types[0].CEL, "name")
		require.NotNil(t, konnect.Types[0].Ops)
		assert.Len(t, konnect.Types[0].Ops, 2)
		assert.Equal(t, "github.com/Kong/sdk-konnect-go/models/components.CreatePortal", konnect.Types[0].Ops["create"].Path)
		assert.Equal(t, "github.com/Kong/sdk-konnect-go/models/components.UpdatePortal", konnect.Types[0].Ops["update"].Path)
		assert.Equal(t, "/v3/portals/{portalId}/teams", konnect.Types[1].Path)
		assert.Nil(t, konnect.Types[1].CEL)
		assert.Nil(t, konnect.Types[1].Ops)

		gateway := cfg.APIGroupVersions["gateway.konghq.com/v1beta1"]
		require.NotNil(t, gateway)
		require.Len(t, gateway.Types, 1)
		assert.Equal(t, "/v3/gateways", gateway.Types[0].Path)
	})

	t.Run("missing apiGroupVersions", func(t *testing.T) {
		content := `
someOtherKey: value
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		assert.ErrorContains(t, err, "apiGroupVersions")
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadProjectConfig("/nonexistent/config.yaml")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "failed to read config file")
	})
}

func TestParseAPIGroupVersion(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantGroup string
		wantVer   string
		wantErr   bool
	}{
		{
			name:      "valid group-version",
			input:     "konnect.konghq.com/v1alpha1",
			wantGroup: "konnect.konghq.com",
			wantVer:   "v1alpha1",
		},
		{
			name:      "simple group-version",
			input:     "apps/v1",
			wantGroup: "apps",
			wantVer:   "v1",
		},
		{
			name:    "no slash",
			input:   "konnect.konghq.com",
			wantErr: true,
		},
		{
			name:    "empty group",
			input:   "/v1alpha1",
			wantErr: true,
		},
		{
			name:    "empty version",
			input:   "konnect.konghq.com/",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			group, version, err := ParseAPIGroupVersion(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantGroup, group)
			assert.Equal(t, tc.wantVer, version)
		})
	}
}

func TestParseSDKTypePath(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantImport string
		wantType   string
		wantErr    bool
	}{
		{
			name:       "valid SDK type path",
			input:      "github.com/Kong/sdk-konnect-go/models/components.CreatePortal",
			wantImport: "github.com/Kong/sdk-konnect-go/models/components",
			wantType:   "CreatePortal",
		},
		{
			name:       "valid path with nested packages",
			input:      "github.com/Kong/sdk-konnect-go/models/operations.ListPortals",
			wantImport: "github.com/Kong/sdk-konnect-go/models/operations",
			wantType:   "ListPortals",
		},
		{
			name:    "no dot separator",
			input:   "noDotAtAll",
			wantErr: true,
		},
		{
			name:    "leading dot",
			input:   ".CreatePortal",
			wantErr: true,
		},
		{
			name:    "trailing dot",
			input:   "github.com/Kong/sdk-konnect-go/models/components.",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			importPath, typeName, err := ParseSDKTypePath(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantImport, importPath)
			assert.Equal(t, tc.wantType, typeName)
		})
	}
}

func TestAPIGroupVersionConfig_GetPaths(t *testing.T) {
	agv := &APIGroupVersionConfig{
		Types: []*TypeConfig{
			{Path: "/v3/portals"},
			{Path: "/v3/portals/{portalId}/teams"},
		},
	}
	assert.Equal(t, []string{"/v3/portals", "/v3/portals/{portalId}/teams"}, agv.GetPaths())
}

func TestAPIGroupVersionConfig_FieldConfig(t *testing.T) {
	t.Run("with cel validations", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/v3/portals",
					CEL: map[string]*FieldConfig{
						"name": {Validations: []string{"+required"}},
					},
				},
				{
					Path: "/v3/portals/{portalId}/teams",
				},
			},
		}

		pathToEntity := map[string]string{
			"/v3/portals":                    "Portal",
			"/v3/portals/{portalId}/teams":   "PortalTeam",
		}

		fc := agv.FieldConfig(pathToEntity)
		require.NotNil(t, fc)
		assert.Equal(t, []string{"+required"}, fc.GetFieldValidations("Portal", "name"))
		assert.Nil(t, fc.GetFieldValidations("PortalTeam", "name"))
	})

	t.Run("no cel validations", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{Path: "/v3/portals"},
			},
		}

		fc := agv.FieldConfig(map[string]string{"/v3/portals": "Portal"})
		require.NotNil(t, fc)
		assert.Empty(t, fc.Entities)
	})

	t.Run("nil types", func(t *testing.T) {
		agv := &APIGroupVersionConfig{}

		fc := agv.FieldConfig(nil)
		require.NotNil(t, fc)
		assert.Empty(t, fc.Entities)
	})
}

func TestAPIGroupVersionConfig_OpsConfig(t *testing.T) {
	t.Run("with ops configured", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/v3/portals",
					Ops: map[string]*OpConfig{
						"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"},
						"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdatePortal"},
					},
				},
				{
					Path: "/v3/portals/{portalId}/teams",
				},
			},
		}

		pathToEntity := map[string]string{
			"/v3/portals":                  "Portal",
			"/v3/portals/{portalId}/teams": "PortalTeam",
		}

		oc := agv.OpsConfig(pathToEntity)
		require.Len(t, oc, 1)
		require.Contains(t, oc, "Portal")
		assert.Len(t, oc["Portal"].Ops, 2)
		assert.Equal(t, "github.com/Kong/sdk-konnect-go/models/components.CreatePortal", oc["Portal"].Ops["create"].Path)
		assert.Equal(t, "github.com/Kong/sdk-konnect-go/models/components.UpdatePortal", oc["Portal"].Ops["update"].Path)
		assert.NotContains(t, oc, "PortalTeam")
	})

	t.Run("no ops configured", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{Path: "/v3/portals"},
			},
		}

		oc := agv.OpsConfig(map[string]string{"/v3/portals": "Portal"})
		assert.Empty(t, oc)
	})

	t.Run("nil types", func(t *testing.T) {
		agv := &APIGroupVersionConfig{}

		oc := agv.OpsConfig(nil)
		assert.Empty(t, oc)
	})
}
