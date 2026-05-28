package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongUpstreamOnKonnectGatewayControlPlane is the index field for KongUpstream -> KonnectGatewayControlPlane.
	IndexFieldKongUpstreamOnKonnectGatewayControlPlane = "kongUpstreamKonnectGatewayControlPlaneRef"
	// IndexFieldKongUpstreamOnReferencedKongCertificate is the index field for KongUpstream -> KongCertificate (clientCertificateRef).
	IndexFieldKongUpstreamOnReferencedKongCertificate = "kongUpstreamKongCertificateRef"
)

// OptionsForKongUpstream returns required Index options for KongUpstream reconciler.
func OptionsForKongUpstream(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongUpstream{},
			Field:          IndexFieldKongUpstreamOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongUpstream](cl),
		},
		{
			Object:         &configurationv1alpha1.KongUpstream{},
			Field:          IndexFieldKongUpstreamOnReferencedKongCertificate,
			ExtractValueFn: kongUpstreamRefersToKongCertificate,
		},
	}
}

// kongUpstreamRefersToKongCertificate extracts the "namespace/name" key for the
// KongCertificate referenced via spec.clientCertificateRef.
func kongUpstreamRefersToKongCertificate(object client.Object) []string {
	upstream, ok := object.(*configurationv1alpha1.KongUpstream)
	if !ok || upstream.Spec.ClientCertificateRef == nil {
		return nil
	}
	ns := upstream.Namespace
	if upstream.Spec.ClientCertificateRef.Namespace != nil && *upstream.Spec.ClientCertificateRef.Namespace != "" {
		ns = *upstream.Spec.ClientCertificateRef.Namespace
	}
	return []string{ns + "/" + upstream.Spec.ClientCertificateRef.Name}
}
