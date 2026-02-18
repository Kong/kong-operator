package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestAdoptKongSNIOverride(t *testing.T) {
	ctx := t.Context()
	sdk := mocks.NewMockSNIsSDK(t)
	sdk.EXPECT().GetSniWithCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.GetSniWithCertificateRequest) bool {
		return req.ControlPlaneID == "cp-1" &&
			req.CertificateID == "cert-1" &&
			req.SNIID == "sni-1"
	})).Return(
		&sdkkonnectops.GetSniWithCertificateResponse{
			Sni: &sdkkonnectcomp.Sni{
				Name: "example.com",
				Certificate: sdkkonnectcomp.SNICertificate{
					ID: new("cert-1"),
				},
			},
		},
		nil,
	)
	sdk.EXPECT().UpsertSniWithCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertSniWithCertificateRequest) bool {
		return req.ControlPlaneID == "cp-1" &&
			req.CertificateID == "cert-1" &&
			req.SNIID == "sni-1" &&
			req.SNIWithoutParents.Name == "example.com"
	})).Return(&sdkkonnectops.UpsertSniWithCertificateResponse{}, nil)

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sni",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: commonv1alpha1.NameRef{Name: "cert"},
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "sni-1",
				},
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: "example.com",
			},
		},
		Status: configurationv1alpha1.KongSNIStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
				ControlPlaneID: "cp-1",
				CertificateID:  "cert-1",
			},
		},
	}

	err := adoptSNI(ctx, sdk, sni)
	require.NoError(t, err)
	assert.Equal(t, "sni-1", sni.GetKonnectID())
}

func TestAdoptKongSNIMatchSuccess(t *testing.T) {
	ctx := t.Context()
	sdk := mocks.NewMockSNIsSDK(t)
	sdk.EXPECT().GetSniWithCertificate(mock.Anything, mock.Anything).Return(
		&sdkkonnectops.GetSniWithCertificateResponse{
			Sni: &sdkkonnectcomp.Sni{
				Name: "example.com",
				Certificate: sdkkonnectcomp.SNICertificate{
					ID: new("cert-1"),
				},
			},
		},
		nil,
	)

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sni",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: commonv1alpha1.NameRef{Name: "cert"},
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "sni-1",
				},
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: "example.com",
			},
		},
		Status: configurationv1alpha1.KongSNIStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
				ControlPlaneID: "cp-1",
				CertificateID:  "cert-1",
			},
		},
	}

	err := adoptSNI(ctx, sdk, sni)
	require.NoError(t, err)
	assert.Equal(t, "sni-1", sni.GetKonnectID())
}

func TestAdoptKongSNIMatchMismatch(t *testing.T) {
	ctx := t.Context()
	sdk := mocks.NewMockSNIsSDK(t)
	sdk.EXPECT().GetSniWithCertificate(mock.Anything, mock.Anything).Return(
		&sdkkonnectops.GetSniWithCertificateResponse{
			Sni: &sdkkonnectcomp.Sni{
				Name: "different.com",
				Certificate: sdkkonnectcomp.SNICertificate{
					ID: new("cert-1"),
				},
			},
		},
		nil,
	)

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sni",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: commonv1alpha1.NameRef{Name: "cert"},
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "sni-1",
				},
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: "example.com",
			},
		},
		Status: configurationv1alpha1.KongSNIStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
				ControlPlaneID: "cp-1",
				CertificateID:  "cert-1",
			},
		},
	}

	err := adoptSNI(ctx, sdk, sni)
	require.Error(t, err)
	var notMatchErr KonnectEntityAdoptionNotMatchError
	assert.ErrorAs(t, err, &notMatchErr)
	assert.Empty(t, sni.GetKonnectID())
}

func TestAdoptKongSNIUIDConflict(t *testing.T) {
	ctx := t.Context()
	sdk := mocks.NewMockSNIsSDK(t)
	sdk.EXPECT().GetSniWithCertificate(mock.Anything, mock.Anything).Return(
		&sdkkonnectops.GetSniWithCertificateResponse{
			Sni: &sdkkonnectcomp.Sni{
				Name: "example.com",
				Tags: []string{"k8s-uid:other-uid"},
				Certificate: sdkkonnectcomp.SNICertificate{
					ID: new("cert-1"),
				},
			},
		},
		nil,
	)

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sni",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: commonv1alpha1.NameRef{Name: "cert"},
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "sni-1",
				},
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: "example.com",
			},
		},
		Status: configurationv1alpha1.KongSNIStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
				ControlPlaneID: "cp-1",
				CertificateID:  "cert-1",
			},
		},
	}

	err := adoptSNI(ctx, sdk, sni)
	require.Error(t, err)
	var uidConflict KonnectEntityAdoptionUIDTagConflictError
	assert.ErrorAs(t, err, &uidConflict)
	assert.Empty(t, sni.GetKonnectID())
}

func TestAdoptKongSNIFetchFailure(t *testing.T) {
	ctx := t.Context()
	sdk := mocks.NewMockSNIsSDK(t)
	sdk.EXPECT().GetSniWithCertificate(mock.Anything, mock.Anything).Return(
		(*sdkkonnectops.GetSniWithCertificateResponse)(nil),
		&sdkkonnecterrs.NotFoundError{},
	)

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sni",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: commonv1alpha1.NameRef{Name: "cert"},
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "sni-1",
				},
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: "example.com",
			},
		},
		Status: configurationv1alpha1.KongSNIStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
				ControlPlaneID: "cp-1",
				CertificateID:  "cert-1",
			},
		},
	}

	err := adoptSNI(ctx, sdk, sni)
	require.Error(t, err)
	var fetchErr KonnectEntityAdoptionFetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Empty(t, sni.GetKonnectID())
}

func TestAdoptKongSNIMissingCertificateID(t *testing.T) {
	ctx := t.Context()
	sdk := mocks.NewMockSNIsSDK(t)

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sni",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: commonv1alpha1.NameRef{Name: "cert"},
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "sni-1",
				},
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: "example.com",
			},
		},
		Status: configurationv1alpha1.KongSNIStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptSNI(ctx, sdk, sni)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a Konnect Certificate ID")
}
