package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/internal/annotations"
	"github.com/kong/kong-operator/modules/manager/config"
)

// secretLabels is the set of labels applied to Secrets to be validated by the webhook.
var secretLabels = map[string]string{
	config.DefaultSecretLabelSelector: "true",
	"konghq.com/validate":             "plugin",
}

func TestAdmissionWebhook_KongPlugins(t *testing.T) {
	t.Parallel()

	_, _, _, ctrlClient := bootstrapGateway(t.Context(), t, env, GetClients().MgrClient) //nolint:dogsled

	testCases := []struct {
		name          string
		kongPlugin    *configurationv1.KongPlugin
		secretBefore  *corev1.Secret
		secretAfter   *corev1.Secret
		errorOnUpdate bool
		errorContains string
	}{
		{
			name: "should fail the validation if secret used in ConfigFrom of KongPlugin generates invalid plugin configuration",
			kongPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rate-limiting-invalid-config-from",
				},
				PluginName: "rate-limiting",
				ConfigFrom: &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{
						Secret: "conf-secret-invalid-config",
						Key:    "rate-limiting-config",
					},
				},
			},
			secretBefore: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "conf-secret-invalid-config",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":5}`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "conf-secret-invalid-config",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":"5"}`),
				},
			},
			errorOnUpdate: true,
			errorContains: "Change on secret will generate invalid configuration for KongPlugin",
		},
		{
			name: "should fail the validation if the secret is used in ConfigPatches of KongPlugin and generates invalid config",
			kongPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rate-limiting-invalid-config-patches",
				},
				PluginName: "rate-limiting",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"limit_by":"consumer","policy":"local"}`),
				},
				ConfigPatches: []configurationv1.ConfigPatch{
					{
						Path: "/minute",
						ValueFrom: configurationv1.ConfigSource{
							SecretValue: configurationv1.SecretValueFromSource{
								Secret: "conf-secret-invalid-field",
								Key:    "rate-limiting-config-minutes",
							},
						},
					},
				},
			},
			secretBefore: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "conf-secret-invalid-field",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config-minutes": []byte("10"),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "conf-secret-invalid-field",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config-minutes": []byte(`"10"`),
				},
			},
			errorOnUpdate: true,
			errorContains: "Change on secret will generate invalid configuration for KongPlugin",
		},
		{
			name: "should pass the validation if the secret used in ConfigPatches of KongPlugin and generates valid config",
			kongPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rate-limiting-valid-config",
				},
				PluginName: "rate-limiting",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"limit_by":"consumer","policy":"local"}`),
				},
				ConfigPatches: []configurationv1.ConfigPatch{
					{
						Path: "/minute",
						ValueFrom: configurationv1.ConfigSource{
							SecretValue: configurationv1.SecretValueFromSource{
								Secret: "conf-secret-valid-field",
								Key:    "rate-limiting-config-minutes",
							},
						},
					},
				},
			},
			secretBefore: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "conf-secret-valid-field",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config-minutes": []byte(`10`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "conf-secret-valid-field",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config-minutes": []byte(`15`),
				},
			},
			errorOnUpdate: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() {
				require.NoError(t, client.IgnoreNotFound(ctrlClient.Delete(ctx, tc.secretBefore)))
				require.NoError(t, client.IgnoreNotFound(ctrlClient.Delete(ctx, tc.secretAfter)))
				require.NoError(t, client.IgnoreNotFound(ctrlClient.Delete(ctx, tc.kongPlugin)))
			})
			require.NoError(t, ctrlClient.Create(ctx, tc.secretBefore))

			require.NoError(t, ctrlClient.Create(ctx, tc.kongPlugin))

			require.EventuallyWithT(t, func(c *assert.CollectT) {
				err := ctrlClient.Update(ctx, tc.secretAfter)
				if tc.errorOnUpdate {
					if !assert.Error(c, err) {
						return
					}
					assert.Contains(c, err.Error(), tc.errorContains)
				} else if !assert.NoError(c, err) {
					t.Logf("Error: %v", err)
				}
			}, 10*time.Second, 100*time.Millisecond)
		})
	}

	// TODO https://github.com/Kong/kubernetes-ingress-controller/issues/5876
	// This repeats all test cases without filtering Secrets in the webhook configuration. This behavior is slated
	// for removal in 4.0, and the following block should be removed along with the behavior.
	// Update requires an object with generated fields populated thus original configuration that name
	// is "validating-webhook-configuration" (see config/webhook/base/manifests.yaml) is hardcoded here.
	for _, tc := range testCases {
		t.Run(tc.name+" (without selector)", func(t *testing.T) {
			// Annoyingly, Create forcibly stores the resulting object in the tc field object we want to reuse.
			// Clearing it manually is a bit silly, but works to let us reuse them.
			tc.secretBefore.ResourceVersion = ""
			tc.secretBefore.UID = ""
			tc.secretAfter.ResourceVersion = ""
			tc.secretAfter.UID = ""
			tc.kongPlugin.ResourceVersion = ""
			tc.kongPlugin.UID = ""
			t.Cleanup(func() {
				require.NoError(t, client.IgnoreNotFound(ctrlClient.Delete(ctx, tc.secretBefore)))
				require.NoError(t, client.IgnoreNotFound(ctrlClient.Delete(ctx, tc.secretAfter)))
				require.NoError(t, client.IgnoreNotFound(ctrlClient.Delete(ctx, tc.kongPlugin)))
			})
			require.NoError(t, ctrlClient.Create(ctx, tc.secretBefore))

			require.NoError(t, ctrlClient.Create(ctx, tc.kongPlugin))

			require.EventuallyWithT(t, func(c *assert.CollectT) {
				err := ctrlClient.Update(ctx, tc.secretAfter)
				if tc.errorOnUpdate {
					if !assert.Error(c, err) {
						return
					}
					assert.Contains(c, err.Error(), tc.errorContains)
				} else if !assert.NoError(c, err) {
					t.Logf("Error: %v", err)
				}
			}, 10*time.Second, 100*time.Millisecond)
		})
	}
}

func TestAdmissionWebhook_KongClusterPlugins(t *testing.T) {
	t.Parallel()

	ctrlClientGlobal := GetClients().MgrClient

	ns, _, ingressClass, ctrlClient := bootstrapGateway(
		t.Context(), t, env, GetClients().MgrClient,
	)

	testCases := []struct {
		name                string
		kongClusterPlugin   *configurationv1.KongClusterPlugin
		expectErrorContains string
		secretBefore        *corev1.Secret
		secretAfter         *corev1.Secret
		errorOnUpdate       bool
		errorContains       string
	}{
		{
			name: "should pass the validation if the secret used in ConfigFrom of KongClusterPlugin generates valid configuration",
			kongClusterPlugin: &configurationv1.KongClusterPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-rate-limiting-valid",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				PluginName: "rate-limiting",
				ConfigFrom: &configurationv1.NamespacedConfigSource{
					SecretValue: configurationv1.NamespacedSecretValueFromSource{
						Namespace: ns.Name,
						Secret:    "cluster-conf-secret-valid",
						Key:       "rate-limiting-config",
					},
				},
			},
			secretBefore: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-valid",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":5}`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-valid",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":10}`),
				},
			},
			errorOnUpdate: false,
		},
		{
			name: "should fail the validation if the secret in ConfigFrom of KongClusterPlugin generates invalid configuration",
			kongClusterPlugin: &configurationv1.KongClusterPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-rate-limiting-invalid",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				PluginName: "rate-limiting",
				ConfigFrom: &configurationv1.NamespacedConfigSource{
					SecretValue: configurationv1.NamespacedSecretValueFromSource{
						Namespace: ns.Name,
						Secret:    "cluster-conf-secret-invalid",
						Key:       "rate-limiting-config",
					},
				},
			},
			secretBefore: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-invalid",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":5}`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-invalid",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":"5"}`),
				},
			},
			errorOnUpdate: true,
			errorContains: "Change on secret will generate invalid configuration for KongClusterPlugin",
		},
		{
			name: "should pass the validation if the secret in ConfigPatches of KongClusterPlugin generates valid configuration",
			kongClusterPlugin: &configurationv1.KongClusterPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-rate-limiting-valid-config-patches",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				PluginName: "rate-limiting",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"limit_by":"consumer","policy":"local"}`),
				},
				ConfigPatches: []configurationv1.NamespacedConfigPatch{
					{
						Path: "/minute",
						ValueFrom: configurationv1.NamespacedConfigSource{
							SecretValue: configurationv1.NamespacedSecretValueFromSource{
								Namespace: ns.Name,
								Secret:    "cluster-conf-secret-valid-patch",
								Key:       "rate-limiting-minute",
							},
						},
					},
				},
			},
			secretBefore: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-valid-patch",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-minute": []byte(`5`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-valid-patch",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-minute": []byte(`10`),
				},
			},
			errorOnUpdate: false,
		},
		{
			name: "should fail the validation if the secret in ConfigPatches of KongClusterPlugin generates invalid configuration",
			kongClusterPlugin: &configurationv1.KongClusterPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-rate-limiting-invalid-config-patches",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				PluginName: "rate-limiting",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"limit_by":"consumer","policy":"local"}`),
				},
				ConfigPatches: []configurationv1.NamespacedConfigPatch{
					{
						Path: "/minute",
						ValueFrom: configurationv1.NamespacedConfigSource{
							SecretValue: configurationv1.NamespacedSecretValueFromSource{
								Namespace: ns.Name,
								Secret:    "cluster-conf-secret-invalid-patch",
								Key:       "rate-limiting-minute",
							},
						},
					},
				},
			},
			secretBefore: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-invalid-patch",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-minute": []byte(`5`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-conf-secret-invalid-patch",
					Labels: secretLabels,
				},
				Data: map[string][]byte{
					"rate-limiting-minute": []byte(`"10"`),
				},
			},
			errorOnUpdate: true,
			errorContains: "Change on secret will generate invalid configuration for KongClusterPlugin",
		},
	}

	const (
		waitTime = 30 * time.Second
		tickTime = 100 * time.Millisecond
	)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, ctrlClient.Create(ctx, tc.secretBefore))
			t.Cleanup(func() {
				require.NoError(t, ctrlClient.Delete(ctx, tc.secretBefore))
			})

			// NOTE: We create the KongClusterPlugin with 'eventually' to avoid
			// flaky errors like:
			// admission webhook "kongclusterplugins.validation.ingress-controller.konghq.com" denied
			// the request: could not parse plugin configuration: Secret "cluster-conf-secret-valid-patch" not found
			require.EventuallyWithT(t, func(t *assert.CollectT) {
				assert.NoError(t, ctrlClientGlobal.Create(ctx, tc.kongClusterPlugin))
			}, waitTime, tickTime,
			)
			t.Cleanup(func() {
				require.NoError(t, ctrlClientGlobal.Delete(ctx, tc.kongClusterPlugin))
			})

			require.EventuallyWithT(t, func(c *assert.CollectT) {
				err := ctrlClient.Update(ctx, tc.secretAfter)
				if tc.errorOnUpdate {
					if !assert.Error(c, err) {
						return
					}
					assert.Contains(c, err.Error(), tc.expectErrorContains)
				} else if !assert.NoError(c, err) {
					t.Logf("Error: %v", err)
				}
			}, waitTime, tickTime,
			)
		})
	}
}
