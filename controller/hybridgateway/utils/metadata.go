package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
)

// GatewaysSliceToAnnotation converts a slice of Gateway objects to a comma-separated string
// in the format "namespace/name,namespace/name,..."
func GatewaysSliceToAnnotation(gateways []gwtypes.Gateway) string {
	gatewayToString := func(gw gwtypes.Gateway) string {
		return client.ObjectKeyFromObject(&gw).String()
	}

	var result string
	for i, gw := range gateways {
		if i == 0 {
			result += gatewayToString(gw)
			continue
		}
		result += "," + gatewayToString(gw)
	}
	return result
}

// SetMetadata sets the metadata for the given Object
func SetMetadata(owner, obj client.Object, hashSpec string, routeAnnotation string, gatewaysAnnotation string) error {
	obj.SetGenerateName(owner.GetName() + "-")
	obj.SetNamespace(owner.GetNamespace())

	labels := map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.ServiceManagedByLabel,
		consts.GatewayOperatorManagedByNameLabel:      owner.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel: owner.GetNamespace(),
		consts.GatewayOperatorHashSpecLabel:           hashSpec,
	}
	obj.SetLabels(labels)

	annotations := map[string]string{
		consts.GatewayOperatorHybridRouteAnnotation:    routeAnnotation,
		consts.GatewayOperatorHybridGatewaysAnnotation: gatewaysAnnotation,
	}
	obj.SetAnnotations(annotations)

	return controllerutil.SetOwnerReference(owner, obj, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
}
