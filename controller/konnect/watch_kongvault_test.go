package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestEnqueueKongVaultForKonnectConfigStore(t *testing.T) {
	configStore := &konnectv1alpha1.KonnectConfigStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-cert-keys",
			Namespace: "kong",
		},
	}

	// KongVault is cluster-scoped, so the enqueued requests carry no namespace.
	vaultReferencing := func(name, refNamespace string) *configurationv1alpha1.KongVault {
		return &configurationv1alpha1.KongVault{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: configurationv1alpha1.KongVaultSpec{
				Backend: "konnect",
				Prefix:  name,
				ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
					Name:      configStore.Name,
					Namespace: refNamespace,
				},
			},
		}
	}

	tests := []struct {
		name     string
		vaults   []client.Object
		expected []ctrl.Request
	}{
		{
			name: "no KongVault references the KonnectConfigStore",
			vaults: []client.Object{
				&configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{Name: "vault-without-ref"},
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "env",
						Prefix:  "env-vault",
					},
				},
			},
		},
		{
			name: "only the referencing KongVault is enqueued",
			vaults: []client.Object{
				vaultReferencing("vault-with-ref", "kong"),
				&configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{Name: "vault-without-ref"},
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "env",
						Prefix:  "env-vault",
					},
				},
			},
			expected: []ctrl.Request{
				{NamespacedName: types.NamespacedName{Name: "vault-with-ref"}},
			},
		},
		{
			name: "a KongVault referencing a same-named store in another namespace is not enqueued",
			vaults: []client.Object{
				vaultReferencing("vault-other-ns", "other"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builderFunc := func() *fakectrlruntimeclient.ClientBuilder {
				return fakectrlruntimeclient.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(append(tt.vaults, configStore)...)
			}

			// The index extractors take a client, but it is the builder that creates
			// it, so build an index-less client for them first.
			clForIndices := builderFunc().Build()
			builder := builderFunc()
			for _, opt := range index.OptionsForKongVault(clForIndices) {
				builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}
			cl := builder.Build()

			requests := enqueueKongVaultForKonnectConfigStore(cl)(t.Context(), configStore)
			require.Equal(t, tt.expected, requests)
		})
	}

	t.Run("a non-KonnectConfigStore object enqueues nothing", func(t *testing.T) {
		cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
		require.Nil(t, enqueueKongVaultForKonnectConfigStore(cl)(t.Context(), &configurationv1alpha1.KongVault{}))
	})
}
