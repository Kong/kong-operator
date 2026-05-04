package builder

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const (
	// KongServiceProtocolHTTP stands for http protocol in Kong Service.
	KongServiceProtocolHTTP = "http"
	// KongServiceProtocolTCP stands for tcp protocol in Kong Service.
	KongServiceProtocolTCP = "tcp"
	// KongServiceProtocolTLS stands for tls protocol in Kong Service.
	KongServiceProtocolTLS = "tls"
)

// KongServiceBuilder is a builder for configurationv1alpha1.KongService resources.
type KongServiceBuilder struct {
	service configurationv1alpha1.KongService
	errors  []error
}

// NewKongService creates and returns a new KongServiceBuilder instance.
func NewKongService() *KongServiceBuilder {
	return &KongServiceBuilder{
		service: configurationv1alpha1.KongService{},
		errors:  make([]error, 0),
	}
}

// WithName sets the name for the KongService being built.
func (b *KongServiceBuilder) WithName(name string) *KongServiceBuilder {
	b.service.Name = name
	return b
}

// WithNamespace sets the namespace for the KongService being built.
func (b *KongServiceBuilder) WithNamespace(namespace string) *KongServiceBuilder {
	b.service.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongService being built.
func (b *KongServiceBuilder) WithLabels(route client.Object, parentRef *gwtypes.ParentReference) *KongServiceBuilder {
	labels := metadata.BuildLabels(route, parentRef)
	if b.service.Labels == nil {
		b.service.Labels = make(map[string]string)
	}
	maps.Copy(b.service.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongService being built.
func (b *KongServiceBuilder) WithAnnotations(route client.Object, parentRef *gwtypes.ParentReference) *KongServiceBuilder {
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.service.Annotations == nil {
		b.service.Annotations = make(map[string]string)
	}
	maps.Copy(b.service.Annotations, annotations)
	return b
}

// WithSpecName sets the name field in the KongService spec.
func (b *KongServiceBuilder) WithSpecName(name string) *KongServiceBuilder {
	b.service.Spec.Name = &name
	return b
}

// WithSpecHost sets the host field in the KongService spec.
func (b *KongServiceBuilder) WithSpecHost(host string) *KongServiceBuilder {
	b.service.Spec.Host = host
	return b
}

// WithControlPlaneRef sets the ControlPlaneRef for the KongService being built.
func (b *KongServiceBuilder) WithControlPlaneRef(cpr commonv1alpha1.ControlPlaneRef) *KongServiceBuilder {
	b.service.Spec.ControlPlaneRef = &cpr
	return b
}

// WithOwner sets the owner reference for the KongService to the given route.
func (b *KongServiceBuilder) WithOwner(owner client.Object) *KongServiceBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetControllerReference(owner, &b.service, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// WithReadTimeout sets the read timeout (milliseconds) for the KongService being built.
// A nil pointer leaves the field unset.
func (b *KongServiceBuilder) WithReadTimeout(v *int64) *KongServiceBuilder {
	if v == nil {
		return b
	}
	b.service.Spec.ReadTimeout = v
	return b
}

// WithProtocol sets the protocol for the KongService being built.
// Supported protocols match the Kong Gateway upstream protocol set.
func (b *KongServiceBuilder) WithProtocol(protocol string) *KongServiceBuilder {
	if protocol == "" {
		protocol = KongServiceProtocolHTTP
	}
	protocol = strings.ToLower(protocol)
	switch protocol {
	case "http":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolHTTP
	case "https":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolHTTPS
	case "grpc":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolGrpc
	case "grpcs":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolGrpcs
	case "ws":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolWs
	case "wss":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolWss
	case "tls":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolTLS
	case "tcp":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolTCP
	case "tls_passthrough":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolTLSPassthrough
	case "udp":
		b.service.Spec.Protocol = sdkkonnectcomp.ProtocolUDP
	default:
		b.errors = append(b.errors, fmt.Errorf("unsupported protocol: %s", protocol))
	}
	return b
}

// Build returns the constructed KongService resource and any accumulated errors.
func (b *KongServiceBuilder) Build() (configurationv1alpha1.KongService, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongService{}, errors.Join(b.errors...)
	}
	return b.service, nil
}

// MustBuild returns the constructed KongService resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongServiceBuilder) MustBuild() configurationv1alpha1.KongService {
	service, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongService: %w", err))
	}
	return service
}
