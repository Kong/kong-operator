package envtest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/ingress-controller/internal/labels"
	"github.com/kong/kong-operator/ingress-controller/test/internal/testenv"
)

func TestAdmissionWebhook_KongVault(t *testing.T) {
	t.Skip("skipping until https://github.com/Kong/kong-operator/issues/2176 is resolved")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		scheme     = Scheme(t, WithKong)
		envcfg     = Setup(t, scheme)
		ctrlClient = NewControllerClient(t, scheme, envcfg)
		ns         = CreateNamespace(ctx, t, ctrlClient)

		kongContainer = runKongEnterprise(ctx, t)
	)

	logs := RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithPublishService(ns.Name),
		WithKongAdminURLs(kongContainer.AdminURL(ctx, t)),
		WithUpdateStatus(),
	)
	WaitForManagerStart(t, logs)

	const prefixForDuplicationTest = "duplicate-prefix"
	prepareKongVaultAlreadyProgrammedInGateway(ctx, t, ctrlClient, prefixForDuplicationTest)

	testCases := []struct {
		name                string
		kongVault           *configurationv1alpha1.KongVault
		expectErrorContains string
	}{
		{
			name: "should pass the validation if the configuration is correct",
			kongVault: &configurationv1alpha1.KongVault{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vault-valid",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend:     "env",
					Prefix:      "env-test",
					Description: "test env vault",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"prefix":"kong_vault_test_"}`),
					},
				},
			},
		},
		{
			name: "should also pass the validation if the description is empty",
			kongVault: &configurationv1alpha1.KongVault{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vault-empty-description",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend: "env",
					Prefix:  "env-empty-desc",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"prefix":"kong_vault_test_"}`),
					},
				},
			},
		},
		{
			name: "should fail the validation if the backend is not supported by Kong gateway",
			kongVault: &configurationv1alpha1.KongVault{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vault-unsupported-backend",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend:     "env1",
					Prefix:      "unsupported-backend",
					Description: "test env vault",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"prefix":"kong-env-test"}`),
					},
				},
			},
			expectErrorContains: `vault configuration in invalid: schema violation (name: vault 'env1' is not installed)`,
		},
		{
			name: "should fail the validation if the spec.config does not pass the schema check of Kong gateway",
			kongVault: &configurationv1alpha1.KongVault{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vault-invalid-config",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend:     "env",
					Prefix:      "invalid-config",
					Description: "test env vault",
					Config: apiextensionsv1.JSON{
						Raw: []byte(`{"prefix":"kong-env-test","foo":"bar"}`),
					},
				},
			},
			expectErrorContains: `vault configuration in invalid: schema violation (config.foo: unknown field)`,
		},
		{
			name: "should fail the validation if spec.prefix is duplicate",
			kongVault: &configurationv1alpha1.KongVault{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vault-dupe",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend:     "env",
					Prefix:      prefixForDuplicationTest, // This is the same prefix as the one created in setup.
					Description: "test env vault",
				},
			},
			expectErrorContains: fmt.Sprintf(`spec.prefix "%s" is duplicate with existing KongVault`, prefixForDuplicationTest),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ctrlClient.Create(ctx, tc.kongVault)
			if tc.expectErrorContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectErrorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAdmissionWebhook_KongPlugins(t *testing.T) {
	t.Skip("skipping until https://github.com/Kong/kong-operator/issues/2176 is resolved")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		scheme           = Scheme(t, WithKong)
		envcfg           = Setup(t, scheme)
		ctrlClientGlobal = NewControllerClient(t, scheme, envcfg)
		ns               = CreateNamespace(ctx, t, ctrlClientGlobal)
		ctrlClient       = client.NewNamespacedClient(ctrlClientGlobal, ns.Name)

		kongContainer = runKongEnterprise(ctx, t)
	)

	logs := RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithPublishService(ns.Name),
		WithKongAdminURLs(kongContainer.AdminURL(ctx, t)),
	)
	WaitForManagerStart(t, logs)

	testCases := []struct {
		name                string
		kongPlugin          *configurationv1.KongPlugin
		expectErrorContains string
		secretBefore        *corev1.Secret
		secretAfter         *corev1.Secret
		errorOnUpdate       bool
		errorContains       string
	}{
		{
			name: "should fail the validation if secret used in ConfigFrom of KongPlugin generates invalid plugin configuration",
			kongPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rate-limiting-invalid-config-from",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
					Name: "conf-secret-invalid-config",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":5}`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "conf-secret-invalid-config",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
					Name: "conf-secret-invalid-field",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
				},
				Data: map[string][]byte{
					"rate-limiting-config-minutes": []byte("10"),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "conf-secret-invalid-field",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
					Name: "conf-secret-valid-field",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
				},
				Data: map[string][]byte{
					"rate-limiting-config-minutes": []byte(`10`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "conf-secret-valid-field",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
					assert.Contains(c, err.Error(), tc.expectErrorContains)
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
	webhookConfig := &admregv1.ValidatingWebhookConfiguration{}
	require.NoError(t, ctrlClient.Get(
		ctx,
		k8stypes.NamespacedName{Name: "validating-webhook-configuration"},
		webhookConfig,
		&client.GetOptions{},
	))
	for i, hook := range webhookConfig.Webhooks {
		if hook.Name == "secrets.plugins.validation.ingress-controller.konghq.com" {
			webhookConfig.Webhooks[i].ObjectSelector = nil
		}
	}
	require.NoError(t, ctrlClient.Update(ctx, webhookConfig))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
					assert.Contains(c, err.Error(), tc.expectErrorContains)
				} else if !assert.NoError(c, err) {
					t.Logf("Error: %v", err)
				}
			}, 10*time.Second, 100*time.Millisecond)
		})
	}
}

