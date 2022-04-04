package controllers

import (
	"fmt"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/internal/manager/logging"
)

// -----------------------------------------------------------------------------
// Private Functions - Logging
// -----------------------------------------------------------------------------

func info(log logr.Logger, msg string, rawOBJ interface{}, keysAndValues ...interface{}) { //nolint:deadcode,unused //FIXME
	if obj, ok := rawOBJ.(client.Object); ok {
		kvs := append([]interface{}{"namespace", obj.GetNamespace(), "name", obj.GetName()}, keysAndValues...)
		log.V(logging.InfoLevel).Info(msg, kvs...)
	} else if req, ok := rawOBJ.(reconcile.Request); ok {
		kvs := append([]interface{}{"namespace", req.Namespace, "name", req.Name}, keysAndValues...)
		log.V(logging.InfoLevel).Info(msg, kvs...)
	} else {
		log.V(logging.InfoLevel).Info(fmt.Sprintf("unexpected type processed for info logging: %T, this is a bug!", rawOBJ))
	}
}

func debug(log logr.Logger, msg string, rawOBJ interface{}, keysAndValues ...interface{}) { //nolint:unparam //FIXME
	if obj, ok := rawOBJ.(client.Object); ok {
		kvs := append([]interface{}{"namespace", obj.GetNamespace(), "name", obj.GetName()}, keysAndValues...)
		log.V(logging.DebugLevel).Info(msg, kvs...)
	} else if req, ok := rawOBJ.(reconcile.Request); ok {
		kvs := append([]interface{}{"namespace", req.Namespace, "name", req.Name}, keysAndValues...)
		log.V(logging.DebugLevel).Info(msg, kvs...)
	} else {
		log.V(logging.DebugLevel).Info(fmt.Sprintf("unexpected type processed for debug logging: %T, this is a bug!", rawOBJ))
	}
}

// -----------------------------------------------------------------------------
// Private Functions - Kubernetes Object Metadata
// -----------------------------------------------------------------------------

func setObjectOwner(owner client.Object, obj client.Object) {
	foundOwnerRef := false
	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.UID == owner.GetUID() {
			foundOwnerRef = true
		}
	}
	if !foundOwnerRef {
		obj.SetOwnerReferences(append(obj.GetOwnerReferences(), createObjectOwnerRef(owner)))
	}
}

func createObjectOwnerRef(obj client.Object) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: getObjectAPIVersion(obj),
		Kind:       obj.GetObjectKind().GroupVersionKind().Kind,
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
	}
}

func getObjectAPIVersion(obj client.Object) string {
	return fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Group, obj.GetObjectKind().GroupVersionKind().Version)
}
