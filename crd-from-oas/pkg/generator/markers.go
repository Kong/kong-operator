package generator

import "fmt"

const (
	kbOptional = "+optional"
	kbRequired = "+required"

	kbValidationMaxLengthFmt = "+kubebuilder:validation:MaxLength=%d"
	kbValidationMinLengthFmt = "+kubebuilder:validation:MinLength=%d"
	kbValidationPatternFmt   = "+kubebuilder:validation:Pattern=`%s`"
	kbValidationMinimumFmt   = "+kubebuilder:validation:Minimum=%v"
	kbValidationMaximumFmt   = "+kubebuilder:validation:Maximum=%v"
	kbValidationEnumFmt      = "+kubebuilder:validation:Enum=%s"

	kbDefaultBoolFmt   = "+kubebuilder:default=%t"
	kbDefaultStringFmt = "+kubebuilder:default=%s"
)

func markerOptional() string { return kbOptional }
func markerRequired() string { return kbRequired }

func markerValidationMaxLength(v int) string  { return fmt.Sprintf(kbValidationMaxLengthFmt, v) }
func markerValidationMinLength(v int) string  { return fmt.Sprintf(kbValidationMinLengthFmt, v) }
func markerValidationPattern(v string) string { return fmt.Sprintf(kbValidationPatternFmt, v) }
func markerValidationMinimum(v any) string    { return fmt.Sprintf(kbValidationMinimumFmt, v) }
func markerValidationMaximum(v any) string    { return fmt.Sprintf(kbValidationMaximumFmt, v) }
func markerValidationEnum(v string) string    { return fmt.Sprintf(kbValidationEnumFmt, v) }

func markerDefaultBool(v bool) string     { return fmt.Sprintf(kbDefaultBoolFmt, v) }
func markerDefaultString(v string) string { return fmt.Sprintf(kbDefaultStringFmt, v) }
