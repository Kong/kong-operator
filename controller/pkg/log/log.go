package log

import (
	"context"

	"github.com/go-logr/logr"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

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

// Error logs a message at the error level.
func Error[T any](log logr.Logger, err error, msg string, rawObj T, keysAndValues ...interface{}) {
	log.Error(err, msg, keysAndValues...)
}

func _log[T any](log logr.Logger, level logging.Level, msg string, rawObj T, keysAndValues ...interface{}) { //nolint:unparam
	log.V(level.Value()).Info(msg, keysAndValues...)
}

// GetLogger returns a configured instance of logger.
func GetLogger(ctx context.Context, controllerName string, developmentMode bool) logr.Logger {
	// if development mode is active, do not add the context to the log line, as we want
	// to have a lighter logging structure
	if developmentMode {
		return ctrllog.Log.WithName(controllerName)
	}
	return ctrllog.FromContext(ctx).WithName(controllerName)
}
