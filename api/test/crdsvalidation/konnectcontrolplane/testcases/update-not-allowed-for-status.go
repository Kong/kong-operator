package testcases

import (
	"github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// updatesNotAllowedForStatus are test cases checking if updates to konnect.authRef
// are indeed blocked for some status conditions.
var updatesNotAllowedForStatus = kcpTestCasesGroup{
	Name: "updates not allowed for status conditions",
	TestCases: []kcpTestCase{
		{
			Name: "konnect.authRef change is not allowed for Programmed=True",
			KonnectControlPlane: konnectv1alpha1.KonnectControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectControlPlaneSpec{
					CreateControlPlaneRequest: components.CreateControlPlaneRequest{
						Name:        "cp-1",
						ClusterType: lo.ToPtr(components.ClusterTypeClusterTypeControlPlane),
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
				Status: konnectv1alpha1.KonnectControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Programmed",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			Update: func(kcp *konnectv1alpha1.KonnectControlPlane) {
				kcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "name-2"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.konnect.authRef is immutable when entity is already Programme"),
		},
		{
			Name: "konnect.authRef change is not allowed for APIAuthValid=True",
			KonnectControlPlane: konnectv1alpha1.KonnectControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectControlPlaneSpec{
					CreateControlPlaneRequest: components.CreateControlPlaneRequest{
						Name:        "cp-1",
						ClusterType: lo.ToPtr(components.ClusterTypeClusterTypeControlPlane),
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
				Status: konnectv1alpha1.KonnectControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "APIAuthValid",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			Update: func(kcp *konnectv1alpha1.KonnectControlPlane) {
				kcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "name-2"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.konnect.authRef is immutable when entity is already Programme"),
		},
		{
			Name: "konnect.authRef change is allowed when cp is not Programmed=True nor APIAuthValid=True",
			KonnectControlPlane: konnectv1alpha1.KonnectControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectControlPlaneSpec{
					CreateControlPlaneRequest: components.CreateControlPlaneRequest{
						Name:        "cp-1",
						ClusterType: lo.ToPtr(components.ClusterTypeClusterTypeControlPlane),
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
				Status: konnectv1alpha1.KonnectControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "APIAuthValid",
							Status: metav1.ConditionFalse,
						},
						{
							Type:   "Programmed",
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			Update: func(kcp *konnectv1alpha1.KonnectControlPlane) {
				kcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "name-2"
			},
		},
	},
}
