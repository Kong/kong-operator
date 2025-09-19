package extensions

import (
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
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
	return append(newExtensions, extensions...)
}

// MergeExtensionsForDataPlane is a wrapper around MergeExtensions for places where
// we do not have an actual object to work on.
func MergeExtensionsForDataPlane(
	defaultExtensions, extensions []commonv1alpha1.ExtensionRef,
) []commonv1alpha1.ExtensionRef {
	dataplane := operatorv1beta1.DataPlane{
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Extensions: extensions,
			},
		},
	}
	return MergeExtensions(defaultExtensions, &dataplane)
}
