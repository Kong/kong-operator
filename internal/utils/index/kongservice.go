package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/metadata"
)

const (
	// IndexFieldKongServiceOnReferencedPluginNames is the index field for KongService -> KongPlugin.
	IndexFieldKongServiceOnReferencedPluginNames = "kongServiceKongPluginRef"
	// IndexFieldKongServiceOnKonnectGatewayControlPlane is the index field for KongService -> KonnectGatewayControlPlane.
	IndexFieldKongServiceOnKonnectGatewayControlPlane = "kongServiceKonnectGatewayControlPlaneRef"
	// IndexFieldKongServiceOnReferencedKongCertificate is the index field for KongService -> KongCertificate (clientCertificateRef).
	IndexFieldKongServiceOnReferencedKongCertificate = "kongServiceKongCertificateRef"
	// IndexFieldKongServiceOnReferencedKongCACertificates is the index field for KongService -> KongCACertificate (caCertificateRefs).
	IndexFieldKongServiceOnReferencedKongCACertificates = "kongServiceKongCACertificateRefs"
)

// OptionsForKongService returns required Index options for KongService reconciler.
func OptionsForKongService(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongService{},
			Field:          IndexFieldKongServiceOnReferencedPluginNames,
			ExtractValueFn: kongServiceUsesPlugins,
		},
		{
			Object:         &configurationv1alpha1.KongService{},
			Field:          IndexFieldKongServiceOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongService](cl),
		},
		{
			Object:         &configurationv1alpha1.KongService{},
			Field:          IndexFieldKongServiceOnReferencedKongCertificate,
			ExtractValueFn: kongServiceRefersToKongCertificate,
		},
		{
			Object:         &configurationv1alpha1.KongService{},
			Field:          IndexFieldKongServiceOnReferencedKongCACertificates,
			ExtractValueFn: kongServiceRefersToKongCACertificates,
		},
	}
}

func kongServiceUsesPlugins(object client.Object) []string {
	svc, ok := object.(*configurationv1alpha1.KongService)
	if !ok {
		return nil
	}

	return metadata.ExtractPluginsWithNamespaces(svc)
}

// kongServiceRefersToKongCertificate extracts the "namespace/name" key for the
// KongCertificate referenced via spec.clientCertificateRef.
func kongServiceRefersToKongCertificate(object client.Object) []string {
	svc, ok := object.(*configurationv1alpha1.KongService)
	if !ok || svc.Spec.ClientCertificateRef == nil {
		return nil
	}
	ns := svc.Namespace
	if svc.Spec.ClientCertificateRef.Namespace != nil && *svc.Spec.ClientCertificateRef.Namespace != "" {
		ns = *svc.Spec.ClientCertificateRef.Namespace
	}
	return []string{ns + "/" + svc.Spec.ClientCertificateRef.Name}
}

// kongServiceRefersToKongCACertificates extracts "namespace/name" keys for each
// KongCACertificate in spec.caCertificateRefs.
func kongServiceRefersToKongCACertificates(object client.Object) []string {
	svc, ok := object.(*configurationv1alpha1.KongService)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(svc.Spec.CACertificateRefs))
	for _, ref := range svc.Spec.CACertificateRefs {
		ns := svc.Namespace
		if ref.Namespace != nil && *ref.Namespace != "" {
			ns = *ref.Namespace
		}
		result = append(result, ns+"/"+ref.Name)
	}
	return result
}
