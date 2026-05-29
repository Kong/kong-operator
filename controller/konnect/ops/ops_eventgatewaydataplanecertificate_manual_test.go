package ops

import (
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
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestCreateEventGatewayDataPlaneCertificate(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testEventGatewayDataPlaneCertificate()

	expectedRequest, err := cert.Spec.APISpec.ToCreateEventGatewayDataPlaneCertificateRequest()
	require.NoError(t, err)

	sdk.On("CreateEventGatewayDataPlaneCertificate", mock.Anything, "gateway-1", expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayDataPlaneCertificateResponse{
			EventGatewayDataPlaneCertificate: &sdkkonnectcomp.EventGatewayDataPlaneCertificate{
				ID: "cert-1",
			},
		}, nil).
		Once()

	err = createEventGatewayDataPlaneCertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "cert-1", cert.GetKonnectID())
}

func TestUpdateEventGatewayDataPlaneCertificate(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testEventGatewayDataPlaneCertificate()
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

	err = updateEventGatewayDataPlaneCertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "cert-1", cert.GetKonnectID())
}

func TestDeleteEventGatewayDataPlaneCertificate(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testEventGatewayDataPlaneCertificate()
	cert.SetKonnectID("cert-1")

	sdk.On("DeleteEventGatewayDataPlaneCertificate", mock.Anything, "gateway-1", "cert-1").
		Return(&sdkkonnectops.DeleteEventGatewayDataPlaneCertificateResponse{}, nil).
		Once()

	err := deleteEventGatewayDataPlaneCertificate(ctx, sdk, cert)
	require.NoError(t, err)
}

func TestGetEventGatewayDataPlaneCertificateForUID(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
	cert := testEventGatewayDataPlaneCertificate()

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
						Certificate: *cert.Spec.APISpec.Certificate.Value,
						Name:        new(cert.Spec.APISpec.Name),
						Description: new(cert.Spec.APISpec.Description),
					},
				},
			},
		}, nil).
		Once()

	id, err := getEventGatewayDataPlaneCertificateForUID(ctx, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "cert-1", id)
}

func TestEventGatewayDataPlaneCertificate_ToCreateEventGatewayDataPlaneCertificateRequest_FromSecretRef(t *testing.T) {
	ctx := t.Context()
	cert := testEventGatewayDataPlaneCertificate()
	cert.Spec.APISpec.Certificate = configurationv1alpha1.SensitiveDataSource{
		Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &commonv1alpha1.NamespacedRef{Name: "tls-secret"},
	}

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

	req, err := cert.ToCreateEventGatewayDataPlaneCertificateRequest(ctx, cl)
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "secret-cert", req.Certificate)
	assert.Equal(t, cert.Spec.APISpec.Name, *req.Name)
}

func TestEventGatewayDataPlaneCertificate_ToUpdateEventGatewayDataPlaneCertificateRequest_FromSecretRef(t *testing.T) {
	ctx := t.Context()
	cert := testEventGatewayDataPlaneCertificate()
	cert.Spec.APISpec.Certificate = configurationv1alpha1.SensitiveDataSource{
		Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &commonv1alpha1.NamespacedRef{Name: "tls-secret"},
	}

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

	req, err := cert.ToUpdateEventGatewayDataPlaneCertificateRequest(ctx, cl)
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "secret-cert", req.Certificate)
	assert.Equal(t, cert.Spec.APISpec.Name, *req.Name)
}

func testEventGatewayDataPlaneCertificate() *configurationv1alpha1.EventGatewayDataPlaneCertificate {
	return &configurationv1alpha1.EventGatewayDataPlaneCertificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayDataPlaneCertificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "event-dp-cert",
			Namespace: "default",
			UID:       "event-dp-cert-uid",
		},
		Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-control-plane",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayDataPlaneCertificateAPISpec{
				Certificate: configurationv1alpha1.SensitiveDataSource{
					Type:  configurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new("inline-cert"),
				},
				Name:        "client-cert",
				Description: "certificate description",
			},
		},
		Status: configurationv1alpha1.EventGatewayDataPlaneCertificateStatus{
			GatewayID: &configurationv1alpha1.KonnectEntityRef{ID: "gateway-1"},
		},
	}
}
