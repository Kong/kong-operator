package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func Test_MapGatewayForTLSSecret(t *testing.T) {
	secretKind := gatewayv1.Kind("Secret")
	ns2 := gatewayv1.Namespace("ns2")

	tests := []struct {
		name          string
		secret        *corev1.Secret
		gateways      []gwtypes.Gateway
		expectedCount int
		expectedGWs   []client.ObjectKey
		listError     bool
	}{
		{
			name: "gateway references secret in same namespace",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: &secretKind,
											Name: "tls-secret",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 1,
			expectedGWs: []client.ObjectKey{
				{Namespace: "ns1", Name: "gw1"},
			},
		},
		{
			name: "gateway references secret in different namespace",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns2",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 1,
			expectedGWs: []client.ObjectKey{
				{Namespace: "ns1", Name: "gw1"},
			},
		},
		{
			name: "multiple gateways reference same secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: &secretKind,
											Name: "tls-secret",
										},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw2",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: &secretKind,
											Name: "tls-secret",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 2,
			expectedGWs: []client.ObjectKey{
				{Namespace: "ns1", Name: "gw1"},
				{Namespace: "ns1", Name: "gw2"},
			},
		},
		{
			name: "gateway references different secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: &secretKind,
											Name: "other-secret",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "gateway has no TLS",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "http",
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "gateway has no certificate refs",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS:  &gatewayv1.ListenerTLSConfig{},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "certificate ref is not a Secret kind",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: func() *gatewayv1.Kind { k := gatewayv1.Kind("ConfigMap"); return &k }(),
											Name: "tls-secret",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "wrong object type",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways:      []gwtypes.Gateway{},
			expectedCount: 0,
		},
		{
			name: "list error",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways:      []gwtypes.Gateway{},
			expectedCount: 0,
			listError:     true,
		},
		{
			name: "gateway with multiple listeners and cert refs",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "ns1",
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https1",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: &secretKind,
											Name: "other-secret",
										},
									},
								},
							},
							{
								Name: "https2",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: &secretKind,
											Name: "tls-secret",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 1,
			expectedGWs: []client.ObjectKey{
				{Namespace: "ns1", Name: "gw1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cl client.Client
			if tt.listError {
				cl = &fakeErrorClient{}
			} else {
				objects := make([]client.Object, len(tt.gateways))
				for i := range tt.gateways {
					objects[i] = &tt.gateways[i]
				}
				cl = fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(objects...).
					WithIndex(&gwtypes.Gateway{}, index.TLSCertificateSecretsOnGatewayIndex, index.TLSCertificateSecretsOnGateway).
					Build()
			}

			mapFunc := MapGatewayForTLSSecret(cl)

			var obj client.Object
			if tt.name == "wrong object type" {
				obj = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "not-a-secret",
						Namespace: "ns1",
					},
				}
			} else {
				obj = tt.secret
			}

			requests := mapFunc(context.Background(), obj)

			assert.Len(t, requests, tt.expectedCount)
			if tt.expectedCount > 0 {
				for _, expectedGW := range tt.expectedGWs {
					found := false
					for _, req := range requests {
						if req.NamespacedName == expectedGW {
							found = true
							break
						}
					}
					assert.True(t, found, "expected gateway %v not found in requests", expectedGW)
				}
			}
		})
	}
}

