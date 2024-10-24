package crdsvalidation_test

import (
	"fmt"
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
			{
				Name: "cluster_type change is not allowed",
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
				},
				Update: func(kcp *konnectv1alpha1.KonnectGatewayControlPlane) {
					kcp.Spec.ClusterType = sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup.ToPointer()
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.cluster_type is immutable"),
			},
		}.Run(t)
	})

	t.Run("labels constraints", func(t *testing.T) {
		CRDValidationTestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "spec.labels of length 40 is allowed",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: func() map[string]string {
								labels := make(map[string]string)
								for i := range 40 {
									labels[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("value-%d", i)
								}
								return labels
							}(),
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
				Name: "spec.labels length must not be greater than 40",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: func() map[string]string {
								labels := make(map[string]string)
								for i := range 41 {
									labels[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("value-%d", i)
								}
								return labels
							}(),
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels must not have more than 40 entries"),
			},
			{
				Name: "spec.labels keys' length must not be greater than 63",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								lo.RandomString(64, lo.AllCharset): "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must be of length 1-63 characters"),
			},
			{
				Name: "spec.labels keys' length must at least 1 character long",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must be of length 1-63 characters"),
			},
			//
			{
				Name: "spec.labels values' length must not be greater than 63",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"key": lo.RandomString(64, lo.AllCharset),
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels values must be of length 1-63 characters"),
			},
			{
				Name: "spec.labels values' length must at least 1 character long",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"key": "",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels values must be of length 1-63 characters"),
			},
			{
				Name: "spec.labels keys must not start with k8s",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"k8s_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'"),
			},
			{
				Name: "spec.labels keys must not start with kong",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"kong_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'"),
			},
			{
				Name: "spec.labels keys must not start with konnect",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"konnect_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'"),
			},
			{
				Name: "spec.labels keys must not start with mesh",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"mesh_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'"),
			},
			{
				Name: "spec.labels keys must not start with kic",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"kic_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'"),
			},
			{
				Name: "spec.labels keys must not start with insomnia",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"insomnia_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'"),
			},
			{
				Name: "spec.labels keys must not start with underscore",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'"),
			},
			{
				Name: "spec.labels keys must satisfy the '^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$' pattern",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"key-": "value",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.labels keys must satisfy the '^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$' pattern"),
			},
		}.Run(t)
	})

	t.Run("restriction on cluster types", func(t *testing.T) {
		CRDValidationTestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "unspecified cluster type (defaulting to CLUSTR_TYPE_CONTROL_PLANE) is allowed",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
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
				Name: "CLUSTER_TYPE_CONTROL_PLANE_GROUP is supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup.ToPointer(),
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
				Name: "CLUSTER_TYPE_CONTROL_PLANE is supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: sdkkonnectcomp.ClusterTypeClusterTypeControlPlane.ToPointer(),
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
				Name: "CLUSTER_TYPE_K8S_INGRESS_CONTROLLER is supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: sdkkonnectcomp.ClusterTypeClusterTypeK8SIngressController.ToPointer(),
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
				Name: "CLUSTER_TYPE_HYBRID not is supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: sdkkonnectcomp.ClusterTypeClusterTypeHybrid.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cluster_type must be one of 'CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE' or 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'"),
			},
			{
				Name: "CLUSTER_TYPE_SERVERLESS is not supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: sdkkonnectcomp.ClusterTypeClusterTypeServerless.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cluster_type must be one of 'CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE' or 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'"),
			},
			{
				Name: "CLUSTER_TYPE_CUSTOM is not supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name:        "cp-1",
							ClusterType: sdkkonnectcomp.ClusterType("CLUSTER_TYPE_CUSTOM").ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cluster_type must be one of 'CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE' or 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'"),
			},
		}.Run(t)
	})
}
