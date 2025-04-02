package logging

import "fmt"

// LoggingMode is the type for the logging mode.
type LoggingMode string

func (l LoggingMode) String() string {
	return string(l)
}

const (
	// DevelopmentMode is the development logging mode.
	DevelopmentMode LoggingMode = "development"
	// ProductionMode is the production logging mode.
	ProductionMode LoggingMode = "production"
)

// NewLoggingMode creates a new LoggingMode from a string.
func NewLoggingMode(mode string) (LoggingMode, error) {
	switch mode {
	case string(DevelopmentMode):
		return DevelopmentMode, nil
	case string(ProductionMode):
		return ProductionMode, nil
	default:
		return "", fmt.Errorf("invalid logging mode: %s", mode)
	}
}
