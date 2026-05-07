package builder

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestKongServiceBuilder_NewKongService(t *testing.T) {
	builder := NewKongService()

	assert.NotNil(t, builder)
	assert.NotNil(t, builder.errors)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1alpha1.KongService{}, builder.service)
}

func TestKongServiceBuilder_WithName(t *testing.T) {
	builder := NewKongService().WithName("test-service")

	service, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-service", service.Name)
}

func TestKongServiceBuilder_WithNamespace(t *testing.T) {
	builder := NewKongService().WithNamespace("test-namespace")

	service, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", service.Namespace)
}

func TestKongServiceBuilder_WithLabels(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongService().WithLabels(httpRoute, parentRef)

	service, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, service.Labels)
	assert.NotEmpty(t, service.Labels)
}

func TestKongServiceBuilder_WithAnnotations(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongService().WithAnnotations(httpRoute, parentRef)

	service, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, service.Annotations)
}

func TestKongServiceBuilder_WithSpecName(t *testing.T) {
	tests := []struct {
		name     string
		specName string
		expected *string
	}{
		{
			name:     "with spec name",
			specName: "test-service-spec",
			expected: new("test-service-spec"),
		},
		{
			name:     "empty spec name",
			specName: "",
			expected: new(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongService().WithSpecName(tt.specName)

			service, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, service.Spec.Name)
		})
	}
}

func TestKongServiceBuilder_WithSpecHost(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "with host",
			host:     "example.com",
			expected: "example.com",
		},
		{
			name:     "empty host",
			host:     "",
			expected: "",
		},
		{
			name:     "with service name and cluster domain",
			host:     "my-service.my-namespace.svc.cluster.local",
			expected: "my-service.my-namespace.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongService().WithSpecHost(tt.host)

			service, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, service.Spec.Host)
		})
	}
}

func TestKongServiceBuilder_WithControlPlaneRef(t *testing.T) {
	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-konnect-cp",
			Namespace: "test-namespace",
		},
	}

	builder := NewKongService().WithControlPlaneRef(controlPlaneRef)

	service, err := builder.Build()
	require.NoError(t, err)

	require.NotNil(t, service.Spec.ControlPlaneRef)
	assert.Equal(t, controlPlaneRef, *service.Spec.ControlPlaneRef)
}

func TestKongServiceBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongService().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		service, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, service.OwnerReferences, 1)
		ownerRef := service.OwnerReferences[0]
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongService().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})

	t.Run("owner reference error", func(t *testing.T) {
		httpRouteWithoutTypeMeta := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-http-route",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
		}

		builder := NewKongService().WithOwner(httpRouteWithoutTypeMeta)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongServiceBuilder_WithProtocol(t *testing.T) {
	tests := []struct {
		name             string
		protocol         string
		expectedProtocol sdkkonnectcomp.Protocol
		expectError      bool
	}{
		{name: "http", protocol: "http", expectedProtocol: sdkkonnectcomp.ProtocolHTTP},
		{name: "https", protocol: "https", expectedProtocol: sdkkonnectcomp.ProtocolHTTPS},
		{name: "grpc", protocol: "grpc", expectedProtocol: sdkkonnectcomp.ProtocolGrpc},
		{name: "grpcs", protocol: "grpcs", expectedProtocol: sdkkonnectcomp.ProtocolGrpcs},
		{name: "ws", protocol: "ws", expectedProtocol: sdkkonnectcomp.ProtocolWs},
		{name: "wss", protocol: "wss", expectedProtocol: sdkkonnectcomp.ProtocolWss},
		{name: "tls", protocol: "tls", expectedProtocol: sdkkonnectcomp.ProtocolTLS},
		{name: "tcp", protocol: "tcp", expectedProtocol: sdkkonnectcomp.ProtocolTCP},
		{name: "tls_passthrough", protocol: "tls_passthrough", expectedProtocol: sdkkonnectcomp.ProtocolTLSPassthrough},
		{name: "udp", protocol: "udp", expectedProtocol: sdkkonnectcomp.ProtocolUDP},
		{name: "empty defaults to http", protocol: "", expectedProtocol: sdkkonnectcomp.ProtocolHTTP},
		{name: "unsupported protocol", protocol: "invalid", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewKongService().WithProtocol(tt.protocol).Build()
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported protocol")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedProtocol, service.Spec.Protocol)
			}
		})
	}
}

func TestKongServiceBuilder_WithPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected *string
	}{
		{name: "empty path leaves field unset", path: "", expected: nil},
		{name: "non-empty path sets field", path: "/api/v1", expected: new("/api/v1")},
		{name: "root path sets field", path: "/", expected: new("/")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewKongService().WithPath(tt.path).Build()
			require.NoError(t, err)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.Path)
			} else {
				require.NotNil(t, service.Spec.Path)
				assert.Equal(t, *tt.expected, *service.Spec.Path)
			}
		})
	}
}

