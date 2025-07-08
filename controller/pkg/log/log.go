package log

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/modules/manager/logging"
)

// Info logs a message at the info level.
func Info(logger logr.Logger, msg string, keysAndValues ...any) {
	_log(logger, logging.InfoLevel, msg, keysAndValues...)
}

// Debug logs a message at the debug level.
func Debug(logger logr.Logger, msg string, keysAndValues ...any) {
	_log(logger, logging.DebugLevel, msg, keysAndValues...)
}

// Trace logs a message at the trace level.
func Trace(logger logr.Logger, msg string, keysAndValues ...any) {
	_log(logger, logging.TraceLevel, msg, keysAndValues...)
}

// Error logs a message at the error level.
func Error(logger logr.Logger, err error, msg string, keysAndValues ...any) {
	if !oddKeyValues(logger, msg, keysAndValues...) {
		return
	}
	logger.Error(err, msg, keysAndValues...)
}

func _log(logger logr.Logger, level logging.Level, msg string, keysAndValues ...any) {
	if !oddKeyValues(logger, msg, keysAndValues...) {
		return
	}
	logger.V(level.Value()).
		Info(msg, keysAndValues...)
}

func oddKeyValues(logger logr.Logger, msg string, keysAndValues ...any) bool {
	if len(keysAndValues)%2 != 0 {
		err := fmt.Errorf("log message has odd number of arguments")
		logger.Error(err, msg)
		return false
	}
	return true
}

// GetLogger returns a configured instance of logger.
func GetLogger(ctx context.Context, controllerName string, loggingMode logging.Mode) logr.Logger {
	// if development mode is active, do not add the context to the log line, as we want
	// to have a lighter logging structure
	if loggingMode == logging.DevelopmentMode {
		return ctrllog.Log.WithName(controllerName)
	}
	return ctrllog.FromContext(ctx).WithName(controllerName)
}
