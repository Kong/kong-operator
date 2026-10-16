package ops

import (
	"context"
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestCreateInputsHashUsesRequestSentToKonnect(t *testing.T) {
	ctx := t.Context()
	secret := &corev1.Secret{Name: "key", Namespace: "default", Data: map[string][]byte{"value": []byte("original")}}
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(secret).Build()
	obj := testGeneratedEventGatewayStaticKeyForSDKOps()
	obj.SetGatewayID("gateway-id")
	obj.Spec.APISpec.Value = configurationv1alpha1.SensitiveDataSource{
		Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: secret.Name, Key: "value"},
	}
	expectedHash, err := obj.CalculateCreateInputsHash(ctx, cl)
	require.NoError(t, err)
	sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
	sdk.EXPECT().CreateEventGatewayStaticKey(mock.Anything, "gateway-id", mock.MatchedBy(func(req *sdkkonnectcomp.EventGatewayStaticKeyCreate) bool {
		return req.Value == "original"
	})).Run(func(context.Context, string, *sdkkonnectcomp.EventGatewayStaticKeyCreate, ...sdkkonnectops.Option) {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(secret), secret))
		secret.Data["value"] = []byte("rotated-during-create")
		require.NoError(t, cl.Update(ctx, secret))
	}).Return(&sdkkonnectops.CreateEventGatewayStaticKeyResponse{
		EventGatewayStaticKey: &sdkkonnectcomp.EventGatewayStaticKey{ID: "created-id"},
	}, nil).Once()
	require.NoError(t, createEventGatewayStaticKey(ctx, cl, sdk, obj))
	assert.Equal(t, expectedHash, obj.Status.CreateInputsHash)
	currentHash, err := obj.CalculateCreateInputsHash(ctx, cl)
	require.NoError(t, err)
	assert.NotEqual(t, currentHash, obj.Status.CreateInputsHash)

	t.Run("failed recreation preserves fingerprint", func(t *testing.T) {
		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		sdk.EXPECT().CreateEventGatewayStaticKey(mock.Anything, "gateway-id", mock.Anything).
			Return(nil, errors.New("creation failed")).Once()
		require.Error(t, recreateEventGatewayStaticKey(ctx, cl, sdk, obj))
		assert.Equal(t, expectedHash, obj.Status.CreateInputsHash)
	})
	t.Run("successful recreation records current inputs", func(t *testing.T) {
		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		sdk.EXPECT().CreateEventGatewayStaticKey(mock.Anything, "gateway-id", mock.Anything).
			Return(&sdkkonnectops.CreateEventGatewayStaticKeyResponse{
				EventGatewayStaticKey: &sdkkonnectcomp.EventGatewayStaticKey{ID: "recreated-id"},
			}, nil).Once()
		require.NoError(t, recreateEventGatewayStaticKey(ctx, cl, sdk, obj))
		assert.Equal(t, currentHash, obj.Status.CreateInputsHash)
		condition, ok := k8sutils.GetCondition(konnectv1alpha1.CreateInputsAppliedConditionType, obj)
		require.True(t, ok)
		assert.Equal(t, konnectv1alpha1.CreateInputsReasonApplied, condition.Reason)
	})
}