func TestKongServiceBuilder_WithTLSVerify(t *testing.T) {
	tests := []struct {
		name     string
		v        *bool
		expected *bool
	}{
		{name: "nil leaves field unset", v: nil, expected: nil},
		{name: "true sets field", v: new(true), expected: new(true)},
		{name: "false sets field", v: new(false), expected: new(false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewKongService().WithTLSVerify(tt.v).Build()
			require.NoError(t, err)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.TLSVerify)
			} else {
				require.NotNil(t, service.Spec.TLSVerify)
				assert.Equal(t, *tt.expected, *service.Spec.TLSVerify)
			}
		})
	}
}

func TestKongServiceBuilder_WithTLSVerifyDepth(t *testing.T) {
	tests := []struct {
		name     string
		v        *int64
		expected *int64
	}{
		{name: "nil leaves field unset", v: nil, expected: nil},
		{name: "zero sets field", v: new(int64(0)), expected: new(int64(0))},
		{name: "positive sets field", v: new(int64(3)), expected: new(int64(3))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewKongService().WithTLSVerifyDepth(tt.v).Build()
			require.NoError(t, err)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.TLSVerifyDepth)
			} else {
				require.NotNil(t, service.Spec.TLSVerifyDepth)
				assert.Equal(t, *tt.expected, *service.Spec.TLSVerifyDepth)
			}
		})
	}
}

func TestKongServiceBuilder_WithConnectTimeout(t *testing.T) {
	tests := []struct {
		name     string
		input    *int64
		expected *int64
	}{
		{name: "nil leaves field unset", input: nil, expected: nil},
		{name: "zero sets field", input: new(int64(0)), expected: new(int64(0))},
		{name: "positive sets field", input: new(int64(5000)), expected: new(int64(5000))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewKongService().WithConnectTimeout(tt.input).Build()
			require.NoError(t, err)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.ConnectTimeout)
			} else {
				require.NotNil(t, service.Spec.ConnectTimeout)
				assert.Equal(t, *tt.expected, *service.Spec.ConnectTimeout)
			}
		})
	}
}

func TestKongServiceBuilder_Build(t *testing.T) {
	t.Run("successful build", func(t *testing.T) {
		builder := NewKongService().
			WithName("test-service").
			WithNamespace("test-namespace").
			WithSpecName("test-spec").
			WithSpecHost("example.com")

		service, err := builder.Build()
		require.NoError(t, err)
		assert.Equal(t, "test-service", service.Name)
		assert.Equal(t, "test-namespace", service.Namespace)
		assert.Equal(t, "test-spec", *service.Spec.Name)
		assert.Equal(t, "example.com", service.Spec.Host)
	})

	t.Run("build with errors", func(t *testing.T) {
		builder := NewKongService().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})
}

func TestKongServiceBuilder_MustBuild(t *testing.T) {
	t.Run("successful must build", func(t *testing.T) {
		builder := NewKongService().WithName("test-service")

		service := builder.MustBuild()
		assert.Equal(t, "test-service", service.Name)
	})

	t.Run("must build with errors panics", func(t *testing.T) {
		builder := NewKongService().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongServiceBuilder_Chaining(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-konnect-cp",
			Namespace: "test-namespace",
		},
	}

	service := NewKongService().
		WithName("test-service").
		WithNamespace("test-namespace").
		WithSpecName("test-spec").
		WithSpecHost("example.com").
		WithControlPlaneRef(controlPlaneRef).
		WithOwner(httpRoute).
		WithLabels(httpRoute, parentRef).
		WithAnnotations(httpRoute, parentRef).
		MustBuild()

	assert.Equal(t, "test-service", service.Name)
	assert.Equal(t, "test-namespace", service.Namespace)
	assert.Equal(t, "test-spec", *service.Spec.Name)
	assert.Equal(t, "example.com", service.Spec.Host)
	assert.Equal(t, controlPlaneRef, *service.Spec.ControlPlaneRef)
	assert.Len(t, service.OwnerReferences, 1)
	assert.NotNil(t, service.Labels)
	assert.NotNil(t, service.Annotations)
}

func TestKongServiceBuilder_MultipleErrors(t *testing.T) {
	builder := NewKongService()

	builder.WithOwner(nil)
	builder.errors = append(builder.errors, assert.AnError)

	_, err := builder.Build()
	require.Error(t, err)

	assert.Contains(t, err.Error(), "owner cannot be nil")
	assert.Contains(t, err.Error(), assert.AnError.Error())
}
