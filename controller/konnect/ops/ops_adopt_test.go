package ops

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/internal/metrics"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

type noOpMetricsRecorder struct{}

func (noOpMetricsRecorder) RecordKonnectEntityOperationSuccess(string, metrics.KonnectEntityOperation, string, time.Duration) {
}
func (noOpMetricsRecorder) RecordKonnectEntityOperationFailure(string, metrics.KonnectEntityOperation, string, time.Duration, int) {
}

var metricRecorder = noOpMetricsRecorder{}

func assertProgrammedCondition(t *testing.T, conditions []metav1.Condition, expectedStatus metav1.ConditionStatus, expectedReason string) {
	t.Helper()
	cond, found := lo.Find(conditions, func(c metav1.Condition) bool {
		return c.Type == string(konnectv1alpha1.KonnectEntityProgrammedConditionType)
	})
	require.True(t, found, "expected Programmed condition to be set")
	assert.Equal(t, expectedStatus, cond.Status)
	assert.Equal(t, expectedReason, cond.Reason)
}

func TestAdoptMatchUnsupportedMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	network := &konnectv1alpha1.KonnectCloudGatewayNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net-mode", Namespace: "default"},
		Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			Name:                          "net-mode",
			CloudGatewayProviderAccountID: "acct-1",
			Region:                        "us-east-1",
			AvailabilityZones:             []string{"us-east-1a"},
			CidrBlock:                     "10.0.0.0/16",
			KonnectConfiguration:          konnectv1alpha2.KonnectConfiguration{APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{Name: "default"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Mode:    "invalid-mode",
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "net-2"},
			},
		},
	}

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, network, *network.Spec.Adopt)
	require.Error(t, err)
	assert.Empty(t, network.GetKonnectID())
}

func TestAdoptMatchMissingKonnectID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	network := &konnectv1alpha1.KonnectCloudGatewayNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net-missing-id", Namespace: "default"},
		Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			Name:                          "net-missing-id",
			CloudGatewayProviderAccountID: "acct-1",
			Region:                        "us-east-1",
			AvailabilityZones:             []string{"us-east-1a"},
			CidrBlock:                     "10.0.0.0/16",
			KonnectConfiguration:          konnectv1alpha2.KonnectConfiguration{APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{Name: "default"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: nil,
			},
		},
	}

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, network, *network.Spec.Adopt)
	require.Error(t, err)
	assert.Empty(t, network.GetKonnectID())
}
