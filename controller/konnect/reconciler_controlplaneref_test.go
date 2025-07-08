package konnect

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

type handleControlPlaneRefTestCase[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]] struct {
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

func TestHandleControlPlaneRef(t *testing.T) {
	var (
		cpOK = &konnectv1alpha2.KonnectGatewayControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cp-ok",
			},
			Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
				KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
					ID: "cp-12345",
				},
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		cpGroup = &konnectv1alpha2.KonnectGatewayControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cp-group",
			},
			Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
				CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
					ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
				},
			},
			Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
				KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
					ID: "cp-group-12345",
				},
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		cpNotProgrammed = &konnectv1alpha2.KonnectGatewayControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cp-not-programmed",
			},
			Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
				KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
					ID: "cp-12345",
				},
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
						Status: metav1.ConditionFalse,
					},
				},
			},
		}

		svcNoCPRef = &configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "svc-no-cp-ref",
			},
		}

		svcCPRefOK = &configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "svc-cp-ok",
			},
			Spec: configurationv1alpha1.KongServiceSpec{
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: "cp-ok",
					},
				},
			},
		}

		svcCPRefNotFound = &configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "svc-cp-not-found",
			},
			Spec: configurationv1alpha1.KongServiceSpec{
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: "cp-not-found",
					},
				},
			},
		}

		svcCPRefIncompatibleType = &configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "svc-cp-incompatible",
			},
			Spec: configurationv1alpha1.KongServiceSpec{
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: "cp-group",
					},
				},
			},
		}

		svcCPRefNotProgrammed = &configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "svc-cp-not-programmed",
			},
			Spec: configurationv1alpha1.KongServiceSpec{
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: "cp-not-programmed",
					},
				},
			},
		}
	)
	testCasesService := []handleControlPlaneRefTestCase[configurationv1alpha1.KongService, *configurationv1alpha1.KongService]{
		{
			name:         "no control plane ref",
			ent:          svcNoCPRef,
			expectResult: ctrl.Result{},
			expectError:  false,
		},
		{
			name: "control plane OK",
			ent:  svcCPRefOK,
			objects: []client.Object{
				cpOK,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(svc *configurationv1alpha1.KongService) (ok bool, message string){
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return svc.GetControlPlaneID() == "cp-12345" && lo.ContainsBy(svc.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType
					}), "service should get control plane ID"
				},
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return len(svc.OwnerReferences) == 0,
						"service should have 0 owner references"
				},
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return lo.ContainsBy(svc.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "service should have ControlPlaneRefValid set to true"
				},
			},
		},
		{
			name:                "control plane not found",
			ent:                 svcCPRefNotFound,
			expectResult:        ctrl.Result{},
			expectError:         true,
			expectErrorContains: `referenced Control Plane "<konnectNamespacedRef:cp-not-found>" does not exist`,
			updatedEntAssertions: []func(svc *configurationv1alpha1.KongService) (ok bool, message string){
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return lo.ContainsBy(svc.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "service should have ControlPlaneRefValid set to False"
				},
			},
		},
		{
			name: "control plane with incompatible cluster type (ControlPlane Group)",
			ent:  svcCPRefIncompatibleType,
			objects: []client.Object{
				cpGroup,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(svc *configurationv1alpha1.KongService) (ok bool, message string){
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return lo.ContainsBy(svc.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "service should have ControlPlaneRefValid set to False"
				},
			},
		},
		{
			name: "control plane not programmed",
			ent:  svcCPRefNotProgrammed,
			objects: []client.Object{
				cpNotProgrammed,
			},
			expectResult: ctrl.Result{Requeue: true},
			expectError:  false,
			updatedEntAssertions: []func(svc *configurationv1alpha1.KongService) (ok bool, message string){
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return lo.ContainsBy(svc.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "service should have ControlPlaneRefValid set to False"
				},
			},
		},
	}

	testHandleControlPlaenRef(t, testCasesService)
}

func testHandleControlPlaenRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T, testCases []handleControlPlaneRefTestCase[T, TEnt],
) {
	t.Helper()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := scheme.Get()
			require.NoError(t, configurationv1alpha1.AddToScheme(s))
			require.NoError(t, konnectv1alpha1.AddToScheme(s))

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.ent).
				WithObjects(tc.objects...).
				// WithStatusSubresource is required for updating status of handled entity.
				WithStatusSubresource(tc.ent).
				Build()
			require.NoError(t, fakeClient.SubResource("status").Update(t.Context(), tc.ent))

			res, err := handleControlPlaneRef(t.Context(), fakeClient, tc.ent)

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

func TestGetControlPlaneRef(t *testing.T) {
	testCases := []func(t *testing.T){
		testGetControlPlaneRef(
			"no control plane ref for KongService",
			&configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "svc-no-cp-ref",
				},
			},
			mo.None[commonv1alpha1.ControlPlaneRef](),
		),
		testGetControlPlaneRef(
			"control plane ref for KongService",
			&configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "svc-cp-ok",
				},
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "cp-ok",
						},
					},
				},
			},
			mo.Some(commonv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: "cp-ok",
				},
			}),
		),
		testGetControlPlaneRef(
			"no control plane ref for KonnectCloudGatewayDataPlaneGroupConfiguration",
			&konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-group-config",
				},
				Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{},
			},
			mo.Some(commonv1alpha1.ControlPlaneRef{}),
		),
		testGetControlPlaneRef(
			"control plane ref for KonnectCloudGatewayDataPlaneGroupConfiguration",
			&konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-group-config",
				},
				Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
					ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "cp-ok",
						},
					},
				},
			},
			mo.Some(commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "cp-ok",
				},
			}),
		),
	}

	for _, tc := range testCases {
		tc(t)
	}
}

func testGetControlPlaneRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	name string, obj TEnt, expected mo.Option[commonv1alpha1.ControlPlaneRef],
) func(t *testing.T) {
	return func(t *testing.T) {
		t.Run(name, func(t *testing.T) {
			result := controlplane.GetControlPlaneRef(obj)
			require.Equal(t, expected, result)
		})
	}
}
