package konnect

import (
	"context"
	"errors"
	"fmt"
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
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestReconcileWithMissingAncestorBeforeAPIAuthLookup(t *testing.T) {
	t.Run("produce policy", func(t *testing.T) {
		testReconcileWithMissingAncestorBeforeAPIAuthLookup[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](
			t, func(ref commonv1alpha1.ObjectRef) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
				return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
					Name:      "produce-policy",
					Namespace: "default",
					Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
						EventGatewayVirtualClusterRef: ref,
					},
				}
			},
		)
	})
	t.Run("consume policy", func(t *testing.T) {
		testReconcileWithMissingAncestorBeforeAPIAuthLookup[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](
			t, func(ref commonv1alpha1.ObjectRef) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
				return &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
					Name:      "consume-policy",
					Namespace: "default",
					Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
						EventGatewayVirtualClusterRef: ref,
					},
				}
			},
		)
	})
}

func testReconcileWithMissingAncestorBeforeAPIAuthLookup[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	newEntity func(commonv1alpha1.ObjectRef) TEnt,
) {
	t.Helper()

	for _, tc := range []struct {
		name                   string
		deleting               bool
		withGateway            bool
		withBackend            bool
		virtualClusterGone     bool
		gatewayGetErr          error
		wantMissingAncestorErr bool
	}{
		{
			name:        "deleting policy with missing gateway",
			deleting:    true,
			withBackend: true,
		},
		{
			name:        "deleting policy with missing backend cluster",
			deleting:    true,
			withGateway: true,
		},
		{
			name:               "virtual cluster disappears before API auth lookup",
			deleting:           true,
			withGateway:        true,
			withBackend:        true,
			virtualClusterGone: true,
		},
		{
			name:                   "active policy with missing gateway keeps finalizer",
			withBackend:            true,
			wantMissingAncestorErr: true,
		},
		{
			name:                   "active policy with missing backend cluster keeps finalizer",
			withGateway:            true,
			wantMissingAncestorErr: true,
		},
		{
			name:          "deleting policy with failed ancestor lookup keeps finalizer",
			deleting:      true,
			withGateway:   true,
			withBackend:   true,
			gatewayGetErr: assert.AnError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent := newEntity(testNamespacedObjectRef("virtual-cluster"))
			gateway := newTestEventGateway(ent.GetNamespace(), "gateway", "api-auth")
			backend := newTestEventGatewayBackendCluster(ent.GetNamespace(), "backend", gateway.Name)
			virtualCluster := newTestEventGatewayVirtualCluster(ent.GetNamespace(), "virtual-cluster", backend.Name)
			virtualCluster.SetKonnectID("virtual-cluster-id")
			virtualCluster.SetParentID("backend-id")
			virtualCluster.SetAncestorID("KonnectEventGateway", "gateway-id")
			virtualCluster.SetConditions([]metav1.Condition{{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			}})
			ent.SetFinalizers([]string{KonnectCleanupFinalizer})

			virtualClusterGets := 0
			builder := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(ent, virtualCluster).
				WithStatusSubresource(ent).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(
						ctx context.Context,
						cl client.WithWatch,
						key client.ObjectKey,
						obj client.Object,
						opts ...client.GetOption,
					) error {
						if key == client.ObjectKeyFromObject(virtualCluster) {
							virtualClusterGets++
							if tc.virtualClusterGone && virtualClusterGets > 1 {
								return apierrors.NewNotFound(
									schema.GroupResource{Group: "configuration.konghq.com", Resource: "eventgatewayvirtualclusters"},
									key.Name,
								)
							}
						}
						if key == client.ObjectKeyFromObject(gateway) && tc.gatewayGetErr != nil {
							return tc.gatewayGetErr
						}
						return cl.Get(ctx, key, obj, opts...)
					},
				})
			if tc.withGateway {
				builder.WithObjects(gateway)
			}
			if tc.withBackend {
				builder.WithObjects(backend)
			}
			cl := builder.Build()
			key := client.ObjectKeyFromObject(ent)
			if tc.deleting {
				require.NoError(t, cl.Delete(t.Context(), ent))
			}
			require.NoError(t, cl.Get(t.Context(), key, ent))

			reconciler := NewKonnectEntityReconciler[T, TEnt](nil, logging.DevelopmentMode, cl)
			res, err := reconciler.Reconcile(t.Context(), ent)
			assert.Equal(t, ctrl.Result{}, res)
			assert.Equal(t, 2, virtualClusterGets, "must reach the API auth parent-chain lookup")

			var updated T
			updatedPtr := TEnt(&updated)
			if tc.wantMissingAncestorErr || tc.gatewayGetErr != nil {
				require.Error(t, err)
				if tc.wantMissingAncestorErr {
					_, ok := errors.AsType[ReferencedObjectDoesNotExistError](err)
					assert.True(t, ok)
				} else {
					require.ErrorIs(t, err, tc.gatewayGetErr)
				}
				require.NoError(t, cl.Get(t.Context(), key, updatedPtr))
				assert.Contains(t, updatedPtr.GetFinalizers(), KonnectCleanupFinalizer)
				return
			}

			require.NoError(t, err)
			assert.True(t, apierrors.IsNotFound(cl.Get(t.Context(), key, updatedPtr)),
				"removing the cleanup finalizer must complete deletion")
		})
	}
}

