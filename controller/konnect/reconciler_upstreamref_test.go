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

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/konnect/constraints"
)

type handleUpstreamRefTestCase[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]] struct {
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

var testKongUpstreamOK = &configurationv1alpha1.KongUpstream{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "upstream-ok",
		Namespace: "default",
	},
	Spec: configurationv1alpha1.KongUpstreamSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-ok",
			},
		},
		KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
			Slots: lo.ToPtr(int64(12345)),
		},
	},
	Status: configurationv1alpha1.KongUpstreamStatus{
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

var testKongUpstreamNotProgrammed = &configurationv1alpha1.KongUpstream{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "upstream-not-programmed",
		Namespace: "default",
	},
	Status: configurationv1alpha1.KongUpstreamStatus{
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionFalse,
			},
		},
	},
}

var testKongUpstreamNoControlPlaneRef = &configurationv1alpha1.KongUpstream{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "upstream-no-cp-ref",
		Namespace: "default",
	},
	Status: configurationv1alpha1.KongUpstreamStatus{
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testKongUpstreamBeingDeleted = &configurationv1alpha1.KongUpstream{
	ObjectMeta: metav1.ObjectMeta{
		Name:              "upstream-being-deleted",
		Namespace:         "default",
		DeletionTimestamp: &metav1.Time{Time: time.Now()},
		Finalizers:        []string{"target-0"},
	},
}

var testKongUpstreamControlPlaneRefNotFound = &configurationv1alpha1.KongUpstream{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "upstream-cpref-not-found",
		Namespace: "default",
	},
	Spec: configurationv1alpha1.KongUpstreamSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-not-found",
			},
		},
		KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
			Slots: lo.ToPtr(int64(12345)),
		},
	},
	Status: configurationv1alpha1.KongUpstreamStatus{
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

var testKongUpstreamControlPlaneRefNotProgrammed = &configurationv1alpha1.KongUpstream{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "upstream-cpref-not-programmed",
		Namespace: "default",
	},
	Spec: configurationv1alpha1.KongUpstreamSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-not-programmed",
			},
		},
		KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
			Slots: lo.ToPtr(int64(12345)),
		},
	},
	Status: configurationv1alpha1.KongUpstreamStatus{
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

var testControlPlaneOK = &konnectv1alpha2.KonnectGatewayControlPlane{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cp-ok",
		Namespace: "default",
	},
	Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{},
	Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
		KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
			ID: "123456789",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testControlPlaneNotProgrammed = &konnectv1alpha2.KonnectGatewayControlPlane{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cp-not-programmed",
		Namespace: "default",
	},
	Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{},
	Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionFalse,
			},
		},
	},
}

