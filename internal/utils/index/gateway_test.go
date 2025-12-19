package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestOptionsForGateway(t *testing.T) {
	options := OptionsForGateway()
	require.Len(t, options, 2)

	opt := options[0]
	require.IsType(t, &gwtypes.Gateway{}, opt.Object)
	require.Equal(t, GatewayClassOnGatewayIndex, opt.Field)
	require.NotNil(t, opt.ExtractValueFn)

	opt2 := options[1]
	require.IsType(t, &gwtypes.Gateway{}, opt2.Object)
	require.Equal(t, TLSCertificateSecretsOnGatewayIndex, opt2.Field)
	require.NotNil(t, opt2.ExtractValueFn)
}

func TestGatewayClassOnGateway(t *testing.T) {
	tests := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "valid GatewayClassName",
			obj: &gwtypes.Gateway{
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "my-class",
				},
			},
			want: []string{"my-class"},
		},
		{
			name: "empty GatewayClassName",
			obj: &gwtypes.Gateway{
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "",
				},
			},
			want: nil,
		},
		{
			name: "wrong type",
			obj:  &gwtypes.HTTPRoute{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GatewayClassOnGateway(tt.obj)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTLSCertificateSecretsOnGateway(t *testing.T) {
	secretGroup := gatewayv1.Group(corev1.GroupName)
	secretKind := gatewayv1.Kind("Secret")
	otherGroup := gatewayv1.Group("other.group")
	otherKind := gatewayv1.Kind("OtherKind")
	namespace1 := gatewayv1.Namespace("ns1")
	namespace2 := gatewayv1.Namespace("ns2")

	tests := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "single listener with one certificate",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1"},
		},
		{
			name: "multiple listeners with multiple certificates",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
									{Name: "cert2"},
								},
							},
						},
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert3"},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1", "default/cert2", "default/cert3"},
		},
		{
			name: "cross-namespace certificate references",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
									{Name: "cert2", Namespace: &namespace1},
									{Name: "cert3", Namespace: &namespace2},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1", "ns1/cert2", "ns2/cert3"},
		},
		{
			name: "duplicate references are deduplicated",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
									{Name: "cert1"},
								},
							},
						},
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1"},
		},
		{
			name: "listener without TLS is skipped",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: nil,
						},
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1"},
		},
		{
			name: "non-Secret group is filtered out",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
									{Name: "cert2", Group: &otherGroup},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1"},
		},
		{
			name: "non-Secret kind is filtered out",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1"},
									{Name: "cert2", Kind: &otherKind},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1"},
		},
		{
			name: "explicit Secret group and kind are accepted",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "cert1", Group: &secretGroup, Kind: &secretKind},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1"},
		},
		{
			name: "empty certificate name is skipped",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: ""},
									{Name: "cert1"},
								},
							},
						},
					},
				},
			},
			want: []string{"default/cert1"},
		},
		{
			name: "no listeners returns nil",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{},
				},
			},
			want: nil,
		},
		{
			name: "no TLS certificates returns nil",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							TLS: &gwtypes.GatewayTLSConfig{
								CertificateRefs: []gatewayv1.SecretObjectReference{},
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "wrong type returns nil",
			obj:  &gwtypes.HTTPRoute{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TLSCertificateSecretsOnGateway(tt.obj)
			switch {
			case tt.want == nil:
				require.Nil(t, got, "expected nil but got %v", got)
			case len(tt.want) == 0:
				require.NotNil(t, got, "expected empty slice but got nil")
				require.Empty(t, got, "expected empty slice but got %v", got)
			default:
				require.ElementsMatch(t, tt.want, got)
			}
		})
	}
}
