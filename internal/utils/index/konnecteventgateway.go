package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectEventGatewayOnAPIAuthConfiguration is the index field for KonnectEventGateway -> APIAuthConfiguration.
	IndexFieldKonnectEventGatewayOnAPIAuthConfiguration = "konnectEventGatewayAPIAuthConfigurationRef"
)

// OptionsForKonnectEventGateway returns required Index options for KonnectEventGateway reconciler.
func OptionsForKonnectEventGateway() []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.KonnectEventGateway{},
			Field:          IndexFieldKonnectEventGatewayOnAPIAuthConfiguration,
			ExtractValueFn: konnectEventGatewayAPIAuthConfigurationRef,
		},
	}
}

func konnectEventGatewayAPIAuthConfigurationRef(object client.Object) []string {
	eg, ok := object.(*konnectv1alpha1.KonnectEventGateway)
	if !ok {
		return nil
	}
	ns := eg.GetNamespace()
	if eg.Spec.KonnectConfiguration.Namespace != nil {
		ns = *eg.Spec.KonnectConfiguration.Namespace
	}
	return []string{ns + "/" + eg.Spec.KonnectConfiguration.Name}
}
