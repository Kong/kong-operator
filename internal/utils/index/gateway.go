package index

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const (
	// GatewayClassOnGatewayIndex is the name of the index that maps GatewayClass names to Gateways referencing them.
	GatewayClassOnGatewayIndex = "GatewayClassOnGateway"
	// TLSCertificateSecretsOnGatewayIndex maps Secret "namespace/name" to Gateway objects that
	// reference them in listeners.tls.certificateRefs.
	TLSCertificateSecretsOnGatewayIndex = "TLSCertificateSecretsOnGateway"
	// TLSKongCertificatesOnGatewayIndex maps KongCertificate "namespace/name" to referencing Gateways.
	TLSKongCertificatesOnGatewayIndex = "TLSKongCertificatesOnGateway"
)

// OptionsForGateway returns a slice of Option configured for indexing Gateway objects by GatewayClass name.
func OptionsForGateway() []Option {
	return []Option{
		{
			Object:         &gwtypes.Gateway{},
			Field:          GatewayClassOnGatewayIndex,
			ExtractValueFn: GatewayClassOnGateway,
		},
		{
			Object:         &gwtypes.Gateway{},
			Field:          TLSCertificateSecretsOnGatewayIndex,
			ExtractValueFn: TLSCertificateSecretsOnGateway,
		},
		{
			Object:         &gwtypes.Gateway{},
			Field:          TLSKongCertificatesOnGatewayIndex,
			ExtractValueFn: TLSKongCertificatesOnGateway,
		},
	}
}

// GatewayClassOnGateway extracts and returns the GatewayClass name referenced by the given Gateway object.
func GatewayClassOnGateway(o client.Object) []string {
	gateway, ok := o.(*gwtypes.Gateway)
	if !ok {
		return nil
	}
	if gateway.Spec.GatewayClassName == "" {
		return nil
	}
	return []string{string(gateway.Spec.GatewayClassName)}
}

// TLSCertificateSecretsOnGateway extracts Secret references (namespace/name) from a Gateway's listeners.tls.certificateRefs.
func TLSCertificateSecretsOnGateway(o client.Object) []string {
	return tlsCertificatesOnGateway(o, corev1.GroupName, "Secret")
}

// TLSKongCertificatesOnGateway extracts KongCertificate references from Gateway TLS listeners.
func TLSKongCertificatesOnGateway(o client.Object) []string {
	return tlsCertificatesOnGateway(o, configurationv1alpha1.GroupVersion.Group, "KongCertificate")
}

func tlsCertificatesOnGateway(o client.Object, group, kind string) []string {
	gw, ok := o.(*gwtypes.Gateway)
	if !ok {
		return nil
	}

	seen := make(map[string]struct{})
	for _, l := range gw.Spec.Listeners {
		if l.TLS == nil {
			continue
		}
		for _, ref := range l.TLS.CertificateRefs {
			refGroup, refKind := "", "Secret"
			if ref.Group != nil {
				refGroup = string(*ref.Group)
			}
			if ref.Kind != nil {
				refKind = string(*ref.Kind)
			}
			if refGroup != group || refKind != kind {
				continue
			}
			ns := gw.Namespace
			if ref.Namespace != nil && *ref.Namespace != "" {
				ns = string(*ref.Namespace)
			}
			if ref.Name == "" {
				continue
			}
			seen[ns+"/"+string(ref.Name)] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	return out
}
