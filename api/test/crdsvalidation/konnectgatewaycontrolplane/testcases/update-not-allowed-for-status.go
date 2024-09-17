package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// updatesNotAllowedForStatus are test cases checking if updates to konnect.authRef
// are indeed blocked for some status conditions.
var updatesNotAllowedForStatus = testCasesGroup{
	Name: "updates not allowed for status conditions",
	TestCases: []testCase{
		{
			Name: "konnect.authRef change is not allowed for Programmed=True",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
				Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Programmed",
							Status:             metav1.ConditionTrue,
							Reason:             "Valid",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			Update: func(kcp *konnectv1alpha1.KonnectGatewayControlPlane) {
				kcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "name-2"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.konnect.authRef is immutable when an entity is already Programme"),
		},
		{
			Name: "konnect.authRef change is not allowed for APIAuthValid=True",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
				Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "APIAuthValid",
							Status:             metav1.ConditionTrue,
							Reason:             "Valid",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			Update: func(kcp *konnectv1alpha1.KonnectGatewayControlPlane) {
				kcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "name-2"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.konnect.authRef is immutable when an entity refers to a Valid API Auth Configuration"),
		},
		{
			Name: "konnect.authRef change is allowed when cp is not Programmed=True nor APIAuthValid=True",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
				Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "APIAuthValid",
							Status:             metav1.ConditionFalse,
							Reason:             "Invalid",
							LastTransitionTime: metav1.Now(),
						},
						{
							Type:               "Programmed",
							Status:             metav1.ConditionFalse,
							Reason:             "NotProgrammed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			Update: func(kcp *konnectv1alpha1.KonnectGatewayControlPlane) {
				kcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "name-2"
			},
		},
	},
}
