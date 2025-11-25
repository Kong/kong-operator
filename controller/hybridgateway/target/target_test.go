package target

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	_ "github.com/kong/kong-operator/controller/hybridgateway/builder" // Used by function under test.
	_ "github.com/kong/kong-operator/controller/hybridgateway/utils"   // Used by function under test.
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// Helper functions for creating test objects.
func createTestEndpointSliceList(items []discoveryv1.EndpointSlice) *discoveryv1.EndpointSliceList {
	return &discoveryv1.EndpointSliceList{
		Items: items,
	}
}

func createTestEndpointSlice(name string, ports []discoveryv1.EndpointPort, endpoints []discoveryv1.Endpoint) discoveryv1.EndpointSlice {
	return discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Ports:     ports,
		Endpoints: endpoints,
	}
}

func createTestEndpointPort(name string, port int32, protocol corev1.Protocol) discoveryv1.EndpointPort {
	return discoveryv1.EndpointPort{
		Name:     ptr.To(name),
		Port:     ptr.To(port),
		Protocol: ptr.To(protocol),
	}
}

func createTestEndpoint(addresses []string, ready bool) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses: addresses,
		Conditions: discoveryv1.EndpointConditions{
			Ready: ptr.To(ready),
		},
	}
}

func createTestServicePort() *corev1.ServicePort {
	return &corev1.ServicePort{
		Name:     "http",
		Protocol: corev1.ProtocolTCP,
	}
}

// Global helper to create HTTPRoute with optional BackendRefs.
func createGlobalTestHTTPRoute(name, namespace string, backendRefs ...[]gwtypes.HTTPBackendRef) *gwtypes.HTTPRoute {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if len(backendRefs) > 0 && backendRefs[0] != nil {
		route.Spec = gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{
				{
					BackendRefs: backendRefs[0],
				},
			},
		}
	}

	return route
}

// Global helper to create HTTPBackendRef - comprehensive version.
func createGlobalTestHTTPBackendRef(name, namespace string, weight, port *int32, group ...*gwtypes.Group) gwtypes.HTTPBackendRef {
	serviceKind := gwtypes.Kind("Service")
	ref := gwtypes.HTTPBackendRef{
		BackendRef: gwtypes.BackendRef{
			BackendObjectReference: gwtypes.BackendObjectReference{
				Name: gwtypes.ObjectName(name),
				Kind: &serviceKind,
			},
			Weight: weight,
		},
	}

	if namespace != "" {
		ns := gwtypes.Namespace(namespace)
		ref.Namespace = &ns
	}

	if port != nil {
		portNum := *port
		ref.Port = &portNum
	}

	if len(group) > 0 && group[0] != nil {
		ref.Group = group[0]
	}

	return ref
}

// createTestScheme creates a scheme for testing with all required types registered.
func createTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)
	_ = gatewayv1beta1.Install(scheme)
	_ = configurationv1alpha1.AddToScheme(scheme)
	return scheme
}

// createTestFakeClient creates a fake client with test scheme and objects.
func createTestFakeClient(objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(createTestScheme()).
		WithObjects(objects...).
		Build()
}

// createTestFakeClientWithInterceptors creates a fake client with test scheme, objects, and interceptors.
func createTestFakeClientWithInterceptors(interceptors interceptor.Funcs, objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(createTestScheme()).
		WithObjects(objects...).
		WithInterceptorFuncs(interceptors).
		Build()
}

// createTestService creates a test Service with specified parameters.
func createTestService(name, namespace string, serviceType corev1.ServiceType, clusterIP, externalName string, ports []corev1.ServicePort) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:  serviceType,
			Ports: ports,
		},
	}
	if clusterIP != "" {
		svc.Spec.ClusterIP = clusterIP
	}
	if externalName != "" {
		svc.Spec.ExternalName = externalName
	}
	return svc
}

// createTestvalidBackendRef creates a test validBackendRef for testing.
//
//nolint:unparam // False positive: namespace parameter receives multiple different values (default, frontend, backend, test-namespace)
func createTestvalidBackendRef(serviceName, namespace string, weight *int32, readyEndpoints []string) validBackendRef {
	serviceKind := gwtypes.Kind("Service")
	actualWeight := int32(100) // Default weight.
	if weight != nil {
		actualWeight = *weight
	}
	return validBackendRef{
		backendRef: &gwtypes.HTTPBackendRef{
			BackendRef: gwtypes.BackendRef{
				BackendObjectReference: gwtypes.BackendObjectReference{
					Name: gwtypes.ObjectName(serviceName),
					Kind: &serviceKind,
				},
				Weight: weight,
			},
		},
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: namespace,
			},
		},
		servicePort: &corev1.ServicePort{
			Name: "http",
			Port: 80,
		},
		readyEndpoints: readyEndpoints,
		targetPort:     8080,         // Default target port.
		weight:         actualWeight, // Use the provided weight or default.
	}
}

// TestFindBackendRefPortInService tests the findBackendRefPortInService function.
func TestFindBackendRefPortInService(t *testing.T) {
	// Helper function to create test HTTPBackendRef.
	createTestHTTPBackendRef := func(port *int32) *gwtypes.HTTPBackendRef {
		ref := &gwtypes.HTTPBackendRef{}
		if port != nil {
			ref.Port = port
		}
		return ref
	}

	// Helper function to create test Service for this specific test.
	createSvc := func(name, namespace string, ports []corev1.ServicePort) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       corev1.ServiceSpec{Ports: ports},
		}
	}

	tests := []struct {
		name          string
		backendRef    *gwtypes.HTTPBackendRef
		service       *corev1.Service
		expectedPort  *corev1.ServicePort
		expectedError string
	}{
		{
			name:       "Valid port found",
			backendRef: createTestHTTPBackendRef(ptr.To[int32](80)),
			service: createSvc("test-service", "default", []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			}),
			expectedPort: &corev1.ServicePort{
				Name:     "http",
				Port:     80,
				Protocol: corev1.ProtocolTCP,
			},
		},
		{
			name:          "Port not specified in BackendRef",
			backendRef:    createTestHTTPBackendRef(nil),
			service:       createSvc("test-service", "default", []corev1.ServicePort{}),
			expectedError: "port not specified in BackendRef",
		},
		{
			name:       "Port not found in service",
			backendRef: createTestHTTPBackendRef(ptr.To[int32](443)),
			service: createSvc("test-service", "default", []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			}),
			expectedError: "port 443 not found in service default/test-service",
		},
		{
			name:       "Multiple ports, correct one found",
			backendRef: createTestHTTPBackendRef(ptr.To[int32](443)),
			service: createSvc("web-service", "prod", []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "metrics",
					Port:     9090,
					Protocol: corev1.ProtocolTCP,
				},
			}),
			expectedPort: &corev1.ServicePort{
				Name:     "https",
				Port:     443,
				Protocol: corev1.ProtocolTCP,
			},
		},
		{
			name:          "Service with no ports",
			backendRef:    createTestHTTPBackendRef(ptr.To[int32](80)),
			service:       createSvc("empty-service", "default", []corev1.ServicePort{}),
			expectedError: "port 80 not found in service default/empty-service",
		},
		{
			name:       "Different port protocols should not matter for matching",
			backendRef: createTestHTTPBackendRef(ptr.To[int32](53)),
			service: createSvc("dns-service", "kube-system", []corev1.ServicePort{
				{
					Name:     "dns-tcp",
					Port:     53,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "dns-udp",
					Port:     53,
					Protocol: corev1.ProtocolUDP,
				},
			}),
			// Should return the first match (TCP one).
			expectedPort: &corev1.ServicePort{
				Name:     "dns-tcp",
				Port:     53,
				Protocol: corev1.ProtocolTCP,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := findBackendRefPortInService(tt.backendRef, tt.service)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, port)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, port)
				assert.Equal(t, tt.expectedPort.Name, port.Name)
				assert.Equal(t, tt.expectedPort.Port, port.Port)
				assert.Equal(t, tt.expectedPort.Protocol, port.Protocol)
			}
		})
	}
}

