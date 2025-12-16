package envtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/ingress-controller/test/internal/testenv"
)

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
