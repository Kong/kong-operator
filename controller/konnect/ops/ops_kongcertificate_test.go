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

func TestKongCertificateToCertificateInput_Tags(t *testing.T) {
	cert := &configurationv1alpha1.KongCertificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongCertificate",
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
		Spec: configurationv1alpha1.KongCertificateSpec{
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert",
				Key:  "key",
				Tags: []string{"tag3", "tag4", "duplicate"},
			},
		},
	}
	output := kongCertificateToCertificateInput(cert)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongCertificate",
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

func TestAdoptKongCertificateOverride(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		nil,
	)
	sdk.EXPECT().UpsertCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCertificateRequest) bool {
		return req.ControlPlaneID == "cp-1" &&
			req.CertificateID == "konnect-cert-id" &&
			req.Certificate.Cert == "cert-data" &&
			req.Certificate.Key == "key-data"
	})).Return(&sdkkonnectops.UpsertCertificateResponse{}, nil)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCertificate(ctx, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-cert-id", cert.GetKonnectID())
}

func TestAdoptKongCertificateMatchSuccess(t *testing.T) {
	ctx := context.Background()
	certAlt := "alt-cert"
	keyAlt := "alt-key"
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert:    "cert-data",
				Key:     "key-data",
				CertAlt: &certAlt,
				KeyAlt:  &keyAlt,
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert:    "cert-data",
				Key:     "key-data",
				CertAlt: certAlt,
				KeyAlt:  keyAlt,
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCertificate(ctx, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-cert-id", cert.GetKonnectID())
}

func TestAdoptKongCertificateMatchMismatch(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert: "other-cert",
				Key:  "key-data",
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCertificate(ctx, sdk, cert)
	require.Error(t, err)
	var notMatchErr KonnectEntityAdoptionNotMatchError
	assert.ErrorAs(t, err, &notMatchErr)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCertificateUIDConflict(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert: "cert-data",
				Key:  "key-data",
				Tags: []string{"k8s-uid:other-uid"},
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCertificate(ctx, sdk, cert)
	require.Error(t, err)
	var uidConflict KonnectEntityAdoptionUIDTagConflictError
	assert.ErrorAs(t, err, &uidConflict)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCertificateFetchFailure(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		(*sdkkonnectops.GetCertificateResponse)(nil),
		&sdkkonnecterrs.NotFoundError{},
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCertificate(ctx, sdk, cert)
	require.Error(t, err)
	var fetchErr KonnectEntityAdoptionFetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Empty(t, cert.GetKonnectID())
}
