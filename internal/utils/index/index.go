package index

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

const (
	// DataPlaneNameIndex is the key to be used to access the .spec.dataplaneName indexed values
	DataPlaneNameIndex = "dataplane"
)

// DataPlaneNameOnControlPlane indexes the ControlPlane .spec.dataplaneName field
// on the "dataplane" key.
func DataPlaneNameOnControlPlane(ctx context.Context, c cache.Cache) error {
	if _, err := c.GetInformer(ctx, &operatorv1beta1.ControlPlane{}); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to get informer for v1beta1 ControlPlane: %w, disabling indexing DataPlanes for ControlPlanes' .spec.dataplaneName", err)
	}

	return c.IndexField(ctx, &operatorv1beta1.ControlPlane{}, DataPlaneNameIndex, func(o client.Object) []string {
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
