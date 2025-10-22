package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/pkg/metadata"
	sdkmocks "github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongCACertificateToCACertificateInput_Tags(t *testing.T) {
	cert := &configurationv1alpha1.KongCACertificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongCACertificate",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cert-1",
			Namespace:  "default",
			Generation: 2,
			UID:        k8stypes.UID(uuid.NewString()),
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
			},
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "cert",
				Tags: []string{"tag3", "tag4", "duplicate"},
			},
		},
	}
	output := kongCACertificateToCACertificateInput(cert)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongCACertificate",
		"k8s-name:cert-1",
		"k8s-uid:" + string(cert.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-namespace:default",
		"tag1",
		"tag2",
		"tag3",
		"tag4",
		"duplicate",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
}

func TestAdoptKongCACertificateOverride(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "ca-cert",
			},
		},
		nil,
	)
	sdk.EXPECT().UpsertCaCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCaCertificateRequest) bool {
		return req.ControlPlaneID == "cp-1" &&
			req.CACertificateID == "konnect-ca-id" &&
			req.CACertificate.Cert == "ca-cert"
	})).Return(&sdkkonnectops.UpsertCaCertificateResponse{}, nil)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-ca-id", cert.GetKonnectID())
}

func TestAdoptKongCACertificateMatchSuccess(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "ca-cert",
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-ca-id", cert.GetKonnectID())
}

func TestAdoptKongCACertificateMatchMismatch(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "other-cert",
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, sdk, cert)
	require.Error(t, err)
	var notMatchErr KonnectEntityAdoptionNotMatchError
	assert.ErrorAs(t, err, &notMatchErr)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCACertificateUIDConflict(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "ca-cert",
				Tags: []string{"k8s-uid:other-uid"},
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, sdk, cert)
	require.Error(t, err)
	var uidConflict KonnectEntityAdoptionUIDTagConflictError
	assert.ErrorAs(t, err, &uidConflict)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCACertificateFetchFailure(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		(*sdkkonnectops.GetCaCertificateResponse)(nil),
		&sdkkonnecterrs.NotFoundError{},
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, sdk, cert)
	require.Error(t, err)
	var fetchErr KonnectEntityAdoptionFetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Empty(t, cert.GetKonnectID())
}
