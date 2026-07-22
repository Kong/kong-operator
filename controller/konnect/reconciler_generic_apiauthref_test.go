package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestReconcileRemovesFinalizerWhenControlPlaneDisappearsBeforeAPIAuthLookup(t *testing.T) {
	upstream := testKongUpstreamOK.DeepCopy()
	controlPlane := testControlPlaneOK.DeepCopy()
	target := &configurationv1alpha1.KongTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "target",
			Namespace:  "default",
			Finalizers: []string{KonnectCleanupFinalizer},
		},
		Spec: configurationv1alpha1.KongTargetSpec{
			UpstreamRef: commonv1alpha1.NamespacedRef{Name: upstream.Name},
		},
	}

	controlPlaneGets := 0
	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(target, upstream, controlPlane).
		WithStatusSubresource(target).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(
				ctx context.Context,
				cl client.WithWatch,
				key client.ObjectKey,
				obj client.Object,
				opts ...client.GetOption,
			) error {
				if _, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane); ok && key == client.ObjectKeyFromObject(controlPlane) {
					controlPlaneGets++
					if controlPlaneGets > 1 {
						return apierrors.NewNotFound(
							schema.GroupResource{Group: "konnect.konghq.com", Resource: "konnectgatewaycontrolplanes"},
							key.Name,
						)
					}
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	reconciler := NewKonnectEntityReconciler[configurationv1alpha1.KongTarget](
		nil, logging.DevelopmentMode, cl,
	)
	res, err := reconciler.Reconcile(t.Context(), target)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)
	assert.Equal(t, 2, controlPlaneGets)

	var updated configurationv1alpha1.KongTarget
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(target), &updated))
	assert.NotContains(t, updated.Finalizers, KonnectCleanupFinalizer)
}

func TestRemoveCleanupFinalizerIfControlPlaneIsGone(t *testing.T) {
	newTarget := func() *configurationv1alpha1.KongTarget {
		return &configurationv1alpha1.KongTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "target",
				Namespace:  "default",
				Finalizers: []string{KonnectCleanupFinalizer},
			},
		}
	}
	missingControlPlaneErr := controlplane.ReferencedControlPlaneDoesNotExistError{
		Reference: commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "missing-control-plane",
			},
		},
		Err: apierrors.NewNotFound(
			schema.GroupResource{Group: "konnect.konghq.com", Resource: "konnectgatewaycontrolplanes"},
			"missing-control-plane",
		),
	}

	t.Run("ignores unrelated errors", func(t *testing.T) {
		target := newTarget()
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(target).Build()

		handled, res, err := removeCleanupFinalizerIfControlPlaneIsGone[configurationv1alpha1.KongTarget](
			t.Context(), cl, target, assert.AnError,
		)

		require.NoError(t, err)
		assert.False(t, handled)
		assert.Equal(t, ctrl.Result{}, res)
		assert.Contains(t, target.Finalizers, KonnectCleanupFinalizer)
	})

	t.Run("removes finalizer when control plane is gone", func(t *testing.T) {
		target := newTarget()
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(target).Build()

		handled, res, err := removeCleanupFinalizerIfControlPlaneIsGone[configurationv1alpha1.KongTarget](
			t.Context(), cl, target, missingControlPlaneErr,
		)

		require.NoError(t, err)
		assert.True(t, handled)
		assert.Equal(t, ctrl.Result{}, res)

		var updated configurationv1alpha1.KongTarget
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(target), &updated))
		assert.NotContains(t, updated.Finalizers, KonnectCleanupFinalizer)
	})

	t.Run("requeues on update conflict", func(t *testing.T) {
		target := newTarget()
		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(target).
			WithInterceptorFuncs(interceptor.Funcs{
				Update: func(
					_ context.Context,
					_ client.WithWatch,
					obj client.Object,
					_ ...client.UpdateOption,
				) error {
					return apierrors.NewConflict(
						schema.GroupResource{Group: "configuration.konghq.com", Resource: "kongtargets"},
						obj.GetName(),
						assert.AnError,
					)
				},
			}).
			Build()

		handled, res, err := removeCleanupFinalizerIfControlPlaneIsGone[configurationv1alpha1.KongTarget](
			t.Context(), cl, target, missingControlPlaneErr,
		)

		require.NoError(t, err)
		assert.True(t, handled)
		assert.Equal(t, ctrl.Result{Requeue: true}, res)
	})
}
