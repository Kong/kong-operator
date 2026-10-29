package konnect

import (
	"context"
	"testing"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestHybridGatewayServiceCertificateRef(t *testing.T) {
	for _, state := range []string{"valid mirror", "wrong CP", "stale", "missing ID", "missing", "deleting", "revoked grant", "removed ref", "removed ref after revocation", "local ref after revocation", "API failure"} {
		t.Run(state, func(t *testing.T) {
			service, cert, cp := hybridGatewayServiceCertificateFixtures()
			mirror := cp.DeepCopy()
			mirror.Name = "certificate-mirror"
			cert.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = mirror.Name
			switch state {
			case "wrong CP":
				mirror.Status.ID = "different-cp"
				cert.Status.Konnect.ControlPlaneID = mirror.Status.ID
			case "stale":
				cert.Generation++
			case "missing ID":
				cert.Status.Konnect.ID = ""
			case "deleting":
				cert.DeletionTimestamp = new(metav1.Now())
				cert.Finalizers = []string{"test-finalizer"}
			case "revoked grant":
				cert.Namespace = "certificates"
				service.Spec.ClientCertificateRef.Namespace = new(cert.Namespace)
			case "removed ref", "removed ref after revocation":
				service.Spec.ClientCertificateRef = nil
				service.Status.Conditions = []metav1.Condition{{
					Type: string(konnectv1alpha1.KongCertificateRefValidConditionType), Status: metav1.ConditionFalse,
				}}
			}
			if state == "removed ref after revocation" || state == "local ref after revocation" {
				service.Status.Conditions = append(service.Status.Conditions, metav1.Condition{
					Type: "ResolvedRefs", Status: metav1.ConditionFalse, Reason: "RefNotPermitted",
					Message: "KongService reference to KongCertificate not allowed",
				})
			}
			objects := []client.Object{service, cp, mirror}
			if state != "missing" {
				objects = append(objects, cert)
			}
			apiErr := apierrors.NewServiceUnavailable("test failure")
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
				WithStatusSubresource(&configurationv1alpha1.KongService{}).
				WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.KongCertificate); ok && state == "API failure" {
						return apiErr
					}
					return cl.Get(ctx, key, obj, opts...)
				}}).Build()
			result, err := handleKongCertificateRef(t.Context(), cl, service)
			require.Equal(t, "service-id", service.GetKonnectID())
			require.Equal(t, cp.Status.ID, service.Status.Konnect.ControlPlaneID, "a certificate must never overwrite the Service ControlPlane")
			switch state {
			case "API failure":
				require.ErrorIs(t, err, apiErr)
				require.Equal(t, "original-certificate-id", service.Status.Konnect.CertificateID)
			case "valid mirror", "local ref after revocation":
				require.NoError(t, err)
				require.True(t, result.IsZero())
				require.Equal(t, cert.GetKonnectID(), service.Status.Konnect.CertificateID)
			case "removed ref", "removed ref after revocation":
				require.NoError(t, err)
				require.Empty(t, service.Status.Konnect.CertificateID)
				for _, condition := range service.Status.Conditions {
					require.NotEqual(t, string(konnectv1alpha1.KongCertificateRefValidConditionType), condition.Type)
				}
			default:
				require.NoError(t, err)
				require.False(t, result.IsZero(), "invalid bindings must stop programming")
				require.Equal(t, "original-certificate-id", service.Status.Konnect.CertificateID, "persist IDs for remote cleanup")
				var current configurationv1alpha1.KongService
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(service), &current))
				condition, found := k8sutils.GetCondition(konnectv1alpha1.KongCertificateRefValidConditionType, &current)
				require.True(t, found)
				require.Equal(t, metav1.ConditionFalse, condition.Status)
			}
			if state == "removed ref after revocation" || state == "local ref after revocation" {
				_, found := k8sutils.GetCondition("ResolvedRefs", service)
				require.False(t, found, "certificate grant failures must not block local or removed refs")
			}
		})
	}
}

