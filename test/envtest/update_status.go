package envtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sdkmocks "github.com/kong/kong-operator/controller/konnect/ops/sdk/mocks"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func updateKongConsumerStatusWithKonnectID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1.KongConsumer,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func updateKongConsumerGroupStatusWithKonnectID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1beta1.KongConsumerGroup,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func updateKongServiceStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongService,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func updateKongRouteStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongRoute,
	id string,
	cpID string,
	serviceID string,
) {
	obj.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndServiceRefs{
		ServiceID:           serviceID,
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func updateKongKeySetStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongKeySet,
	id, cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func updateKongUpstreamStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongUpstream,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func konnectEntityStatus(id string) konnectv1alpha1.KonnectEntityStatus {
	return konnectv1alpha1.KonnectEntityStatus{
		ID:        id,
		ServerURL: sdkmocks.SDKServerURL,
		OrgID:     "org-id",
	}
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
