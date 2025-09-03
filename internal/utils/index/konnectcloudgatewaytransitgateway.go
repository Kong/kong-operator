package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/apis/v1alpha1"
)

const (
	// IndexFieldKonnectCloudGatewayTransitGatewayOnKonnectNetworkRef is the index field for KonnectCloudGatewayTransitGateway -> KonnectCloudGatewayNetwork.
	IndexFieldKonnectCloudGatewayTransitGatewayOnKonnectNetworkRef = "KonnectCloudGatewayTransitGatewayOnKonnectNetworkRef"
)

func OptionsForKonnectCloudGatewayTransitGateway() []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.KonnectCloudGatewayTransitGateway{},
			Field:          IndexFieldKonnectCloudGatewayTransitGatewayOnKonnectNetworkRef,
			ExtractValueFn: konnectCloudGatewayTransitGatewayNetworkRef,
		},
	}
}

func konnectCloudGatewayTransitGatewayNetworkRef(obj client.Object) []string {
	tg, ok := obj.(*konnectv1alpha1.KonnectCloudGatewayTransitGateway)
	if !ok {
		return nil
	}
	if tg.Spec.NetworkRef.NamespacedRef == nil {
		return nil
	}
	return []string{
		tg.Spec.NetworkRef.NamespacedRef.Name,
	}
}
