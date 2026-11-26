package translator

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestBackendRefsToKongStateBackends(t *testing.T) {
	testcases := []struct {
		name        string
		route       client.Object
		backendRefs []gwtypes.BackendRef
		allowed     map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo
		objects     store.FakeObjects
		expected    kongstate.ServiceBackends
	}{
		{
			name: "correct ReferenceGrant and an existing Service as backendRef returns a KongStateBackend with a Service",
			route: &gwtypes.HTTPRoute{
				Name:      "basic-httproute",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: commonRouteSpecMock("fake-gateway-1"),
					Hostnames: []gwtypes.Hostname{
						"konghq.com",
						"www.konghq.com",
					},
					Rules: []gwtypes.HTTPRouteRule{{
						// BackendRefs are taken from the backendRefs argument.
						// This is done this way because the backendRefs are
						// extracted from the route's spec before.
						// Potentially this could be refactored to be extracted
						// in backendRefsToKongStateBackends using a type switch.
					}},
				},
			},
			backendRefs: []gwtypes.BackendRef{
				builder.NewBackendRef("fake-service").WithPort(80).Build(),
			},
			allowed: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{
				gwtypes.Namespace(corev1.NamespaceDefault): {
					{
						Group: "",
						Kind:  "Service",
						Name:  new(gwtypes.ObjectName("fake-service")),
					},
				},
			},
			objects: store.FakeObjects{
				Services: []*corev1.Service{
					{
						Name:      "fake-service",
						Namespace: corev1.NamespaceDefault,
						Spec: corev1.ServiceSpec{
							Ports: []corev1.ServicePort{
								builder.NewServicePort().WithPort(80).Build(),
							},
						},
					},
				},
			},
			expected: func() kongstate.ServiceBackends {
				svcBackend, err := kongstate.NewServiceBackend(
					kongstate.ServiceBackendTypeKubernetesService,
					k8stypes.NamespacedName{Namespace: corev1.NamespaceDefault, Name: "fake-service"},
					kongstate.PortDef{
						Mode:   kongstate.PortModeByNumber,
						Number: 80,
					},
				)
				require.NoError(t, err)
				return kongstate.ServiceBackends{svcBackend}
			}(),
		},
		{
			name: "no ReferenceGrant and an existing Service as backendRef doesn't return a KongStateBackend with the Service",
			route: &gwtypes.HTTPRoute{
				Name:      "basic-httproute",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: commonRouteSpecMock("fake-gateway-1"),
					Hostnames: []gwtypes.Hostname{
						"konghq.com",
						"www.konghq.com",
					},
				},
			},
			allowed: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{
				gwtypes.Namespace(corev1.NamespaceDefault): {
					{
						Group: "",
						Kind:  "ImaginaryKind",
						Name:  new(gwtypes.ObjectName("fake-service")),
					},
				},
			},
			objects: store.FakeObjects{
				Services: []*corev1.Service{
					{
						Name:      "fake-service",
						Namespace: corev1.NamespaceDefault,
					},
				},
			},
			expected: kongstate.ServiceBackends{},
		},
		{
			name: "ReferenceGrant and a non existing Service as backendRef doesn't return a KongStateBackend with the Service",
			route: &gwtypes.HTTPRoute{
				Name:      "basic-httproute",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: commonRouteSpecMock("fake-gateway-1"),
					Hostnames: []gwtypes.Hostname{
						"konghq.com",
						"www.konghq.com",
					},
				},
			},
			backendRefs: []gwtypes.BackendRef{
				builder.NewBackendRef("fake-service").WithPort(80).Build(),
			},
			allowed: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{
				gwtypes.Namespace(corev1.NamespaceDefault): {
					{
						Group: "",
						Kind:  "Service",
						Name:  new(gwtypes.ObjectName("fake-service")),
					},
				},
			},
			expected: kongstate.ServiceBackends{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fakestore, err := store.NewFakeStore(tc.objects)
			require.NoError(t, err)
			logger := logr.Discard()
			ret := backendRefsToKongStateBackends(logger, fakestore, tc.route, tc.backendRefs, tc.allowed)
			require.Equal(t, tc.expected, ret)
		})
	}
}
