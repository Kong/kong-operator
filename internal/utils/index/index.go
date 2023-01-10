package index

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

const (
	// DataplaneNameIndex is the key to be used to access the .spec.dataplaneName indexed values
	DataplaneNameIndex = "dataplane"
)

// IndexDataPlaneNameOnControlPlane indexes the ControlPlane .spec.dataplaneName field
// on the "dataplane" key.
func IndexDataPlaneNameOnControlPlane(c cache.Cache) error {
	return c.IndexField(context.Background(), &operatorv1alpha1.ControlPlane{}, DataplaneNameIndex, func(o client.Object) []string {
		controlPlane, ok := o.(*operatorv1alpha1.ControlPlane)
		if !ok {
			return []string{}
		}
		if controlPlane.Spec.DataPlane != nil {
			return []string{*controlPlane.Spec.DataPlane}
		}
		return []string{}
	})
}
