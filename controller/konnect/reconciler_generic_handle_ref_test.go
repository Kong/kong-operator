package konnect

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestHandleRefResult(t *testing.T) {
	t.Run("zero result and nil error continues reconciliation", func(t *testing.T) {
		ent := testKonnectEventDataPlaneCertificateForEventGatewayRefResult()

		cl := fake.NewClientBuilder().Build()
		stop, result, err := handleRefResult(t.Context(), cl, ent, ctrl.Result{}, nil)

		require.NoError(t, err)
		assert.False(t, stop)
		assert.Equal(t, ctrl.Result{}, result)
	})

	t.Run("referenced object being deleted requeues until its deletion timestamp", func(t *testing.T) {
		ent := testKonnectEventDataPlaneCertificateForEventGatewayRefResult()

		deletionTime := time.Now().Add(time.Minute)
		stop, result, err := handleRefResult(
			t.Context(),
			fake.NewClientBuilder().Build(),
			ent,
			ctrl.Result{},
			ReferencedObjectIsBeingDeletedError{
				Reference:         client.ObjectKeyFromObject(ent),
				DeletionTimestamp: deletionTime,
			},
		)

		require.NoError(t, err)
		assert.True(t, stop)
		assert.Greater(t, result.RequeueAfter, time.Duration(0))
		assert.LessOrEqual(t, result.RequeueAfter, time.Minute)
	})

	t.Run("missing referenced object removes cleanup finalizer", func(t *testing.T) {
		ent := testKonnectEventDataPlaneCertificateForEventGatewayRefResult()
		ent.Finalizers = []string{KonnectCleanupFinalizer}

		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(ent).
			Build()

		stop, result, err := handleRefResult(
			t.Context(),
			cl,
			ent,
			ctrl.Result{},
			ReferencedObjectDoesNotExistError{
				Reference: types.NamespacedName{
					Namespace: "default",
					Name:      "missing-event-control-plane",
				},
				Err: apierrors.NewNotFound(
					schema.GroupResource{
						Group:    konnectv1alpha1.GroupVersion.Group,
						Resource: "konnecteventgateways",
					},
					"missing-event-control-plane",
				),
			},
		)

		require.NoError(t, err)
		assert.True(t, stop)
		assert.Equal(t, ctrl.Result{}, result)

		var updated konnectv1alpha1.KonnectEventDataPlaneCertificate
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), &updated))
		assert.Empty(t, updated.Finalizers)
	})

	t.Run("conflict while removing cleanup finalizer requeues without backoff", func(t *testing.T) {
		ent := testKonnectEventDataPlaneCertificateForEventGatewayRefResult()
		ent.Finalizers = []string{KonnectCleanupFinalizer}

		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(ent).
			WithInterceptorFuncs(interceptor.Funcs{
				Update: func(
					ctx context.Context,
					client client.WithWatch,
					obj client.Object,
					opts ...client.UpdateOption,
				) error {
					return apierrors.NewConflict(
						schema.GroupResource{
							Group:    konnectv1alpha1.GroupVersion.Group,
							Resource: "konnecteventdataplanecertificates",
						},
						obj.GetName(),
						assert.AnError,
					)
				},
			}).
			Build()

		stop, result, err := handleRefResult(
			t.Context(),
			cl,
			ent,
			ctrl.Result{},
			ReferencedObjectDoesNotExistError{
				Reference: types.NamespacedName{
					Namespace: "default",
					Name:      "missing-event-control-plane",
				},
				Err: apierrors.NewNotFound(
					schema.GroupResource{
						Group:    konnectv1alpha1.GroupVersion.Group,
						Resource: "konnecteventgateways",
					},
					"missing-event-control-plane",
				),
			},
		)

		require.NoError(t, err)
		assert.True(t, stop)
		assert.Equal(t, ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, result)
	})

	t.Run("requeue causes stop to be set to true", func(t *testing.T) {
		ent := testKonnectEventDataPlaneCertificateForEventGatewayRefResult()

		stop, result, err := handleRefResult(
			t.Context(),
			fake.NewClientBuilder().Build(),
			ent,
			ctrl.Result{RequeueAfter: time.Minute},
			nil,
		)

		require.NoError(t, err)
		assert.True(t, stop)
		assert.False(t, result.IsZero())
	})

}

func testKonnectEventDataPlaneCertificateForEventGatewayRefResult() *konnectv1alpha1.KonnectEventDataPlaneCertificate {
	return &konnectv1alpha1.KonnectEventDataPlaneCertificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "KonnectEventDataPlaneCertificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "event-dp-cert",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-control-plane",
				},
			},
		},
	}
}