func TestReconcileRemovesFinalizerWhenControlPlaneDisappearsBeforeAPIAuthLookup(t *testing.T) {
	upstream := testKongUpstreamOK.DeepCopy()
	controlPlane := testControlPlaneOK.DeepCopy()
	target := &configurationv1alpha1.KongTarget{
		Name:       "target",
		Namespace:  "default",
		Finalizers: []string{KonnectCleanupFinalizer},
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

func TestRemoveCleanupFinalizerIfAncestorIsGone(t *testing.T) {
	newTarget := func() *configurationv1alpha1.KongTarget {
		return &configurationv1alpha1.KongTarget{
			Name:       "target",
			Namespace:  "default",
			Finalizers: []string{KonnectCleanupFinalizer},
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

	missingAncestorErr := fmt.Errorf("failed to resolve API auth: %w", ReferencedObjectDoesNotExistError{
		Reference: client.ObjectKey{Namespace: "default", Name: "missing-parent"},
		Err: apierrors.NewNotFound(
			schema.GroupResource{Group: "configuration.konghq.com", Resource: "eventgatewayvirtualclusters"},
			"missing-parent",
		),
	})

	for _, tc := range []struct {
		name      string
		lookupErr error
		deleting  bool
	}{
		{name: "ignores nil error"},
		{name: "ignores unrelated error", lookupErr: assert.AnError, deleting: true},
		{name: "keeps active entity finalizer", lookupErr: missingAncestorErr},
		{
			name:      "ignores untyped not found error",
			lookupErr: apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "secret"),
			deleting:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := newTarget()
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(target).Build()
			if tc.deleting {
				require.NoError(t, cl.Delete(t.Context(), target))
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(target), target))
			}

			handled, res, err := removeCleanupFinalizerIfAncestorIsGone[configurationv1alpha1.KongTarget](
				t.Context(), cl, target, tc.lookupErr,
			)

			require.NoError(t, err)
			assert.False(t, handled)
			assert.Equal(t, ctrl.Result{}, res)
			assert.Contains(t, target.Finalizers, KonnectCleanupFinalizer)
		})
	}

	for _, tc := range []struct {
		name                    string
		withoutCleanupFinalizer bool
		updateErr               error
		wantResult              ctrl.Result
		wantError               bool
	}{
		{name: "removes only cleanup finalizer"},
		{name: "cleanup finalizer already absent", withoutCleanupFinalizer: true},
		{
			name: "requeues on conflict",
			updateErr: apierrors.NewConflict(
				schema.GroupResource{Resource: "kongtargets"}, "target", assert.AnError,
			),
			wantResult: ctrl.Result{Requeue: true},
		},
		{
			name:      "ignores entity already gone",
			updateErr: apierrors.NewNotFound(schema.GroupResource{Resource: "kongtargets"}, "target"),
		},
		{
			name:      "returns update failure",
			updateErr: assert.AnError,
			wantError: true,
		},
	} {
		t.Run("deleting entity with missing ancestor/"+tc.name, func(t *testing.T) {
			target := newTarget()
			target.Finalizers = append(target.Finalizers, "test.example/other-finalizer")
			if tc.withoutCleanupFinalizer {
				target.Finalizers = []string{"test.example/other-finalizer"}
			}
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(target).
				WithInterceptorFuncs(interceptor.Funcs{
					Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						assert.False(t, tc.withoutCleanupFinalizer, "must not update if the cleanup finalizer is already absent")
						if tc.updateErr != nil {
							return tc.updateErr
						}
						return cl.Update(ctx, obj, opts...)
					},
				}).
				Build()
			require.NoError(t, cl.Delete(t.Context(), target))
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(target), target))

			handled, res, err := removeCleanupFinalizerIfAncestorIsGone[configurationv1alpha1.KongTarget](
				t.Context(), cl, target, missingAncestorErr,
			)

			assert.True(t, handled)
			assert.Equal(t, tc.wantResult, res)
			if tc.wantError {
				require.ErrorIs(t, err, assert.AnError)
				assert.ErrorContains(t, err, "failed to remove finalizer")
			} else {
				require.NoError(t, err)
			}
			var updated configurationv1alpha1.KongTarget
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(target), &updated))
			if tc.updateErr == nil {
				assert.Equal(t, []string{"test.example/other-finalizer"}, updated.Finalizers)
			} else {
				assert.Contains(t, updated.Finalizers, KonnectCleanupFinalizer)
			}
		})
	}

	t.Run("removes finalizer when control plane is gone", func(t *testing.T) {
		target := newTarget()
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(target).Build()

		handled, res, err := removeCleanupFinalizerIfAncestorIsGone[configurationv1alpha1.KongTarget](
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

		handled, res, err := removeCleanupFinalizerIfAncestorIsGone[configurationv1alpha1.KongTarget](
			t.Context(), cl, target, missingControlPlaneErr,
		)

		require.NoError(t, err)
		assert.True(t, handled)
		assert.Equal(t, ctrl.Result{Requeue: true}, res)
	})
}