// TestExtractReadyEndpointAddresses tests the extractReadyEndpointAddresses function.
func TestExtractReadyEndpointAddresses(t *testing.T) {
	tests := []struct {
		name              string
		endpointSlices    *discoveryv1.EndpointSliceList
		servicePort       *corev1.ServicePort
		expectedAddresses []string
	}{
		{
			name: "Extract ready endpoints with matching port",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolTCP),
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1", "10.0.1.2"}, true),
						createTestEndpoint([]string{"10.0.1.3"}, true),
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: []string{"10.0.1.1", "10.0.1.2", "10.0.1.3"},
		},
		{
			name: "Skip not ready endpoints",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolTCP),
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1"}, true),  // Ready.
						createTestEndpoint([]string{"10.0.1.2"}, false), // Not ready.
						createTestEndpoint([]string{"10.0.1.3"}, true),  // Ready.
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: []string{"10.0.1.1", "10.0.1.3"},
		},
		{
			name: "Skip endpoints with nil Ready condition",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolTCP),
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1"}, true), // Ready.
						{
							Addresses: []string{"10.0.1.2"},
							Conditions: discoveryv1.EndpointConditions{
								Ready: nil, // Nil ready condition should be skipped.
							},
						},
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: []string{"10.0.1.1"},
		},
		{
			name: "Skip ports with different name",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("https", 8443, corev1.ProtocolTCP), // Different name.
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1"}, true),
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: nil,
		},
		{
			name: "Skip ports with different protocol",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolUDP), // Different protocol.
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1"}, true),
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: nil,
		},
		{
			name: "Skip ports with nil port",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						{
							Name:     ptr.To("http"),
							Port:     nil, // Nil port should be skipped.
							Protocol: ptr.To(corev1.ProtocolTCP),
						},
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1"}, true),
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: nil,
		},
		{
			name: "Skip ports with negative port number",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", -1, corev1.ProtocolTCP), // Negative port.
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1"}, true),
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: nil,
		},
		{
			name: "Multiple endpoint slices with matching ports",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolTCP),
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.1"}, true),
					},
				),
				createTestEndpointSlice("test-slice-2",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolTCP),
					},
					[]discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.1.2", "10.0.1.3"}, true),
					},
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: []string{"10.0.1.1", "10.0.1.2", "10.0.1.3"},
		},
		{
			name:              "Empty endpoint slices",
			endpointSlices:    createTestEndpointSliceList([]discoveryv1.EndpointSlice{}),
			servicePort:       createTestServicePort(),
			expectedAddresses: nil,
		},
		{
			name: "Endpoint slice with no endpoints",
			endpointSlices: createTestEndpointSliceList([]discoveryv1.EndpointSlice{
				createTestEndpointSlice("test-slice-1",
					[]discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolTCP),
					},
					[]discoveryv1.Endpoint{}, // No endpoints.
				),
			}),
			servicePort:       createTestServicePort(),
			expectedAddresses: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addresses := extractReadyEndpointAddresses(tt.endpointSlices, tt.servicePort)
			assert.Equal(t, tt.expectedAddresses, addresses)
		})
	}
}

// TestResolveFQDNEndpoints tests the resolveFQDNEndpoints function.
func TestResolveFQDNEndpoints(t *testing.T) {
	tests := []struct {
		name          string
		service       *corev1.Service
		clusterDomain string
		expected      []string
	}{
		{
			name: "Default cluster domain (empty) uses short form",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service",
					Namespace: "default",
				},
			},
			clusterDomain: "",
			expected:      []string{"my-service.default.svc"},
		},
		{
			name: "Custom cluster domain uses full FQDN",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service",
					Namespace: "default",
				},
			},
			clusterDomain: "cluster.local",
			expected:      []string{"my-service.default.svc.cluster.local"},
		},
		{
			name: "Service with different namespace and custom domain",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "api-service",
					Namespace: "backend",
				},
			},
			clusterDomain: "my-cluster.local",
			expected:      []string{"api-service.backend.svc.my-cluster.local"},
		},
		{
			name: "Service with hyphenated names and empty domain",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-frontend-service",
					Namespace: "production-ns",
				},
			},
			clusterDomain: "",
			expected:      []string{"web-frontend-service.production-ns.svc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveFQDNEndpoints(tt.service, tt.clusterDomain)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveExternalNameEndpoints tests the resolveExternalNameEndpoints function.
func TestResolveExternalNameEndpoints(t *testing.T) {
	logger := ctrllog.Log.WithName("test")

	tests := []struct {
		name               string
		service            *corev1.Service
		expectedEndpoints  []string
		expectedShouldSkip bool
		expectedError      error
	}{
		{
			name: "ExternalName service with valid external name",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "external-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "external.example.com",
				},
			},
			expectedEndpoints:  []string{"external.example.com"},
			expectedShouldSkip: false,
			expectedError:      nil,
		},
		{
			name: "ExternalName service with empty external name should be skipped",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "external-service-empty",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "",
				},
			},
			expectedEndpoints:  nil,
			expectedShouldSkip: true,
			expectedError:      nil,
		},
		{
			name: "ExternalName service with FQDN external name",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "database-service",
					Namespace: "production",
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "database.prod.example.com",
				},
			},
			expectedEndpoints:  []string{"database.prod.example.com"},
			expectedShouldSkip: false,
			expectedError:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoints, shouldSkip, err := resolveExternalNameEndpoints(logger, tt.service)

			assert.Equal(t, tt.expectedEndpoints, endpoints)
			assert.Equal(t, tt.expectedShouldSkip, shouldSkip)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}

// TestResolveTargetPort tests the resolveTargetPort function.
func TestResolveTargetPort(t *testing.T) {
	tests := []struct {
		name                 string
		service              *corev1.Service
		servicePort          *corev1.ServicePort
		fqdn                 bool
		expectedPort         int
		existingSlices       []discoveryv1.EndpointSlice
		interceptorFuncs     *interceptor.Funcs
		expectError          bool
		expectedErrorMessage string
	}{
		{
			name: "FQDN mode with regular service should use service port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1", // Non-headless
				},
			},
			servicePort: &corev1.ServicePort{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8080,
				},
			},
			fqdn:         true,
			expectedPort: 80,
		},
		{
			name: "FQDN mode with headless service should use target port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP: "None", // Headless
				},
			},
			servicePort: &corev1.ServicePort{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8080,
				},
			},
			fqdn:         true,
			expectedPort: 8080,
		},
		{
			name: "ExternalName service should use service port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "external.example.com",
				},
			},
			servicePort: &corev1.ServicePort{
				Port: 443,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8443,
				},
			},
			fqdn:         false,
			expectedPort: 443,
		},
		{
			name: "Regular service without FQDN should use target port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
				},
			},
			servicePort: &corev1.ServicePort{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 3000,
				},
			},
			fqdn:         false,
			expectedPort: 3000,
		},
		{
			name: "Service without target port specified should use service port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
				},
			},
			servicePort: &corev1.ServicePort{
				Port:       8080,
				TargetPort: intstr.IntOrString{}, // No target port specified
			},
			fqdn:         false,
			expectedPort: 8080,
		},
		{
			name: "Regular service with named targetPort resolved from EndpointSlice",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "named-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
				},
			},
			servicePort: &corev1.ServicePort{
				Name: "http",
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "http",
				},
				Protocol: corev1.ProtocolTCP,
			},
			fqdn:         false,
			expectedPort: 8080,
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "named-svc-slice",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "named-svc",
						},
					},
					Ports: []discoveryv1.EndpointPort{
						createTestEndpointPort("http", 8080, corev1.ProtocolTCP),
					},
				},
			},
		},
		{
			name: "EndpointSlice List error propagates in resolveTargetPort",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "err-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
				},
			},
			servicePort: &corev1.ServicePort{
				Name: "http",
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "http",
				},
				Protocol: corev1.ProtocolTCP,
			},
			fqdn: false,
			// Provide an interceptor that causes List to fail so we hit the error branch.
			interceptorFuncs: &interceptor.Funcs{
				List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return fmt.Errorf("simulated list failure")
				},
			},
			expectError:          true,
			expectedErrorMessage: "error fetching EndpointSlices for service default/err-svc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Provide a context and a fake client for functions that may query EndpointSlices.
			ctx := context.Background()

			// Build objects list for the fake client.
			var objects []client.Object
			for i := range tt.existingSlices {
				objects = append(objects, &tt.existingSlices[i])
			}

			var cl client.Client
			if tt.interceptorFuncs != nil {
				cl = fake.NewClientBuilder().WithScheme(createTestScheme()).WithObjects(objects...).WithInterceptorFuncs(*tt.interceptorFuncs).Build()
			} else {
				cl = fake.NewClientBuilder().WithScheme(createTestScheme()).WithObjects(objects...).Build()
			}

			result, err := resolveTargetPort(ctx, cl, tt.service, tt.servicePort, tt.fqdn)
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedErrorMessage != "" {
					assert.Contains(t, err.Error(), tt.expectedErrorMessage)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPort, result)
		})
	}
}

