package index

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// DataPlaneNameIndex is the key to be used to access the .spec.dataplaneName indexed values.
	DataPlaneNameIndex = "dataplane"

	// KongPluginInstallationsIndex is the key to be used to access the .spec.pluginsToInstall indexed values,
	// in a form of list of namespace/name strings.
	KongPluginInstallationsIndex = "KongPluginInstallations"

	// KonnectExtensionIndex is the key to be used to access the .spec.extensions indexed values,
	// in a form of list of namespace/name strings.
	KonnectExtensionIndex = "KonnectExtension"
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

// KongPluginInstallationsOnDataPlane indexes the DataPlane .spec.pluginsToInstall field
// on the "kongPluginInstallations" key.
func KongPluginInstallationsOnDataPlane(ctx context.Context, c cache.Cache) error {
	if _, err := c.GetInformer(ctx, &operatorv1beta1.DataPlane{}); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to get informer for v1beta1 DataPlane: %w, disabling indexing KongPluginInstallations for DataPlanes' .spec.pluginsToInstall", err)
	}
	return c.IndexField(
		ctx,
		&operatorv1beta1.DataPlane{},
		KongPluginInstallationsIndex,
		func(o client.Object) []string {
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
		},
	)
}

// DataPlaneOnDataPlaneKonnecExtension indexes the DataPlane .spec.extensions field
// on the "KonnectExtension" key.
func DataPlaneOnDataPlaneKonnecExtension(ctx context.Context, c cache.Cache) error {
	if _, err := c.GetInformer(ctx, &operatorv1beta1.DataPlane{}); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to get informer for v1alpha1 KonnectExtension: %w, disabling indexing KonnectExtensions for DataPlanes' .spec.extensions", err)
	}
	return c.IndexField(
		ctx,
		&operatorv1beta1.DataPlane{},
		KonnectExtensionIndex,
		func(o client.Object) []string {
			dp, ok := o.(*operatorv1beta1.DataPlane)
			if !ok {
				return nil
			}
			result := []string{}
			if len(dp.Spec.Extensions) > 0 {
				for _, ext := range dp.Spec.Extensions {
					namespace := dp.Namespace
					if ext.Group != operatorv1alpha1.SchemeGroupVersion.Group ||
						ext.Kind != konnectv1alpha1.KonnectExtensionKind {
						continue
					}
					if ext.Namespace != nil && *ext.Namespace != namespace {
						continue
					}
					result = append(result, namespace+"/"+ext.NamespacedRef.Name)
				}
			}
			return result
		},
	)
}
