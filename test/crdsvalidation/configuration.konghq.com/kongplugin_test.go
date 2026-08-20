package configuration_test

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKongPlugin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("config and configFrom fields validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1.KongPlugin]{
			{
				Name: "using both config and configFrom should fail",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"minute": 5}`),
					},
					ConfigFrom: &configurationv1.ConfigSource{
						SecretValue: configurationv1.SecretValueFromSource{
							Secret: "test-secret",
							Key:    "config",
						},
					},
				},
				ExpectedErrorMessage: new("Using both config and configFrom fields is not allowed."),
			},
			{
				Name: "using both configFrom and configPatches should fail",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					ConfigFrom: &configurationv1.ConfigSource{
						SecretValue: configurationv1.SecretValueFromSource{
							Secret: "test-secret",
							Key:    "config",
						},
					},
					ConfigPatches: []configurationv1.ConfigPatch{
						{
							Path: "/minute",
							ValueFrom: configurationv1.ConfigSource{
								SecretValue: configurationv1.SecretValueFromSource{
									Secret: "test-secret",
									Key:    "minute",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("Using both configFrom and configPatches fields is not allowed."),
			},
			{
				Name: "using only config should succeed",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"minute": 5}`),
					},
				},
			},
			{
				Name: "using only configFrom should succeed",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					ConfigFrom: &configurationv1.ConfigSource{
						SecretValue: configurationv1.SecretValueFromSource{
							Secret: "test-secret",
							Key:    "config",
						},
					},
				},
			},
			{
				Name: "using only configPatches should succeed",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					ConfigPatches: []configurationv1.ConfigPatch{
						{
							Path: "/minute",
							ValueFrom: configurationv1.ConfigSource{
								SecretValue: configurationv1.SecretValueFromSource{
									Secret: "test-secret",
									Key:    "minute",
								},
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("plugin field immutability", func(t *testing.T) {
		// Note: This test validates that the plugin field is immutable on update
		// The actual immutability check requires an update operation which is tested
		// via the CRD validation framework during actual cluster operations
		common.TestCasesGroup[*configurationv1.KongPlugin]{
			{
				Name: "plugin field should be present",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
				},
			},
			{
				Name: "plugin field change should fail on update",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
				},
				Update: func(obj *configurationv1.KongPlugin) {
					obj.PluginName = "cors"
				},
				ExpectedUpdateErrorMessage: new("The plugin field is immutable"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("tags field validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1.KongPlugin]{
			{
				Name: "tags field with valid tags should succeed",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					Tags:       []string{"tag1", "tag2"},
				},
			},
			{
				Name: "tags field with invalid tag should fail",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					Tags:       []string{"tag1", "a-too-long-tag-that-has-the-length-greater-than-the-maximum-length-of-one-hundred-and-twentyeight-characters-which-is-not-allowed"},
				},
				ExpectedErrorMessage: new("tags entries must not be longer than 128 characters"),
			},
			{
				Name: "tags field with too many tags should fail",
				TestObject: &configurationv1.KongPlugin{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					PluginName: "rate-limiting",
					Tags:       []string{"tag1", "tag2", "tag3", "tag4", "tag5", "tag6", "tag7", "tag8", "tag9", "tag10", "tag11", "tag12", "tag13", "tag14", "tag15", "tag16", "tag17", "tag18", "tag19", "tag20", "tag21"},
				},
				ExpectedErrorMessage: new("must have at most 20 items"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