// TestResolveEndpointSliceEndpoints tests the resolveEndpointSliceEndpoints function.
func TestResolveEndpointSliceEndpoints(t *testing.T) {
	ctx := context.Background()
	logger := ctrllog.Log.WithName("test")

	tests := []struct {
		name               string
		service            *corev1.Service
		servicePort        *corev1.ServicePort
		existingSlices     []discoveryv1.EndpointSlice
		mockError          error
		expectedEndpoints  []string
		expectedShouldSkip bool
		expectedError      bool
		expectedErrorMsg   string
	}{
		{
			name: "Service with ready endpoints should return endpoints",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
			},
			servicePort: createTestServicePort(),
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "slice-1",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "test-service",
						},
					},
					Ports: []discoveryv1.EndpointPort{
						createTestEndpointPort("http", 80, corev1.ProtocolTCP),
					},
					Endpoints: []discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.0.0.1"}, true),
						createTestEndpoint([]string{"10.0.0.2"}, true),
					},
				},
			},
			expectedEndpoints:  []string{"10.0.0.1", "10.0.0.2"},
			expectedShouldSkip: false,
			expectedError:      false,
		},
		{
			name: "Service with no ready endpoints should be skipped",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-service",
					Namespace: "default",
				},
			},
			servicePort:        createTestServicePort(),
			existingSlices:     []discoveryv1.EndpointSlice{},
			expectedEndpoints:  nil,
			expectedShouldSkip: true,
			expectedError:      false,
		},
		{
			name: "EndpointSlices not found should be skipped",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "notfound-service",
					Namespace: "default",
				},
			},
			servicePort:        createTestServicePort(),
			mockError:          k8serrors.NewNotFound(discoveryv1.Resource("endpointslices"), "notfound-service"),
			expectedEndpoints:  nil,
			expectedShouldSkip: true,
			expectedError:      false,
		},
		{
			name: "Network error should return error",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "error-service",
					Namespace: "default",
				},
			},
			servicePort:        createTestServicePort(),
			mockError:          fmt.Errorf("network timeout"),
			expectedEndpoints:  nil,
			expectedShouldSkip: false,
			expectedError:      true,
			expectedErrorMsg:   "error fetching EndpointSlices for service default/error-service:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := createTestScheme()
			var cl client.Client

			if tt.mockError != nil {
				// Create a client that returns the mock error.
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithInterceptorFuncs(interceptor.Funcs{
						List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							return tt.mockError
						},
					}).
					Build()
			} else {
				// Create a normal fake client with existing slices.
				objects := make([]client.Object, len(tt.existingSlices))
				for i, slice := range tt.existingSlices {
					objects[i] = &slice
				}
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(objects...).
					Build()
			}

			endpoints, shouldSkip, err := resolveEndpointSliceEndpoints(ctx, logger, cl, tt.service, tt.servicePort)

			assert.Equal(t, tt.expectedEndpoints, endpoints)
			assert.Equal(t, tt.expectedShouldSkip, shouldSkip)

			if tt.expectedError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestResolveServiceEndpoints tests the resolveServiceEndpoints function.
func TestResolveServiceEndpoints(t *testing.T) {
	ctx := context.Background()
	logger := ctrllog.Log.WithName("test")

	tests := []struct {
		name               string
		service            *corev1.Service
		servicePort        *corev1.ServicePort
		fqdn               bool
		clusterDomain      string
		existingSlices     []discoveryv1.EndpointSlice
		expectedEndpoints  []string
		expectedShouldSkip bool
		expectedError      bool
	}{
		{
			name: "FQDN mode with regular service should use FQDN",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1", // Non-headless
				},
			},
			servicePort:        createTestServicePort(),
			fqdn:               true,
			expectedEndpoints:  []string{"web-service.default.svc"},
			expectedShouldSkip: false,
			expectedError:      false,
		},
		{
			name: "ExternalName service should use external name",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "external-db",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "database.example.com",
				},
			},
			servicePort:        createTestServicePort(),
			fqdn:               false,
			expectedEndpoints:  []string{"database.example.com"},
			expectedShouldSkip: false,
			expectedError:      false,
		},
		{
			name: "ExternalName service with empty external name should be skipped",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-external",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "",
				},
			},
			servicePort:        createTestServicePort(),
			fqdn:               false,
			expectedEndpoints:  nil,
			expectedShouldSkip: true,
			expectedError:      false,
		},
		{
			name: "Regular service without FQDN should use EndpointSlices",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backend-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
				},
			},
			servicePort: createTestServicePort(),
			fqdn:        false,
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-slice",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "backend-service",
						},
					},
					Ports: []discoveryv1.EndpointPort{
						createTestEndpointPort("http", 80, corev1.ProtocolTCP),
					},
					Endpoints: []discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.1.0.1"}, true),
						createTestEndpoint([]string{"10.1.0.2"}, true),
					},
				},
			},
			expectedEndpoints:  []string{"10.1.0.1", "10.1.0.2"},
			expectedShouldSkip: false,
			expectedError:      false,
		},
		{
			name: "Headless service with FQDN should still use EndpointSlices",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "headless-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "None", // Headless
				},
			},
			servicePort: createTestServicePort(),
			fqdn:        true,
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "headless-slice",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "headless-service",
						},
					},
					Ports: []discoveryv1.EndpointPort{
						createTestEndpointPort("http", 80, corev1.ProtocolTCP),
					},
					Endpoints: []discoveryv1.Endpoint{
						createTestEndpoint([]string{"10.2.0.1"}, true),
					},
				},
			},
			expectedEndpoints:  []string{"10.2.0.1"},
			expectedShouldSkip: false,
			expectedError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := createTestScheme()

			// Create objects for the fake client.
			objects := make([]client.Object, len(tt.existingSlices))
			for i, slice := range tt.existingSlices {
				objects[i] = &slice
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			endpoints, shouldSkip, err := resolveServiceEndpoints(ctx, logger, cl, tt.service, tt.servicePort, tt.fqdn, tt.clusterDomain)

			assert.Equal(t, tt.expectedEndpoints, endpoints)
			assert.Equal(t, tt.expectedShouldSkip, shouldSkip)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetEndpointSlicesForService tests the getEndpointSlicesForService function.
func TestGetEndpointSlicesForService(t *testing.T) {
	tests := []struct {
		name                string
		service             *corev1.Service
		existingSlices      []discoveryv1.EndpointSlice
		expectError         bool
		expectedErrorString string
		expectedSliceNames  []string
	}{
		{
			name: "Service with matching endpoint slices",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
			},
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-slice-1",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "test-service",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-slice-2",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "test-service",
						},
					},
				},
			},
			expectedSliceNames: []string{"test-service-slice-1", "test-service-slice-2"},
		},
		{
			name: "Service with no endpoint slices",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-service",
					Namespace: "test-ns",
				},
			},
			existingSlices:     []discoveryv1.EndpointSlice{},
			expectedSliceNames: []string{},
		},
		{
			name: "Service with slices in different namespace should not match",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cross-ns-service",
					Namespace: "namespace-a",
				},
			},
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cross-ns-service-slice",
						Namespace: "namespace-b", // Different namespace.
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "cross-ns-service",
						},
					},
				},
			},
			expectedSliceNames: []string{},
		},
		{
			name: "Service with slices with different service name should not match",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-a",
					Namespace: "default",
				},
			},
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-b-slice",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "service-b", // Different service name.
						},
					},
				},
			},
			expectedSliceNames: []string{},
		},
		{
			name: "Service with mixed matching and non-matching slices",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mixed-service",
					Namespace: "prod",
				},
			},
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mixed-service-slice-1",
						Namespace: "prod",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "mixed-service", // Matches.
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-service-slice",
						Namespace: "prod",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "other-service", // Doesn't match.
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mixed-service-slice-2",
						Namespace: "prod",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "mixed-service", // Matches.
						},
					},
				},
			},
			expectedSliceNames: []string{"mixed-service-slice-1", "mixed-service-slice-2"},
		},
		{
			name: "Service with slice missing service name label",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "label-test-service",
					Namespace: "default",
				},
			},
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "label-test-slice-1",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "label-test-service", // Has label.
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "label-test-slice-2",
						Namespace: "default",
						Labels:    map[string]string{}, // Missing service name label.
					},
				},
			},
			expectedSliceNames: []string{"label-test-slice-1"},
		},
		{
			name: "Service with empty name",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "default",
				},
			},
			existingSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "empty-name-slice",
						Namespace: "default",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "", // Empty service name.
						},
					},
				},
			},
			expectedSliceNames: []string{"empty-name-slice"},
		},
		{
			name: "Client List operation error should be handled",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "error-test-service",
					Namespace: "default",
				},
			},
			existingSlices:      []discoveryv1.EndpointSlice{},
			expectError:         true,
			expectedErrorString: "failed to list endpointslices for service default/error-test-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create objects for the fake client.
			var objects []client.Object
			for i := range tt.existingSlices {
				objects = append(objects, &tt.existingSlices[i])
			}

			var fakeClient client.Client
			// Add interceptor for client error test case.
			if tt.expectedErrorString == "failed to list endpointslices for service default/error-test-service" {
				fakeClient = createTestFakeClientWithInterceptors(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						return fmt.Errorf("simulated client list error")
					},
				}, objects...)
			} else {
				fakeClient = createTestFakeClient(objects...)
			}

			// Call the function.
			ctx := context.Background()
			result, err := getEndpointSlicesForService(ctx, fakeClient, tt.service)

			// Verify error expectations.
			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedErrorString != "" {
					assert.Contains(t, err.Error(), tt.expectedErrorString)
				}
				return
			}

			// Verify success case.
			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Len(t, result.Items, len(tt.expectedSliceNames))

			// Verify returned slice names.
			actualNames := make([]string, len(result.Items))
			for i, slice := range result.Items {
				actualNames[i] = slice.Name
			}
			assert.ElementsMatch(t, tt.expectedSliceNames, actualNames)

			// Verify that all returned slices have the correct service label.
			for _, slice := range result.Items {
				assert.Equal(t, tt.service.Name, slice.Labels[discoveryv1.LabelServiceName])
				assert.Equal(t, tt.service.Namespace, slice.Namespace)
			}
		})
	}
}

