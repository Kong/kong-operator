package extensions

import (
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
)

// MergeExtensions merges the default extensions with the extensions provided by the user.
// The provided extensions take precedence over the default extensions: in case
// the user provides an extension that is also present in the default extensions,
// the user's extension will be used.
func MergeExtensions(defaultExtensions, extensions []commonv1alpha1.ExtensionRef) []commonv1alpha1.ExtensionRef {
	newExtensions := make([]commonv1alpha1.ExtensionRef, 0)
	for _, dext := range defaultExtensions {
		if _, found := lo.Find(extensions, func(ext commonv1alpha1.ExtensionRef) bool {
			return ext.Group == dext.Group && ext.Kind == dext.Kind
		}); !found {
			newExtensions = append(newExtensions, dext)
		}
	}
	return append(newExtensions, extensions...)
}
