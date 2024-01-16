package log

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ctrlruntimelog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/modules/manager/logging"
)

// Info logs a message at the info level.
func Info[T any](log logr.Logger, msg string, rawObj T, keysAndValues ...interface{}) {
	_log(log, logging.InfoLevel, msg, rawObj, keysAndValues...)
}

// Debug logs a message at the debug level.
func Debug[T any](log logr.Logger, msg string, rawObj T, keysAndValues ...interface{}) {
	_log(log, logging.DebugLevel, msg, rawObj, keysAndValues...)
}

// Trace logs a message at the trace level.
func Trace[T any](log logr.Logger, msg string, rawObj T, keysAndValues ...interface{}) {
	_log(log, logging.TraceLevel, msg, rawObj, keysAndValues...)
}

type nameNamespacer interface {
	GetName() string
	GetNamespace() string
}

func keyValuesFromObj[T any](rawObj T) []interface{} {
	if obj, ok := any(rawObj).(nameNamespacer); ok {
		return []interface{}{
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		}
	} else if obj, ok := any((&rawObj)).(nameNamespacer); ok {
		return []interface{}{
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		}
	} else if req, ok := any(rawObj).(reconcile.Request); ok {
		return []interface{}{
			"namespace", req.Namespace,
			"name", req.Name,
		}
	}

	return nil
}

func _log[T any](log logr.Logger, level logging.Level, msg string, rawObj T, keysAndValues ...interface{}) {
	kvs := keyValuesFromObj(rawObj)
	if kvs == nil {
		log.V(level.Value()).Info(
			fmt.Sprintf("unexpected type processed for %s logging: %T, this is a bug!",
				level.String(), rawObj,
			),
		)
		return
	}

	log.V(level.Value()).Info(msg, append(kvs, keysAndValues...)...)
}

// GetLogger returns a configured instance of logger.
func GetLogger(ctx context.Context, controllerName string, developmentMode bool) logr.Logger {
	// if development mode is active, do not add the context to the log line, as we want
	// to have a lighter logging structure
	if developmentMode {
		return ctrlruntimelog.Log.WithName(controllerName)
	}
	return ctrlruntimelog.FromContext(ctx).WithName("controlplane")
}