// TestFiltervalidBackendRefs tests the filterValidBackendRefs function.
func TestFiltervalidBackendRefs(t *testing.T) {
	// Create logger for testing.
	logger := ctrllog.Log.WithName("test")

	// Use global helpers instead of local duplicates.
	createTestHTTPRoute := createGlobalTestHTTPRoute
	createTestHTTPBackendRef := func(name string, namespace *string, port *int32) gwtypes.HTTPBackendRef {
		ns := ""
		if namespace != nil {
			ns = *namespace
		}
		return createGlobalTestHTTPBackendRef(name, ns, nil, port)
	}

	tests := []struct {
		name                   string
		httpRoute              *gwtypes.HTTPRoute
		backendRefs            []gwtypes.HTTPBackendRef
		referenceGrantEnabled  bool
		fqdn                   bool
		existingServices       []corev1.Service
		existingEndpointSlices []discoveryv1.EndpointSlice
		expectError            bool
		expectedErrorString    string
		expectedValidCount     int
		validateResults        func(t *testing.T, results []validBackendRef)
	}{
		{
			name:      "Valid backend ref with regular service",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", nil, ptr.To[int32](80)),
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices: []corev1.Service{
				*createTestService("test-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
					{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
				}),
			},
			existingEndpointSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-slice",
						Namespace: "default",
						Labels:    map[string]string{discoveryv1.LabelServiceName: "test-service"},
					},
					Ports: []discoveryv1.EndpointPort{
						{Name: ptr.To("http"), Port: ptr.To[int32](8080), Protocol: ptr.To(corev1.ProtocolTCP)},
					},
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses:  []string{"10.0.1.1"},
							Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
						},
					},
				},
			},
			expectedValidCount: 1,
			validateResults: func(t *testing.T, results []validBackendRef) {
				require.Len(t, results, 1)
				assert.Equal(t, "test-service", string(results[0].backendRef.Name))
				assert.Equal(t, []string{"10.0.1.1"}, results[0].readyEndpoints)
				assert.Equal(t, 8080, results[0].targetPort) // Should use target port for direct endpoint access.
			},
		},
		{
			name:      "FQDN mode with regular service",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", nil, ptr.To[int32](80)),
			},
			referenceGrantEnabled: false,
			fqdn:                  true,
			existingServices: []corev1.Service{
				*createTestService("test-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
					{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				}),
			},
			expectedValidCount: 1,
			validateResults: func(t *testing.T, results []validBackendRef) {
				require.Len(t, results, 1)
				assert.Equal(t, []string{"test-service.default.svc.cluster.local"}, results[0].readyEndpoints)
				assert.Equal(t, 80, results[0].targetPort) // Should use service port for FQDN mode.
			},
		},
		{
			name:      "ExternalName service",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("external-service", nil, ptr.To[int32](443)),
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices: []corev1.Service{
				*createTestService("external-service", "default", corev1.ServiceTypeExternalName, "", "external.example.com", []corev1.ServicePort{
					{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
				}),
			},
			expectedValidCount: 1,
			validateResults: func(t *testing.T, results []validBackendRef) {
				require.Len(t, results, 1)
				assert.Equal(t, []string{"external.example.com"}, results[0].readyEndpoints)
				assert.Equal(t, 443, results[0].targetPort) // Should use service port for ExternalName.
			},
		},
		{
			name:      "Headless service",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("headless-service", nil, ptr.To[int32](80)),
			},
			referenceGrantEnabled: false,
			fqdn:                  true, // Even with FQDN, headless services should use endpoints.
			existingServices: []corev1.Service{
				*createTestService("headless-service", "default", corev1.ServiceTypeClusterIP, "None", "", []corev1.ServicePort{
					{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
				}),
			},
			existingEndpointSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "headless-service-slice",
						Namespace: "default",
						Labels:    map[string]string{discoveryv1.LabelServiceName: "headless-service"},
					},
					Ports: []discoveryv1.EndpointPort{
						{Name: ptr.To("http"), Port: ptr.To[int32](8080), Protocol: ptr.To(corev1.ProtocolTCP)},
					},
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses:  []string{"10.0.1.1", "10.0.1.2"},
							Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
						},
					},
				},
			},
			expectedValidCount: 1,
			validateResults: func(t *testing.T, results []validBackendRef) {
				require.Len(t, results, 1)
				assert.Equal(t, []string{"10.0.1.1", "10.0.1.2"}, results[0].readyEndpoints)
				assert.Equal(t, 8080, results[0].targetPort) // Should use target port for headless service.
			},
		},
		{
			name:      "Service not found should be skipped",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("missing-service", nil, ptr.To[int32](80)),
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices:      []corev1.Service{}, // No services.
			expectedValidCount:    0,
		},
		{
			name:      "Invalid port should be skipped",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", nil, ptr.To[int32](9999)), // Port not in service.
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices: []corev1.Service{
				*createTestService("test-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
					{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				}),
			},
			expectedValidCount: 0,
		},
		{
			name:      "Service with no ready endpoints should be skipped",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("no-endpoints-service", nil, ptr.To[int32](80)),
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices: []corev1.Service{
				*createTestService("no-endpoints-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
					{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				}),
			},
			existingEndpointSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-endpoints-service-slice",
						Namespace: "default",
						Labels:    map[string]string{discoveryv1.LabelServiceName: "no-endpoints-service"},
					},
					Ports: []discoveryv1.EndpointPort{
						{Name: ptr.To("http"), Port: ptr.To[int32](8080), Protocol: ptr.To(corev1.ProtocolTCP)},
					},
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses:  []string{"10.0.1.1"},
							Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(false)}, // Not ready.
						},
					},
				},
			},
			expectedValidCount: 0,
		},
		{
			name:      "ExternalName service with empty externalName should be skipped",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("empty-external-service", nil, ptr.To[int32](443)),
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices: []corev1.Service{
				*createTestService("empty-external-service", "default", corev1.ServiceTypeExternalName, "", "", []corev1.ServicePort{
					{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
				}),
			},
			expectedValidCount: 0,
		},
		{
			name:      "Multiple backend refs with mixed validity",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("valid-service", nil, ptr.To[int32](80)),
				createTestHTTPBackendRef("missing-service", nil, ptr.To[int32](80)),
				createTestHTTPBackendRef("valid-service", nil, ptr.To[int32](443)), // Invalid port.
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices: []corev1.Service{
				*createTestService("valid-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
					{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
				}),
			},
			existingEndpointSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-service-slice",
						Namespace: "default",
						Labels:    map[string]string{discoveryv1.LabelServiceName: "valid-service"},
					},
					Ports: []discoveryv1.EndpointPort{
						{Name: ptr.To("http"), Port: ptr.To[int32](8080), Protocol: ptr.To(corev1.ProtocolTCP)},
					},
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses:  []string{"10.0.1.1"},
							Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
						},
					},
				},
			},
			expectedValidCount: 1, // Only the first backend ref should be valid.
			validateResults: func(t *testing.T, results []validBackendRef) {
				require.Len(t, results, 1)
				assert.Equal(t, "valid-service", string(results[0].backendRef.Name))
			},
		},
		{
			name:      "Backend ref without port should be skipped",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", nil, nil), // No port specified.
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices: []corev1.Service{
				*createTestService("test-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
					{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				}),
			},
			expectedValidCount: 0,
		},
		{
			name:      "Unsupported backend ref kind should be skipped",
			httpRoute: createTestHTTPRoute("test-route", "default"),
			backendRefs: []gwtypes.HTTPBackendRef{
				func() gwtypes.HTTPBackendRef {
					unsupportedKind := gwtypes.Kind("ConfigMap")
					ref := createTestHTTPBackendRef("test-configmap", nil, ptr.To[int32](80))
					ref.Kind = &unsupportedKind
					return ref
				}(),
			},
			referenceGrantEnabled: false,
			fqdn:                  false,
			existingServices:      []corev1.Service{}, // No services needed since it should be skipped.
			expectedValidCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create objects for the fake client.
			var objects []client.Object
			for i := range tt.existingServices {
				objects = append(objects, &tt.existingServices[i])
			}
			for i := range tt.existingEndpointSlices {
				objects = append(objects, &tt.existingEndpointSlices[i])
			}

			// Create fake client.
			fakeClient := createTestFakeClient(objects...)

			// Call the function.
			ctx := context.Background()
			results, err := filterValidBackendRefs(ctx, logger, fakeClient, tt.httpRoute, tt.backendRefs, tt.referenceGrantEnabled, tt.fqdn, "cluster.local") // Verify error expectations.
			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedErrorString != "" {
					assert.Contains(t, err.Error(), tt.expectedErrorString)
				}
				return
			}

			// Verify success case.
			require.NoError(t, err)
			assert.Len(t, results, tt.expectedValidCount)

			// Run custom validation if provided.
			if tt.validateResults != nil {
				tt.validateResults(t, results)
			}
		})
	}

	// Additional test for EndpointSlices error handling.
	t.Run("EndpointSlices fetch error should be returned", func(t *testing.T) {
		existingServices := []corev1.Service{
			*createTestService("test-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
			}),
		}

		// Create objects for the fake client.
		var objects []client.Object
		for i := range existingServices {
			objects = append(objects, &existingServices[i])
		}

		// Create fake client with interceptor that simulates network error.
		interceptorFunc := interceptor.Funcs{
			List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*discoveryv1.EndpointSliceList); ok {
					return fmt.Errorf("simulated network error")
				}
				return client.List(ctx, list, opts...)
			},
		}

		fakeClient := createTestFakeClientWithInterceptors(interceptorFunc, objects...)

		httpRoute := createTestHTTPRoute("test-route", "default")
		backendRefs := []gwtypes.HTTPBackendRef{
			createTestHTTPBackendRef("test-service", nil, ptr.To[int32](80)),
		}

		// Call the function.
		ctx := context.Background()
		_, err := filterValidBackendRefs(ctx, logger, fakeClient, httpRoute, backendRefs, false, false, "cluster.local")

		// Verify that the error is returned.
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "error fetching EndpointSlices for service default/test-service")
		assert.Contains(t, err.Error(), "simulated network error")
	})

	// Additional test for cross-namespace backend ref (namespace determination logic).
	t.Run("Cross-namespace backend ref without ReferenceGrant should be skipped", func(t *testing.T) {
		existingServices := []corev1.Service{
			*createTestService("cross-ns-service", "other-namespace", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
			}),
		}

		// Create objects for the fake client.
		var objects []client.Object
		for i := range existingServices {
			objects = append(objects, &existingServices[i])
		}

		fakeClient := createTestFakeClient(objects...)

		httpRoute := createTestHTTPRoute("test-route", "default")
		backendRefs := []gwtypes.HTTPBackendRef{
			createTestHTTPBackendRef("cross-ns-service", ptr.To("other-namespace"), ptr.To[int32](80)),
		}

		// Call the function with ReferenceGrant disabled.
		ctx := context.Background()
		results, err := filterValidBackendRefs(ctx, logger, fakeClient, httpRoute, backendRefs, false, false, "cluster.local") // Should succeed but return no valid backend refs since ReferenceGrant is disabled.
		require.NoError(t, err)
		assert.Len(t, results, 0) // Cross-namespace access blocked without ReferenceGrant.
	})

	// Test ReferenceGrant scenarios.
	t.Run("ReferenceGrant enabled scenarios", func(t *testing.T) {
		existingServices := []corev1.Service{
			*createTestService("cross-ns-service", "other-namespace", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
			}),
		}

		existingEndpointSlices := []discoveryv1.EndpointSlice{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cross-ns-service-slice",
					Namespace: "other-namespace",
					Labels:    map[string]string{discoveryv1.LabelServiceName: "cross-ns-service"},
				},
				Ports: []discoveryv1.EndpointPort{
					{Name: ptr.To("http"), Port: ptr.To[int32](8080), Protocol: ptr.To(corev1.ProtocolTCP)},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses:  []string{"10.0.1.1"},
						Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
					},
				},
			},
		}

		// Test case 1: No ReferenceGrant exists - should be blocked.
		t.Run("No ReferenceGrant exists", func(t *testing.T) {
			// Create objects for the fake client (no ReferenceGrant).
			var objects []client.Object
			for i := range existingServices {
				objects = append(objects, &existingServices[i])
			}
			for i := range existingEndpointSlices {
				objects = append(objects, &existingEndpointSlices[i])
			}

			fakeClient := createTestFakeClient(objects...)

			httpRoute := createTestHTTPRoute("test-route", "default")
			backendRefs := []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("cross-ns-service", ptr.To("other-namespace"), ptr.To[int32](80)),
			}

			// Call the function with ReferenceGrant enabled.
			ctx := context.Background()
			results, err := filterValidBackendRefs(ctx, logger, fakeClient, httpRoute, backendRefs, true, false, "cluster.local")

			// Should succeed but return no valid backend refs since no ReferenceGrant exists.
			require.NoError(t, err)
			assert.Len(t, results, 0) // Cross-namespace access blocked without ReferenceGrant.
		})

		// Test case 2: ReferenceGrant exists but doesn't permit - should be blocked.
		t.Run("ReferenceGrant exists but doesn't permit", func(t *testing.T) {
			// Create a ReferenceGrant that doesn't permit the reference.
			nonPermittingGrant := &gatewayv1beta1.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-permitting-grant",
					Namespace: "other-namespace",
				},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{
						{
							Group:     gwtypes.GroupName,
							Kind:      "HTTPRoute",
							Namespace: "wrong-namespace", // Wrong source namespace.
						},
					},
					To: []gatewayv1beta1.ReferenceGrantTo{
						{
							Group: "",
							Kind:  "Service",
						},
					},
				},
			}

			// Create objects for the fake client.
			var objects []client.Object
			for i := range existingServices {
				objects = append(objects, &existingServices[i])
			}
			for i := range existingEndpointSlices {
				objects = append(objects, &existingEndpointSlices[i])
			}
			objects = append(objects, nonPermittingGrant)

			fakeClient := createTestFakeClient(objects...)

			httpRoute := createTestHTTPRoute("test-route", "default")
			backendRefs := []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("cross-ns-service", ptr.To("other-namespace"), ptr.To[int32](80)),
			}

			// Call the function with ReferenceGrant enabled.
			ctx := context.Background()
			results, err := filterValidBackendRefs(ctx, logger, fakeClient, httpRoute, backendRefs, true, false, "cluster.local")

			// Should succeed but return no valid backend refs since ReferenceGrant doesn't permit.
			require.NoError(t, err)
			assert.Len(t, results, 0) // Cross-namespace access blocked by non-permitting ReferenceGrant.
		})

		// Test case 3: Error in CheckReferenceGrant - should return error.
		t.Run("Error in CheckReferenceGrant", func(t *testing.T) {
			// Create fake client with interceptor that simulates ReferenceGrant list error.
			interceptorFunc := interceptor.Funcs{
				List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*gatewayv1beta1.ReferenceGrantList); ok {
						return fmt.Errorf("simulated ReferenceGrant list error")
					}
					return client.List(ctx, list, opts...)
				},
			}

			// Create objects for the fake client.
			var objects []client.Object
			for i := range existingServices {
				objects = append(objects, &existingServices[i])
			}

			fakeClient := createTestFakeClientWithInterceptors(interceptorFunc, objects...)

			httpRoute := createTestHTTPRoute("test-route", "default")
			backendRefs := []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("cross-ns-service", ptr.To("other-namespace"), ptr.To[int32](80)),
			}

			// Call the function with ReferenceGrant enabled.
			ctx := context.Background()
			_, err := filterValidBackendRefs(ctx, logger, fakeClient, httpRoute, backendRefs, true, false, "cluster.local")

			// Should return an error.
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "error checking ReferenceGrant for BackendRef cross-ns-service")
			assert.Contains(t, err.Error(), "simulated ReferenceGrant list error")
		})
	})

	// Additional test for EndpointSlices NotFound scenario.
	t.Run("Service with EndpointSlices NotFound should be skipped", func(t *testing.T) {
		existingServices := []corev1.Service{
			*createTestService("no-endpointslices-service", "default", corev1.ServiceTypeClusterIP, "10.0.0.1", "", []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
			}),
		}

		// Create objects for the fake client (no EndpointSlices).
		var objects []client.Object
		for i := range existingServices {
			objects = append(objects, &existingServices[i])
		}

		// Create fake client with interceptor that simulates NotFound error for EndpointSlices.
		interceptorFunc := interceptor.Funcs{
			List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*discoveryv1.EndpointSliceList); ok {
					// Return a NotFound error for EndpointSlices.
					return &k8serrors.StatusError{
						ErrStatus: metav1.Status{
							Status:  metav1.StatusFailure,
							Code:    404,
							Reason:  metav1.StatusReasonNotFound,
							Message: "endpointslices.discovery.k8s.io not found",
						},
					}
				}
				return client.List(ctx, list, opts...)
			},
		}

		fakeClient := createTestFakeClientWithInterceptors(interceptorFunc, objects...)

		httpRoute := createTestHTTPRoute("test-route", "default")
		backendRefs := []gwtypes.HTTPBackendRef{
			createTestHTTPBackendRef("no-endpointslices-service", nil, ptr.To[int32](80)),
		}

		// Call the function.
		ctx := context.Background()
		results, err := filterValidBackendRefs(ctx, logger, fakeClient, httpRoute, backendRefs, false, false, "cluster.local")

		// Should succeed but return no valid backend refs since no EndpointSlices found.
		require.NoError(t, err)
		assert.Len(t, results, 0) // Service skipped due to no EndpointSlices.
	})
}

