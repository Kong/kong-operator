package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestCreateKonnectEventDataPlaneCertificate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testKonnectEventDataPlaneCertificate()

	expectedRequest, err := cert.Spec.APISpec.ToCreateEventGatewayDataPlaneCertificateRequest()
	require.NoError(t, err)

	sdk.On("CreateEventGatewayDataPlaneCertificate", mock.Anything, "gateway-1", expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayDataPlaneCertificateResponse{
			EventGatewayDataPlaneCertificate: &sdkkonnectcomp.EventGatewayDataPlaneCertificate{
				ID: "cert-1",
			},
		}, nil).
		Once()

	err = createKonnectEventDataPlaneCertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "cert-1", cert.GetKonnectID())
}

func TestUpdateKonnectEventDataPlaneCertificate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testKonnectEventDataPlaneCertificate()
	cert.SetKonnectID("cert-1")

	expectedRequest, err := cert.Spec.APISpec.ToUpdateEventGatewayDataPlaneCertificateRequest()
	require.NoError(t, err)

	sdk.On("UpdateEventGatewayDataPlaneCertificate", mock.Anything, sdkkonnectops.UpdateEventGatewayDataPlaneCertificateRequest{
		GatewayID:     "gateway-1",
		CertificateID: "cert-1",
		UpdateEventGatewayDataPlaneCertificateRequest: expectedRequest,
	}).
		Return(&sdkkonnectops.UpdateEventGatewayDataPlaneCertificateResponse{
			EventGatewayDataPlaneCertificate: &sdkkonnectcomp.EventGatewayDataPlaneCertificate{
				ID: "cert-1",
			},
		}, nil).
		Once()

	err = updateKonnectEventDataPlaneCertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "cert-1", cert.GetKonnectID())
}

func TestDeleteKonnectEventDataPlaneCertificate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testKonnectEventDataPlaneCertificate()
	cert.SetKonnectID("cert-1")

	sdk.On("DeleteEventGatewayDataPlaneCertificate", mock.Anything, "gateway-1", "cert-1").
		Return(&sdkkonnectops.DeleteEventGatewayDataPlaneCertificateResponse{}, nil).
		Once()

	err := deleteKonnectEventDataPlaneCertificate(ctx, sdk, cert)
	require.NoError(t, err)
}

func TestGetKonnectEventDataPlaneCertificateForUID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testKonnectEventDataPlaneCertificate()

	sdk.On("ListEventGatewayDataPlaneCertificates", mock.Anything, sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest{
		GatewayID: "gateway-1",
	}).
		Return(&sdkkonnectops.ListEventGatewayDataPlaneCertificatesResponse{
			ListEventGatewayDataPlaneCertificatesResponse: &sdkkonnectcomp.ListEventGatewayDataPlaneCertificatesResponse{
				Data: []sdkkonnectcomp.EventGatewayDataPlaneCertificate{
					{
						ID:          "cert-other",
						Certificate: "other-cert",
					},
					{
						ID:          "cert-1",
						Certificate: cert.Spec.APISpec.Certificate,
						Name:        new(cert.Spec.APISpec.Name),
						Description: new(cert.Spec.APISpec.Description),
					},
				},
			},
		}, nil).
		Once()

	id, err := getKonnectEventDataPlaneCertificateForUID(ctx, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "cert-1", id)
}

func TestKongEventDataPlaneCertificateCreateRequestFromSecretRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sourceType := konnectv1alpha1.SensitiveDataSourceTypeSecretRef
	cert := testKonnectEventDataPlaneCertificate()
	cert.Spec.Type = &sourceType
	cert.Spec.SecretRef = &commonv1alpha1.NamespacedRef{Name: "tls-secret"}
	cert.Spec.APISpec.Certificate = ""

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"tls.crt": []byte("secret-cert"),
			},
		}).
		Build()

	req, err := kongEventDataPlaneCertificateCreateRequest(ctx, cl, cert)
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "secret-cert", req.Certificate)
	assert.Equal(t, cert.Spec.APISpec.Name, *req.Name)
}

func testKonnectEventDataPlaneCertificate() *konnectv1alpha1.KonnectEventDataPlaneCertificate {
	return &konnectv1alpha1.KonnectEventDataPlaneCertificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "KonnectEventDataPlaneCertificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "event-dp-cert",
			Namespace: "default",
			UID:       "event-dp-cert-uid",
		},
		Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-control-plane",
				},
			},
			APISpec: konnectv1alpha1.KonnectEventDataPlaneCertificateAPISpec{
				Certificate: "inline-cert",
				Name:        "client-cert",
				Description: "certificate description",
			},
		},
		Status: konnectv1alpha1.KonnectEventDataPlaneCertificateStatus{
			GatewayID: &konnectv1alpha1.KonnectEntityRef{ID: "gateway-1"},
		},
	}
}

//go:fix inline
// func stringPtr(s string) *string {
// 	return &s
// }
