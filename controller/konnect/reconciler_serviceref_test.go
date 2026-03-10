package konnect

import (
	"fmt"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
)

type handleServiceRefTestCase[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
] struct {
	name                string
	ent                 TEnt
	objects             []client.Object
	expectResult        ctrl.Result
	expectError         bool
	expectErrorContains string
	// Returns true if the updated entity satisfy the assertion.
	// Returns false and error message if entity fails to satisfy it.
	updatedEntAssertions []func(TEnt) (ok bool, message string)
}

var testKongServiceOK = &configurationv1alpha1.KongService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "svc-ok",
		Namespace: "default",
	},
	Spec: configurationv1alpha1.KongServiceSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-ok",
			},
		},
	},
	Status: configurationv1alpha1.KongServiceStatus{
		Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: "12345",
			},
			ControlPlaneID: "123456789",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testKongServiceWithCPRefUnprogrammed = &configurationv1alpha1.KongService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "svc-with-cp-ref-unprogrammed",
		Namespace: "default",
	},
	Spec: configurationv1alpha1.KongServiceSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-not-programmed",
			},
		},
	},
	Status: configurationv1alpha1.KongServiceStatus{
		Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: "12345",
			},
			ControlPlaneID: "123456789",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testKongServiceNotProgrammed = &configurationv1alpha1.KongService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "svc-not-programmed",
		Namespace: "default",
	},
	Status: configurationv1alpha1.KongServiceStatus{
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionFalse,
			},
		},
	},
}

// testKongServiceNotProgrammedWithCPRef is a KongService with KonnectEntityProgrammed=False,
// a ControlPlane ref, and Status.Konnect == nil (never synced to Konnect).
// Used to test the nil pointer dereference regression in handleKongServiceRef.
var testKongServiceNotProgrammedWithCPRef = &configurationv1alpha1.KongService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "svc-not-programmed-with-cp-ref",
		Namespace: "default",
	},
	Spec: configurationv1alpha1.KongServiceSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-ok",
			},
		},
	},
	Status: configurationv1alpha1.KongServiceStatus{
		// Status.Konnect is intentionally nil to simulate a service not yet synced to Konnect.
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionFalse,
			},
		},
	},
}

var testKongServiceBeingDeleted = &configurationv1alpha1.KongService{
	ObjectMeta: metav1.ObjectMeta{
		Name:              "svc-being-deleted",
		Namespace:         "default",
		DeletionTimestamp: &metav1.Time{Time: time.Now()},
		Finalizers:        []string{KonnectCleanupFinalizer},
	},
}