// TestRecalculateWeightsAcrossBackendRefs tests the recalculateWeightsAcrossBackendRefs function.
func TestRecalculateWeightsAcrossBackendRefs(t *testing.T) {
	// Helper function to create test validBackendRef.
	createTestvalidBackendRef := func(serviceName, namespace string, weight *int32, readyEndpoints []string) validBackendRef {
		serviceKind := gwtypes.Kind("Service")
		return validBackendRef{
			backendRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name: gwtypes.ObjectName(serviceName),
						Kind: &serviceKind,
					},
					Weight: weight,
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			servicePort: &corev1.ServicePort{
				Name: "http",
				Port: 80,
			},
			readyEndpoints: readyEndpoints,
			targetPort:     8080,
			weight:         0, // This will be calculated by the function.
		}
	}

	tests := []struct {
		name           string
		input          []validBackendRef
		validateResult func(t *testing.T, result []validBackendRef)
	}{
		{
			name:  "Empty input should return empty output",
			input: []validBackendRef{},
			validateResult: func(t *testing.T, result []validBackendRef) {
				assert.Len(t, result, 0)
			},
		},
		{
			name: "Single backend ref should get calculated weight",
			input: []validBackendRef{
				createTestvalidBackendRef("service1", "default", ptr.To[int32](100), []string{"10.0.1.1", "10.0.1.2"}),
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 1)
				// With single backend, weight should be simplified ratio.
				// 100 weight / 2 endpoints = 50/1 = 50, but simplified to 1.
				assert.Equal(t, int32(1), result[0].weight)
			},
		},
		{
			name: "Multiple backend refs with equal weights",
			input: []validBackendRef{
				createTestvalidBackendRef("service1", "default", ptr.To[int32](50), []string{"10.0.1.1"}),
				createTestvalidBackendRef("service2", "default", ptr.To[int32](50), []string{"10.0.2.1"}),
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 2)
				// Both services have equal weight/endpoint ratios (50/1), so they get equal weight.
				assert.Equal(t, int32(1), result[0].weight)
				assert.Equal(t, int32(1), result[1].weight)
			},
		},
		{
			name: "Multiple backend refs with different weights",
			input: []validBackendRef{
				createTestvalidBackendRef("service1", "default", ptr.To[int32](80), []string{"10.0.1.1"}),
				createTestvalidBackendRef("service2", "default", ptr.To[int32](20), []string{"10.0.2.1"}),
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 2)
				// service1: 80/1 = 80, service2: 20/1 = 20. Ratio 80:20 = 4:1.
				assert.Equal(t, int32(4), result[0].weight)
				assert.Equal(t, int32(1), result[1].weight)
			},
		},
		{
			name: "Backend refs with different endpoint counts",
			input: []validBackendRef{
				createTestvalidBackendRef("service1", "default", ptr.To[int32](50), []string{"10.0.1.1", "10.0.1.2"}), // 2 endpoints.
				createTestvalidBackendRef("service2", "default", ptr.To[int32](50), []string{"10.0.2.1"}),             // 1 endpoint.
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 2)
				// service1: 50/2 = 25, service2: 50/1 = 50. Ratio 25:50 = 1:2.
				assert.Equal(t, int32(1), result[0].weight)
				assert.Equal(t, int32(2), result[1].weight)
			},
		},
		{
			name: "Backend refs with nil weights should default to 1",
			input: []validBackendRef{
				createTestvalidBackendRef("service1", "default", nil, []string{"10.0.1.1"}),              // No weight (defaults to 1).
				createTestvalidBackendRef("service2", "default", ptr.To[int32](3), []string{"10.0.2.1"}), // Weight 3.
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 2)
				// service1: 1/1 = 1, service2: 3/1 = 3. Ratio 1:3.
				assert.Equal(t, int32(1), result[0].weight)
				assert.Equal(t, int32(3), result[1].weight)
			},
		},
		{
			name: "Backend refs with no ready endpoints should get zero weight",
			input: []validBackendRef{
				createTestvalidBackendRef("service-with-endpoints", "default", ptr.To[int32](50), []string{"10.0.1.1"}),
				createTestvalidBackendRef("service-no-endpoints", "default", ptr.To[int32](50), []string{}), // No endpoints.
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 2)
				// Service with endpoints should get positive weight.
				assert.True(t, result[0].weight > 0, "service with endpoints should have positive weight")
				// Service with no endpoints should get zero weight.
				assert.Equal(t, int32(0), result[1].weight, "service with no endpoints should have zero weight")
			},
		},
		{
			name: "Backend ref with zero weight should get zero weight regardless of endpoints",
			input: []validBackendRef{
				createTestvalidBackendRef("service-normal", "default", ptr.To[int32](50), []string{"10.0.1.1"}),
				createTestvalidBackendRef("service-zero-weight", "default", ptr.To[int32](0), []string{"10.0.2.1"}), // Zero weight.
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 2)
				// Normal service should get positive weight.
				assert.True(t, result[0].weight > 0, "normal service should have positive weight")
				// Zero weight service should get zero weight.
				assert.Equal(t, int32(0), result[1].weight, "zero weight service should have zero weight")
			},
		},
		{
			name: "Complex scenario with multiple services and varying endpoints",
			input: []validBackendRef{
				createTestvalidBackendRef("web", "frontend", ptr.To[int32](60), []string{"10.0.1.1", "10.0.1.2", "10.0.1.3"}), // 3 endpoints.
				createTestvalidBackendRef("api", "backend", ptr.To[int32](30), []string{"10.0.2.1", "10.0.2.2"}),              // 2 endpoints.
				createTestvalidBackendRef("cache", "backend", ptr.To[int32](10), []string{"10.0.3.1"}),                        // 1 endpoint.
			},
			validateResult: func(t *testing.T, result []validBackendRef) {
				require.Len(t, result, 3)
				// Complex weight calculation based on CalculateEndpointWeights logic.
				// The exact values depend on the weight distribution algorithm.
				assert.True(t, result[0].weight > 0, "web service should have positive weight")
				assert.True(t, result[1].weight > 0, "api service should have positive weight")
				assert.True(t, result[2].weight > 0, "cache service should have positive weight")
				// Total weight should be reasonable.
				totalWeight := result[0].weight + result[1].weight + result[2].weight
				assert.True(t, totalWeight > 0 && totalWeight <= 200, "total weight should be reasonable")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function.
			result := recalculateWeightsAcrossBackendRefs(tt.input)

			// Check basic expectations.
			assert.Len(t, result, len(tt.input))

			// Run custom validation if provided.
			if tt.validateResult != nil {
				tt.validateResult(t, result)
			}

			// Verify that input structs are modified (weights updated).
			for i, vbRef := range result {
				// Weight should be >= 0 (can be 0 for backends with no endpoints or zero weight).
				assert.True(t, vbRef.weight >= 0, "weight should be non-negative for backend %d", i)
				// Verify that other fields are preserved.
				assert.Equal(t, tt.input[i].backendRef, vbRef.backendRef, "backendRef should be preserved")
				assert.Equal(t, tt.input[i].service, vbRef.service, "service should be preserved")
				assert.Equal(t, tt.input[i].readyEndpoints, vbRef.readyEndpoints, "readyEndpoints should be preserved")
			}
		})
	}
}

