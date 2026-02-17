package validatingwebhook

import (
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/annotations"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestAdmissionWebhook_KongCustomEntities(t *testing.T) {
	ctx := t.Context()
	_, cleaner, ingressClass, ctrlClient, _ := bootstrapGateway(
		ctx, t, integration.GetEnv(), integration.GetClients().MgrClient,
	)

	testCases := []struct {
		name   string
		entity *configurationv1alpha1.KongCustomEntity

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
					ControllerName: ingressClass,
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"key":"value"}`),
					},
				},
			},
			errContains: "failed to get schema of Kong entity type 'invalid_entity'",
		},
		{
			name: "valid session entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "session-1",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType: "sessions",
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"session_id":"session1"}`),
					},
				},
			},
		},
		{
			name: "invalid session entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "session-1",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType: "sessions",
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"session_id":"session2","foo":"bar"}`),
					},
				},
			},
			errContains: "schema violation (foo: unknown field)",
		},
		{
			name: "valid degraphql_route entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "degraphql-route-1",
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType:     "degraphql_routes",
					ControllerName: ingressClass,
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"uri":"/me","query":"query{ viewer { login}}"}`),
					},
				},
			},
		},
		{
			name: "invalid degraphql_route entity",
			entity: &configurationv1alpha1.KongCustomEntity{
				ObjectMeta: metav1.ObjectMeta{
					Name: "degraphql-route-1",
				},
				Spec: configurationv1alpha1.KongCustomEntitySpec{
					EntityType:     "degraphql_routes",
					ControllerName: ingressClass,
					Fields: apiextensionsv1.JSON{
						Raw: []byte(`{"uri":"/me","query":"query{ viewer { login}}","foo":"bar"}`),
					},
				},
			},
			errContains: "schema violation (foo: unknown field)",
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
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ctrlClient.Create(ctx, tc.entity)
			if tc.errContains == "" {
				require.NoError(t, err)
				cleaner.Add(tc.entity)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			}
		})
	}
}
