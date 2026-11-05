package refs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	hgerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestResolveServiceKongCertificate(t *testing.T) {
	for _, tt := range []struct {
		name       string
		crossNS    bool
		grantFrom  string
		missing    bool
		apiFailure bool
		mutate     func(*configurationv1alpha1.KongCertificate)
		wantReason string
	}{
		{name: "same namespace no grant and distinct Kubernetes CP mirrors"},
		{name: "cross namespace authorized from actual generated Service namespace", crossNS: true, grantFrom: "gateways"},
		{name: "no grant", crossNS: true, wantReason: "RefNotPermitted"},
		{name: "grant from backend namespace is insufficient", crossNS: true, grantFrom: "backends", wantReason: "RefNotPermitted"},
		{name: "missing", missing: true, wantReason: "InvalidCertificateRef"},
		{name: "API failure is not invalid desired state", apiFailure: true},
		{name: "stale certificate", mutate: func(cert *configurationv1alpha1.KongCertificate) {
			cert.Generation++
		}, wantReason: "InvalidCertificateRef"},
		{name: "unprogrammed certificate", mutate: func(cert *configurationv1alpha1.KongCertificate) {
			cert.Status.Conditions[0].Status = metav1.ConditionFalse
		}, wantReason: "InvalidCertificateRef"},
		{name: "missing certificate ID", mutate: func(cert *configurationv1alpha1.KongCertificate) {
			cert.Status.Konnect.ID = ""
		}, wantReason: "InvalidCertificateRef"},
		{name: "persisted certificate CP differs", mutate: func(cert *configurationv1alpha1.KongCertificate) {
			cert.Status.Konnect.ControlPlaneID = "another-cp"
		}, wantReason: "InvalidCertificateRef"},
		{name: "different effective certificate CP", mutate: func(cert *configurationv1alpha1.KongCertificate) {
			cert.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "wrong-cp"
		}, wantReason: "InvalidCertificateRef"},
		{name: "deleting certificate", mutate: func(cert *configurationv1alpha1.KongCertificate) {
			cert.DeletionTimestamp = new(metav1.Now())
			cert.Finalizers = []string{"test-finalizer"}
		}, wantReason: "InvalidCertificateRef"},
		{name: "no certificate CP", mutate: func(cert *configurationv1alpha1.KongCertificate) {
			cert.Spec.ControlPlaneRef = nil
		}, wantReason: "InvalidCertificateRef"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, cert, objects := listenerCertificateFixtures()
			service := &configurationv1alpha1.KongService{
				Namespace: gateway.Namespace,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "gateway-cp",
						},
					},
					ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: cert.Name},
				},
			}
			if tt.crossNS {
				cert.Namespace = "certificates"
				cert.Spec.ControlPlaneRef.KonnectNamespacedRef.Namespace = gateway.Namespace
				service.Spec.ClientCertificateRef.Namespace = new(cert.Namespace)
			}
			if tt.grantFrom != "" {
				objects = append(objects, &configurationv1alpha1.KongReferenceGrant{
					Name: "client-certificate", Namespace: cert.Namespace,
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{{
							Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
							Kind:  "KongService", Namespace: configurationv1alpha1.Namespace(tt.grantFrom),
						}},
						To: []configurationv1alpha1.ReferenceGrantTo{{
							Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
							Kind:  "KongCertificate", Name: new(configurationv1alpha1.ObjectName(cert.Name)),
						}},
					},
				})
			}
			if tt.mutate != nil {
				tt.mutate(cert)
			}
			if !tt.missing {
				objects = append(objects, cert)
			}
			objects = append(objects, &konnectv1alpha2.KonnectGatewayControlPlane{
				Name: "wrong-cp", Namespace: gateway.Namespace,
				Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "different-remote-cp"},
					Conditions:          []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue}},
				},
			})
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
				WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.KongCertificate); ok && tt.apiFailure {
						return apierrors.NewServiceUnavailable("test failure")
					}
					return cl.Get(ctx, key, obj, opts...)
				}}).Build()
			resolved, err := ResolveServiceKongCertificate(t.Context(), cl, service)
			switch {
			case tt.wantReason != "":
				invalid, ok := errors.AsType[*hgerrors.ClientCertificateRefError](err)
				require.True(t, ok, "%v", err)
				require.Equal(t, tt.wantReason, invalid.Reason)
				require.Nil(t, resolved)
			case tt.apiFailure:
				require.Error(t, err)
				require.False(t, hgerrors.OnlyClientCertificateRefErrors(err))
			default:
				require.NoError(t, err)
				require.Equal(t, cert, resolved)
			}
		})
	}
}
