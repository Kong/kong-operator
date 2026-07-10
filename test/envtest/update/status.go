package update

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func UpdateKonnectEventGatewayStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *konnectv1alpha1.KonnectEventGateway,
	id string,
) {
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
			return
		}
		obj.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
			ID:        id,
			ServerURL: sdkmocks.SDKServerURL,
			OrgID:     "org-id",
		}
		obj.Status.Conditions = []metav1.Condition{
			programmedCondition(obj.GetGeneration()),
		}
		assert.NoError(ct, cl.Status().Update(ctx, obj))
	}, consts.WaitTime, consts.TickTime)
}

func programmedCondition(generation int64) metav1.Condition {
	return k8sutils.NewConditionWithGeneration(
		konnectv1alpha1.KonnectEntityProgrammedConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
		"",
		generation,
	)
}
