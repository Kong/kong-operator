package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
)

const (
	// KongPluginInstallationsIndex is the key to be used to access the .spec.pluginsToInstall indexed values,
	// in a form of list of namespace/name strings.
	KongPluginInstallationsIndex = "KongPluginInstallations"
)

// DataPlaneFlags contains flags that control which indexes are created for the DataPlane object.
type DataPlaneFlags struct {
	KongPluginInstallationControllerEnabled bool
	KonnectControllersEnabled               bool
}

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