func TestCreateTargetsFromvalidBackendRefs(t *testing.T) {
	// Use global helper.
	createTestHTTPRoute := func(name, namespace string, backendRefs []gwtypes.HTTPBackendRef) *gwtypes.HTTPRoute {
		return createGlobalTestHTTPRoute(name, namespace, backendRefs)
	}

	// Use global helper.
	createTestHTTPBackendRef := func(name string, namespace string, group *gwtypes.Group, port *int32) gwtypes.HTTPBackendRef {
		return createGlobalTestHTTPBackendRef(name, namespace, nil, port, group)
	}

	tests := []struct {
		name             string
		httpRoute        *gwtypes.HTTPRoute
		pRef             *gwtypes.ParentReference
		upstreamName     string
		validBackendRefs []validBackendRef
		expectedTargets  int
		expectedError    bool
		validateResult   func(t *testing.T, targets []configurationv1alpha1.KongTarget)
	}{
		{
			name: "Empty valid backend refs should return empty targets",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "test-namespace", nil, ptr.To[int32](80)),
			}),
			pRef:             &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:     "test-upstream",
			validBackendRefs: []validBackendRef{},
			expectedTargets:  0,
			expectedError:    false,
		},
		{
			name: "Backend refs with no endpoints should be skipped",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "test-namespace", nil, ptr.To[int32](80)),
			}),
			pRef:         &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName: "test-upstream",
			validBackendRefs: []validBackendRef{
				createTestvalidBackendRef("service1", "test-namespace", ptr.To[int32](50), []string{}), // No endpoints.
			},
			expectedTargets: 0,
			expectedError:   false,
		},
		{
			name: "Single backend ref with single endpoint should create one target",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "test-namespace", nil, ptr.To[int32](80)),
			}),
			pRef:         &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName: "test-upstream",
			validBackendRefs: []validBackendRef{
				createTestvalidBackendRef("service1", "test-namespace", ptr.To[int32](100), []string{"10.0.0.1"}),
			},
			expectedTargets: 1,
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 1)
				target := targets[0]

				// Verify target fields.
				assert.Contains(t, target.Name, "test-upstream.")
				assert.Equal(t, "test-namespace", target.Namespace)
				assert.Equal(t, "test-upstream", target.Spec.UpstreamRef.Name)
				assert.Equal(t, "10.0.0.1:8080", target.Spec.Target) // Default port 8080 from createTestvalidBackendRef.
				assert.Equal(t, 100, target.Spec.Weight)

				// Verify labels and annotations exist.
				assert.NotEmpty(t, target.Labels)
				assert.NotEmpty(t, target.Annotations)
			},
		},
		{
			name: "Single backend ref with multiple endpoints should create multiple targets",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "test-namespace", nil, ptr.To[int32](80)),
			}),
			pRef:         &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName: "test-upstream",
			validBackendRefs: []validBackendRef{
				createTestvalidBackendRef("service1", "test-namespace", ptr.To[int32](50), []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}),
			},
			expectedTargets: 3,
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 3)

				expectedAddresses := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
				actualAddresses := make([]string, len(targets))

				for i, target := range targets {
					// All should have same weight and upstream.
					assert.Equal(t, 50, target.Spec.Weight)
					assert.Equal(t, "test-upstream", target.Spec.UpstreamRef.Name)
					assert.Equal(t, "test-namespace", target.Namespace)

					// Extract the IP address from target spec (format: "IP:PORT").
					assert.Contains(t, target.Spec.Target, ":8080")
					actualAddresses[i] = target.Spec.Target[:len(target.Spec.Target)-5] // Remove ":8080".
				}

				// Verify all expected addresses are present.
				for _, expected := range expectedAddresses {
					assert.Contains(t, actualAddresses, expected)
				}
			},
		},
		{
			name: "Multiple backend refs with different endpoints and weights",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "test-namespace", nil, ptr.To[int32](80)),
				createTestHTTPBackendRef("service2", "test-namespace", nil, ptr.To[int32](90)),
			}),
			pRef:         &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName: "test-upstream",
			validBackendRefs: []validBackendRef{
				createTestvalidBackendRef("service1", "test-namespace", ptr.To[int32](30), []string{"10.0.1.1", "10.0.1.2"}),
				createTestvalidBackendRef("service2", "test-namespace", ptr.To[int32](70), []string{"10.0.2.1"}),
			},
			expectedTargets: 3, // 2 from service1 + 1 from service2.
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 3)

				service1Targets := 0
				service2Targets := 0

				for _, target := range targets {
					switch target.Spec.Target {
					case "10.0.1.1:8080", "10.0.1.2:8080":
						service1Targets++
						assert.Equal(t, 30, target.Spec.Weight)
					case "10.0.2.1:8080":
						service2Targets++
						assert.Equal(t, 70, target.Spec.Weight)
					default:
						t.Errorf("Unexpected target: %s", target.Spec.Target)
					}

					// Common validations.
					assert.Equal(t, "test-upstream", target.Spec.UpstreamRef.Name)
					assert.Contains(t, target.Name, "test-upstream.")
				}

				assert.Equal(t, 2, service1Targets, "Should have 2 targets from service1")
				assert.Equal(t, 1, service2Targets, "Should have 1 target from service2")
			},
		},
		{
			name: "Mixed scenario with some backends having no endpoints",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "test-namespace", nil, ptr.To[int32](80)),
				createTestHTTPBackendRef("service2", "test-namespace", nil, ptr.To[int32](90)),
				createTestHTTPBackendRef("service3", "test-namespace", nil, ptr.To[int32](85)),
			}),
			pRef:         &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName: "test-upstream",
			validBackendRefs: []validBackendRef{
				createTestvalidBackendRef("service1", "test-namespace", ptr.To[int32](0), []string{}),                        // No endpoints, should be skipped.
				createTestvalidBackendRef("service2", "test-namespace", ptr.To[int32](60), []string{"10.0.2.1"}),             // 1 endpoint.
				createTestvalidBackendRef("service3", "test-namespace", ptr.To[int32](40), []string{"10.0.3.1", "10.0.3.2"}), // 2 endpoints.
			},
			expectedTargets: 3, // 0 from service1 + 1 from service2 + 2 from service3.
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 3)

				// Should only have targets from service2 and service3.
				targetAddresses := make([]string, len(targets))
				for i, target := range targets {
					targetAddresses[i] = target.Spec.Target
				}

				assert.Contains(t, targetAddresses, "10.0.2.1:8080")
				assert.Contains(t, targetAddresses, "10.0.3.1:8080")
				assert.Contains(t, targetAddresses, "10.0.3.2:8080")

				// Verify no service1 targets exist.
				for _, addr := range targetAddresses {
					assert.NotContains(t, addr, "10.0.1.")
				}
			},
		},
		{
			name: "Backend ref with custom port should use correct target port",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "test-namespace", nil, ptr.To[int32](8080)),
			}),
			pRef:         &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName: "test-upstream",
			validBackendRefs: []validBackendRef{
				{
					backendRef: &gwtypes.HTTPBackendRef{
						BackendRef: gwtypes.BackendRef{
							BackendObjectReference: gwtypes.BackendObjectReference{
								Name: "service1",
								Kind: ptr.To(gwtypes.Kind("Service")),
							},
						},
					},
					service: &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service1",
							Namespace: "test-namespace",
						},
					},
					servicePort: &corev1.ServicePort{
						Name: "http",
						Port: 8080,
					},
					readyEndpoints: []string{"10.0.0.1"},
					targetPort:     9090, // Custom target port.
					weight:         100,
				},
			},
			expectedTargets: 1,
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 1)
				target := targets[0]

				// Should use the custom target port.
				assert.Equal(t, "10.0.0.1:9090", target.Spec.Target)
				assert.Equal(t, 100, target.Spec.Weight)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test context and fake client
			ctx := context.Background()
			fakeClient := createTestFakeClient() // Empty client since we expect new targets to be created

			// Call the function.
			targets, err := createTargetsFromValidBackendRefs(ctx, logr.Discard(), fakeClient, tt.httpRoute, tt.pRef, tt.upstreamName, tt.validBackendRefs)

			// Check error expectation.
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Check number of targets.
			assert.Len(t, targets, tt.expectedTargets)

			// Run custom validation if provided.
			if tt.validateResult != nil {
				tt.validateResult(t, targets)
			}

			// General validations for all targets.
			for _, target := range targets {
				// All targets should have the correct upstream reference.
				assert.Equal(t, tt.upstreamName, target.Spec.UpstreamRef.Name)

				// All targets should be in the same namespace as the HTTPRoute.
				assert.Equal(t, tt.httpRoute.Namespace, target.Namespace)

				// All target names should contain the upstream name.
				assert.Contains(t, target.Name, tt.upstreamName+".")

				// Weight should be set.
				assert.NotZero(t, target.Spec.Weight)

				// Target should have an address:port format.
				assert.Contains(t, target.Spec.Target, ":")
			}
		})
	}
}

