package logging

import (
	"strings"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// -----------------------------------------------------------------------------
// logging - Development mode
// -----------------------------------------------------------------------------

const (
	// the fields are separated by a dash.
	defaultDevConsoleSeparator = " - "
)

var (
	// ISO8601 time encoding (e.g., 2022-08-25T14:05:51.352+0200)\.
	defaultDevTimeEncoder = zapcore.ISO8601TimeEncoder
	// debug, info, error placeholders are capitalized and colored.
	defaultDevEncodeLevel = zapcore.CapitalColorLevelEncoder
	// the logger name is capitalized.
	defaultDevEncodeName = func(loggerName string, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(strings.ToUpper(loggerName))
	}
)

// SetupLogEncoder sets additional logger configuration options when development mode is enabled.
// In this way, the log structure is lighter and more human-friendly when the development mode
// is enabled.
func SetupLogEncoder(loggingMode Mode, options *zap.Options) *zap.Options {
	if loggingMode == DevelopmentMode {
		options.Development = true
		options.TimeEncoder = defaultDevTimeEncoder
		options.EncoderConfigOptions = []zap.EncoderConfigOption{
			func(ec *zapcore.EncoderConfig) {
				ec.ConsoleSeparator = defaultDevConsoleSeparator
				ec.EncodeName = defaultDevEncodeName
				ec.EncodeLevel = defaultDevEncodeLevel
			},
		}
	}
	return options
}
