package index

import (
	"strings"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

const (
	// KongPluginInstallationsIndex is the key to be used to access the .spec.pluginsToInstall indexed values,
	// in a form of list of namespace/name strings.
	KongPluginInstallationsIndex = "KongPluginInstallations"
	// DataPlaneOnOwnerGatewayIndex is the key to index the DataPlanes based on the owner Gateways.
	DataPlaneOnOwnerGatewayIndex = "DataPlaneOnOwnerGateway"
)

// DataPlaneFlags contains flags that control which indexes are created for the DataPlane object.
type DataPlaneFlags struct {
	KongPluginInstallationControllerEnabled bool
	KonnectControllersEnabled               bool
	GatewayAPIGatewayControllerEnabled      bool
}

// OptionsForDataPlane returns indexing options for the DataPlane object,
// based on the provided flags.
func OptionsForDataPlane(flags DataPlaneFlags) []Option {
	var opts []Option

	if flags.KonnectControllersEnabled {
		opts = append(opts, Option{
			Object:         &operatorv1beta1.DataPlane{},
			Field:          KonnectExtensionIndex,
			ExtractValueFn: extendableOnKonnectExtension[*operatorv1beta1.DataPlane](),
		})
	}
	if flags.KongPluginInstallationControllerEnabled {
		opts = append(opts, Option{
			Object:         &operatorv1beta1.DataPlane{},
			Field:          KongPluginInstallationsIndex,
			ExtractValueFn: kongPluginInstallationsOnDataPlane,
		})
	}
	if flags.GatewayAPIGatewayControllerEnabled {
		opts = append(opts, Option{
			Object:         &operatorv1beta1.DataPlane{},
			Field:          DataPlaneOnOwnerGatewayIndex,
			ExtractValueFn: OwnerGatewayOnDataPlane,
		})
	}

	return opts
}

// kongPluginInstallationsOnDataPlane indexes the DataPlane .spec.pluginsToInstall field
// on the "kongPluginInstallations" key.
func kongPluginInstallationsOnDataPlane(o client.Object) []string {
	dp, ok := o.(*operatorv1beta1.DataPlane)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(dp.Spec.PluginsToInstall))
	for _, kpi := range dp.Spec.PluginsToInstall {
		if kpi.Namespace == "" {
			kpi.Namespace = dp.Namespace
		}
		result = append(result, kpi.Namespace+"/"+kpi.Name)
	}
	return result
}

// OwnerGatewayOnDataPlane indexes the DataPlane based on its owner Gateway reference.
// It returns a 1 element slice with "namespace/name" of the owner Gateway,
// or an empty slice if no such owner reference exists.
func OwnerGatewayOnDataPlane(o client.Object) []string {
	dp, ok := o.(*operatorv1beta1.DataPlane)
	if !ok {
		return nil
	}
	ownerGateway, ok := lo.Find(dp.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
		return ref.Kind == "Gateway" &&
			strings.HasPrefix(ref.APIVersion, gatewayv1.GroupName)
	})
	if !ok {
		return []string{}
	}

	return []string{dp.Namespace + "/" + ownerGateway.Name}
}
