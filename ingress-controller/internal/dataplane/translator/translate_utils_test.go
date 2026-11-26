package translator

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestGenerateKongServiceFromBackendRef(t *testing.T) {
	grants := []*gwtypes.ReferenceGrant{
		{
			Name:      uuid.NewString(),
			Namespace: "fitrat",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("garbage"),
					},
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("behbudiy"),
					},
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("qodiriy"),
					},
				},
				To: []gwtypes.ReferenceGrantTo{
					{
						Group: gwtypes.Group(""),
						Kind:  gwtypes.Kind("Service"),
					},
				},
			},
		},
		{
			Name:      uuid.NewString(),
			Namespace: "cholpon",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("UDPRoute"),
						Namespace: gwtypes.Namespace("behbudiy"),
					},
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("qodiriy"),
					},
				},
				To: []gwtypes.ReferenceGrantTo{
					{
						Group: gwtypes.Group(""),
						Kind:  gwtypes.Kind("Service"),
					},
				},
			},
		},
	}
	// empty since we always want to actually generate a service for tests
	// static values for the basic string format inputs since nothing interesting happens with them
	rules := ingressRules{ServiceNameToServices: map[string]kongstate.Service{}}
	ruleNumber := 999
	protocol := "example"
	port := gwtypes.PortNumber(7777)
	redObjName := gwtypes.ObjectName("red-service")
	blueObjName := gwtypes.ObjectName("blue-service")
	cholponNamespace := gwtypes.Namespace("cholpon")
	serviceKind := gwtypes.Kind("Service")
	serviceGroup := gwtypes.Group("")

	fakestore, err := store.NewFakeStore(store.FakeObjects{
		ReferenceGrants: grants,
		// After https://github.com/Kong/kubernetes-ingress-controller/pull/5392
		// is merged the backendRef will be checked for existence in the store
		// so we need to add them here.
		Services: []*corev1.Service{
			{
				Name:      string(redObjName),
				Namespace: string(cholponNamespace),
			},
			{
				Name:      string(blueObjName),
				Namespace: string(cholponNamespace),
			},
			{
				Name:      string(redObjName),
				Namespace: "behbudiy",
			},
		},
	})
	require.NoError(t, err)
	p := mustNewTranslator(t, fakestore)

	tests := []struct {
		msg     string
		route   client.Object
		refs    []gwtypes.BackendRef
		result  kongstate.Service
		wantErr bool
	}{
		{
			msg: "all backends in route namespace",
			route: &gwtypes.HTTPRoute{
				// normally the k8s api call populates TypeMeta properly, but we have no such luxuries here
				Kind:       "HTTPRoute",
				APIVersion: "gateway.networking.k8s.io/v1alpha2",
				Name:       "tong-sirlari",
				Namespace:  "cholpon",
			},
			refs: []gwtypes.BackendRef{
				{
					Name:  blueObjName,
					Kind:  &serviceKind,
					Port:  &port,
					Group: &serviceGroup,
				},
				{
					Name:  redObjName,
					Kind:  &serviceKind,
					Port:  &port,
					Group: &serviceGroup,
				},
			},
			result: kongstate.Service{
				Service: kong.Service{
					Name:           new("httproute.cholpon.tong-sirlari.999"),
					Host:           new("httproute.cholpon.tong-sirlari.999"),
					Protocol:       new(protocol),
					ConnectTimeout: new(DefaultServiceTimeout),
					ReadTimeout:    new(DefaultServiceTimeout),
					WriteTimeout:   new(DefaultServiceTimeout),
					Retries:        new(DefaultRetries),
				},
				Namespace: "cholpon",
				Backends: []kongstate.ServiceBackend{
					builder.NewKongstateServiceBackend(string(blueObjName)).
						WithNamespace(string(cholponNamespace)).
						WithPortNumber(int(port)).
						MustBuild(),
					builder.NewKongstateServiceBackend(string(redObjName)).
						WithNamespace(string(cholponNamespace)).
						WithPortNumber(int(port)).
						MustBuild(),
				},
				Parent: &gwtypes.HTTPRoute{
					Name:       "tong-sirlari",
					Namespace:  "cholpon",
					Kind:       "HTTPRoute",
					APIVersion: "gateway.networking.k8s.io/v1alpha2",
				},
			},
			wantErr: false,
		},
		{
			msg: "same and different ns backend",
			route: &gwtypes.UDPRoute{
				Kind:       "UDPRoute",
				APIVersion: "gateway.networking.k8s.io/v1alpha2",
				Name:       "padarkush",
				Namespace:  "behbudiy",
			},
			refs: []gwtypes.BackendRef{
				{
					Name:      blueObjName,
					Port:      &port,
					Kind:      &serviceKind,
					Namespace: &cholponNamespace,
					Group:     &serviceGroup,
				},
				{
					Name:  redObjName,
					Port:  &port,
					Kind:  &serviceKind,
					Group: &serviceGroup,
				},
			},
			result: kongstate.Service{
				Service: kong.Service{
					Name:           new("udproute.behbudiy.padarkush.999"),
					Host:           new("udproute.behbudiy.padarkush.999"),
					Protocol:       new(protocol),
					ConnectTimeout: new(DefaultServiceTimeout),
					ReadTimeout:    new(DefaultServiceTimeout),
					WriteTimeout:   new(DefaultServiceTimeout),
					Retries:        new(DefaultRetries),
				},
				Namespace: "behbudiy",
				Backends: []kongstate.ServiceBackend{
					builder.NewKongstateServiceBackend(string(blueObjName)).
						WithNamespace(string(cholponNamespace)).
						WithPortNumber(int(port)).
						MustBuild(),
					builder.NewKongstateServiceBackend(string(redObjName)).
						WithNamespace("behbudiy").
						WithPortNumber(int(port)).
						MustBuild(),
				},
				Parent: &gwtypes.UDPRoute{
					Name:       "padarkush",
					Namespace:  "behbudiy",
					Kind:       "UDPRoute",
					APIVersion: "gateway.networking.k8s.io/v1alpha2",
				},
			},
			wantErr: false,
		},
		{
			msg: "only not permitted remote ns",
			route: &gwtypes.TCPRoute{
				Kind:       "TCPRoute",
				APIVersion: "gateway.networking.k8s.io/v1alpha2",
				Name:       "kitab-ul-atfol",
				Namespace:  "behbudiy",
			},
			refs: []gwtypes.BackendRef{
				{
					Name:      blueObjName,
					Port:      &port,
					Kind:      &serviceKind,
					Namespace: &cholponNamespace,
					Group:     &serviceGroup,
				},
			},
			result: kongstate.Service{
				Service: kong.Service{
					Name:           new("tcproute.behbudiy.kitab-ul-atfol.999"),
					Host:           new("tcproute.behbudiy.kitab-ul-atfol.999.invalid"),
					Protocol:       new(protocol),
					ConnectTimeout: new(DefaultServiceTimeout),
					ReadTimeout:    new(DefaultServiceTimeout),
					WriteTimeout:   new(DefaultServiceTimeout),
					Retries:        new(DefaultRetries),
				},
				Namespace: "behbudiy",
				Backends:  []kongstate.ServiceBackend{},
				Plugins: []kong.Plugin{
					{
						Name: new("request-termination"),
						Config: kong.Configuration{
							"status_code": 500,
							"message":     "no existing backendRef provided",
						},
					},
				},
				Parent: &gwtypes.TCPRoute{
					Name:       "kitab-ul-atfol",
					Namespace:  "behbudiy",
					Kind:       "TCPRoute",
					APIVersion: "gateway.networking.k8s.io/v1alpha2",
				},
			},
			wantErr: false,
		},
		{
			msg: "same and different ns backend",
			route: &gwtypes.TCPRoute{
				Kind:       "TCPRoute",
				APIVersion: "gateway.networking.k8s.io/v1alpha2",
				Name:       "muntaxabi-jugrofiyai-umumiy",
				Namespace:  "behbudiy",
			},
			refs: []gwtypes.BackendRef{
				{
					Name:      blueObjName,
					Port:      &port,
					Kind:      &serviceKind,
					Namespace: &cholponNamespace,
					Group:     &serviceGroup,
				},
				{
					Name:  redObjName,
					Port:  &port,
					Kind:  &serviceKind,
					Group: &serviceGroup,
				},
			},
			result: kongstate.Service{
				Service: kong.Service{
					Name:           new("tcproute.behbudiy.muntaxabi-jugrofiyai-umumiy.999"),
					Host:           new("tcproute.behbudiy.muntaxabi-jugrofiyai-umumiy.999"),
					Protocol:       new(protocol),
					ConnectTimeout: new(DefaultServiceTimeout),
					ReadTimeout:    new(DefaultServiceTimeout),
					WriteTimeout:   new(DefaultServiceTimeout),
					Retries:        new(DefaultRetries),
				},
				Namespace: "behbudiy",
				Backends: []kongstate.ServiceBackend{
					builder.NewKongstateServiceBackend(string(redObjName)).
						WithNamespace("behbudiy").
						WithPortNumber(int(port)).
						MustBuild(),
				},
				Parent: &gwtypes.TCPRoute{
					Name:       "muntaxabi-jugrofiyai-umumiy",
					Namespace:  "behbudiy",
					Kind:       "TCPRoute",
					APIVersion: "gateway.networking.k8s.io/v1alpha2",
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			result, err := generateKongServiceFromBackendRefWithRuleNumber(p.logger, p.storer, &rules, tt.route, ruleNumber, protocol, tt.refs...)
			assert.Equal(t, tt.result, result)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
