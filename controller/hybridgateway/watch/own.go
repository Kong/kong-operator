package watch

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// Owns returns a slice of client.Object types that are owned by the provided obj.
// It supports different types of root objects and returns nil if the type is unsupported.
func Owns(obj client.Object) []client.Object {
	switch obj.(type) {
	case *gwtypes.HTTPRoute:
		return []client.Object{
			&configurationv1alpha1.KongRoute{},
			&configurationv1alpha1.KongService{},
			&configurationv1alpha1.KongUpstream{},
			&configurationv1alpha1.KongTarget{},
			&configurationv1alpha1.KongPluginBinding{},
			&configurationv1.KongPlugin{},
		}
	default:
		return nil
	}
}
