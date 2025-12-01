package index

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// TLSCertificateSecretsOnGatewayIndex maps Secret "namespace/name" to Gateway objects that
	// reference them in listeners.tls.certificateRefs.
	TLSCertificateSecretsOnGatewayIndex = "TLSCertificateSecretsOnGateway"
)

// OptionsForGatewayTLSSecret returns index options for Gateways referencing Secrets via TLS certificateRefs.
func OptionsForGatewayTLSSecret() []Option {
	return []Option{
		{
			Object:         &gwtypes.Gateway{},
			Field:          TLSCertificateSecretsOnGatewayIndex,
			ExtractValueFn: tlsCertificateSecretsOnGateway,
		},
	}
}

// tlsCertificateSecretsOnGateway extracts Secret references (namespace/name) from a Gateway's listeners.tls.certificateRefs.
func tlsCertificateSecretsOnGateway(o client.Object) []string {
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
			// Only index core/v1 Secret references (or defaulted ones when group/kind are nil).
			if ref.Group != nil && string(*ref.Group) != corev1.GroupName {
				continue
			}
			if ref.Kind != nil && string(*ref.Kind) != "Secret" {
				continue
			}
			ns := gw.Namespace
			if ref.Namespace != nil {
				ns = string(*ref.Namespace)
			}
			if ref.Name == "" {
				continue
			}
			seen[ns+"/"+string(ref.Name)] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	return out
}
