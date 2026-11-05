package refs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	referencegranthelpers "github.com/kong/kong-operator/v2/test/helpers/referencegrant"
)

func TestIsTLSPassthroughListener(t *testing.T) {
	for _, tt := range []struct {
		name        string
		listener    gatewayv1.Listener
		passthrough bool
	}{
		{name: "no TLS", listener: gatewayv1.Listener{Protocol: gatewayv1.TLSProtocolType}},
		{name: "default termination", listener: gatewayv1.Listener{
			Protocol: gatewayv1.TLSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{},
		}},
		{name: "explicit termination", listener: gatewayv1.Listener{
			Protocol: gatewayv1.TLSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{Mode: new(gatewayv1.TLSModeTerminate)},
		}},
		{name: "TLS passthrough", passthrough: true, listener: gatewayv1.Listener{
			Protocol: gatewayv1.TLSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{Mode: new(gatewayv1.TLSModePassthrough)},
		}},
		{name: "HTTPS cannot use passthrough", listener: gatewayv1.Listener{
			Protocol: gatewayv1.HTTPSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{Mode: new(gatewayv1.TLSModePassthrough)},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.passthrough, IsTLSPassthroughListener(tt.listener))
		})
	}
}

func TestResolveListenerKongCertificate(t *testing.T) {
	tests := []struct {
		name         string
		crossNS      bool
		gatewayGrant bool
		kongGrant    bool
		missing      bool
		onPrem       bool
		otherCP      bool
		apiError     bool
		mutate       func(*configurationv1alpha1.KongCertificate)
		expected     gatewayv1.ListenerConditionReason
		message      string
	}{
		{name: "same namespace uses existing certificate"},
		{name: "different mirror objects can represent the same CP", crossNS: true, gatewayGrant: true, kongGrant: true},
		{name: "missing Gateway ReferenceGrant", crossNS: true, kongGrant: true, expected: gatewayv1.ListenerReasonRefNotPermitted},
		{name: "missing SNI KongReferenceGrant", crossNS: true, gatewayGrant: true, expected: gatewayv1.ListenerReasonRefNotPermitted},
		{name: "certificate does not exist", missing: true, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "on premises Gateway", onPrem: true, expected: gatewayv1.ListenerReasonInvalidCertificateRef,
			message: "KongCertificate references are only supported for Konnect Gateways"},
		{name: "missing certificate before Gateway ControlPlane", onPrem: true, missing: true,
			expected: gatewayv1.ListenerReasonInvalidCertificateRef,
			message:  "KongCertificate gateways/vault-certificate does not exist"},
		{name: "unprogrammed certificate before Gateway ControlPlane", onPrem: true,
			mutate: func(c *configurationv1alpha1.KongCertificate) {
				c.Status.Conditions[0].Status = metav1.ConditionFalse
			}, expected: gatewayv1.ListenerReasonInvalidCertificateRef, message: "is not programmed in Konnect yet"},
		{name: "deleting certificate before Gateway ControlPlane", onPrem: true,
			mutate: func(c *configurationv1alpha1.KongCertificate) {
				c.DeletionTimestamp = new(metav1.Now())
				c.Finalizers = []string{"test.example/cleanup"}
			}, expected: gatewayv1.ListenerReasonInvalidCertificateRef, message: "is being deleted"},
		{name: "certificate API failure before Gateway ControlPlane", onPrem: true, apiError: true},
		{name: "different actual ControlPlane", otherCP: true, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "certificate not programmed", mutate: func(c *configurationv1alpha1.KongCertificate) {
			c.Status.Conditions[0].Status = metav1.ConditionFalse
		}, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "stale certificate status", mutate: func(c *configurationv1alpha1.KongCertificate) {
			c.Generation++
		}, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "certificate ID missing", mutate: func(c *configurationv1alpha1.KongCertificate) {
			c.Status.Konnect.ID = ""
		}, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "certificate still programmed in old CP", mutate: func(c *configurationv1alpha1.KongCertificate) {
			c.Status.Konnect.ControlPlaneID = "old-cp-id"
		}, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "certificate deleting", mutate: func(c *configurationv1alpha1.KongCertificate) {
			c.DeletionTimestamp = new(metav1.Now())
			c.Finalizers = []string{"test.example/cleanup"}
		}, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "no certificate ControlPlane", mutate: func(c *configurationv1alpha1.KongCertificate) {
			c.Spec.ControlPlaneRef = nil
		}, expected: gatewayv1.ListenerReasonInvalidCertificateRef},
		{name: "Kubernetes API failure", apiError: true},
	}
	for _, gv := range referencegranthelpers.Versions() {
		t.Run(gv.Version, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					ctx := t.Context()
					gateway, cert, objects := listenerCertificateFixtures()
					if tt.crossNS {
						cert.Namespace = "certificates"
					}
					certCP := objects[1].(*konnectv1alpha2.KonnectGatewayControlPlane)
					certCP.Namespace = cert.Namespace
					if tt.otherCP {
						certCP.Status.ID = "other-cp-id"
						cert.Status.Konnect.ControlPlaneID = certCP.Status.ID
					}
					if tt.mutate != nil {
						tt.mutate(cert)
					}
					if tt.onPrem {
						objects = objects[:2]
					}
					if !tt.missing {
						objects = append(objects, cert)
					}
					if tt.gatewayGrant {
						objects = append(objects, &gatewayv1.ReferenceGrant{
							Name: "gateway-certificate", Namespace: cert.Namespace,
							Spec: gatewayv1.ReferenceGrantSpec{
								From: []gatewayv1.ReferenceGrantFrom{{Group: gatewayv1.GroupName, Kind: "Gateway", Namespace: gatewayv1.Namespace(gateway.Namespace)}},
								To: []gatewayv1.ReferenceGrantTo{{
									Group: gatewayv1.Group(configurationv1alpha1.GroupVersion.Group), Kind: "KongCertificate", Name: new(gatewayv1.ObjectName(cert.Name)),
								}},
							},
						})
					}
					if tt.kongGrant {
						objects = append(objects, &configurationv1alpha1.KongReferenceGrant{
							Name: "sni-certificate", Namespace: cert.Namespace,
							Spec: configurationv1alpha1.KongReferenceGrantSpec{
								From: []configurationv1alpha1.ReferenceGrantFrom{{
									Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group), Kind: "KongSNI", Namespace: configurationv1alpha1.Namespace(gateway.Namespace),
								}},
								To: []configurationv1alpha1.ReferenceGrantTo{{
									Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group), Kind: "KongCertificate", Name: new(configurationv1alpha1.ObjectName(cert.Name)),
								}},
							},
						})
					}
					sentinel := errors.New("Kubernetes API unavailable")
					cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
						WithObjects(referencegranthelpers.AsVersion(gv, objects)...).
						WithInterceptorFuncs(interceptor.Funcs{
							Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
								if _, ok := obj.(*configurationv1alpha1.KongCertificate); ok && tt.apiError {
									return sentinel
								}
								return cl.Get(ctx, key, obj, opts...)
							},
						}).Build()
					ref := gatewayv1.SecretObjectReference{
						Group: new(gatewayv1.Group(configurationv1alpha1.GroupVersion.Group)), Kind: new(gatewayv1.Kind("KongCertificate")),
						Name: gatewayv1.ObjectName(cert.Name), Namespace: new(gatewayv1.Namespace(cert.Namespace)),
					}
					resolved, err := ResolveListenerKongCertificate(ctx, cl, gv, gateway, ref)
					switch {
					case tt.apiError:
						require.ErrorIs(t, err, sentinel)
					case tt.expected != "":
						var invalid *ListenerCertificateRefError
						require.ErrorAs(t, err, &invalid)
						require.Equal(t, tt.expected, invalid.Reason)
						if tt.message != "" {
							require.Contains(t, invalid.Message, tt.message)
						}
						require.Nil(t, resolved)
					default:
						require.NoError(t, err)
						require.Equal(t, cert.Spec, resolved.Spec)
						require.Empty(t, resolved.OwnerReferences)
					}
				})
			}
		})
	}
}

