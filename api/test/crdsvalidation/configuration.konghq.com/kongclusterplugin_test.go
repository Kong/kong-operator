package configuration_test

import (
	"testing"

	"github.com/samber/lo"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKongClusterPlugin(t *testing.T) {
	t.Run("config and configFrom fields validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1.KongClusterPlugin]{
			{
				Name: "using both config and configFrom should fail",
				TestObject: &configurationv1.KongClusterPlugin{
					ObjectMeta: common.CommonObjectMeta,
					PluginName: "rate-limiting",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"minute": 5}`),
					},
					ConfigFrom: &configurationv1.NamespacedConfigSource{
						SecretValue: configurationv1.NamespacedSecretValueFromSource{
							Namespace: "default",
							Secret:    "test-secret",
							Key:       "config",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Using both config and configFrom fields is not allowed."),
			},
			{
				Name: "using both configFrom and configPatches should fail",
				TestObject: &configurationv1.KongClusterPlugin{
					ObjectMeta: common.CommonObjectMeta,
					PluginName: "rate-limiting",
					ConfigFrom: &configurationv1.NamespacedConfigSource{
						SecretValue: configurationv1.NamespacedSecretValueFromSource{
							Namespace: "default",
							Secret:    "test-secret",
							Key:       "config",
						},
					},
					ConfigPatches: []configurationv1.NamespacedConfigPatch{
						{
							Path: "/minute",
							ValueFrom: configurationv1.NamespacedConfigSource{
								SecretValue: configurationv1.NamespacedSecretValueFromSource{
									Namespace: "default",
									Secret:    "test-secret",
									Key:       "minute",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Using both configFrom and configPatches fields is not allowed."),
			},
			{
				Name: "using only config should succeed",
				TestObject: &configurationv1.KongClusterPlugin{
					ObjectMeta: common.CommonObjectMeta,
					PluginName: "rate-limiting",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"minute": 5}`),
					},
				},
			},
			{
				Name: "using only configFrom should succeed",
				TestObject: &configurationv1.KongClusterPlugin{
					ObjectMeta: common.CommonObjectMeta,
					PluginName: "rate-limiting",
					ConfigFrom: &configurationv1.NamespacedConfigSource{
						SecretValue: configurationv1.NamespacedSecretValueFromSource{
							Namespace: "default",
							Secret:    "test-secret",
							Key:       "config",
						},
					},
				},
			},
			{
				Name: "using only configPatches should succeed",
				TestObject: &configurationv1.KongClusterPlugin{
					ObjectMeta: common.CommonObjectMeta,
					PluginName: "rate-limiting",
					ConfigPatches: []configurationv1.NamespacedConfigPatch{
						{
							Path: "/minute",
							ValueFrom: configurationv1.NamespacedConfigSource{
								SecretValue: configurationv1.NamespacedSecretValueFromSource{
									Namespace: "default",
									Secret:    "test-secret",
									Key:       "minute",
								},
							},
						},
					},
				},
			},
		}.Run(t)
	})

	t.Run("plugin field immutability", func(t *testing.T) {
		// Note: This test validates that the plugin field is immutable on update
		// The actual immutability check requires an update operation which is tested
		// via the CRD validation framework during actual cluster operations
		common.TestCasesGroup[*configurationv1.KongClusterPlugin]{
			{
				Name: "plugin field should be present",
				TestObject: &configurationv1.KongClusterPlugin{
					ObjectMeta: common.CommonObjectMeta,
					PluginName: "rate-limiting",
				},
			},
			{
				Name: "plugin field change should fail on update",
				TestObject: &configurationv1.KongClusterPlugin{
					ObjectMeta: common.CommonObjectMeta,
					PluginName: "rate-limiting",
				},
				Update: func(obj *configurationv1.KongClusterPlugin) {
					obj.PluginName = "cors"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("The plugin field is immutable"),
			},
		}.Run(t)
	})
}