func TestHandleServiceRef(t *testing.T) {
	testCases := []handleServiceRefTestCase[configurationv1alpha1.KongRoute, *configurationv1alpha1.KongRoute]{
		{
			name: "has service ref",
			ent: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-1",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "svc-ok",
						},
					},
				},
			},
			objects: []client.Object{
				testKongServiceOK,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongRoute) (bool, string){
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongServiceRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongRoute does not have KongServiceRefValid condition set to True"
				},
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return len(ks.OwnerReferences) == 0,
						"OwnerReference of KongRoute is set but shouldn't be"
				},
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return ks.Status.Konnect.ServiceID == "12345",
						"KongRoute does not have Konnect Service ID set"
				},
			},
		},
		{
			name: "with service ref to a service that is being deleted",
			ent: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-with-service-ref-being-deleted",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "svc-ok",
						},
					},
				},
			},
			objects: []client.Object{
				testKongServiceBeingDeleted,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{},
			expectError:  true,
		},
		{
			name: "has service ref to an unprogrammed service",
			ent: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-1",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "svc-not-programmed",
						},
					},
				},
			},
			objects: []client.Object{
				testKongServiceNotProgrammed,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{
				Requeue: false,
			},
			expectError: false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongRoute) (bool, string){
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongServiceRefValidConditionType &&
							c.Status == metav1.ConditionFalse &&
							c.Reason == konnectv1alpha1.KongServiceRefReasonInvalid
					}), "KongRoute does not have KongServiceRefValid condition set to False"
				},
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return lo.NoneBy(ks.OwnerReferences, func(o metav1.OwnerReference) bool {
						return o.Kind == "KongService" && o.Name == "svc-ok"
					}), "OwnerReference of KongRoute is set but it shouldn't be"
				},
			},
		},
		{
			// Regression test: on the 2nd+ reconciliation the KongRoute already has both the
			// KongServiceRefValid=False condition AND Status.Konnect initialized (from the 1st
			// reconciliation's SetKonnectID("") call). In that state, patch.ApplyStatusPatchIfNotEmpty
			// returns op.Noop (nothing changed), causing a fall-through to the ServiceID assignment.
			// If KongService.Status.Konnect is nil, the old code panicked with a nil pointer dereference
			// on kongSvc.Status.Konnect.GetKonnectID().
			// The fix uses kongSvc.GetKonnectID() which is nil-safe.
			name: "has service ref to an unprogrammed service with nil Konnect status (2nd reconciliation, regression for nil panic)",
			ent: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-1",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "svc-not-programmed-with-cp-ref",
						},
					},
				},
				Status: configurationv1alpha1.KongRouteStatus{
					// Both Konnect and condition are pre-set as they would be after the 1st reconciliation.
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{},
					Conditions: []metav1.Condition{
						{
							Type:               konnectv1alpha1.KongServiceRefValidConditionType,
							Status:             metav1.ConditionFalse,
							Reason:             konnectv1alpha1.KongServiceRefReasonInvalid,
							Message:            "Referenced KongService default/svc-not-programmed-with-cp-ref is not programmed yet",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			objects: []client.Object{
				testKongServiceNotProgrammedWithCPRef,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongRoute) (bool, string){
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					if ks.Status.Konnect == nil {
						return false, "KongRoute.Status.Konnect is nil"
					}
					return ks.Status.Konnect.ServiceID == "",
						fmt.Sprintf(
							"KongRoute.Status.Konnect.ServiceID should be empty (KongService has no Konnect ID), got %q",
							ks.Status.Konnect.ServiceID,
						)
				},
			},
		},
		{
			name: "has service ref which has an unprogrammed cp",
			ent: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-1",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "svc-with-cp-ref-unprogrammed",
						},
					},
				},
			},
			objects: []client.Object{
				testKongServiceWithCPRefUnprogrammed,
				testControlPlaneNotProgrammed,
			},
			expectResult: ctrl.Result{
				Requeue: true,
			},
			expectError: false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongRoute) (bool, string){
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType &&
							c.Status == metav1.ConditionFalse &&
							c.Reason == konnectv1alpha1.ControlPlaneRefReasonInvalid
					}), "KongRoute does not have ControlPlaneRef condition set to False"
				},
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongServiceRefValidConditionType &&
							c.Status == metav1.ConditionTrue &&
							c.Reason == konnectv1alpha1.KongServiceRefReasonValid
					}), "KongRoute does not have KongServiceRefValid condition set to True"
				},
				func(ks *configurationv1alpha1.KongRoute) (bool, string) {
					return lo.NoneBy(ks.OwnerReferences, func(o metav1.OwnerReference) bool {
						return o.Kind == "KongService" && o.Name == "svc-ok"
					}), "OwnerReference of KongRoute is set but it shouldn't be"
				},
			},
		},
	}

	testHandleServiceRef(t, testCases)
}

func testHandleServiceRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	testCases []handleServiceRefTestCase[T, TEnt],
) {
	t.Helper()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha2.AddToScheme(scheme))
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.ent).
				WithObjects(tc.objects...).
				// WithStatusSubresource is required for updating status of handled entity.
				WithStatusSubresource(tc.ent).
				Build()
			require.NoError(t, fakeClient.SubResource("status").Update(t.Context(), tc.ent))

			res, err := handleKongServiceRef(t.Context(), fakeClient, tc.ent)

			updatedEnt := tc.ent.DeepCopyObject().(TEnt)
			require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(tc.ent), updatedEnt))
			for _, assertion := range tc.updatedEntAssertions {
				ok, msg := assertion(updatedEnt)
				require.True(t, ok, msg)
			}

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectErrorContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectResult, res)
		})
	}
}
