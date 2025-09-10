package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
)

// SetMetadata sets the metadata for the given Object
func SetMetadata(owner, obj client.Object, hashSpec string) error {
	obj.SetGenerateName(owner.GetName() + "-")
	obj.SetNamespace(owner.GetNamespace())

	labels := map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.ServiceManagedByLabel,
		consts.GatewayOperatorManagedByNameLabel:      owner.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel: owner.GetNamespace(),
		consts.GatewayOperatorHashSpecLabel:           hashSpec,
	}
	obj.SetLabels(labels)

	return controllerutil.SetOwnerReference(owner, obj, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
}
