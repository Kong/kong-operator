package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane is the index field for KonnectCloudGatewayDataPlaneGroupConfiguration -> KonnectGatewayControlPlane.
	IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane = "konnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlaneRef"
	// IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef is the index field for KonnectCloudGatewayDataPlaneGroupConfiguration -> KonnectNetwork.
	IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef = "konnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef"
)

// OptionsForKonnectCloudGatewayDataPlaneGroupConfiguration returns required Index options for KonnectCloudGatewayDataPlaneGroupConfiguration reconciler.
func OptionsForKonnectCloudGatewayDataPlaneGroupConfiguration(cl client.Client) []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{},
			Field:          IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](cl),
		},
		{
			Object:         &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{},
			Field:          IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef,
			ExtractValueFn: konnectCloudGatewayNetworkDataPlaneGroupConfigurationRef,
		},
	}
}

func konnectCloudGatewayNetworkDataPlaneGroupConfigurationRef(object client.Object) []string {
	dpg, ok := object.(*konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration)
	if !ok {
		return nil
	}

	return lo.FilterMap(
		dpg.Spec.DataplaneGroups,
		func(dpg konnectv1alpha1.KonnectConfigurationDataPlaneGroup, _ int) (string, bool) {
			switch dpg.NetworkRef.Type {
			case commonv1alpha1.ObjectRefTypeNamespacedRef:
				return dpg.NetworkRef.NamespacedRef.Name, true
			default:
				return "", false
			}
		},
	)
}
