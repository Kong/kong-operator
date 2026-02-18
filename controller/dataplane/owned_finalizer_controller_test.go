package dataplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

func TestRequestsForDataPlaneOwnedObjects(t *testing.T) {
	ownerDp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dataplane",
			Namespace: "test-namespace",
			UID:       "owner-uid",
		},
	}
	nonOwnerDp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-owner-dp",
			Namespace: "test-namespace",
			UID:       "non-owner-uid",
		},
	}

	ownedObjMeta := func(name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-namespace",
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: ownerDp.UID,
				},
			},
		}
	}
	const otherNs = "other-namespace"
	cl := fake.NewFakeClient(
		&appsv1.Deployment{ObjectMeta: ownedObjMeta("deployment")},
		&corev1.Service{ObjectMeta: ownedObjMeta("service")},
		&corev1.Secret{ObjectMeta: ownedObjMeta("secret")},

		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "not-owned-deployment", Namespace: ownerDp.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "not-owned-service", Namespace: ownerDp.Namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "not-owned-secret", Namespace: ownerDp.Namespace}},

		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "not-owned-diff-ns-deployment", Namespace: otherNs}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "not-owned-diff-ns-service", Namespace: otherNs}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "not-owned-diff-ns-secret", Namespace: otherNs}},
	)

	ctx := t.Context()
	expectedRequest := func(name string) ctrl.Request {
		return ctrl.Request{
			NamespacedName: k8stypes.NamespacedName{
				Namespace: "test-namespace",
				Name:      name,
			},
		}
	}

	t.Run("service", func(t *testing.T) {
		require.Empty(t, requestsForDataPlaneOwnedObjects[corev1.Service](cl)(ctx, nonOwnerDp))
		requests := requestsForDataPlaneOwnedObjects[corev1.Service](cl)(ctx, ownerDp)
		require.Len(t, requests, 1)
		require.Equal(t, expectedRequest("service"), requests[0])
	})
	t.Run("secrets", func(t *testing.T) {
		require.Empty(t, requestsForDataPlaneOwnedObjects[corev1.Service](cl)(ctx, nonOwnerDp))
		requests := requestsForDataPlaneOwnedObjects[corev1.Secret](cl)(ctx, ownerDp)
		require.Len(t, requests, 1)
		require.Equal(t, expectedRequest("secret"), requests[0])
	})
	t.Run("deployments", func(t *testing.T) {
		require.Empty(t, requestsForDataPlaneOwnedObjects[appsv1.Deployment](cl)(ctx, nonOwnerDp))
		requests := requestsForDataPlaneOwnedObjects[appsv1.Deployment](cl)(ctx, ownerDp)
		require.Len(t, requests, 1)
		require.Equal(t, expectedRequest("deployment"), requests[0])
	})
}
