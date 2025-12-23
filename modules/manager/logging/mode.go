package logging

import "fmt"

// Mode is the type for the logging mode.
type Mode string

// String returns the string representation of the Mode.
func (l Mode) String() string {
	return string(l)
}

const (
	// DevelopmentMode is the development logging mode.
	DevelopmentMode Mode = "development"
	// ProductionMode is the production logging mode.
	ProductionMode Mode = "production"
)

// NewMode creates a new Mode from a string.
func NewMode(mode string) (Mode, error) {
	switch mode {
	case string(DevelopmentMode):
		return DevelopmentMode, nil
	case string(ProductionMode):
		return ProductionMode, nil
	default:
		return "", fmt.Errorf("invalid logging mode: %s", mode)
	}
}
