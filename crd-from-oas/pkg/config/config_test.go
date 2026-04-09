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
		content := "apiGroupVersions:\n" +
			"  konnect.konghq.com/v1alpha1:\n" +
			"    types:\n" +
			"      - path: /v3/portals\n" +
			"        cel:\n" +
			"          name:\n" +
			"            _validations:\n" +
			"              - \"+kubebuilder:validation:MinLength=1\"\n" +
			"        ops:\n" +
			"          create:\n" +
			"            path: github.com/Kong/sdk-konnect-go/models/components.CreatePortal\n" +
			"          update:\n" +
			"            path: github.com/Kong/sdk-konnect-go/models/components.UpdatePortal\n" +
			"        funcs:\n" +
			"          GetKonnectStatus:\n" +
			"            returnType:\n" +
			"              package: github.com/kong/kong-operator/v2/api/konnect/v1alpha2\n" +
			"              alias: konnectv1alpha2\n" +
			"              type: KonnectEntityStatus\n" +
			"      - path: /v3/portals/{portalId}/teams\n" +
			"  gateway.konghq.com/v1beta1:\n" +
			"    types:\n" +
			"      - path: /v3/gateways\n"
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
		require.NotNil(t, konnect.Types[0].Funcs)
		require.Contains(t, konnect.Types[0].Funcs, "GetKonnectStatus")
		require.NotNil(t, konnect.Types[0].Funcs["GetKonnectStatus"].ReturnType)
		assert.Equal(t, "github.com/kong/kong-operator/v2/api/konnect/v1alpha2", konnect.Types[0].Funcs["GetKonnectStatus"].ReturnType.Package)
		assert.Equal(t, "konnectv1alpha2", konnect.Types[0].Funcs["GetKonnectStatus"].ReturnType.Alias)
		assert.Equal(t, "KonnectEntityStatus", konnect.Types[0].Funcs["GetKonnectStatus"].ReturnType.Type)
		assert.Equal(t, "/v3/portals/{portalId}/teams", konnect.Types[1].Path)
		assert.Nil(t, konnect.Types[1].CEL)
		assert.Nil(t, konnect.Types[1].Ops)
		assert.Nil(t, konnect.Types[1].Funcs)

		gateway := cfg.APIGroupVersions["gateway.konghq.com/v1beta1"]
		require.NotNil(t, gateway)
		require.Len(t, gateway.Types, 1)
		assert.Equal(t, "/v3/gateways", gateway.Types[0].Path)
	})

	t.Run("valid config with commonTypes import", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    commonTypes:
      objectRef:
        import:
          path: github.com/kong/kong-operator/v2/api/common/v1alpha1
          alias: commonv1alpha1
    types:
      - path: /v3/portals
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect)
		require.NotNil(t, konnect.CommonTypes)
		require.NotNil(t, konnect.CommonTypes.ObjectRef)
		assert.Nil(t, konnect.CommonTypes.ObjectRef.Generate, "generate should be nil when not specified")
		require.NotNil(t, konnect.CommonTypes.ObjectRef.Import)
		assert.Equal(t, "github.com/kong/kong-operator/v2/api/common/v1alpha1", konnect.CommonTypes.ObjectRef.Import.Path)
		assert.Equal(t, "commonv1alpha1", konnect.CommonTypes.ObjectRef.Import.Alias)
	})

	t.Run("valid config with commonTypes generate", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    commonTypes:
      objectRef:
        generate: true
    types:
      - path: /v3/portals
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect.CommonTypes)
		require.NotNil(t, konnect.CommonTypes.ObjectRef)
		require.NotNil(t, konnect.CommonTypes.ObjectRef.Generate)
		assert.True(t, bool(*konnect.CommonTypes.ObjectRef.Generate))
		assert.Nil(t, konnect.CommonTypes.ObjectRef.Import)
	})

	t.Run("valid config without commonTypes", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v3/portals
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		assert.Nil(t, konnect.CommonTypes)
	})

	t.Run("invalid: generate and import both set", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    commonTypes:
      objectRef:
        generate: true
        import:
          path: github.com/kong/kong-operator/v2/api/common/v1alpha1
    types:
      - path: /v3/portals
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		assert.ErrorContains(t, err, "generate and import are mutually exclusive")
	})

	t.Run("empty objectRef defaults to generate true", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    commonTypes:
      objectRef: {}
    types:
      - path: /v3/portals
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect.CommonTypes.ObjectRef)
		require.NotNil(t, konnect.CommonTypes.ObjectRef.Generate)
		assert.True(t, bool(*konnect.CommonTypes.ObjectRef.Generate))
	})

	t.Run("invalid: generate false without import", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    commonTypes:
      objectRef:
        generate: false
    types:
      - path: /v3/portals
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		assert.ErrorContains(t, err, "import is required when generate is false")
	})

	t.Run("invalid: import with empty path", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    commonTypes:
      objectRef:
        import:
          path: ""
    types:
      - path: /v3/portals
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		assert.ErrorContains(t, err, "path is required")
	})

	t.Run("invalid: funcs with unsupported method", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v3/portals
        funcs:
          UnknownFunc: {}
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		assert.ErrorContains(t, err, "funcs.UnknownFunc is not supported")
	})

	t.Run("invalid: funcs returnType missing package", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v3/portals
        funcs:
          GetKonnectStatus:
            returnType:
              type: KonnectEntityStatus
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		assert.ErrorContains(t, err, "funcs.GetKonnectStatus.returnType.package is required")
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

func TestLoadProjectConfig_NameOverride(t *testing.T) {
	content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways
        name: KonnectEventControlPlane
      - path: /v3/portals
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)

	konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
	require.NotNil(t, konnect)
	assert.Equal(t, "KonnectEventControlPlane", konnect.Types[0].Name)
	assert.Empty(t, konnect.Types[1].Name)
}

