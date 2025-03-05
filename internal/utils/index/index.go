package index

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/controller/pkg/extensions"
	"github.com/samber/lo"

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

// ExtendableOnKonnectExtension indexes the Object .spec.extensions field
// on the "KonnectExtension" key.
func ExtendableOnKonnectExtension[T extensions.ExtendableT](ctx context.Context, c cache.Cache, obj T) error {
	if _, err := c.GetInformer(ctx, obj); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to get informer for %v: %w", obj, err)
	}
	return c.IndexField(
		ctx,
		obj,
		KonnectExtensionIndex,
		func(o client.Object) []string {
			obj, ok := o.(T)
			if !ok {
				return nil
			}

			result := []string{}
			if len(obj.GetExtensions()) > 0 {
				for _, ext := range obj.GetExtensions() {
					namespace := obj.GetNamespace()
					if ext.Group != konnectv1alpha1.SchemeGroupVersion.Group ||
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

// ExtendableObjectListT is an interface that defines the list types that can be
// extended with KonnectExtension objects.
type ExtendableObjectListT interface {
	client.ObjectList

	*operatorv1beta1.DataPlaneList |
		*operatorv1beta1.ControlPlaneList |
		*operatorv1beta1.GatewayConfigurationList
}

// ListObjectsReferencingKonnectExtension returns a handler.MapFunc that lists
// objects of the given type that reference the given KonnectExtension.
func ListObjectsReferencingKonnectExtension[t ExtendableObjectListT](
	c client.Client,
	objList t,
) handler.TypedMapFunc[*konnectv1alpha1.KonnectExtension, reconcile.Request] {
	return func(
		ctx context.Context, ext *konnectv1alpha1.KonnectExtension,
	) []reconcile.Request {
		logger := ctrllog.FromContext(ctx)

		if err := c.List(ctx, objList, client.MatchingFields{
			KonnectExtensionIndex: ext.Namespace + "/" + ext.Name,
		}); err != nil {
			logger.Error(err, "Failed to list  in watch", "extensionKind", konnectv1alpha1.KonnectExtensionKind)
			return nil
		}

		var items []client.Object
		switch o := any(objList).(type) {
		case *operatorv1beta1.DataPlaneList:
			items = lo.Map(o.Items, func(dp operatorv1beta1.DataPlane, _ int) client.Object {
				return &dp
			})
		case *operatorv1beta1.ControlPlaneList:
			items = lo.Map(o.Items, func(cp operatorv1beta1.ControlPlane, _ int) client.Object {
				return &cp
			})
		case *operatorv1beta1.GatewayConfigurationList:
			items = lo.Map(o.Items, func(gc operatorv1beta1.GatewayConfiguration, _ int) client.Object {
				return &gc
			})
		default:
			// This should never happen.
			panic("object not implemented")
		}

		return lo.Map(items, func(obj client.Object, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(obj),
			}
		})
	}
}
