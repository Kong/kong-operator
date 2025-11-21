package extensions

import (
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	konnectextensionpkg "github.com/kong/kong-operator/controller/pkg/extensions/konnect"
)

// MergeExtensions merges the default extensions with the extensions from the
// provided extendable object.
// The provided extensions take precedence over the default extensions: in case
// the user provides an extension that is also present in the default extensions,
// the user's extension will be used.
func MergeExtensions[
	extendable Extendable,
](
	defaultExtensions []commonv1alpha1.ExtensionRef,
	extended extendable,
) []commonv1alpha1.ExtensionRef {
	var (
		newExtensions = make([]commonv1alpha1.ExtensionRef, 0)
		extensions    = extended.GetExtensions()
	)
	extensionMatcher := func(dext commonv1alpha1.ExtensionRef) func(commonv1alpha1.ExtensionRef) bool {
		return func(ext commonv1alpha1.ExtensionRef) bool {
			return ext.Group == dext.Group && ext.Kind == dext.Kind
		}
	}

	for _, dext := range defaultExtensions {
		// Perform type specific checks for extensions.
		// This is necessary to allow users to define extensions at the shared
		// GatewayConfiguration level in the API and delegate the logic of merging
		// them to the operator.

		if dext.Group == operatorv1alpha1.SchemeGroupVersion.Group &&
			dext.Kind == operatorv1alpha1.DataPlaneMetricsExtensionKind {

			if _, ok := any(extended).(*operatorv1beta1.DataPlane); ok {
				// Do not add the DataPlaneMetricsExtension to the DataPlane.
				// That's a ControlPlane extension.
				continue
			}
		}

		if !lo.ContainsBy(extensions, extensionMatcher(dext)) {
			newExtensions = append(newExtensions, dext)
		}
	}
	if len(extensions) == 0 && len(newExtensions) == 0 {
		return nil
	}
	return append(newExtensions, extensions...)
}

// MergeExtensionsForDataPlane is a wrapper around MergeExtensions for places where
// we do not have an actual object to work on.
func MergeExtensionsForDataPlane(
	configExtensions []commonv1alpha1.ExtensionRef,
	konnectExtension *konnectv1alpha2.KonnectExtension,
) []commonv1alpha1.ExtensionRef {
	// Initialize the Konnect Extension by using the provided konnectExtension parameter.
	// In case the GatewayConfiguration statically defines a KonnectExtension in its
	// extensions list, the provided one will take precedence and be used.
	extensionList := []commonv1alpha1.ExtensionRef{}
	konnectExtensionRef := konnectextensionpkg.KonnectExtensionToExtensionRef(konnectExtension)
	if konnectExtensionRef != nil {
		extensionList = append(extensionList, *konnectExtensionRef)
	}
	return mergeExtensionsForDataPlane(configExtensions, extensionList)
}

func mergeExtensionsForDataPlane(
	configExtensions []commonv1alpha1.ExtensionRef,
	extensions []commonv1alpha1.ExtensionRef,
) []commonv1alpha1.ExtensionRef {
	dataplane := operatorv1beta1.DataPlane{
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Extensions: extensions,
			},
		},
	}
	return MergeExtensions(configExtensions, &dataplane)
}
