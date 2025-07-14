package index

import (
	"context"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/pkg/extensions"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// KonnectExtensionIndex is the key to be used to access the .spec.extensions indexed values, in a form of list of
	// namespace/name strings. It can be used with object types that support extensions (e.g. DataPlane, ControlPlane,
	// GatewayConfiguration).
	KonnectExtensionIndex = "KonnectExtension"
)

// extendableOnKonnectExtension indexes the Object .spec.extensions field on the "KonnectExtension" key. It can be used
// with object types that support extensions (e.g. DataPlane, ControlPlane, GatewayConfiguration).
func extendableOnKonnectExtension[T extensions.ExtendableT]() client.IndexerFunc {
	return func(o client.Object) []string {
		obj, ok := o.(T)
		if !ok {
			return nil
		}

		result := []string{}
		if len(obj.GetExtensions()) > 0 {
			for _, ext := range obj.GetExtensions() {
				namespace := obj.GetNamespace()
				if ext.Group != konnectv1alpha1.SchemeGroupVersion.Group ||
					ext.Kind != konnectv1alpha2.KonnectExtensionKind {
					continue
				}
				if ext.Namespace != nil && *ext.Namespace != namespace {
					continue
				}
				result = append(result, namespace+"/"+ext.Name)
			}
		}
		return result
	}
}

// ExtendableObjectListT is an interface that defines the list types that can be
// extended with KonnectExtension objects.
type ExtendableObjectListT interface {
	client.ObjectList

	*operatorv1beta1.DataPlaneList |
		*gwtypes.ControlPlaneList |
		*operatorv1beta1.GatewayConfigurationList
}

// ListObjectsReferencingKonnectExtension returns a handler.MapFunc that lists objects of the given type that reference
// the given KonnectExtension.  It can be used with object types that support extensions (e.g. DataPlane, ControlPlane,
// GatewayConfiguration).
func ListObjectsReferencingKonnectExtension[t ExtendableObjectListT](
	c client.Client,
	objList t,
) handler.TypedMapFunc[*konnectv1alpha2.KonnectExtension, reconcile.Request] {
	return func(ctx context.Context, ext *konnectv1alpha2.KonnectExtension) []reconcile.Request {
		logger := ctrllog.FromContext(ctx)

		if err := c.List(ctx, objList, client.MatchingFields{
			KonnectExtensionIndex: ext.Namespace + "/" + ext.Name,
		}); err != nil {
			logger.Error(err, "Failed to list in watch", "extensionKind", konnectv1alpha2.KonnectExtensionKind)
			return nil
		}

		var items []client.Object
		switch o := any(objList).(type) {
		case *operatorv1beta1.DataPlaneList:
			items = lo.Map(o.Items, func(dp operatorv1beta1.DataPlane, _ int) client.Object {
				return &dp
			})
		case *gwtypes.ControlPlaneList:
			items = lo.Map(o.Items, func(cp gwtypes.ControlPlane, _ int) client.Object {
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
