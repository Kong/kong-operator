package index

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

const (
	// DataPlaneNameIndex is the key to be used to access the .spec.dataplaneName indexed values
	DataPlaneNameIndex = "dataplane"
)

// IndexDataPlaneNameOnControlPlane indexes the ControlPlane .spec.dataplaneName field
// on the "dataplane" key.
func IndexDataPlaneNameOnControlPlane(c cache.Cache) error {
	return c.IndexField(context.Background(), &operatorv1beta1.ControlPlane{}, DataPlaneNameIndex, func(o client.Object) []string {
		controlPlane, ok := o.(*operatorv1beta1.ControlPlane)
		if !ok {
			return []string{}
		}
		if controlPlane.Spec.DataPlane != nil {
			return []string{*controlPlane.Spec.DataPlane}
		}
		return []string{}
	})
}
