package konnect

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane is the index field for KonnectCloudGatewayDataPlaneGroupConfiguration -> KonnectGatewayControlPlane.
	IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane = "konnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlaneRef"
	// IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef is the index field for KonnectCloudGatewayDataPlaneGroupConfiguration -> KonnectNetwork.
	IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef = "konnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef"
)

// IndexOptionsForKonnectCloudGatewayDataPlaneGroupConfiguration returns required Index options for KonnectCloudGatewayDataPlaneGroupConfiguration reconciler.
func IndexOptionsForKonnectCloudGatewayDataPlaneGroupConfiguration(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{},
			IndexField:   IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](cl),
		},
		{
			IndexObject:  &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{},
			IndexField:   IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef,
			ExtractValue: konnectCloudGatewayNetworkDataPlaneGroupConfigurationRef,
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
