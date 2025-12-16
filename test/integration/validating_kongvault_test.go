package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/internal/annotations"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
)

func TestAdmissionWebhook_KongVault(t *testing.T) {
	t.Parallel()

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ctrlClient := client.NewNamespacedClient(GetClients().MgrClient, namespace.Name)

	ingressClass := envconf.RandomName("ingressclass", 16)

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name, func(gc *operatorv2beta1.GatewayConfiguration) {
		gc.Spec.ControlPlaneOptions = &operatorv2beta1.GatewayConfigControlPlaneOptions{
			ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
				IngressClass: lo.ToPtr(ingressClass),
			},
		}
	})
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	require.NoError(t, ctrlClient.Create(ctx, gatewayConfig))
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	require.NoError(t, ctrlClient.Create(ctx, gatewayClass))
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass)
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	require.NoError(t, ctrlClient.Create(ctx, gateway))
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, GetCtx(), gatewayNSN, clients), 3*time.Minute, time.Second)
	t.Log("Gateway is programmed, proceeding with the test cases")

	const prefixForDuplicationTest = "duplicate-prefix"
	prepareKongVaultAlreadyProgrammedInGateway(ctx, t, GetClients().MgrClient, prefixForDuplicationTest, ingressClass)

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
						annotations.IngressClassKey: ingressClass,
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
						annotations.IngressClassKey: ingressClass,
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
						annotations.IngressClassKey: ingressClass,
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
						annotations.IngressClassKey: ingressClass,
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
						annotations.IngressClassKey: ingressClass,
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

// prepareKongVaultAlreadyProgrammedInGateway creates a KongVault and waits until it gets programmed in Gateway.
func prepareKongVaultAlreadyProgrammedInGateway(
	ctx context.Context,
	t *testing.T,
	ctrlClient client.Client,
	vaultPrefix string,
	ingressClass string,
) {
	t.Helper()

	const (
		programmedWaitTimeout  = 2 * time.Minute
		programmedWaitInterval = time.Second
	)

	name := "vault-programmed-" + uuid.NewString()[:8]
	vault := &configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				annotations.IngressClassKey: ingressClass,
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
		if err := ctrlClient.Get(ctx, types.NamespacedName{Name: name}, kv); err != nil {
			return false
		}
		programmed, ok := lo.Find(kv.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == "Programmed"
		})
		return ok && programmed.Status == metav1.ConditionTrue
	}, programmedWaitTimeout, programmedWaitInterval, "KongVault %s was expected to be programmed", name)
}