func TestAdmissionWebhook_KongClusterPlugins(t *testing.T) {
	t.Skip("skipping until https://github.com/Kong/kong-operator/issues/2176 is resolved")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		scheme           = Scheme(t, WithKong)
		envcfg           = Setup(t, scheme)
		ctrlClientGlobal = NewControllerClient(t, scheme, envcfg)
		ns               = CreateNamespace(ctx, t, ctrlClientGlobal)
		ctrlClient       = client.NewNamespacedClient(ctrlClientGlobal, ns.Name)

		kongContainer = runKongEnterprise(ctx, t)
	)

	logs := RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithPublishService(ns.Name),
		WithKongAdminURLs(kongContainer.AdminURL(ctx, t)),
	)
	WaitForManagerStart(t, logs)

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
						annotations.IngressClassKey: annotations.DefaultIngressClass,
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
					Name: "cluster-conf-secret-valid",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":5}`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-conf-secret-valid",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
						annotations.IngressClassKey: annotations.DefaultIngressClass,
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
					Name: "cluster-conf-secret-invalid",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
				},
				Data: map[string][]byte{
					"rate-limiting-config": []byte(`{"limit_by":"consumer","policy":"local","minute":5}`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-conf-secret-invalid",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
						annotations.IngressClassKey: annotations.DefaultIngressClass,
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
					Name: "cluster-conf-secret-valid-patch",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
				},
				Data: map[string][]byte{
					"rate-limiting-minute": []byte(`5`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-conf-secret-valid-patch",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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
						annotations.IngressClassKey: annotations.DefaultIngressClass,
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
					Name: "cluster-conf-secret-invalid-patch",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
				},
				Data: map[string][]byte{
					"rate-limiting-minute": []byte(`5`),
				},
			},
			secretAfter: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-conf-secret-invalid-patch",
					Labels: map[string]string{
						labels.ValidateLabel: "plugin",
					},
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

func TestAdmissionWebhook_KongConsumers(t *testing.T) {
	t.Skip("skipping until https://github.com/Kong/kong-operator/issues/2176 is resolved")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		scheme           = Scheme(t, WithKong)
		envcfg           = Setup(t, scheme)
		ctrlClientGlobal = NewControllerClient(t, scheme, envcfg)
		ns               = CreateNamespace(ctx, t, ctrlClientGlobal)
		ctrlClient       = client.NewNamespacedClient(ctrlClientGlobal, ns.Name)

		kongContainer = runKongEnterprise(ctx, t)
	)

	logs := RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithPublishService(ns.Name),
		WithKongAdminURLs(kongContainer.AdminURL(ctx, t)),
	)
	WaitForManagerStart(t, logs)

	t.Logf("creating some static credentials in %s namespace which will be used to test global validation", ns.Name)
	for _, secret := range []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tuxcreds1",
				Labels: map[string]string{
					labels.CredentialTypeLabel: "basic-auth",
				},
			},
			StringData: map[string]string{
				"username": "tux1",
				"password": "testpass",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tuxcreds2",
				Labels: map[string]string{
					labels.CredentialTypeLabel: "basic-auth",
				},
			},
			StringData: map[string]string{
				"username": "tux2",
				"password": "testpass",
			},
		},
	} {
		secret := secret.DeepCopy()
		require.NoError(t, ctrlClient.Create(ctx, secret))
		t.Cleanup(func() {
			if err := ctrlClient.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
				assert.NoError(t, err)
			}
		})
	}

	t.Logf("creating a static consumer in %s namespace which will be used to test global validation", ns.Name)
	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "statis-consumer-",
			Annotations: map[string]string{
				annotations.IngressClassKey: annotations.DefaultIngressClass,
			},
		},
		Username: "tux",
		CustomID: uuid.NewString(),
		Credentials: []string{
			"tuxcreds1",
			"tuxcreds2",
		},
	}
	require.NoError(t, ctrlClient.Create(ctx, consumer))
	t.Cleanup(func() {
		ctx := t.Context()
		if err := ctrlClient.Delete(ctx, consumer); err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
			assert.NoError(t, err)
		}
	})

	testCases := []struct {
		name           string
		consumer       *configurationv1.KongConsumer
		credentials    []*corev1.Secret
		wantErr        bool
		wantPartialErr string
	}{
		{
			name: "a consumer with no credentials should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testconsumer",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Username: uuid.NewString(),
				CustomID: uuid.NewString(),
			},
			credentials: nil,
			wantErr:     false,
		},
		{
			name: "a consumer with valid credentials should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Username:    "electron",
				CustomID:    uuid.NewString(),
				Credentials: []string{"electronscreds"},
			},
			credentials: []*corev1.Secret{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "electronscreds",
					Labels: map[string]string{
						labels.CredentialTypeLabel: "basic-auth",
					},
				},
				StringData: map[string]string{
					"username": "electron",
					"password": "testpass",
				},
			}},
			wantErr: false,
		},
		{
			name: "a consumer with duplicate credentials which are NOT constrained should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Username: "proton",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"protonscreds1",
					"protonscreds2",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "protonscreds1",
						Labels: map[string]string{
							labels.CredentialTypeLabel: "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "proton",
						"password": "testpass",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "protonscreds2",
						Labels: map[string]string{
							labels.CredentialTypeLabel: "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "electron", // username is unique constrained
						"password": "testpass", // password is not unique constrained
					},
				},
			},
			wantErr: false,
		},
		{
			name: "a consumer referencing credentials secrets which do not yet exist should fail validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Username: "repairedlawnmower",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"nonexistentcreds",
				},
			},
			wantErr:        true,
			wantPartialErr: "not found",
		},
		{
			name: "a consumer with duplicate credentials which ARE constrained should fail validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "brokenshovel",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Username: "neutron",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"neutronscreds1",
					"neutronscreds2",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "neutronscreds1",
						Labels: map[string]string{
							labels.CredentialTypeLabel: "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "neutron",
						"password": "testpass",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "neutronscreds2",
						Labels: map[string]string{
							labels.CredentialTypeLabel: "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "neutron", // username is unique constrained
						"password": "testpass",
					},
				},
			},
			wantErr:        true,
			wantPartialErr: "unique key constraint violated for username",
		},
		{
			name: "a consumer that provides duplicate credentials which are NOT in violation of unique key constraints should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Username: "reasonablehammer",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"reasonablehammer",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "reasonablehammer",
						Labels: map[string]string{
							labels.CredentialTypeLabel: "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "reasonablehammer",
						"password": "testpass", // not unique constrained, so even though someone else is using this password this should pass
					},
				},
			},
			wantErr: false,
		},
		{
			name: "a consumer that provides credentials that are in violation of unique constraints globally against other existing consumers should fail validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "violating-uniqueness-",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Username: "unreasonablehammer",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"unreasonablehammer",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "unreasonablehammer",
						Labels: map[string]string{
							labels.CredentialTypeLabel: "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "tux1", // unique constrained with previous created static consumer credentials
						"password": "testpass",
					},
				},
			},
			wantErr:        true,
			wantPartialErr: "unique key constraint violated for username",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, credential := range tc.credentials {
				require.NoError(t, ctrlClient.Create(ctx, credential))
				t.Cleanup(func() { //nolint:contextcheck
					ctx := context.Background()
					if err := ctrlClient.Delete(ctx, credential); err != nil && !apierrors.IsNotFound(err) {
						assert.NoError(t, err)
					}
				})
			}

			err := ctrlClient.Create(ctx, tc.consumer)
			if tc.wantErr {
				require.Error(t, err, "consumer %s should fail to create", tc.consumer.Name)
				assert.Contains(t, err.Error(), tc.wantPartialErr,
					"got error string %q, want a superstring of %q", err.Error(), tc.wantPartialErr,
				)
			} else {
				t.Cleanup(func() {
					if err := ctrlClient.Delete(ctx, tc.consumer); err != nil && !apierrors.IsNotFound(err) {
						assert.NoError(t, err)
					}
				})
				require.NoError(t, err, "consumer %s should create successfully", tc.consumer.Name)
			}
		})
	}
}