func TestHandleUpstreamRef(t *testing.T) {
	// The test cases here includes test cases for handling upstream ref for KongTarget, which are expected to have KongUpstream reference.
	// We can define test cases for other types and call `testHandleUpstreamRef` to test handling entities with other types.
	testCases := []handleUpstreamRefTestCase[configurationv1alpha1.KongTarget, *configurationv1alpha1.KongTarget]{
		{
			name: "has upstream ref and control plane ref",
			ent: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-ok",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NameRef{
						Name: "upstream-ok",
					},
				},
			},
			objects: []client.Object{
				testKongUpstreamOK,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongTarget) (bool, string){
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return lo.ContainsBy(kt.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongUpstreamRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongTarget does not have KongUpstreamRefValid condition set to True"
				},
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return lo.ContainsBy(kt.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongTarget does not have ControlPlaneRefValid condition set to True"
				},
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return !lo.ContainsBy(kt.OwnerReferences, func(o metav1.OwnerReference) bool {
						return o.Kind == "KongUpstream" && o.Name == "upstream-ok"
					}), "OwnerReference of KongTarget is set but shouldn't"
				},
			},
		},
		{
			name: "upstream ref not found",
			ent: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-upstream-notfound",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NameRef{
						Name: "upstream-nonexist",
					},
				},
			},
			expectError:         true,
			expectErrorContains: "referenced Kong Upstream default/upstream-nonexist does not exist",
			updatedEntAssertions: []func(*configurationv1alpha1.KongTarget) (bool, string){
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return lo.ContainsBy(kt.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongUpstreamRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongTarget does not have KongUpstreamRefValid condition set to False"
				},
			},
		},
		{
			name: "referenced KongUpstream not programmed",
			ent: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-upstream-not-programmed",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NameRef{
						Name: "upstream-not-programmed",
					},
				},
			},
			objects:      []client.Object{testKongUpstreamNotProgrammed},
			expectError:  false,
			expectResult: ctrl.Result{Requeue: true},
			updatedEntAssertions: []func(*configurationv1alpha1.KongTarget) (bool, string){
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return lo.ContainsBy(kt.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongUpstreamRefValidConditionType && c.Status == metav1.ConditionFalse &&
							c.Message == fmt.Sprintf("Referenced KongUpstream %s/%s is not programmed yet",
								testKongUpstreamNotProgrammed.Namespace, testKongUpstreamNotProgrammed.Name)
					}), "KongTarget does not have KongUpstreamRefValid condition set to False"
				},
			},
		},
		{
			name: "referenced KongUpstream has no ControlPlaneRef",
			ent: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-upstream-no-cpref",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NameRef{
						Name: "upstream-no-cp-ref",
					},
				},
			},
			objects:     []client.Object{testKongUpstreamNoControlPlaneRef},
			expectError: true,
			expectErrorContains: fmt.Sprintf("references a KongUpstream %s/%s which does not have a ControlPlane ref",
				testKongUpstreamNoControlPlaneRef.Namespace, testKongUpstreamNoControlPlaneRef.Name),
			updatedEntAssertions: []func(*configurationv1alpha1.KongTarget) (bool, string){
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return lo.ContainsBy(kt.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongUpstreamRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongTarget does not have KongUpstreamRefValid condition set to True"
				},
			},
		},
		{
			name: "referenced KongUpstream is being deleted",
			ent: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-upstream-being-deleted",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NameRef{
						Name: "upstream-being-deleted",
					},
				},
			},
			objects:             []client.Object{testKongUpstreamBeingDeleted},
			expectError:         true,
			expectErrorContains: fmt.Sprintf("referenced Kong Upstream %s/%s is being deleted", testKongUpstreamBeingDeleted.Namespace, testKongUpstreamBeingDeleted.Name),
		},
		{
			name: "ControlPlaneRef not found",
			ent: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-upstream-cpref-not-found",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NameRef{
						Name: "upstream-cpref-not-found",
					},
				},
			},
			objects:     []client.Object{testKongUpstreamControlPlaneRefNotFound},
			expectError: true,
			expectErrorContains: fmt.Sprintf(`referenced Control Plane %q does not exist`,
				testKongUpstreamControlPlaneRefNotFound.Spec.ControlPlaneRef.String(),
			),
		},
		{
			name: "ControlPlaneRef not programmed",
			ent: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-upstream-cpref-not-programmed",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NameRef{
						Name: "upstream-cpref-not-programmed",
					},
				},
			},
			objects: []client.Object{
				testKongUpstreamControlPlaneRefNotProgrammed,
				testControlPlaneNotProgrammed,
			},
			expectError:  false,
			expectResult: ctrl.Result{Requeue: true},
			updatedEntAssertions: []func(*configurationv1alpha1.KongTarget) (bool, string){
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return lo.ContainsBy(kt.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongUpstreamRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongTarget does not have KongUpstreamRefValid condition set to True"
				},
				func(kt *configurationv1alpha1.KongTarget) (bool, string) {
					return lo.ContainsBy(kt.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongTarget does not have ControlPlaneRefValid condition set to False"
				},
			},
		},
	}
	testHandleUpstreamRef(t, testCases)
}

func testHandleUpstreamRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	t *testing.T, testCases []handleUpstreamRefTestCase[T, TEnt],
) {
	t.Helper()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha2.AddToScheme(scheme))
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(tc.ent).WithObjects(tc.objects...).
				// WithStatusSubresource is required for updating status of handled entity.
				WithStatusSubresource(tc.ent).Build()
			require.NoError(t, fakeClient.SubResource("status").Update(t.Context(), tc.ent))

			res, err := handleKongUpstreamRef(t.Context(), fakeClient, tc.ent)

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
