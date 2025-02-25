package clientops

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func testDeleteAll[
	T any,
	TPtr interface {
		*T
		client.Object
	},
](
	t *testing.T,
	name string,
	scheme *runtime.Scheme,
	objects []T,
) {
	t.Run(name, func(t *testing.T) {
		objectsClient := make([]client.Object, len(objects))
		for i, obj := range objects {
			var objClient TPtr = &obj
			objectsClient[i] = objClient
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objectsClient...).
			Build()
		require.NoError(t, DeleteAll[T, TPtr](t.Context(), cl, objects))

		for _, obj := range objects {
			var tPtr TPtr = &obj
			nn := client.ObjectKeyFromObject(tPtr)
			err := cl.Get(t.Context(), nn, tPtr)
			require.Truef(t, apierrors.IsNotFound(err), "object should not be found, %v", err)
		}
	})
}

func TestDeleteAll(t *testing.T) {
	scheme := scheme.Get()

	testDeleteAll(t, "no objects", scheme,
		[]corev1.Pod{},
	)

	testDeleteAll(t, "1 pod", scheme,
		[]corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
				},
			},
		},
	)

	testDeleteAll(t, "2 pods", scheme,
		[]corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod2",
					Namespace: "default",
				},
			},
		},
	)
}

func testDeleteAllFromList[
	TList interface {
		GetItems() []T
	},
	TListPtr interface {
		*TList
		client.ObjectList
		GetItems() []T
	},
	T constraints.SupportedKonnectEntityType,
	TT constraints.EntityType[T],
](
	t *testing.T,
	name string,
	scheme *runtime.Scheme,
	list TListPtr,
) {
	t.Run(name, func(t *testing.T) {
		objects := list.GetItems()
		objectsClient := make([]client.Object, len(objects))
		for i, obj := range objects {
			var objClient TT = &obj
			objectsClient[i] = objClient
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects().
			Build()
		require.NoError(t, DeleteAllFromList[TList, TListPtr, T, TT](t.Context(), cl, list))

		for _, obj := range objects {
			var tPtr TT = &obj
			nn := client.ObjectKeyFromObject(tPtr)
			err := cl.Get(t.Context(), nn, tPtr)
			require.Truef(t, apierrors.IsNotFound(err), "object should not be found, %v", err)
		}
	})
}

func TestDeleteAllFromList(t *testing.T) {
	scheme := scheme.Get()

	testDeleteAllFromList(t, "empty list", scheme,
		&konnectv1alpha1.KonnectGatewayControlPlaneList{
			Items: []konnectv1alpha1.KonnectGatewayControlPlane{},
		},
	)

	testDeleteAllFromList(t, "1 KonnectGatewayControlPlane", scheme,
		&konnectv1alpha1.KonnectGatewayControlPlaneList{
			Items: []konnectv1alpha1.KonnectGatewayControlPlane{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
				},
			},
		},
	)

	testDeleteAllFromList(t, "2 KonnectGatewayControlPlanes", scheme,
		&konnectv1alpha1.KonnectGatewayControlPlaneList{
			Items: []konnectv1alpha1.KonnectGatewayControlPlane{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-2",
						Namespace: "default",
					},
				},
			},
		},
	)
}