func TestAdmissionWebhook_KongCustomEntities(t *testing.T) {
	t.Skip("skipping until https://github.com/Kong/kong-operator/issues/2176 is resolved")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		scheme           = Scheme(t, WithKong)
		envcfg           = Setup(t, scheme)
		ctrlClientGlobal = NewControllerClient(t, scheme, envcfg)
		ns               = CreateNamespace(ctx, t, ctrlClientGlobal)
		ctrlClient       = client.NewNamespacedClient(ctrlClientGlobal, ns.Name)

		kongContainer = runKongEnterprise(ctx, t)
	)

	logs := RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithPublishService(ns.Name),
		WithKongAdminURLs(kongContainer.AdminURL(ctx, t)),
	)
	WaitForManagerStart(t, logs)

	testCases := []struct {
		name                     string
		requireEnterpriseLicense bool
		entity                   *configurationv1alpha1.KongCustomEntity
		valid                    bool

		errContains string
	}{
		{
			name: "entity not supported in Kong",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "not-supported-entity",
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType:     "invalid_entity",
					ControllerName: annotations.DefaultIngressClass,
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"key":"value"}`),
					},
				},
			},
			valid:       false,
			errContains: "failed to get schema of Kong entity type 'invalid_entity'",
		},
		{
			name: "valid session entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "session-1",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType: "sessions",
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"session_id":"session1"}`),
					},
				},
			},
			valid: true,
		},
		{
			name: "invalid session entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "session-1",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.DefaultIngressClass,
					},
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType: "sessions",
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"session_id":"session2","foo":"bar"}`),
					},
				},
			},
			valid: false,
		},
		{
			name: "valid degraphql_route entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "degraphql-route-1",
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType:     "degraphql_routes",
					ControllerName: annotations.DefaultIngressClass,
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"uri":"/me","query":"query{ viewer { login}}"}`),
					},
				},
			},
			requireEnterpriseLicense: true,
			valid:                    true,
		},
		{
			name: "invalid degraphql_route entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "degraphql-route-1",
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType:     "degraphql_routes",
					ControllerName: annotations.DefaultIngressClass,
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"uri":"/me","query":"query{ viewer { login}}","foo":"bar"}`),
					},
				},
			},
			requireEnterpriseLicense: true,
			valid:                    false,
		},
		{
			name: "KongCustomEntity not controlled by the current controller",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unrelated-entity",
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType: "unrelated_entity",
					Fields: apiextensionsv1.JSON{
						Raw: []byte("{}"),
					},
				},
			},
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.requireEnterpriseLicense && !testenv.KongEnterpriseEnabled() {
				t.Skip("Skipped because Kong enterprise is not enabled")
			}
			err := ctrlClient.Create(ctx, tc.entity)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			}
		})
	}
}

// prepareKongVaultAlreadyProgrammedInGateway creates a KongVault and waits until it gets programmed in Gateway.
func prepareKongVaultAlreadyProgrammedInGateway(
	ctx context.Context,
	t *testing.T,
	ctrlClient client.Client,
	vaultPrefix string,
) {
	t.Helper()

	const (
		programmedWaitTimeout  = 30 * time.Second
		programmedWaitInterval = 20 * time.Millisecond
	)

	name := uuid.NewString()
	vault := &configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				annotations.IngressClassKey: annotations.DefaultIngressClass,
			},
		},
		Spec: configurationv1alpha1.KongVaultSpec{
			Backend:     "env",
			Prefix:      vaultPrefix,
			Description: "vault description",
		},
	}
	err := ctrlClient.Create(ctx, vault)
	require.NoError(t, err)

	t.Logf("Waiting for KongVault %s to be programmed...", name)
	require.Eventuallyf(t, func() bool {
		kv := &configurationv1alpha1.KongVault{}
		err := ctrlClient.Get(ctx, k8stypes.NamespacedName{Name: name}, kv)
		if err != nil {
			return false
		}
		programmed, ok := lo.Find(kv.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == "Programmed"
		})
		return ok && programmed.Status == metav1.ConditionTrue
	}, programmedWaitTimeout, programmedWaitInterval, "KongVault %s was expected to be programmed", name)
}
