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

// TestGetEventGatewayDataPlaneCertificateForUID covers the lookup of Event
// Gateway data plane certificates, which have no labels in Konnect: the
// certificate is matched by its (Secret-resolved) certificate, name and
// description, so one with the same name but another certificate is never
// matched.
func TestGetEventGatewayDataPlaneCertificateForUID(t *testing.T) {
	inlineCert := testEventGatewayDataPlaneCertificate()
	secretCert := testEventGatewayDataPlaneCertificate()
	secretCert.Spec.APISpec.Certificate = configurationv1alpha1.SensitiveDataSource{
		Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "tls-secret", Key: "tls.crt"},
	}
	clWithSecret := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(&corev1.Secret{
			Name:      "tls-secret",
			Namespace: "default",
			Data:      map[string][]byte{"tls.crt": []byte("secret-cert\n")},
		}).
		Build()
	entry := func(id, certificate string, obj *configurationv1alpha1.EventGatewayDataPlaneCertificate) sdkkonnectcomp.EventGatewayDataPlaneCertificate {
		return sdkkonnectcomp.EventGatewayDataPlaneCertificate{
			ID:          id,
			Certificate: certificate,
			Name:        new(obj.Spec.APISpec.Name),
			Description: new(obj.Spec.APISpec.Description),
		}
	}

	testCases := []struct {
		name       string
		obj        *configurationv1alpha1.EventGatewayDataPlaneCertificate
		entries    []sdkkonnectcomp.EventGatewayDataPlaneCertificate
		noList     bool
		expectedID string
	}{
		{
			name: "matches the inline certificate, name and description",
			obj:  inlineCert,
			entries: []sdkkonnectcomp.EventGatewayDataPlaneCertificate{
				{ID: "cert-other", Certificate: "other-cert"},
				entry("cert-1", *inlineCert.Spec.APISpec.Certificate.Value, inlineCert),
			},
			expectedID: "cert-1",
		},
		{
			name: "matches the certificate resolved from its Secret",
			obj:  secretCert,
			entries: []sdkkonnectcomp.EventGatewayDataPlaneCertificate{
				entry("cert-1", "secret-cert", secretCert),
			},
			expectedID: "cert-1",
		},
		{
			name: "does not match the same name and description with another certificate",
			obj:  secretCert,
			entries: []sdkkonnectcomp.EventGatewayDataPlaneCertificate{
				entry("created-outside-the-operator", "another-cert", secretCert),
			},
		},
		{
			name:   "returns not found without listing when the certificate Secret is missing",
			obj:    testEventGatewayDataPlaneCertificateWithMissingSecret(),
			noList: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk := sdkmocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
			if !tc.noList {
				sdk.On("ListEventGatewayDataPlaneCertificates", mock.Anything, sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest{
					PageSize:  new(listPageSize),
					GatewayID: "gateway-1",
				}).
					Return(&sdkkonnectops.ListEventGatewayDataPlaneCertificatesResponse{
						ListEventGatewayDataPlaneCertificatesResponse: &sdkkonnectcomp.ListEventGatewayDataPlaneCertificatesResponse{
							Data: tc.entries,
						},
					}, nil).
					Once()
			}

			id, err := getEventGatewayDataPlaneCertificateForUID(t.Context(), sdk, clWithSecret, tc.obj)
			if tc.expectedID != "" {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedID, id)
				return
			}
			assert.Empty(t, id)
			var notFound EntityWithMatchingUIDNotFoundError
			require.ErrorAs(t, err, &notFound)
		})
	}
}

func testEventGatewayDataPlaneCertificateWithMissingSecret() *configurationv1alpha1.EventGatewayDataPlaneCertificate {
	cert := testEventGatewayDataPlaneCertificate()
	cert.Spec.APISpec.Certificate = configurationv1alpha1.SensitiveDataSource{
		Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "missing", Key: "tls.crt"},
	}
	return cert
}

func TestEventGatewayDataPlaneCertificate_ToCreateEventGatewayDataPlaneCertificateRequest_FromSecretRef(t *testing.T) {
	ctx := t.Context()
	cert := testEventGatewayDataPlaneCertificate()
	cert.Spec.APISpec.Certificate = configurationv1alpha1.SensitiveDataSource{
		Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "tls-secret", Key: "tls.crt"},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(&corev1.Secret{
			Name:      "tls-secret",
			Namespace: "default",
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
		SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "tls-secret", Key: "tls.crt"},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(&corev1.Secret{
			Name:      "tls-secret",
			Namespace: "default",
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
		APIVersion: konnectv1alpha1.GroupVersion.String(),
		Kind:       "EventGatewayDataPlaneCertificate",
		Name:       "event-dp-cert",
		Namespace:  "default",
		UID:        "event-dp-cert-uid",
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
