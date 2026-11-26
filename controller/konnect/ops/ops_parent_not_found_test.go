package ops

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	kcfgkonnect "github.com/kong/kong-operator/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
)

func TestUpdateWhenParentDoesNotExistInKonnect(t *testing.T) {
	t.Parallel()

	sdk, requests := newSDKWithStatus(t, http.StatusNotFound)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
	ent := testGeneratedAIGatewayAuthStrategyForSDKOps()
	ent.SetGatewayID("gateway-id")
	ent.SetKonnectID("auth-strategy-id")

	update := func(syncPeriod time.Duration) error {
		_, err := Update(t.Context(), sdk, syncPeriod, cl, &metricsmocks.MockRecorder{}, ent)
		return err
	}

	require.NoError(t, update(time.Minute))
	assert.Equal(t, int64(2), requests.Load(), "the update sends a PUT and a POST")
	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
	require.True(t, ok)
	assert.Equal(t, string(kcfgkonnect.KonnectEntitiesParentNotFoundReason), cond.Reason)
	assert.Contains(t, cond.Message, "gateway gateway-id")

	t.Run("no request in the sync period", func(t *testing.T) {
		res, err := Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, ent)
		require.NoError(t, err)
		assert.Equal(t, int64(2), requests.Load())
		assert.Positive(t, res.RequeueAfter)
		assert.LessOrEqual(t, res.RequeueAfter, time.Minute)
	})

	t.Run("new request after the sync period", func(t *testing.T) {
		require.NoError(t, update(0))
		assert.Equal(t, int64(4), requests.Load())
	})

	t.Run("new request when the parent ID changes", func(t *testing.T) {
		ent.SetGatewayID("new-gateway-id")
		require.NoError(t, update(time.Minute))
		assert.Equal(t, int64(6), requests.Load())
		cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
		require.True(t, ok)
		assert.Contains(t, cond.Message, "gateway new-gateway-id")
	})

	t.Run("new request when the generation changes", func(t *testing.T) {
		ent.SetGeneration(ent.GetGeneration() + 1)
		require.NoError(t, update(time.Minute))
		assert.Equal(t, int64(8), requests.Load())
	})
}

func TestHandleUpdateErrorWhenCreateFails(t *testing.T) {
	t.Parallel()

	notFound := &sdkkonnecterrs.NotFoundError{Status: 404}
	ent := testGeneratedAIGatewayAuthStrategyForSDKOps()
	ent.SetGatewayID("gateway-id")

	testCases := []struct {
		name           string
		createErr      error
		parentNotFound bool
	}{
		{
			name:           "typed not found",
			createErr:      notFound,
			parentNotFound: true,
		},
		{
			name:           "generic SDK error with 404",
			createErr:      &sdkkonnecterrs.SDKError{Message: "API error occurred", StatusCode: 404},
			parentNotFound: true,
		},
		{
			name: "relations error with not found",
			createErr: KonnectEntityCreatedButRelationsFailedError{
				KonnectID: "new-id",
				Err:       notFound,
			},
		},
		{
			name:      "other error",
			createErr: &sdkkonnecterrs.SDKError{Message: "API error occurred", StatusCode: 400},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := handleUpdateError[aiconfigurationv1alpha1.AIGatewayAuthStrategy](
				t.Context(), notFound, ent,
				func(context.Context) error { return tc.createErr },
			)
			_, ok := errors.AsType[ParentNotFoundInKonnectError](err)
			assert.Equal(t, tc.parentNotFound, ok)
			assert.ErrorIs(t, err, tc.createErr)
		})
	}
}