func TestAPIGroupVersionConfig_NameOverrides(t *testing.T) {
	t.Run("with overrides", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{Path: "/v1/event-gateways", Name: "KonnectEventControlPlane"},
				{Path: "/v3/portals"},
			},
		}
		overrides := agv.NameOverrides()
		assert.Equal(t, map[string]string{
			"/v1/event-gateways": "KonnectEventControlPlane",
		}, overrides)
	})

	t.Run("no overrides", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{Path: "/v3/portals"},
			},
		}
		overrides := agv.NameOverrides()
		assert.Empty(t, overrides)
	})

	t.Run("nil types", func(t *testing.T) {
		agv := &APIGroupVersionConfig{}
		overrides := agv.NameOverrides()
		assert.Empty(t, overrides)
	})
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
			"/v3/portals":                  "Portal",
			"/v3/portals/{portalId}/teams": "PortalTeam",
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

func TestAPIGroupVersionConfig_FuncsConfig(t *testing.T) {
	t.Run("with funcs configured", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/v3/portals",
					Funcs: map[string]*FuncConfig{
						"GetKonnectStatus": {
							ReturnType: &TypeRefConfig{
								Package: "github.com/kong/kong-operator/v2/api/konnect/v1alpha2",
								Alias:   "konnectv1alpha2",
								Type:    "KonnectEntityStatus",
							},
						},
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

		fc := agv.FuncsConfig(pathToEntity)
		require.Len(t, fc, 1)
		require.Contains(t, fc, "Portal")
		require.Contains(t, fc["Portal"].Funcs, "GetKonnectStatus")
		assert.Equal(t, "KonnectEntityStatus", fc["Portal"].Funcs["GetKonnectStatus"].ReturnType.Type)
		assert.NotContains(t, fc, "PortalTeam")
	})

	t.Run("no funcs configured", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{{Path: "/v3/portals"}},
		}

		fc := agv.FuncsConfig(map[string]string{"/v3/portals": "Portal"})
		assert.Empty(t, fc)
	})
}
