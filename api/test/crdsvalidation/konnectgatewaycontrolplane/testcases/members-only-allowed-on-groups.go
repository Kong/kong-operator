package testcases

import (
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

var membersCanOnlyBeSetForControlPlaneGroups = testCasesGroup{
	Name: "members can only be set on groups",
	TestCases: []testCase{
		{
			Name: "members can be set on control-plane group",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cpg-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
			},
		},
		{
			Name: "members cannot be set on regular control-planes",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cpg-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.members is only applicable for ControlPlanes that are created as groups"),
		},
		{
			Name: "members cannot be set on a KIC control-planes",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cpg-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeK8SIngressController),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.members is only applicable for ControlPlanes that are created as groups"),
		},
		{
			Name: "members cannot be set on hybrid control-planes",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cpg-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeHybrid),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.members is only applicable for ControlPlanes that are created as groups"),
		},
		{
			Name: "members cannot be set on serverless control-planes",
			KonnectGatewayControlPlane: konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: commonObjectMeta,
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cpg-1",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeServerless),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "name-1",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.members is only applicable for ControlPlanes that are created as groups"),
		},
	},
}
