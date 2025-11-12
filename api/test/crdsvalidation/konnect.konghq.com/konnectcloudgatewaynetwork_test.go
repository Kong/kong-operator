package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKonnectNetwork(t *testing.T) {
	t.Run("mutability based on Programmed status condition", func(t *testing.T) {
		t.Run("name", func(t *testing.T) {
			fieldMutabilityBasedOnProgrammedTest(t, "name", func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) {
				n.Spec.Name = "new-name"
			})
		})

		t.Run("availability_zones", func(t *testing.T) {
			fieldMutabilityBasedOnProgrammedTest(t, "availability_zones", func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) {
				n.Spec.AvailabilityZones = []string{
					"us-west-1b",
				}
			})
		})

		t.Run("cidr_block", func(t *testing.T) {
			fieldMutabilityBasedOnProgrammedTest(t, "cidr_block", func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) {
				n.Spec.CidrBlock = "10.0.0.2/16"
			})
		})

		t.Run("cloud_gateway_provider_account_id", func(t *testing.T) {
			fieldMutabilityBasedOnProgrammedTest(t, "cloud_gateway_provider_account_id", func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) {
				n.Spec.CloudGatewayProviderAccountID = "id-new-1234567890"
			})
		})

		t.Run("region", func(t *testing.T) {
			fieldMutabilityBasedOnProgrammedTest(t, "region", func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) {
				n.Spec.Region = "us-east"
			})
		})
	})

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectCloudGatewayNetwork]{
			{
				Name: "all required fields are set",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayNetwork{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-konnect-api-auth-configuration",
							},
						},
						Name:   "test-network",
						Region: "us-west",
						AvailabilityZones: []string{
							"us-west-1a",
							"us-west-1b",
						},
						CidrBlock:                     "10.0.0.1/24",
						CloudGatewayProviderAccountID: "test-cloud-gateway-provider-account-id",
					},
				},
			},
		}.Run(t)
	})
}

func fieldMutabilityBasedOnProgrammedTest(
	t *testing.T,
	field string,
	update func(*konnectv1alpha1.KonnectCloudGatewayNetwork),
) {
	t.Helper()

	var (
		programmedConditionTrue = metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionTrue,
			Reason:             "Valid",
			LastTransitionTime: metav1.Now(),
		}
		programmedConditionFalse = metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionFalse,
			Reason:             "NotProgrammed",
			LastTransitionTime: metav1.Now(),
		}
		spec = konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: "test-konnect-api-auth-configuration",
				},
			},
			Name:   "test-network",
			Region: "us-west",
			AvailabilityZones: []string{
				"us-west-1a",
				"us-west-1b",
			},
			CidrBlock:                     "10.0.0.1/24",
			CloudGatewayProviderAccountID: "test-cloud-gateway-provider-account-id",
		}
	)

	common.TestCasesGroup[*konnectv1alpha1.KonnectCloudGatewayNetwork]{
		{
			Name: "is immutable when Programmed=true",
			TestObject: &konnectv1alpha1.KonnectCloudGatewayNetwork{
				ObjectMeta: common.CommonObjectMeta,
				Spec:       spec,
				Status: konnectv1alpha1.KonnectCloudGatewayNetworkStatus{
					Conditions: []metav1.Condition{
						programmedConditionTrue,
					},
				},
			},
			Update: update,
			ExpectedUpdateErrorMessage: lo.ToPtr(
				fmt.Sprintf("spec.%s is immutable when an entity is already Programmed", field),
			),
		},
		{
			Name: "is mutable when Programmed=false",
			TestObject: &konnectv1alpha1.KonnectCloudGatewayNetwork{
				ObjectMeta: common.CommonObjectMeta,
				Spec:       spec,
				Status: konnectv1alpha1.KonnectCloudGatewayNetworkStatus{
					Conditions: []metav1.Condition{
						programmedConditionFalse,
					},
				},
			},
			Update: update,
		},
		{
			Name: "is mutable when Programmed status condition is missing",
			TestObject: &konnectv1alpha1.KonnectCloudGatewayNetwork{
				ObjectMeta: common.CommonObjectMeta,
				Spec:       spec,
			},
			Update: update,
		},
	}.Run(t)
}