func Test_MapGatewayForReferenceGrant(t *testing.T) {
	secretKind := gatewayv1.Kind("Secret")
	gatewayKind := gatewayv1beta1.Kind("Gateway")
	ns1 := gatewayv1.Namespace("ns1")
	ns2 := gatewayv1.Namespace("ns2")
	gwGroup := gatewayv1.Group(gwtypes.GroupName)

	tests := []struct {
		name          string
		rg            *gwtypes.ReferenceGrant
		gateways      []gwtypes.Gateway
		expectedCount int
		expectedGWs   []client.ObjectKey
		listError     bool
	}{
		{
			name: "gateway with cross-namespace secret ref matches ReferenceGrant",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 1,
			expectedGWs: []client.ObjectKey{
				{Namespace: "ns1", Name: "gw1"},
			},
		},
		{
			name: "ReferenceGrant does not allow Secrets",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Service",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "gateway in same namespace as secret - not cross-namespace",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns1",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: &secretKind,
											Name: "tls-secret",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "gateway references secret in different namespace than ReferenceGrant",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns3",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "ReferenceGrant from namespace does not match gateway namespace",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: gatewayv1.Namespace("ns3"),
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "ReferenceGrant from kind is not Gateway",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      "HTTPRoute",
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "ReferenceGrant from group is wrong",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     "wrong.group",
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "ReferenceGrant from group is empty - should match",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     "",
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      &secretKind,
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 1,
			expectedGWs: []client.ObjectKey{
				{Namespace: "ns1", Name: "gw1"},
			},
		},
		{
			name: "wrong object type",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways:      []gwtypes.Gateway{},
			expectedCount: 0,
		},
		{
			name: "list error",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways:      []gwtypes.Gateway{},
			expectedCount: 0,
			listError:     true,
		},
		{
			name: "gateway with no TLS",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "http",
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "gateway with no certificate refs",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS:  &gatewayv1.ListenerTLSConfig{},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "certificate ref is not Secret kind",
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Kind: "Secret",
						},
					},
				},
			},
			gateways: []gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw1",
						Namespace: "ns1",
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name: "https",
								TLS: &gatewayv1.ListenerTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind:      func() *gatewayv1.Kind { k := gatewayv1.Kind("ConfigMap"); return &k }(),
											Name:      "tls-secret",
											Namespace: &ns2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cl client.Client
			if tt.listError {
				cl = &fakeErrorClient{}
			} else {
				objects := make([]client.Object, len(tt.gateways))
				for i := range tt.gateways {
					objects[i] = &tt.gateways[i]
				}
				cl = fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(objects...).
					Build()
			}

			mapFunc := MapGatewayForReferenceGrant(cl)

			var obj client.Object
			if tt.name == "wrong object type" {
				obj = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "not-a-rg",
						Namespace: "ns1",
					},
				}
			} else {
				obj = tt.rg
			}

			requests := mapFunc(context.Background(), obj)

			assert.Len(t, requests, tt.expectedCount)
			if tt.expectedCount > 0 {
				for _, expectedGW := range tt.expectedGWs {
					found := false
					for _, req := range requests {
						if req.NamespacedName == expectedGW {
							found = true
							break
						}
					}
					assert.True(t, found, "expected gateway %v not found in requests", expectedGW)
				}
			}
		})
	}
}

func Test_hasMatchingCrossNamespaceSecretRef(t *testing.T) {
	secretKind := gatewayv1.Kind("Secret")
	gatewayKind := gatewayv1beta1.Kind("Gateway")
	ns1 := gatewayv1.Namespace("ns1")
	ns2 := gatewayv1.Namespace("ns2")
	gwGroup := gatewayv1.Group(gwtypes.GroupName)

	tests := []struct {
		name     string
		gw       gwtypes.Gateway
		rg       *gwtypes.ReferenceGrant
		expected bool
	}{
		{
			name: "matches - cross-namespace secret ref with correct ReferenceGrant",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS: &gatewayv1.ListenerTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Kind:      &secretKind,
										Name:      "tls-secret",
										Namespace: &ns2,
									},
								},
							},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "no match - same namespace",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS: &gatewayv1.ListenerTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Kind: &secretKind,
										Name: "tls-secret",
									},
								},
							},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns1",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no match - wrong namespace in ReferenceGrant from",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS: &gatewayv1.ListenerTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Kind:      &secretKind,
										Name:      "tls-secret",
										Namespace: &ns2,
									},
								},
							},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: gatewayv1.Namespace("ns3"),
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no match - no TLS",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "http",
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no match - no certificate refs",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS:  &gatewayv1.ListenerTLSConfig{},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no match - wrong kind",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS: &gatewayv1.ListenerTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Kind:      func() *gatewayv1.Kind { k := gatewayv1.Kind("ConfigMap"); return &k }(),
										Name:      "tls-secret",
										Namespace: &ns2,
									},
								},
							},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no match - wrong from kind",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS: &gatewayv1.ListenerTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Kind:      &secretKind,
										Name:      "tls-secret",
										Namespace: &ns2,
									},
								},
							},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwGroup,
							Kind:      "HTTPRoute",
							Namespace: ns1,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no match - wrong from group",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS: &gatewayv1.ListenerTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Kind:      &secretKind,
										Name:      "tls-secret",
										Namespace: &ns2,
									},
								},
							},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     "wrong.group",
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "match - empty from group",
			gw: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw1",
					Namespace: "ns1",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							TLS: &gatewayv1.ListenerTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Kind:      &secretKind,
										Name:      "tls-secret",
										Namespace: &ns2,
									},
								},
							},
						},
					},
				},
			},
			rg: &gwtypes.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rg1",
					Namespace: "ns2",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     "",
							Kind:      gatewayKind,
							Namespace: ns1,
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasMatchingCrossNamespaceSecretRef(tt.gw, tt.rg)
			assert.Equal(t, tt.expected, result)
		})
	}
}