func TestDeleteHybridGatewayServiceWithInvalidCertificate(t *testing.T) {
	for _, state := range []string{"missing", "revoked grant", "wrong CP", "stale", "deleting certificate"} {
		t.Run(state, func(t *testing.T) {
			service, cert, cp := hybridGatewayServiceCertificateFixtures()
			cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "auth"
			auth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: cp.Namespace,
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeToken, Token: "test-token", ServerURL: sdkmocks.SDKServerURL,
				},
				Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{Conditions: []metav1.Condition{{
					Type: konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, Status: metav1.ConditionTrue,
					Reason: konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
				}}},
			}

			service.Finalizers = []string{KonnectCleanupFinalizer}
			switch state {
			case "revoked grant":
				service.Spec.ClientCertificateRef.Namespace = new("certificates")
				cert.Namespace = "certificates"
			case "wrong CP":
				cert.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "wrong-cp"
				cert.Status.Konnect.ControlPlaneID = "wrong-cp"
			case "stale":
				cert.Generation++
			case "deleting certificate":
				cert.DeletionTimestamp = new(metav1.Now())
				cert.Finalizers = []string{"test-finalizer"}
			}
			objects := []client.Object{service, cp, auth}
			if state != "missing" {
				objects = append(objects, cert)
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
				WithStatusSubresource(&configurationv1alpha1.KongService{}, &konnectv1alpha1.KonnectAPIAuthConfiguration{}).Build()
			require.NoError(t, cl.Delete(t.Context(), service))
			factory := sdkmocks.NewMockSDKFactory(t)
			factory.SDK.ServicesSDK.EXPECT().DeleteService(mock.Anything, cp.Status.ID, "service-id").
				Return(&sdkkonnectops.DeleteServiceResponse{}, nil).Once()
			reconciler := NewKonnectEntityReconciler[configurationv1alpha1.KongService](
				factory, logging.DevelopmentMode, cl,
				WithMetricRecorder[configurationv1alpha1.KongService](&metricsmocks.MockRecorder{}),
			)
			key := client.ObjectKeyFromObject(service)
			for range 6 {
				if err := cl.Get(t.Context(), key, service); apierrors.IsNotFound(err) {
					break
				} else {
					require.NoError(t, err)
				}
				_, err := reconciler.Reconcile(t.Context(), service)
				require.NoError(t, err)
			}
			require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), key, service)))
			if state != "missing" {
				var current configurationv1alpha1.KongCertificate
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cert), &current), "external certificates survive Service cleanup")
			}
		})
	}
}

func TestReconcileHybridGatewayServiceInvalidCertificateDoesNotProgram(t *testing.T) {
	for _, state := range []string{"wrong CP", "API failure"} {
		t.Run(state, func(t *testing.T) {
			service, cert, cp := hybridGatewayServiceCertificateFixtures()
			service.Finalizers = []string{KonnectCleanupFinalizer}
			if state == "wrong CP" {
				cert.Status.Konnect.ControlPlaneID = "another-cp"
			}
			apiErr := apierrors.NewServiceUnavailable("test failure")
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(service, cert, cp).
				WithStatusSubresource(&configurationv1alpha1.KongService{}).
				WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.KongCertificate); ok && state == "API failure" {
						return apiErr
					}
					return cl.Get(ctx, key, obj, opts...)
				}}).Build()
			// No SDK calls are expected before certificate validation succeeds.
			factory := sdkmocks.NewMockSDKFactory(t)
			reconciler := NewKonnectEntityReconciler[configurationv1alpha1.KongService](
				factory, logging.DevelopmentMode, cl,
				WithMetricRecorder[configurationv1alpha1.KongService](&metricsmocks.MockRecorder{}),
			)
			var sawAPIError bool
			for range 6 {
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(service), service))
				_, err := reconciler.Reconcile(t.Context(), service)
				if err != nil {
					require.Equal(t, "API failure", state)
					require.ErrorIs(t, err, apiErr)
					sawAPIError = true
				}
			}
			require.Equal(t, state == "API failure", sawAPIError)
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(service), service))
			require.Equal(t, cp.Status.ID, service.Status.Konnect.ControlPlaneID)
			require.Equal(t, "service-id", service.GetKonnectID())
			require.Equal(t, "original-certificate-id", service.Status.Konnect.CertificateID)
		})
	}
}

func hybridGatewayServiceCertificateFixtures() (*configurationv1alpha1.KongService, *configurationv1alpha1.KongCertificate, *konnectv1alpha2.KonnectGatewayControlPlane) {
	cp := testControlPlaneOK.DeepCopy()
	cert := testKongCertOK.DeepCopy()
	service := &configurationv1alpha1.KongService{
		Name: "service", Namespace: cp.Namespace,
		Labels: map[string]string{consts.GatewayOperatorManagedByLabel: "HTTPRoute"},
		Annotations: map[string]string{
			consts.GatewayOperatorHybridGatewaysAnnotation:        cp.Namespace + "/gateway",
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: cp.Namespace + "/route",
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			},
			ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: cert.Name},
			Host:                 "upstream",
		},
		Status: configurationv1alpha1.KongServiceStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{
				ID: "service-id", ControlPlaneID: cp.Status.ID, CertificateID: "original-certificate-id",
			},
		},
	}
	return service, cert, cp
}
