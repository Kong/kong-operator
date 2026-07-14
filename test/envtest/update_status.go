package envtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func UpdateKongConsumerStatusWithKonnectID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1.KongConsumer,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func UpdateKongConsumerGroupStatusWithKonnectID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1beta1.KongConsumerGroup,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func UpdateKongServiceStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongService,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func UpdateKongRouteStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongRoute,
	id string,
	cpID string,
	serviceID string,
) {
	obj.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
		ServiceID:           serviceID,
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func UpdateKongKeySetStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongKeySet,
	id, cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func UpdateKongUpstreamStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.KongUpstream,
	id string,
	cpID string,
) {
	obj.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
		ControlPlaneID:      cpID,
		KonnectEntityStatus: konnectEntityStatus(id),
	}
	obj.Status.Conditions = []metav1.Condition{
		programmedCondition(obj.GetGeneration()),
	}

	require.NoError(t, cl.Status().Update(ctx, obj))
}

func updateKonnectAIGatewayStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *konnectv1alpha1.KonnectAIGateway,
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
	}, waitTime, tickTime)
}

func updateKonnectEventGatewayStatusWithProgrammed(
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
	}, waitTime, tickTime)
}

func updateEventGatewayDataPlaneCertificateStatusWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayDataPlaneCertificate,
) {
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
			return
		}
		obj.Status.Conditions = []metav1.Condition{
			programmedCondition(obj.GetGeneration()),
		}
		assert.NoError(ct, cl.Status().Update(ctx, obj))
	}, waitTime, tickTime)
}

func konnectEntityStatus(id string) konnectv1alpha2.KonnectEntityStatus {
	return konnectv1alpha2.KonnectEntityStatus{
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
