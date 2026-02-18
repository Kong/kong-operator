package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func newCredentialStatus() *konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs {
	return &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs{
		ControlPlaneID: "cp-123",
		ConsumerID:     "consumer-123",
	}
}

func TestAdoptKongCredentialAPIKey(t *testing.T) {
	ctx := t.Context()

	newCredential := func(mode commonv1alpha1.AdoptMode) *configurationv1alpha1.KongCredentialAPIKey {
		return &configurationv1alpha1.KongCredentialAPIKey{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "apikey",
				Namespace: "default",
				UID:       types.UID("uid-1"),
			},
			Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
				ConsumerRef: corev1.LocalObjectReference{Name: "consumer"},
				Adopt: &commonv1alpha1.AdoptOptions{
					From:    commonv1alpha1.AdoptSourceKonnect,
					Mode:    mode,
					Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "key-1"},
				},
				KongCredentialAPIKeyAPISpec: configurationv1alpha1.KongCredentialAPIKeyAPISpec{
					Key: "key-value",
				},
			},
			Status: configurationv1alpha1.KongCredentialAPIKeyStatus{
				Konnect: newCredentialStatus(),
			},
		}
	}

	t.Run("override success", func(t *testing.T) {
		sdk := mocks.NewMockAPIKeysSDK(t)
		sdk.EXPECT().GetKeyAuthWithConsumer(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.GetKeyAuthWithConsumerRequest) bool {
			return req.KeyAuthID == "key-1" &&
				req.ControlPlaneID == "cp-123" &&
				req.ConsumerIDForNestedEntities == "consumer-123"
		})).Return(&sdkkonnectops.GetKeyAuthWithConsumerResponse{
			KeyAuth: &sdkkonnectcomp.KeyAuth{},
		}, nil)
		sdk.EXPECT().UpsertKeyAuthWithConsumer(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertKeyAuthWithConsumerRequest) bool {
			return req.KeyAuthID == "key-1" &&
				req.ControlPlaneID == "cp-123" &&
				req.ConsumerIDForNestedEntities == "consumer-123"
		})).Return(&sdkkonnectops.UpsertKeyAuthWithConsumerResponse{}, nil)

		cred := newCredential(commonv1alpha1.AdoptModeOverride)

		err := adoptKongCredentialAPIKey(ctx, sdk, cred)
		require.NoError(t, err)
		assert.Equal(t, "key-1", cred.GetKonnectID())
	})

	t.Run("match success", func(t *testing.T) {
		sdk := mocks.NewMockAPIKeysSDK(t)
		sdk.EXPECT().GetKeyAuthWithConsumer(mock.Anything, mock.Anything).Return(&sdkkonnectops.GetKeyAuthWithConsumerResponse{
			KeyAuth: &sdkkonnectcomp.KeyAuth{
				Key:  new("key-value"),
				Tags: []string{"k8s-uid:uid-1"},
			},
		}, nil)

		cred := newCredential(commonv1alpha1.AdoptModeMatch)

		err := adoptKongCredentialAPIKey(ctx, sdk, cred)
		require.NoError(t, err)
		assert.Equal(t, "key-1", cred.GetKonnectID())
		sdk.AssertNotCalled(t, "UpsertKeyAuthWithConsumer", mock.Anything, mock.Anything)
	})
}

