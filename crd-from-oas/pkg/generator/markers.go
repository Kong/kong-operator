package generator

import (
	"fmt"
	"strconv"
)

const (
	kbOptional = "+optional"
	kbRequired = "+required"

	kbValidationMaxLengthFmt     = "+kubebuilder:validation:MaxLength=%d"
	kbValidationMinLengthFmt     = "+kubebuilder:validation:MinLength=%d"
	kbValidationPatternFmt       = "+kubebuilder:validation:Pattern=`%s`"
	kbValidationMinimumFmt       = "+kubebuilder:validation:Minimum=%s"
	kbValidationMaximumFmt       = "+kubebuilder:validation:Maximum=%s"
	kbValidationEnumFmt          = "+kubebuilder:validation:Enum=%s"
	kbValidationMaxPropertiesFmt = "+kubebuilder:validation:MaxProperties=%d"
	kbValidationMaxItemsFmt      = "+kubebuilder:validation:MaxItems=%d"
)

func markerOptional() string { return kbOptional }
func markerRequired() string { return kbRequired }

func markerValidationMaxLength(v int) string     { return fmt.Sprintf(kbValidationMaxLengthFmt, v) }
func markerValidationMinLength(v int) string     { return fmt.Sprintf(kbValidationMinLengthFmt, v) }
func markerValidationPattern(v string) string    { return fmt.Sprintf(kbValidationPatternFmt, v) }
func markerValidationEnum(v string) string       { return fmt.Sprintf(kbValidationEnumFmt, v) }
func markerValidationMaxProperties(v int) string { return fmt.Sprintf(kbValidationMaxPropertiesFmt, v) }
func markerValidationMaxItems(v int) string      { return fmt.Sprintf(kbValidationMaxItemsFmt, v) }

// markerValidationMinimum/Maximum format via [strconv.FormatFloat] (not fmt's %v/%g) because %g
// switches to scientific notation for large magnitudes (e.g. 2147483646 -> 2.147483646e+09),
// which kubebuilder's CRD schema generator does not accept as a plain integer bound.
func markerValidationMinimum(v float64) string {
	return fmt.Sprintf(kbValidationMinimumFmt, strconv.FormatFloat(v, 'f', -1, 64))
}

func markerValidationMaximum(v float64) string {
	return fmt.Sprintf(kbValidationMaximumFmt, strconv.FormatFloat(v, 'f', -1, 64))
}
