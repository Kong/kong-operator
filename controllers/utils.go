package controllers

import (
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/manager/logging"
)

// -----------------------------------------------------------------------------
// Private Vars
// -----------------------------------------------------------------------------

const requeueWithoutBackoff = time.Millisecond * 200

// -----------------------------------------------------------------------------
// Private Functions - Logging
// -----------------------------------------------------------------------------

func info(log logr.Logger, msg string, rawOBJ interface{}, keysAndValues ...interface{}) {
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

func debug(log logr.Logger, msg string, rawOBJ interface{}, keysAndValues ...interface{}) {
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
// DeploymentOptions - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func deploymentOptionsDeepEqual(opts1, opts2 *operatorv1alpha1.DeploymentOptions) bool {
	if !reflect.DeepEqual(opts1.ContainerImage, opts2.ContainerImage) {
		return false
	}

	if !reflect.DeepEqual(opts1.Version, opts2.Version) {
		return false
	}

	if !reflect.DeepEqual(opts1.Env, opts2.Env) {
		return false
	}

	if !reflect.DeepEqual(opts1.EnvFrom, opts2.EnvFrom) {
		return false
	}

	return true
}