func TestTargetsForBackendRefs(t *testing.T) {
	// Helper function to create test context.
	createTestContext := context.Background

	// Helper function to create test logger.
	createTestLogger := func() logr.Logger {
		return ctrllog.Log.WithName("test")
	}

	// Use global helper.
	createTestHTTPRoute := func(name, namespace string, backendRefs []gwtypes.HTTPBackendRef) *gwtypes.HTTPRoute {
		return createGlobalTestHTTPRoute(name, namespace, backendRefs)
	}

	// Use global helper.
	createTestHTTPBackendRef := func(name string, namespace string, weight *int32, port *int32) gwtypes.HTTPBackendRef {
		return createGlobalTestHTTPBackendRef(name, namespace, weight, port)
	}

	tests := []struct {
		name                  string
		httpRoute             *gwtypes.HTTPRoute
		backendRefs           []gwtypes.HTTPBackendRef
		pRef                  *gwtypes.ParentReference
		upstreamName          string
		referenceGrantEnabled bool
		fqdn                  bool
		services              []corev1.Service
		endpointSlices        []discoveryv1.EndpointSlice
		referenceGrants       []gatewayv1beta1.ReferenceGrant
		expectedTargets       int
		expectedError         bool
		clientErrors          map[string]error
		validateResult        func(t *testing.T, targets []configurationv1alpha1.KongTarget)
	}{
		{
			name: "Error from filterValidBackendRefs should be propagated",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", "other-namespace", nil, ptr.To[int32](80)),
			}),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", "other-namespace", nil, ptr.To[int32](80)),
			},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: true, // This will cause ReferenceGrant check.
			fqdn:                  false,
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-service", Namespace: "other-namespace"},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
					},
				},
			},
			endpointSlices:  []discoveryv1.EndpointSlice{},
			referenceGrants: []gatewayv1beta1.ReferenceGrant{},
			clientErrors: map[string]error{
				"list-referencegrant": fmt.Errorf("simulated ReferenceGrant list error"),
			},
			expectedError: true,
		},

		{
			name: "Error from getEndpointSlicesForService should be propagated",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", "", nil, ptr.To[int32](80)),
			}),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", "", nil, ptr.To[int32](80)),
			},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: false,
			fqdn:                  false,
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-service", Namespace: "test-namespace"},
					Spec: corev1.ServiceSpec{
						Ports:     []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
						ClusterIP: "10.0.0.1", // Regular service, not headless.
					},
				},
			},
			endpointSlices:  []discoveryv1.EndpointSlice{},
			referenceGrants: []gatewayv1beta1.ReferenceGrant{},
			clientErrors: map[string]error{
				"list-endpointslice": fmt.Errorf("simulated EndpointSlice list error"),
			},
			expectedError: true,
		},

		{
			name:                  "Empty backend refs should return empty targets",
			httpRoute:             createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{}),
			backendRefs:           []gwtypes.HTTPBackendRef{},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: false,
			fqdn:                  false,
			services:              []corev1.Service{},
			endpointSlices:        []discoveryv1.EndpointSlice{},
			referenceGrants:       []gatewayv1beta1.ReferenceGrant{},
			expectedTargets:       0,
			expectedError:         false,
		},
		{
			name: "Single valid backend ref should create targets",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", "", nil, ptr.To[int32](80)),
			}),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("test-service", "", nil, ptr.To[int32](80)),
			},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: false,
			fqdn:                  false,
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
						},
						Type: corev1.ServiceTypeClusterIP,
					},
				},
			},
			endpointSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-slice",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"kubernetes.io/service-name": "test-service",
						},
					},
					Ports:     []discoveryv1.EndpointPort{createTestEndpointPort("http", 8080, corev1.ProtocolTCP)},
					Endpoints: []discoveryv1.Endpoint{createTestEndpoint([]string{"10.0.0.1", "10.0.0.2"}, true)},
				},
			},
			referenceGrants: []gatewayv1beta1.ReferenceGrant{},
			expectedTargets: 2, // 2 endpoints.
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 2)

				// Verify both targets have correct upstream and namespace.
				for _, target := range targets {
					assert.Equal(t, "test-upstream", target.Spec.UpstreamRef.Name)
					assert.Equal(t, "test-namespace", target.Namespace)
					assert.Contains(t, []string{"10.0.0.1:8080", "10.0.0.2:8080"}, target.Spec.Target)
				}
			},
		},
		{
			name: "Multiple backend refs with different weights should distribute correctly",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "", ptr.To[int32](70), ptr.To[int32](80)),
				createTestHTTPBackendRef("service2", "", ptr.To[int32](30), ptr.To[int32](80)),
			}),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("service1", "", ptr.To[int32](70), ptr.To[int32](80)),
				createTestHTTPBackendRef("service2", "", ptr.To[int32](30), ptr.To[int32](80)),
			},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: false,
			fqdn:                  false,
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test-namespace"},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)}},
						Type:  corev1.ServiceTypeClusterIP,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test-namespace"},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)}},
						Type:  corev1.ServiceTypeClusterIP,
					},
				},
			},
			endpointSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service1-slice",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"kubernetes.io/service-name": "service1",
						},
					},
					Ports:     []discoveryv1.EndpointPort{createTestEndpointPort("http", 8080, corev1.ProtocolTCP)},
					Endpoints: []discoveryv1.Endpoint{createTestEndpoint([]string{"10.0.1.1"}, true)},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service2-slice",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"kubernetes.io/service-name": "service2",
						},
					},
					Ports:     []discoveryv1.EndpointPort{createTestEndpointPort("http", 8080, corev1.ProtocolTCP)},
					Endpoints: []discoveryv1.Endpoint{createTestEndpoint([]string{"10.0.2.1"}, true)},
				},
			},
			referenceGrants: []gatewayv1beta1.ReferenceGrant{},
			expectedTargets: 2,
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 2)

				// Verify weight distribution (exact values depend on the algorithm).
				service1Target := findTargetByAddress(targets, "10.0.1.1:8080")
				service2Target := findTargetByAddress(targets, "10.0.2.1:8080")

				require.NotNil(t, service1Target, "service1 target should exist")
				require.NotNil(t, service2Target, "service2 target should exist")

				// Service1 should have higher weight than service2 (70 vs 30).
				assert.True(t, service1Target.Spec.Weight > service2Target.Spec.Weight,
					"service1 weight (%d) should be higher than service2 weight (%d)",
					service1Target.Spec.Weight, service2Target.Spec.Weight)
			},
		},
		{
			name: "Cross-namespace backend refs with ReferenceGrant should work",
			httpRoute: createTestHTTPRoute("test-route", "frontend-ns", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("backend-service", "backend-ns", nil, ptr.To[int32](80)),
			}),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("backend-service", "backend-ns", nil, ptr.To[int32](80)),
			},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: true,
			fqdn:                  false,
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "backend-service", Namespace: "backend-ns"},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)}},
						Type:  corev1.ServiceTypeClusterIP,
					},
				},
			},
			endpointSlices: []discoveryv1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-service-slice",
						Namespace: "backend-ns",
						Labels: map[string]string{
							"kubernetes.io/service-name": "backend-service",
						},
					},
					Ports:     []discoveryv1.EndpointPort{createTestEndpointPort("http", 8080, corev1.ProtocolTCP)},
					Endpoints: []discoveryv1.Endpoint{createTestEndpoint([]string{"10.0.3.1"}, true)},
				},
			},
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "allow-frontend-to-backend", Namespace: "backend-ns"},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{Group: gwtypes.GroupName, Kind: "HTTPRoute", Namespace: "frontend-ns"},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{Group: "", Kind: "Service"},
						},
					},
				},
			},
			expectedTargets: 1,
			expectedError:   false,
		},
		{
			name: "Cross-namespace backend refs without ReferenceGrant should fail",
			httpRoute: createTestHTTPRoute("test-route", "frontend-ns", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("backend-service", "backend-ns", nil, ptr.To[int32](80)),
			}),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("backend-service", "backend-ns", nil, ptr.To[int32](80)),
			},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: true,
			fqdn:                  false,
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "backend-service", Namespace: "backend-ns"},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)}},
						Type:  corev1.ServiceTypeClusterIP,
					},
				},
			},
			endpointSlices:  []discoveryv1.EndpointSlice{},
			referenceGrants: []gatewayv1beta1.ReferenceGrant{}, // No ReferenceGrant.
			expectedTargets: 0,                                 // Should have no valid targets due to missing ReferenceGrant.
			expectedError:   false,                             // Should not error, just no targets.
		},

		{
			name: "FQDN mode should work correctly",
			httpRoute: createTestHTTPRoute("test-route", "test-namespace", []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("external-service", "", nil, ptr.To[int32](80)),
			}),
			backendRefs: []gwtypes.HTTPBackendRef{
				createTestHTTPBackendRef("external-service", "", nil, ptr.To[int32](80)),
			},
			pRef:                  &gwtypes.ParentReference{Name: "test-gateway"},
			upstreamName:          "test-upstream",
			referenceGrantEnabled: false,
			fqdn:                  true, // FQDN mode.
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "external-service", Namespace: "test-namespace"},
					Spec: corev1.ServiceSpec{
						Ports:        []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
						Type:         corev1.ServiceTypeExternalName,
						ExternalName: "api.example.com",
					},
				},
			},
			endpointSlices:  []discoveryv1.EndpointSlice{}, // ExternalName services don't use EndpointSlices.
			referenceGrants: []gatewayv1beta1.ReferenceGrant{},
			expectedTargets: 1,
			expectedError:   false,
			validateResult: func(t *testing.T, targets []configurationv1alpha1.KongTarget) {
				require.Len(t, targets, 1)
				target := targets[0]

				// In FQDN mode, it should use the FQDN format for the service name.
				// The actual behavior might be using cluster DNS format instead of ExternalName.
				assert.Contains(t, target.Spec.Target, "external-service")
				assert.Contains(t, target.Spec.Target, ":80")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with test objects.
			objects := []client.Object{}
			for i := range tt.services {
				objects = append(objects, &tt.services[i])
			}
			for i := range tt.endpointSlices {
				objects = append(objects, &tt.endpointSlices[i])
			}
			for i := range tt.referenceGrants {
				objects = append(objects, &tt.referenceGrants[i])
			}

			var cl client.Client
			// Add client error interceptors if specified.
			if tt.clientErrors != nil {
				cl = createTestFakeClientWithInterceptors(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if errorKey, exists := tt.clientErrors["get-service"]; exists {
							if _, ok := obj.(*corev1.Service); ok && key.Name == "test-service" {
								return errorKey
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						// Handle ReferenceGrant list error.
						if errorKey, exists := tt.clientErrors["list-referencegrant"]; exists {
							if _, ok := list.(*gatewayv1beta1.ReferenceGrantList); ok {
								return errorKey
							}
						}
						// Handle EndpointSlice list error.
						if errorKey, exists := tt.clientErrors["list-endpointslice"]; exists {
							if _, ok := list.(*discoveryv1.EndpointSliceList); ok {
								return errorKey
							}
						}
						return client.List(ctx, list, opts...)
					},
				}, objects...)
			} else {
				cl = createTestFakeClient(objects...)
			}

			// Call the function.
			targets, err := TargetsForBackendRefs(
				createTestContext(),
				createTestLogger(),
				cl,
				tt.httpRoute,
				tt.backendRefs,
				tt.pRef,
				tt.upstreamName,
				tt.referenceGrantEnabled,
				tt.fqdn,
				"cluster.local",
			) // Check error expectation.
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Check number of targets.
			assert.Len(t, targets, tt.expectedTargets)

			// Run custom validation if provided.
			if tt.validateResult != nil {
				tt.validateResult(t, targets)
			}

			// General validations for all targets.
			for _, target := range targets {
				// All targets should have the correct upstream reference.
				assert.Equal(t, tt.upstreamName, target.Spec.UpstreamRef.Name)

				// All targets should be in the same namespace as the HTTPRoute.
				assert.Equal(t, tt.httpRoute.Namespace, target.Namespace)

				// All target names should contain the upstream name.
				assert.Contains(t, target.Name, tt.upstreamName+".")

				// Target should have an address:port format.
				assert.Contains(t, target.Spec.Target, ":")
			}
		})
	}
}

// Helper function to find a target by its address.
func findTargetByAddress(targets []configurationv1alpha1.KongTarget, address string) *configurationv1alpha1.KongTarget {
	for i := range targets {
		if targets[i].Spec.Target == address {
			return &targets[i]
		}
	}
	return nil
}
