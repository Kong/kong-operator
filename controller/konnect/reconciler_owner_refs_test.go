package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestRemoveOwnerRefIfSet(t *testing.T) {
	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer",
			Namespace: "default",
		},
	}

	testRemoveOwnerRefIfSet(t,
		"credential with owner",
		&configurationv1alpha1.KongCredentialBasicAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "credential",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "configuration.konghq.com/v1",
						Kind:       "KongConsumer",
						Name:       "consumer",
					},
				},
			},
		},
		consumer,
		func(t *testing.T, obj *configurationv1alpha1.KongCredentialBasicAuth, result ctrl.Result, err error) {
			require.NoError(t, err)
			require.False(t, result.Requeue)
		},
	)

	testRemoveOwnerRefIfSet(t,
		"credential without owner",
		&configurationv1alpha1.KongCredentialBasicAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "credential",
				Namespace: "default",
			},
		},
		consumer,
		func(t *testing.T, obj *configurationv1alpha1.KongCredentialBasicAuth, result ctrl.Result, err error) {
			require.NoError(t, err)
			require.False(t, result.Requeue)
		},
	)
}

func testRemoveOwnerRefIfSet[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
	TOwner constraints.SupportedKonnectEntityType,
	TEntOwner constraints.EntityType[TOwner],
](
	t *testing.T,
	name string,
	obj TEnt,
	owner TEntOwner,
	asserts ...func(*testing.T, TEnt, ctrl.Result, error),
) {
	t.Helper()

	t.Run(name, func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(obj, owner).
			Build()

		result, err := RemoveOwnerRefIfSet(t.Context(), cl, obj, owner)
		for _, assert := range asserts {
			assert(t, obj, result, err)
		}

		hasOwnerRef, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), owner, scheme.Get())
		require.NoError(t, err)
		require.False(t, hasOwnerRef)
	})
}
