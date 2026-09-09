package konnect

import (
	"context"
	"testing"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// TestReconcileDeleteDoesNotDoubleDeleteOnConflict is a regression test for a CI
// flake in TestKonnectConfigStore: the manager's cached copy of an entity being
// deleted can have a stale resourceVersion, so the finalizer-removal write can
// conflict. Before the fix that used r.Client.Update, which returned a 409 on a
// stale resourceVersion; the reconciler silently requeued and, on the next pass,
// deleted the same entity from Konnect a second time. The fix removes the
// finalizer with a non-optimistic merge patch instead, so a stale resourceVersion
// no longer causes a duplicate Konnect delete.
//
// The interceptor below simulates that staleness deterministically: it forces
// every r.Client.Update of the KonnectConfigStore to conflict, exactly as a real
// stale-cache Update would. The deletion branch under test must not call Update
// at all, so the interceptor should never fire.
func TestReconcileDeleteDoesNotDoubleDeleteOnConflict(t *testing.T) {
	const (
		cpKonnectID = "cp-12345"
		csKonnectID = "config-store-12345"
	)

	apiAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		Name:      "api-auth",
		Namespace: "default",
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
			Token:     "kpat_test",
			ServerURL: sdkmocks.SDKServerURL,
		},
		Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
					Status:             metav1.ConditionTrue,
					Reason:             konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		Name:      "cp",
		Namespace: "default",
		Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: apiAuth.Name,
				},
			},
		},
		Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: cpKonnectID},
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	configStore := &konnectv1alpha1.KonnectConfigStore{
		Name:       "config-store",
		Namespace:  "default",
		Finalizers: []string{KonnectCleanupFinalizer},
		Spec: konnectv1alpha1.KonnectConfigStoreSpec{
			ControlPlaneRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: cp.Name},
			},
		},
		Status: konnectv1alpha1.KonnectConfigStoreStatus{
			KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: csKonnectID},
			ControlPlaneID:      &konnectv1alpha1.KonnectEntityRef{ID: cpKonnectID},
		},
	}
	key := client.ObjectKeyFromObject(configStore)

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(apiAuth, cp, configStore).
		WithStatusSubresource(&konnectv1alpha1.KonnectConfigStore{}).
		WithInterceptorFuncs(interceptor.Funcs{
			// Simulate the informer cache lag that caused the CI flake: any
			// finalizer-removal write on the entity being deleted conflicts, as
			// if the manager's cached copy carried a stale resourceVersion.
			Update: func(
				ctx context.Context,
				cl client.WithWatch,
				obj client.Object,
				opts ...client.UpdateOption,
			) error {
				if _, ok := obj.(*konnectv1alpha1.KonnectConfigStore); ok {
					return apierrors.NewConflict(
						schema.GroupResource{Group: "konnect.konghq.com", Resource: "konnectconfigstores"},
						obj.GetName(), assert.AnError,
					)
				}
				return cl.Update(ctx, obj, opts...)
			},
		}).
		Build()

	require.NoError(t, cl.Delete(t.Context(), configStore))

	factory := sdkmocks.NewMockSDKFactory(t)
	factory.SDK.ConfigStoresSDK.EXPECT().
		DeleteConfigStore(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteConfigStoreRequest) bool {
			return req.ControlPlaneID == cpKonnectID && req.ConfigStoreID == csKonnectID
		})).
		Return(&sdkkonnectops.DeleteConfigStoreResponse{}, nil).
		Once()

	reconciler := NewKonnectEntityReconciler[konnectv1alpha1.KonnectConfigStore](
		factory, logging.DevelopmentMode, cl,
		WithMetricRecorder[konnectv1alpha1.KonnectConfigStore](&metricsmocks.MockRecorder{}),
	)

	// Drive Reconcile like a real controller would across several watch-triggered
	// passes (each status patch here would trigger a fresh reconcile in
	// production), re-reading the object from the client each time. Bounded to a
	// generous number of passes; the loop exits early once the object is gone.
	for range 6 {
		var cur konnectv1alpha1.KonnectConfigStore
		if err := cl.Get(t.Context(), key, &cur); apierrors.IsNotFound(err) {
			break
		} else {
			require.NoError(t, err)
		}

		res, err := reconciler.Reconcile(t.Context(), &cur)
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res, "deletion must not conflict-requeue")
	}

	var gone konnectv1alpha1.KonnectConfigStore
	assert.True(t, apierrors.IsNotFound(cl.Get(t.Context(), key, &gone)),
		"finalizer must be released so the object is deleted")

	factory.SDK.ConfigStoresSDK.AssertExpectations(t)
}
