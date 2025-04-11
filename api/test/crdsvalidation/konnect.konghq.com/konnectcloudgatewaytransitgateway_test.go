package crdsvalidation

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestKonnectCloudGatewayTransitGateway(t *testing.T) {
	var konnectTransitGatewayTypeMeta = metav1.TypeMeta{
		APIVersion: konnectv1alpha1.GroupVersion.String(),
		Kind:       "KonnectCloudGatewayTransitGateway",
	}

	var testAWSTransitGatewayConfig = &konnectv1alpha1.AWSTransitGateway{
		Name: "aws-transit-gateway",
		CIDRBlocks: []string{
			"10.11.12.0/24",
		},
		AttachmentConfig: konnectv1alpha1.AwsTransitGatewayAttachmentConfig{
			TransitGatewayID: "transit-gateway",
			RAMShareArn:      "ram_share_arn",
		},
	}

	var testAzureTransitGatewayConfig = &konnectv1alpha1.AzureTransitGateway{
		Name: "azure-transit-gateway",
		AttachmentConfig: konnectv1alpha1.AzureVNETPeeringAttachmentConfig{
			TenantID:          "azure-tenant",
			SubscriptionID:    "azure-subscription",
			ResourceGroupName: "azure-resource-group",
			VnetName:          "azure-vnet",
		},
	}

	var namespacedNetworkRef = commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: "konnect-network",
		},
	}

	var programmedCondition = metav1.Condition{
		Type:               "Programmed",
		Status:             metav1.ConditionTrue,
		Reason:             "Valid",
		LastTransitionTime: metav1.Now(),
	}

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectCloudGatewayTransitGateway]{
			{
				Name: "spec.networkRef can only use namespaced ref",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: commonv1alpha1.ObjectRef{
							Type:      commonv1alpha1.ObjectRefTypeKonnectID,
							KonnectID: lo.ToPtr("konnect-id"),
						},
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:              konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
							AWSTransitGateway: testAWSTransitGatewayConfig,
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("only namespacedRef is supported currently"),
			},
			{
				Name: "spec.type must in supported value",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type: konnectv1alpha1.TransitGatewayType("unsupported-type"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.type: Unsupported value"),
			},
			{
				Name: "spec.awsTransitGateway.name cannot be empty",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type: konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
							AWSTransitGateway: &konnectv1alpha1.AWSTransitGateway{
								Name: "",
								CIDRBlocks: []string{
									"10.11.12.0/24",
								},
								AttachmentConfig: konnectv1alpha1.AwsTransitGatewayAttachmentConfig{
									TransitGatewayID: "transit-gateway",
									RAMShareArn:      "ram_share_arn",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.awsTransitGateway.name: Invalid value"),
			},
			{
				Name: "spec.awsTransitGateway.cidr_blocks is required",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type: konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
							AWSTransitGateway: &konnectv1alpha1.AWSTransitGateway{
								Name: "aws-transit-gateway",
								AttachmentConfig: konnectv1alpha1.AwsTransitGatewayAttachmentConfig{
									TransitGatewayID: "transit-gateway",
									RAMShareArn:      "ram_share_arn",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.awsTransitGateway.cidr_blocks: Required value"),
			},
			{
				Name: "spec.azureTransitGateway.name cannot be empty",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type: konnectv1alpha1.TransitGatewayTypeAzureTransitGateway,
							AzureTransitGateway: &konnectv1alpha1.AzureTransitGateway{
								Name: "",
								AttachmentConfig: konnectv1alpha1.AzureVNETPeeringAttachmentConfig{
									TenantID:          "azure-tenant",
									SubscriptionID:    "azure-subscription",
									ResourceGroupName: "azure-resource-group",
									VnetName:          "azure-vnet",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.azureTransitGateway.name: Invalid value"),
			},

			{
				Name: "Must set awsTransitGateway when type = awsTransitConfig",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type: konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("must set spec.awsTransitGateway when spec.type is 'AWSTransitGateway'"),
			},
			{
				Name: "Must not set awsTransitGateway when type != awsTransitConfig",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:                konnectv1alpha1.TransitGatewayTypeAzureTransitGateway,
							AWSTransitGateway:   testAWSTransitGatewayConfig,
							AzureTransitGateway: testAzureTransitGatewayConfig,
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("must not set spec.awsTransitGateway when spec.type is not 'AWSTransitGateway'"),
			},
			{
				Name: "Must set azureTransitGatway when spec.type = azureTransitGateway",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type: konnectv1alpha1.TransitGatewayTypeAzureTransitGateway,
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("must set spec.azureTransitGateway when spec.type is 'AzureTransitGateway'"),
			},
			{
				Name: "Must not set azureTransitGateway when type != azureTransitGateway",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:                konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
							AWSTransitGateway:   testAWSTransitGatewayConfig,
							AzureTransitGateway: testAzureTransitGatewayConfig,
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("must not set spec.azureTransitGateway when spec.type is not 'AzureTransitGateway'"),
			},
			{
				Name: "spec.type is immutable",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:              konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
							AWSTransitGateway: testAWSTransitGatewayConfig,
						},
					},
				},
				Update: func(ktg *konnectv1alpha1.KonnectCloudGatewayTransitGateway) {
					ktg.Spec.Type = konnectv1alpha1.TransitGatewayTypeAzureTransitGateway
					ktg.Spec.AWSTransitGateway = nil
					ktg.Spec.AzureTransitGateway = testAzureTransitGatewayConfig
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.type is immutable"),
			},
			{
				Name: "spec.awsTransitGateway.name is mutable when not programmed",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:              konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
							AWSTransitGateway: testAWSTransitGatewayConfig,
						},
					},
				},
				Update: func(ktg *konnectv1alpha1.KonnectCloudGatewayTransitGateway) {
					ktg.Spec.AWSTransitGateway.Name = "yet-another-name"
				},
			},
			{
				Name: "spec.awsTransitGateway.name is immutable when programmed",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:              konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
							AWSTransitGateway: testAWSTransitGatewayConfig,
						},
					},
					Status: konnectv1alpha1.KonnectCloudGatewayTransitGatewayStatus{
						Conditions: []metav1.Condition{programmedCondition},
					},
				},
				Update: func(ktg *konnectv1alpha1.KonnectCloudGatewayTransitGateway) {
					ktg.Spec.AWSTransitGateway.Name = "yet-another-name"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.awsTransitGateway.name is immutable when transit gateway is already Programmed"),
			},
			{
				Name: "spec.azureTransitGateway.name is mutable when not programmed",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:                konnectv1alpha1.TransitGatewayTypeAzureTransitGateway,
							AzureTransitGateway: testAzureTransitGatewayConfig,
						},
					},
				},
				Update: func(ktg *konnectv1alpha1.KonnectCloudGatewayTransitGateway) {
					ktg.Spec.AzureTransitGateway.Name = "yet-another-name"
				},
			},
			{
				Name: "spec.azureTransitGateway.name is immutable when programmed",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
					TypeMeta:   konnectTransitGatewayTypeMeta,
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
						NetworkRef: namespacedNetworkRef,
						KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
							Type:                konnectv1alpha1.TransitGatewayTypeAzureTransitGateway,
							AzureTransitGateway: testAzureTransitGatewayConfig,
						},
					},
					Status: konnectv1alpha1.KonnectCloudGatewayTransitGatewayStatus{
						Conditions: []metav1.Condition{programmedCondition},
					},
				},
				Update: func(ktg *konnectv1alpha1.KonnectCloudGatewayTransitGateway) {
					ktg.Spec.AzureTransitGateway.Name = "yet-another-name"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.azureTransitGateway.name is immutable when transit gateway is already Programmed"),
			},
		}.Run(t)
	})
}
