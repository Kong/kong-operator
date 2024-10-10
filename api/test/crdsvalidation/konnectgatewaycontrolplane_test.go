package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKonnectGatewayControlPlane(t *testing.T) {
	t.Run("members can only be set on groups", func(t *testing.T) {
		CRDValidationTestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "members can be set on control-plane group",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
		}.Run(t)
	})

	t.Run("updates not allowed for status conditions", func(t *testing.T) {
		CRDValidationTestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "konnect.authRef change is not allowed for Programmed=True",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
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
		}.Run(t)
	})
}