func TestAdoptKongCredentialBasicAuth(t *testing.T) {
	ctx := t.Context()

	newCredential := func(mode commonv1alpha1.AdoptMode) *configurationv1alpha1.KongCredentialBasicAuth {
		return &configurationv1alpha1.KongCredentialBasicAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "basicauth",
				Namespace: "default",
				UID:       types.UID("uid-2"),
			},
			Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
				ConsumerRef: corev1.LocalObjectReference{Name: "consumer"},
				Adopt: &commonv1alpha1.AdoptOptions{
					From:    commonv1alpha1.AdoptSourceKonnect,
					Mode:    mode,
					Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "basic-1"},
				},
				KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
					Username: "user",
					Password: "pass",
				},
			},
			Status: configurationv1alpha1.KongCredentialBasicAuthStatus{
				Konnect: newCredentialStatus(),
			},
		}
	}

	t.Run("override success", func(t *testing.T) {
		sdk := mocks.NewMockBasicAuthCredentialsSDK(t)
		sdk.EXPECT().GetBasicAuthWithConsumer(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.GetBasicAuthWithConsumerRequest) bool {
			return req.BasicAuthID == "basic-1" &&
				req.ControlPlaneID == "cp-123" &&
				req.ConsumerIDForNestedEntities == "consumer-123"
		})).Return(&sdkkonnectops.GetBasicAuthWithConsumerResponse{
			BasicAuth: &sdkkonnectcomp.BasicAuth{},
		}, nil)
		sdk.EXPECT().UpsertBasicAuthWithConsumer(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertBasicAuthWithConsumerRequest) bool {
			return req.BasicAuthID == "basic-1"
		})).Return(&sdkkonnectops.UpsertBasicAuthWithConsumerResponse{}, nil)

		cred := newCredential(commonv1alpha1.AdoptModeOverride)
		err := adoptKongCredentialBasicAuth(ctx, sdk, cred)
		require.NoError(t, err)
		assert.Equal(t, "basic-1", cred.GetKonnectID())
	})

	t.Run("match success", func(t *testing.T) {
		sdk := mocks.NewMockBasicAuthCredentialsSDK(t)
		sdk.EXPECT().GetBasicAuthWithConsumer(mock.Anything, mock.Anything).Return(&sdkkonnectops.GetBasicAuthWithConsumerResponse{
			BasicAuth: &sdkkonnectcomp.BasicAuth{
				Username: "user",
				// TODO(pmalek): Password field is write only and is not returned https://github.com/Kong/kong-operator/issues/2535
				// Password: "pass",
				Tags: []string{"k8s-uid:uid-2"},
			},
		}, nil)

		cred := newCredential(commonv1alpha1.AdoptModeMatch)
		err := adoptKongCredentialBasicAuth(ctx, sdk, cred)
		require.NoError(t, err)
		assert.Equal(t, "basic-1", cred.GetKonnectID())
		sdk.AssertNotCalled(t, "UpsertBasicAuthWithConsumer", mock.Anything, mock.Anything)
	})
}

func TestAdoptKongCredentialACL(t *testing.T) {
	ctx := t.Context()

	newCredential := func(mode commonv1alpha1.AdoptMode) *configurationv1alpha1.KongCredentialACL {
		return &configurationv1alpha1.KongCredentialACL{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "acl",
				Namespace: "default",
				UID:       types.UID("uid-3"),
			},
			Spec: configurationv1alpha1.KongCredentialACLSpec{
				ConsumerRef: corev1.LocalObjectReference{Name: "consumer"},
				Adopt: &commonv1alpha1.AdoptOptions{
					From:    commonv1alpha1.AdoptSourceKonnect,
					Mode:    mode,
					Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "acl-1"},
				},
				KongCredentialACLAPISpec: configurationv1alpha1.KongCredentialACLAPISpec{
					Group: "group-1",
				},
			},
			Status: configurationv1alpha1.KongCredentialACLStatus{
				Konnect: newCredentialStatus(),
			},
		}
	}

	t.Run("match success", func(t *testing.T) {
		sdk := mocks.NewMockACLsSDK(t)
		sdk.EXPECT().GetACLWithConsumer(mock.Anything, mock.Anything).Return(&sdkkonnectops.GetACLWithConsumerResponse{
			ACL: &sdkkonnectcomp.ACL{
				Group: "group-1",
				Tags:  []string{"k8s-uid:uid-3"},
			},
		}, nil)

		cred := newCredential(commonv1alpha1.AdoptModeMatch)
		err := adoptKongCredentialACL(ctx, sdk, cred)
		require.NoError(t, err)
		assert.Equal(t, "acl-1", cred.GetKonnectID())
	})
}

