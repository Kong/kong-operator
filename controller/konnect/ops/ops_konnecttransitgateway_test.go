package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	kcfgkonnect "github.com/kong/kong-operator/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestAdoptMatchTransitGatewaySuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	networkID := "net-1"

	tg := &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tg",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
			NetworkRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "net"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "tg-1"},
			},
			KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
				Type: konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
				AWSTransitGateway: &konnectv1alpha1.AWSTransitGateway{
					Name:       "test-tg",
					CIDRBlocks: []string{"10.0.0.0/16"},
					DNSConfig: []konnectv1alpha1.TransitGatewayDNSConfig{
						{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
					},
					AttachmentConfig: konnectv1alpha1.AwsTransitGatewayAttachmentConfig{
						TransitGatewayID: "tgw-123",
						RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
					},
				},
			},
		},
	}
	tg.Status.NetworkID = networkID

	awsResp := sdkkonnectcomp.CreateTransitGatewayResponseAwsTransitGatewayResponse(
		sdkkonnectcomp.AwsTransitGatewayResponse{
			ID:         "tg-1",
			Name:       "test-tg",
			CidrBlocks: []string{"10.0.0.0/16"},
			DNSConfig: []sdkkonnectcomp.TransitGatewayDNSConfig{
				{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
			},
			TransitGatewayAttachmentConfig: sdkkonnectcomp.AwsTransitGatewayAttachmentConfig{
				TransitGatewayID: "tgw-123",
				RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
			},
			State: sdkkonnectcomp.TransitGatewayStateReady,
		},
	)

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetTransitGateway(mock.Anything, networkID, "tg-1").
		Return(&sdkkonnectops.GetTransitGatewayResponse{TransitGatewayResponse: &awsResp}, nil).
		Once()

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, tg, *tg.Spec.Adopt)
	require.NoError(t, err)
	assert.Equal(t, "tg-1", tg.GetKonnectID())
	assert.Equal(t, sdkkonnectcomp.TransitGatewayStateReady, tg.Status.State)
	assertProgrammedCondition(t, tg.GetConditions(), metav1.ConditionTrue, string(konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed))
}

func TestAdoptMatchTransitGatewayMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	networkID := "net-1"

	tg := &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tg-mismatch",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
			NetworkRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "net"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "tg-2"},
			},
			KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
				Type: konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
				AWSTransitGateway: &konnectv1alpha1.AWSTransitGateway{
					Name:       "test-tg",
					CIDRBlocks: []string{"10.0.0.0/16"},
					DNSConfig: []konnectv1alpha1.TransitGatewayDNSConfig{
						{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
					},
					AttachmentConfig: konnectv1alpha1.AwsTransitGatewayAttachmentConfig{
						TransitGatewayID: "tgw-123",
						RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
					},
				},
			},
		},
	}
	tg.Status.NetworkID = networkID

	awsResp := sdkkonnectcomp.CreateTransitGatewayResponseAwsTransitGatewayResponse(
		sdkkonnectcomp.AwsTransitGatewayResponse{
			ID:         "tg-2",
			Name:       "test-tg",
			CidrBlocks: []string{"10.1.0.0/16"}, // mismatch on purpose
			DNSConfig: []sdkkonnectcomp.TransitGatewayDNSConfig{
				{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
			},
			TransitGatewayAttachmentConfig: sdkkonnectcomp.AwsTransitGatewayAttachmentConfig{
				TransitGatewayID: "tgw-123",
				RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
			},
			State: sdkkonnectcomp.TransitGatewayStateReady,
		},
	)

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetTransitGateway(mock.Anything, networkID, "tg-2").
		Return(&sdkkonnectops.GetTransitGatewayResponse{TransitGatewayResponse: &awsResp}, nil).
		Once()

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, tg, *tg.Spec.Adopt)
	require.Error(t, err)
	assert.Empty(t, tg.GetKonnectID())
	assertProgrammedCondition(t, tg.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}