func listenerCertificateFixtures() (*gwtypes.Gateway, *configurationv1alpha1.KongCertificate, []client.Object) {
	gateway := &gwtypes.Gateway{Name: "gateway", Namespace: "gateways", UID: "gateway-uid"}
	conditions := []metav1.Condition{{
		Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionTrue, ObservedGeneration: 1,
	}}
	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		Name: "gateway-cp", Namespace: gateway.Namespace, Generation: 1,
		Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "cp-id"}, Conditions: conditions,
		},
	}
	certCP := cp.DeepCopy()
	certCP.Name = "certificate-cp"
	cert := &configurationv1alpha1.KongCertificate{
		Name: "vault-certificate", Namespace: gateway.Namespace, Generation: 1,
		Spec: configurationv1alpha1.KongCertificateSpec{
			Type: new(configurationv1alpha1.KongCertificateSourceTypeInline),
			Cert: "{vault://certificates/tls/certificate}", Key: "{vault://certificates/tls/key}",
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: certCP.Name},
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect:    &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{ID: "cert-id", ControlPlaneID: cp.Status.ID},
			Conditions: conditions,
		},
	}
	extension := &konnectv1alpha2.KonnectExtension{
		Name: "gateway-extension", Namespace: gateway.Namespace,
		Labels: map[string]string{"gateway-operator.konghq.com/managed-by": "gateway"},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID,
		}},
		Spec: konnectv1alpha2.KonnectExtensionSpec{Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
			ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
				Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			}},
		}},
	}
	return gateway, cert, []client.Object{cp, certCP, extension}
}
