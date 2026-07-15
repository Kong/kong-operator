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
		assert.Equal(t, "/v3/portals/{portalId}/teams", konnect.Types[1].Path)
		assert.Nil(t, konnect.Types[1].CEL)
		assert.Nil(t, konnect.Types[1].Ops)

		gateway := cfg.APIGroupVersions["gateway.konghq.com/v1beta1"]
		require.NotNil(t, gateway)
		require.Len(t, gateway.Types, 1)
		assert.Equal(t, "/v3/gateways", gateway.Types[0].Path)
	})

	t.Run("valid config with per-type schema field omissions", func(t *testing.T) {
		content := `
apiGroupVersions:
  configuration.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways/{gatewayId}/virtual-clusters/{virtualClusterId}/consume-policies
        schemaFieldOmissions:
          EventGatewayModifyHeadersPolicyCreate:
            - parentPolicyID
          EventGatewaySkipRecordPolicyCreate:
            - parentPolicyID
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		configuration := cfg.APIGroupVersions["configuration.konghq.com/v1alpha1"]
		require.NotNil(t, configuration)
		require.Len(t, configuration.Types, 1)
		require.NotNil(t, configuration.Types[0].SchemaFieldOmissions)
		assert.Equal(t, []string{"parentPolicyID"}, configuration.Types[0].SchemaFieldOmissions["EventGatewayModifyHeadersPolicyCreate"])
		assert.Equal(t, []string{"parentPolicyID"}, configuration.Types[0].SchemaFieldOmissions["EventGatewaySkipRecordPolicyCreate"])
	})

	t.Run("valid config with ops requireClient", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways/{gatewayId}/data-plane-certificates
        secretReferences:
          - path: spec.apiSpec.certificate
            type: Secret
        ops:
          requireClient: true
          create:
            path: github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect)
		require.Len(t, konnect.Types, 1)
		require.Len(t, konnect.Types[0].SecretReferences, 1)
		assert.Equal(t, "spec.apiSpec.certificate", konnect.Types[0].SecretReferences[0].Path)
		assert.Equal(t, "Secret", konnect.Types[0].SecretReferences[0].Type)
		assert.True(t, konnect.Types[0].OpsRequireClient)
		require.NotNil(t, konnect.Types[0].Ops)
		assert.Equal(t,
			"github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest",
			konnect.Types[0].Ops["create"].Path,
		)
	})

	t.Run("valid config with delete asPUT", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v3/portals/{portalId}/customization
        ops:
          create:
            path: github.com/Kong/sdk-konnect-go/models/components.PortalCustomization
          update:
            path: github.com/Kong/sdk-konnect-go/models/components.PortalCustomization
          delete:
            asPUT: true
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect)
		require.Len(t, konnect.Types, 1)
		require.NotNil(t, konnect.Types[0].Ops)
		require.NotNil(t, konnect.Types[0].Ops["delete"])
		assert.True(t, konnect.Types[0].Ops["delete"].AsPUT)
	})

	t.Run("invalid config with delete asPUT but no update path", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v3/portals/{portalId}/customization
        ops:
          create:
            path: github.com/Kong/sdk-konnect-go/models/components.PortalCustomization
          delete:
            asPUT: true
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ops.delete.asPUT requires ops.update.path")
	})

	t.Run("valid config with ops uid tag filter", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /services
        ops:
          useUIDTagFilter: true
          create:
            path: github.com/Kong/sdk-konnect-go/models/components.ServiceInput
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect)
		require.Len(t, konnect.Types, 1)
		assert.True(t, konnect.Types[0].OpsUseUIDTagFilter)
	})

	t.Run("valid config with ops getForUID match fields", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways/{gatewayId}/data-plane-certificates
        ops:
          getForUID:
            matchFields:
              - objectField: Spec.APISpec.Certificate
                responseField: Certificate
              - objectField: Spec.APISpec.Name
                responseField: Name
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect)
		require.Len(t, konnect.Types, 1)
		require.NotNil(t, konnect.Types[0].OpsGetForUID)
		require.Len(t, konnect.Types[0].OpsGetForUID.MatchFields, 2)
		assert.Equal(t, "Spec.APISpec.Certificate", konnect.Types[0].OpsGetForUID.MatchFields[0].ObjectField)
		assert.Equal(t, "Certificate", konnect.Types[0].OpsGetForUID.MatchFields[0].ResponseField)
	})

	t.Run("valid config with getForUID root union", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways/{gatewayId}/listeners/{eventGatewayListenerId}/policies
        ops:
          getForUID:
            listItemsSource: slice
            rootUnion:
              unionField: Spec.APISpec.EventGatewayListenerPolicyConfig
              cases:
                - typeValue: tlsServer
                  variantField: EventGatewayTLSListen
                  responseTypeValue: tls_server
                  matchFields:
                    - objectField: Name
                      responseField: GetName()
                - typeValue: forwardToVirtualCluster
                  variantField: ForwardToVirtualClust
                  responseTypeValue: forward_to_virtual_cluster
                  matchFields:
                    - objectField: Name
                      responseField: GetName()
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
		require.NotNil(t, konnect)
		require.Len(t, konnect.Types, 1)
		require.NotNil(t, konnect.Types[0].OpsGetForUID)
		assert.Equal(t, GetForUIDListItemsSourceSlice, konnect.Types[0].OpsGetForUID.ListItemsSource)
		require.NotNil(t, konnect.Types[0].OpsGetForUID.RootUnion)
		assert.Equal(t, "Spec.APISpec.EventGatewayListenerPolicyConfig", konnect.Types[0].OpsGetForUID.RootUnion.UnionField)
		require.Len(t, konnect.Types[0].OpsGetForUID.RootUnion.Cases, 2)
		assert.Equal(t, "tlsServer", konnect.Types[0].OpsGetForUID.RootUnion.Cases[0].TypeValue)
		assert.Equal(t, "EventGatewayTLSListen", konnect.Types[0].OpsGetForUID.RootUnion.Cases[0].VariantField)
		assert.Equal(t, "tls_server", konnect.Types[0].OpsGetForUID.RootUnion.Cases[0].ResponseTypeValue)
		assert.Equal(t, "Name", konnect.Types[0].OpsGetForUID.RootUnion.Cases[0].MatchFields[0].ObjectField)
		assert.Equal(t, "GetName()", konnect.Types[0].OpsGetForUID.RootUnion.Cases[0].MatchFields[0].ResponseField)
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

	t.Run("invalid ops skipGetForUID with getForUID config", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways/{gatewayId}/data-plane-certificates
        ops:
          skipGetForUID: true
          getForUID:
            matchFields:
              - objectField: Spec.APISpec.Certificate
                responseField: Certificate
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ops.skipGetForUID and ops.getForUID are mutually exclusive")
	})

	t.Run("invalid getForUID listItemsSource", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /services
        ops:
          getForUID:
            listItemsSource: banana
            matchFields:
              - objectField: Spec.APISpec.Name
                responseField: Name
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ops.getForUID.listItemsSource must be one of")
	})

	t.Run("invalid getForUID matchFields with rootUnion", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /services
        ops:
          getForUID:
            matchFields:
              - objectField: Spec.APISpec.Name
                responseField: Name
            rootUnion:
              unionField: Spec.APISpec.SomeUnion
              cases:
                - typeValue: foo
                  variantField: Foo
                  responseTypeValue: foo
                  matchFields:
                    - objectField: Name
                      responseField: GetName()
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ops.getForUID.matchFields and ops.getForUID.rootUnion are mutually exclusive")
	})

	t.Run("invalid getForUID rootUnion without cases", func(t *testing.T) {
		content := `
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    types:
      - path: /services
        ops:
          getForUID:
            rootUnion:
              unionField: Spec.APISpec.SomeUnion
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := LoadProjectConfig(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ops.getForUID.rootUnion.cases is required")
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
        name: KonnectEventGateway
      - path: /v3/portals
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)

	konnect := cfg.APIGroupVersions["konnect.konghq.com/v1alpha1"]
	require.NotNil(t, konnect)
	assert.Equal(t, "KonnectEventGateway", konnect.Types[0].Name)
	assert.Empty(t, konnect.Types[1].Name)
}

func TestAPIGroupVersionConfig_NameOverrides(t *testing.T) {
	t.Run("with overrides", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{Path: "/v1/event-gateways", Name: "KonnectEventGateway"},
				{Path: "/v3/portals"},
			},
		}
		overrides := agv.NameOverrides()
		assert.Equal(t, map[string]string{
			"/v1/event-gateways": "KonnectEventGateway",
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

func TestTypeConfig_ValidateAssociations(t *testing.T) {
	t.Run("valid single-kind association", func(t *testing.T) {
		tc := &TypeConfig{
			Path:         "/v1/ai-gateways/{gatewayId}/consumers",
			Associations: []AssociationConfig{{Name: "consumerGroups", Kinds: []string{"AIGatewayConsumerGroup"}, SDKMethod: "UpdateAiGatewayConsumerGroupsForConsumer"}},
		}
		require.NoError(t, tc.validate())
	})
	t.Run("empty name errors", func(t *testing.T) {
		tc := &TypeConfig{Associations: []AssociationConfig{{Kinds: []string{"X"}, SDKMethod: "M"}}}
		require.ErrorContains(t, tc.validate(), "name must not be empty")
	})
	t.Run("empty kinds errors", func(t *testing.T) {
		tc := &TypeConfig{Associations: []AssociationConfig{{Name: "consumerGroups", SDKMethod: "M"}}}
		require.ErrorContains(t, tc.validate(), "kinds must not be empty")
	})
	t.Run("multi-kind not yet supported", func(t *testing.T) {
		tc := &TypeConfig{Associations: []AssociationConfig{{Name: "x", Kinds: []string{"A", "B"}, SDKMethod: "M"}}}
		require.ErrorContains(t, tc.validate(), "multi-kind associations are not yet supported")
	})
	t.Run("empty sdkMethod errors", func(t *testing.T) {
		tc := &TypeConfig{Associations: []AssociationConfig{{Name: "consumerGroups", Kinds: []string{"A"}}}}
		require.ErrorContains(t, tc.validate(), "sdkMethod must not be empty")
	})
	t.Run("duplicate name errors", func(t *testing.T) {
		tc := &TypeConfig{Associations: []AssociationConfig{
			{Name: "consumerGroups", Kinds: []string{"A"}, SDKMethod: "M"},
			{Name: "consumerGroups", Kinds: []string{"A"}, SDKMethod: "M"},
		}}
		require.ErrorContains(t, tc.validate(), "duplicate name")
	})
}

func TestAPIGroupVersionConfig_AssociationsConfig(t *testing.T) {
	agv := &APIGroupVersionConfig{
		Types: []*TypeConfig{
			{Path: "/v1/ai-gateways/{gatewayId}/consumers", Associations: []AssociationConfig{{Name: "consumerGroups", Kinds: []string{"AIGatewayConsumerGroup"}}}},
			{Path: "/v3/portals"},
		},
	}
	got := agv.AssociationsConfig(map[string]string{
		"/v1/ai-gateways/{gatewayId}/consumers": "AIGatewayConsumer",
		"/v3/portals":                           "Portal",
	})
	require.Len(t, got, 1)
	require.Equal(t, "consumerGroups", got["AIGatewayConsumer"][0].Name)
	require.Equal(t, "AIGatewayConsumerGroupRef", got["AIGatewayConsumer"][0].RefTypeName())
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

	t.Run("nested cel validations parsed correctly", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/v1/entities/{entityId}/sub",
					CEL: map[string]*FieldConfig{
						"tls": {
							Fields: map[string]*FieldConfig{
								"client_identity": {
									Fields: map[string]*FieldConfig{
										"certificate": {
											Validations: []string{"+kubebuilder:validation:MaxLength=1024"},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		pathToEntity := map[string]string{
			"/v1/entities/{entityId}/sub": "SubEntity",
		}

		fc := agv.FieldConfig(pathToEntity)
		require.NotNil(t, fc)

		// GetFieldConfig with single segment returns the tls FieldConfig with sub-fields.
		tlsCfg := fc.GetFieldConfig("SubEntity", "tls")
		require.NotNil(t, tlsCfg)
		assert.Empty(t, tlsCfg.Validations, "tls has no direct validations")
		require.NotNil(t, tlsCfg.Sub("client_identity"))

		// Multi-segment path resolves to the leaf.
		certCfg := fc.GetFieldConfig("SubEntity", "tls", "client_identity", "certificate")
		require.NotNil(t, certCfg)
		assert.Equal(t, []string{"+kubebuilder:validation:MaxLength=1024"}, certCfg.Validations)

		// Single-segment lookup returns nil for a non-leaf path segment without Validations.
		assert.Nil(t, fc.GetFieldValidations("SubEntity", "tls"))
	})
}

func TestAPIGroupVersionConfig_SchemaFieldOmissionsConfig(t *testing.T) {
	agv := &APIGroupVersionConfig{
		Types: []*TypeConfig{
			{
				Path: "/v1/consume-policies",
				SchemaFieldOmissions: map[string][]string{
					"EventGatewayModifyHeadersPolicyCreate": {"parentPolicyID"},
				},
			},
			{
				Path: "/v1/produce-policies",
				SchemaFieldOmissions: map[string][]string{
					"EventGatewayModifyHeadersPolicyCreate":             {"name"},
					"EventGatewayParsedRecordEncryptFieldsPolicyCreate": {"parentPolicyID"},
				},
			},
		},
	}

	omissions := agv.SchemaFieldOmissionsConfig()
	require.Len(t, omissions, 2)
	assert.True(t, omissions["EventGatewayModifyHeadersPolicyCreate"]["parentPolicyID"])
	assert.True(t, omissions["EventGatewayModifyHeadersPolicyCreate"]["name"])
	assert.True(t, omissions["EventGatewayParsedRecordEncryptFieldsPolicyCreate"]["parentPolicyID"])
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
		assert.False(t, oc["Portal"].RequireClient)
		assert.Equal(t, "github.com/Kong/sdk-konnect-go/models/components.CreatePortal", oc["Portal"].Ops["create"].Path)
		assert.Equal(t, "github.com/Kong/sdk-konnect-go/models/components.UpdatePortal", oc["Portal"].Ops["update"].Path)
		assert.NotContains(t, oc, "PortalTeam")
	})

	t.Run("requireClient is explicit or inferred", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/v1/event-gateways/{gatewayId}/data-plane-certificates",
					Ops: map[string]*OpConfig{
						"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest"},
					},
					SecretReferences: []SecretReferenceConfig{
						{Path: "spec.apiSpec.certificate", Type: "Secret"},
					},
				},
				{
					Path: "/v3/portals",
					Ops: map[string]*OpConfig{
						"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"},
					},
					OpsRequireClient: true,
				},
			},
		}

		pathToEntity := map[string]string{
			"/v1/event-gateways/{gatewayId}/data-plane-certificates": "KonnectEventDataPlaneCertificate",
			"/v3/portals": "Portal",
		}

		oc := agv.OpsConfig(pathToEntity)
		require.Len(t, oc, 2)
		assert.True(t, oc["KonnectEventDataPlaneCertificate"].RequireClient)
		assert.True(t, oc["Portal"].RequireClient)
	})

	t.Run("uid tag filter is propagated", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/services",
					Ops: map[string]*OpConfig{
						"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.ServiceInput"},
					},
					OpsUseUIDTagFilter: true,
				},
			},
		}

		oc := agv.OpsConfig(map[string]string{"/services": "KongService"})
		require.Len(t, oc, 1)
		assert.True(t, oc["KongService"].UseUIDTagFilter)
	})

	t.Run("getForUID config is propagated", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/v1/event-gateways/{gatewayId}/data-plane-certificates",
					Ops: map[string]*OpConfig{
						"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest"},
					},
					OpsGetForUID: &GetForUIDConfig{
						MatchFields: []GetForUIDMatchField{
							{
								ObjectField:   "Spec.APISpec.Certificate",
								ResponseField: "Certificate",
							},
						},
					},
				},
			},
		}

		oc := agv.OpsConfig(map[string]string{
			"/v1/event-gateways/{gatewayId}/data-plane-certificates": "KonnectEventDataPlaneCertificate",
		})
		require.Len(t, oc, 1)
		require.NotNil(t, oc["KonnectEventDataPlaneCertificate"].GetForUID)
		require.Len(t, oc["KonnectEventDataPlaneCertificate"].GetForUID.MatchFields, 1)
		assert.Equal(t, "Spec.APISpec.Certificate", oc["KonnectEventDataPlaneCertificate"].GetForUID.MatchFields[0].ObjectField)
		assert.Equal(t, "Certificate", oc["KonnectEventDataPlaneCertificate"].GetForUID.MatchFields[0].ResponseField)
	})

	t.Run("getForUID rootUnion config is propagated", func(t *testing.T) {
		agv := &APIGroupVersionConfig{
			Types: []*TypeConfig{
				{
					Path: "/v1/event-gateways/{gatewayId}/listeners/{eventGatewayListenerId}/policies",
					Ops: map[string]*OpConfig{
						"create": {Path: "github.com/Kong/sdk-konnect-go/models/operations.CreateEventGatewayListenerPolicyRequest"},
					},
					OpsGetForUID: &GetForUIDConfig{
						ListItemsSource: GetForUIDListItemsSourceSlice,
						RootUnion: &GetForUIDRootUnionConfig{
							UnionField: "Spec.APISpec.EventGatewayListenerPolicyConfig",
							Cases: []GetForUIDRootUnionCase{
								{
									TypeValue:         "tlsServer",
									VariantField:      "EventGatewayTLSListen",
									ResponseTypeValue: "tls_server",
									MatchFields: []GetForUIDMatchField{
										{
											ObjectField:   "Name",
											ResponseField: "GetName()",
										},
									},
								},
							},
						},
					},
				},
			},
		}

		oc := agv.OpsConfig(map[string]string{
			"/v1/event-gateways/{gatewayId}/listeners/{eventGatewayListenerId}/policies": "EventGatewayListenerPolicy",
		})
		require.Len(t, oc, 1)
		require.NotNil(t, oc["EventGatewayListenerPolicy"].GetForUID)
		assert.Equal(t, GetForUIDListItemsSourceSlice, oc["EventGatewayListenerPolicy"].GetForUID.ListItemsSource)
		require.NotNil(t, oc["EventGatewayListenerPolicy"].GetForUID.RootUnion)
		assert.Equal(t, "Spec.APISpec.EventGatewayListenerPolicyConfig", oc["EventGatewayListenerPolicy"].GetForUID.RootUnion.UnionField)
		require.Len(t, oc["EventGatewayListenerPolicy"].GetForUID.RootUnion.Cases, 1)
		assert.Equal(t, "tlsServer", oc["EventGatewayListenerPolicy"].GetForUID.RootUnion.Cases[0].TypeValue)
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

func TestAPIGroupVersionConfig_Categories(t *testing.T) {
	t.Run("categories parsed from YAML", func(t *testing.T) {
		yaml := `
apiGroupVersions:
  test/v1:
    categories:
      - konnect
      - kong
    types:
      - path: /v1/gateways
        reconciler: {}
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		agv := cfg.APIGroupVersions["test/v1"]
		require.NotNil(t, agv)
		assert.Equal(t, []string{"konnect", "kong"}, agv.Categories)
	})

	t.Run("categories absent when not set", func(t *testing.T) {
		yaml := `
apiGroupVersions:
  test/v1:
    types:
      - path: /v1/gateways
        reconciler: {}
`
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

		cfg, err := LoadProjectConfig(path)
		require.NoError(t, err)

		agv := cfg.APIGroupVersions["test/v1"]
		require.NotNil(t, agv)
		assert.Empty(t, agv.Categories)
	})
}

func TestReconcilerConfig_IsRootInference(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantIsRoot bool
	}{
		{
			name: "root path without params infers isRoot true",
			yaml: `
apiGroupVersions:
  test/v1:
    types:
      - path: /v1/gateways
        reconciler: {}
`,
			wantIsRoot: true,
		},
		{
			name: "child path with params infers isRoot false",
			yaml: `
apiGroupVersions:
  test/v1:
    types:
      - path: /v1/gateways/{gatewayId}/listeners
        reconciler: {}
`,
			wantIsRoot: false,
		},
		{
			name: "explicit isRoot true overrides inferred false on child path",
			yaml: `
apiGroupVersions:
  test/v1:
    types:
      - path: /v1/gateways/{gatewayId}/listeners
        reconciler:
          isRoot: true
`,
			wantIsRoot: true,
		},
		{
			name: "explicit isRoot false overrides inferred true on root path",
			yaml: `
apiGroupVersions:
  test/v1:
    types:
      - path: /v1/gateways
        reconciler:
          isRoot: false
`,
			wantIsRoot: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600))

			cfg, err := LoadProjectConfig(path)
			require.NoError(t, err)

			agv := cfg.APIGroupVersions["test/v1"]
			require.NotNil(t, agv)
			require.Len(t, agv.Types, 1)
			require.NotNil(t, agv.Types[0].Reconciler)
			require.NotNil(t, agv.Types[0].Reconciler.IsRoot)
			assert.Equal(t, tc.wantIsRoot, *agv.Types[0].Reconciler.IsRoot)
		})
	}
}

func TestLoadProjectConfig_ReconcilerEntityGVKs(t *testing.T) {
	yaml := `
apiGroupVersions:
  configuration.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways/{gatewayId}/listeners/{eventGatewayListenerId}/policies
        reconciler:
          parentEntityGVK:
            kind: EventGatewayListener
            group: configuration.konghq.com
          ancestorEntityGVKs:
            - kind: KonnectEventGateway
              group: konnect.konghq.com
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)

	agv := cfg.APIGroupVersions["configuration.konghq.com/v1alpha1"]
	require.NotNil(t, agv)
	require.Len(t, agv.Types, 1)
	require.NotNil(t, agv.Types[0].Reconciler)

	rc := agv.Types[0].Reconciler
	require.NotNil(t, rc.ParentEntityGVK)
	assert.Equal(t, "EventGatewayListener", rc.ParentEntityKind())
	assert.Equal(t, "configuration.konghq.com", rc.ParentEntityGroup("ignored.example.com"))
	require.Len(t, rc.AncestorEntityGVKs, 1)
	assert.Equal(t, []string{"KonnectEventGateway"}, rc.AncestorEntityKinds())
	assert.Equal(t, "konnect.konghq.com", rc.AncestorEntityGVKs[0].Group)
}

func TestReferenceConfigValidation(t *testing.T) {
	base := func(mut func(*ReferenceConfig)) *APIGroupVersionConfig {
		rc := ReferenceConfig{
			Path:       "spec.apiSpec.policies",
			Kinds:      []string{"AIGatewayPolicy"},
			ResolvesTo: "id",
		}
		if mut != nil {
			mut(&rc)
		}
		return &APIGroupVersionConfig{
			Types: []*TypeConfig{{
				Path:       "/v1/ai-gateways/{gatewayId}/agents",
				Name:       "AIGatewayAgent",
				References: []ReferenceConfig{rc},
			}},
		}
	}

	tests := []struct {
		name    string
		cfg     *APIGroupVersionConfig
		wantErr string
	}{
		{name: "valid single-kind id ref", cfg: base(nil)},
		{
			name:    "empty kinds rejected",
			cfg:     base(func(rc *ReferenceConfig) { rc.Kinds = nil }),
			wantErr: "kinds must not be empty",
		},
		{
			name:    "bad resolvesTo rejected",
			cfg:     base(func(rc *ReferenceConfig) { rc.ResolvesTo = "uuid" }),
			wantErr: `resolvesTo must be "id" or "name"`,
		},
		{
			name: "resolvesTo name accepted",
			cfg:  base(func(rc *ReferenceConfig) { rc.ResolvesTo = "name" }),
		},
		{
			name: "nested path accepted",
			cfg:  base(func(rc *ReferenceConfig) { rc.Path = "spec.apiSpec.access.acls.allow.allow" }),
		},
		{
			name:    "path outside spec.apiSpec rejected",
			cfg:     base(func(rc *ReferenceConfig) { rc.Path = "spec.policies" }),
			wantErr: `must start with "spec.apiSpec."`,
		},
		{
			name: "multi-kind requires refTypeName",
			cfg: base(func(rc *ReferenceConfig) {
				rc.Kinds = []string{"AIGatewayConsumer", "AIGatewayConsumerGroup"}
			}),
			wantErr: "refTypeName is required when multiple kinds",
		},
		{
			name: "refTypeName forbidden for single kind",
			cfg: base(func(rc *ReferenceConfig) {
				rc.RefTypeName = "CustomRef"
			}),
			wantErr: "refTypeName must not be set when a single kind",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestReferenceConfigTypeName(t *testing.T) {
	require.Equal(t, "AIGatewayPolicyRef", ReferenceConfig{Kinds: []string{"AIGatewayPolicy"}}.TypeName())
	require.Equal(t, "AIGatewayACLRef", ReferenceConfig{
		Kinds:       []string{"AIGatewayConsumer", "AIGatewayConsumerGroup"},
		RefTypeName: "AIGatewayACLRef",
	}.TypeName())
}
