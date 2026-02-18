package extensions

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// GetAllDataPlaneMetricsExtensionRefsForControlPlane gets all DataPlaneMetricsExtension
// refs set in the ControlPlane's spec.
func GetAllDataPlaneMetricsExtensionRefsForControlPlane(controlplane *gwtypes.ControlPlane) []commonv1alpha1.ExtensionRef {
	return lo.Filter(controlplane.Spec.Extensions,
		func(ef commonv1alpha1.ExtensionRef, _ int) bool {
			return ef.Kind == operatorv1alpha1.DataPlaneMetricsExtensionKind &&
				ef.Group == operatorv1alpha1.SchemeGroupVersion.Group
		},
	)
}

// GetAllDataPlaneMetricExtensionsForControlPlane returns all DataPlaneMetricsExtensions
// that are referenced in the ControlPlane's spec.extensions.
func GetAllDataPlaneMetricExtensionsForControlPlane(
	ctx context.Context, cl client.Client, controlplane *gwtypes.ControlPlane,
) ([]operatorv1alpha1.DataPlaneMetricsExtension, error) {
	extensionsRefs := GetAllDataPlaneMetricsExtensionRefsForControlPlane(controlplane)

	// For all the refs, get the DataPlaneMetricsExtensions using the client.
	extensions := make([]operatorv1alpha1.DataPlaneMetricsExtension, 0, len(extensionsRefs))
	for _, ext := range extensionsRefs {
		metricsExt := operatorv1alpha1.DataPlaneMetricsExtension{}
		nn := types.NamespacedName{
			Name:      ext.Name,
			Namespace: controlplane.Namespace,
		}
		if ext.Namespace != nil {
			nn.Namespace = *ext.Namespace
		}
		if err := cl.Get(ctx, nn, &metricsExt); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to get %s %s", operatorv1alpha1.DataPlaneMetricsExtensionKind, nn)
			}
			return nil, err
		}
		extensions = append(extensions, metricsExt)
	}
	return extensions, nil
}
