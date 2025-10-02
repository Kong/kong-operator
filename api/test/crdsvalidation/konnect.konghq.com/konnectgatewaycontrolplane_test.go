package crdsvalidation_test

import (
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	common "github.com/kong/kong-operator/test/crdsvalidation/common"
)

func TestKonnectGatewayControlPlane(t *testing.T) {
	t.Run("members can only be set on groups", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "members can be set on control-plane group",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cpg-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
						},
						Members: []corev1.LocalObjectReference{
							{
								Name: "cp-1",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "members cannot be set on regular control-planes",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cpg-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
						},
						Members: []corev1.LocalObjectReference{
							{
								Name: "cp-1",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cpg-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController),
						},
						Members: []corev1.LocalObjectReference{
							{
								Name: "cp-1",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cpg-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeServerless),
						},
						Members: []corev1.LocalObjectReference{
							{
								Name: "cp-1",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
		common.TestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "konnect.authRef change is not allowed for Programmed=True",
				// Tracking issue: https://github.com/Kong/kubernetes-configuration/issues/504
				SkipReason: "Fails when v1alpha2 is the storage version due to missing conversion logic. This test could be re-enabled by implementing conversion logic using a conversion webhook: https://github.com/Kong/kubernetes-configuration/issues/504",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
				// Tracking issue: https://github.com/Kong/kubernetes-configuration/issues/504
				SkipReason: "Fails when v1alpha2 is the storage version due to missing conversion logic. This test could be re-enabled by implementing conversion logic using a conversion webhook: https://github.com/Kong/kubernetes-configuration/issues/504",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
				// Tracking issue: https://github.com/Kong/kubernetes-configuration/issues/504
				SkipReason: "Fails when v1alpha2 is the storage version due to missing conversion logic. This test could be re-enabled by implementing conversion logic using a conversion webhook: https://github.com/Kong/kubernetes-configuration/issues/504",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				Update: func(kcp *konnectv1alpha1.KonnectGatewayControlPlane) {
					kcp.Spec.ClusterType = sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup.ToPointer()
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.cluster_type is immutable"),
			},
		}.Run(t)
	})

	t.Run("labels constraints", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "spec.labels of length 40 is allowed",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: func() map[string]string {
								labels := make(map[string]string)
								for i := range 40 {
									labels[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("value-%d", i)
								}
								return labels
							}(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "spec.labels length must not be greater than 40",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: func() map[string]string {
								labels := make(map[string]string)
								for i := range 41 {
									labels[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("value-%d", i)
								}
								return labels
							}(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								lo.RandomString(64, lo.AllCharset): "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"key": lo.RandomString(64, lo.AllCharset),
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"key": "",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"k8s_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"kong_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"konnect_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"mesh_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"kic_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"insomnia_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
							Labels: map[string]string{
								"_key": "value",
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
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
		common.TestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "unspecified cluster type (defaulting to CLUSTR_TYPE_CONTROL_PLANE) is allowed",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name: lo.ToPtr("cp-1"),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "CLUSTER_TYPE_CONTROL_PLANE_GROUP is supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "CLUSTER_TYPE_CONTROL_PLANE is supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "CLUSTER_TYPE_K8S_INGRESS_CONTROLLER is supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "CLUSTER_TYPE_SERVERLESS is not supported",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeServerless.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterType("CLUSTER_TYPE_CUSTOM").ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cluster_type must be one of 'CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE' or 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'"),
			},
			{
				Name: "cluster type is immutable",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name: lo.ToPtr("cp-1"),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				Update: func(cp *konnectv1alpha1.KonnectGatewayControlPlane) {
					cp.Spec.ClusterType = sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup.ToPointer()
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.cluster_type is immutable"),
			},
			{
				Name: "cluster type is immutable when having it set and then trying to unset it",
				// Tracking issue: https://github.com/Kong/kubernetes-configuration/issues/504
				SkipReason: "Fails when v1alpha2 is the storage version due to missing conversion logic. This test could be re-enabled by implementing conversion logic using a conversion webhook: https://github.com/Kong/kubernetes-configuration/issues/504",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				Update: func(cp *konnectv1alpha1.KonnectGatewayControlPlane) {
					cp.Spec.ClusterType = nil
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.cluster_type is immutable"),
			},
			{
				Name: "cannot set cloud_gateway to true for cluster_type CLUSTER_TYPE_K8S_INGRESS_CONTROLLER",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:         lo.ToPtr("cp-1"),
							ClusterType:  sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController.ToPointer(),
							CloudGateway: lo.ToPtr(true),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cloud_gateway cannot be set for cluster_type 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'"),
			},
			{
				Name: "cannot set cloud_gateway to false for cluster_type CLUSTER_TYPE_K8S_INGRESS_CONTROLLER",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:         lo.ToPtr("cp-1"),
							ClusterType:  sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController.ToPointer(),
							CloudGateway: lo.ToPtr(false),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cloud_gateway cannot be set for cluster_type 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'"),
			},
			{
				Name: "not setting cloud_gateway for cluster_type CLUSTER_TYPE_K8S_INGRESS_CONTROLLER passes",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name:        lo.ToPtr("cp-1"),
							ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController.ToPointer(),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
		}.Run(t)
	})

	t.Run("controlPlane types", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectGatewayControlPlane]{
			{
				Name: "no type specificed, no KonnectID specified",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name: lo.ToPtr("cp-1"),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "mirror type, no KonnectID specified",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceMirror),
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("mirror field must be set for type Mirror"),
			},
			{
				Name: "mirror type, well-formed KonnectID specified",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha1.MirrorSpec{
							Konnect: konnectv1alpha1.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("8ae65120-cdec-4310-84c1-4b19caf67967"),
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
			{
				Name: "mirror type, malformed KonnectID specified",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha1.MirrorSpec{
							Konnect: konnectv1alpha1.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("malformed-id"),
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.mirror.konnect.id in body should match '^[0-9a-f]{8}(?:\\-[0-9a-f]{4}){3}-[0-9a-f]{12}$'"),
			},
			{
				Name: "mirror type, KonnectID specified, controlPlane spec specified",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha1.MirrorSpec{
							Konnect: konnectv1alpha1.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("8ae65120-cdec-4310-84c1-4b19caf67967"),
							},
						},
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name: lo.ToPtr("cp-1"),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("createControlPlaneRequest fields cannot be set for type Mirror"),
			},
			{
				Name: "origin type, KonnectID specified",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						Mirror: &konnectv1alpha1.MirrorSpec{
							Konnect: konnectv1alpha1.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("8ae65120-cdec-4310-84c1-4b19caf67967"),
							},
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("mirror field cannot be set for type Origin"),
			},
			{
				Name: "origin type, no KonnectID specified, empty controlPlane spec",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Name must be set for type Origin"),
			},
			{
				Name: "origin type, no KonnectID specified, with controlPlane name",
				TestObject: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						CreateControlPlaneRequest: konnectv1alpha1.CreateControlPlaneRequest{
							Name: lo.ToPtr("cp-1"),
						},
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "name-1",
							},
						},
					},
				},
			},
		}.Run(t)
	})
}