func TestAdoptKongCredentialHMAC(t *testing.T) {
	ctx := t.Context()

	username := "hmac-user"
	secret := "hmac-secret"
	id := "hmac-1"

	newCredential := func(mode commonv1alpha1.AdoptMode) *configurationv1alpha1.KongCredentialHMAC {
		return &configurationv1alpha1.KongCredentialHMAC{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "hmac",
				Namespace: "default",
				UID:       types.UID("uid-4"),
			},
			Spec: configurationv1alpha1.KongCredentialHMACSpec{
				ConsumerRef: corev1.LocalObjectReference{Name: "consumer"},
				Adopt: &commonv1alpha1.AdoptOptions{
					From:    commonv1alpha1.AdoptSourceKonnect,
					Mode:    mode,
					Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: id},
				},
				KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
					Username: &username,
					Secret:   &secret,
					ID:       &id,
				},
			},
			Status: configurationv1alpha1.KongCredentialHMACStatus{
				Konnect: newCredentialStatus(),
			},
		}
	}

	t.Run("match success", func(t *testing.T) {
		sdk := mocks.NewMockHMACAuthCredentialsSDK(t)
		sdk.EXPECT().GetHmacAuthWithConsumer(mock.Anything, mock.Anything).Return(&sdkkonnectops.GetHmacAuthWithConsumerResponse{
			HMACAuth: &sdkkonnectcomp.HMACAuth{
				ID:       new(id),
				Username: username,
				Secret:   new(secret),
				Tags:     []string{"k8s-uid:uid-4"},
			},
		}, nil)

		cred := newCredential(commonv1alpha1.AdoptModeMatch)
		err := adoptKongCredentialHMAC(ctx, sdk, cred)
		require.NoError(t, err)
		assert.Equal(t, id, cred.GetKonnectID())
	})
}

func TestAdoptKongCredentialJWT(t *testing.T) {
	ctx := t.Context()

	key := "jwt-key"
	secret := "jwt-secret"
	rsa := "rsa"

	newCredential := func(mode commonv1alpha1.AdoptMode) *configurationv1alpha1.KongCredentialJWT {
		return &configurationv1alpha1.KongCredentialJWT{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "jwt",
				Namespace: "default",
				UID:       types.UID("uid-5"),
			},
			Spec: configurationv1alpha1.KongCredentialJWTSpec{
				ConsumerRef: corev1.LocalObjectReference{Name: "consumer"},
				Adopt: &commonv1alpha1.AdoptOptions{
					From:    commonv1alpha1.AdoptSourceKonnect,
					Mode:    mode,
					Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "jwt-1"},
				},
				KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
					Algorithm:    "HS256",
					Key:          &key,
					Secret:       &secret,
					RSAPublicKey: &rsa,
				},
			},
			Status: configurationv1alpha1.KongCredentialJWTStatus{
				Konnect: newCredentialStatus(),
			},
		}
	}

	t.Run("match success", func(t *testing.T) {
		sdk := mocks.NewMockJWTsSDK(t)
		alg := sdkkonnectcomp.JWTAlgorithmHs256
		sdk.EXPECT().GetJwtWithConsumer(mock.Anything, mock.Anything).Return(&sdkkonnectops.GetJwtWithConsumerResponse{
			Jwt: &sdkkonnectcomp.Jwt{
				ID:           new("jwt-1"),
				Key:          new(key),
				Secret:       new(secret),
				RsaPublicKey: new(rsa),
				Algorithm:    alg.ToPointer(),
				Tags:         []string{"k8s-uid:uid-5"},
			},
		}, nil)

		cred := newCredential(commonv1alpha1.AdoptModeMatch)
		err := adoptKongCredentialJWT(ctx, sdk, cred)
		require.NoError(t, err)
		assert.Equal(t, "jwt-1", cred.GetKonnectID())
		sdk.AssertNotCalled(t, "UpsertJwtWithConsumer", mock.Anything, mock.Anything)
	})
}
