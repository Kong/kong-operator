package kic

import "strings"

// SanitizeVersionConstraint is a helper that replaces the basic constraint symbols
// with textual ones, to be used as suffixes on files and functions naming.
// The complete list of constraint symbols can be found here:
// https://github.com/Masterminds/semver#basic-comparisons
func SanitizeVersionConstraint(constraint string) string {
	constraint = strings.ReplaceAll(constraint, " ", "")
	constraint = strings.ReplaceAll(constraint, ",", "_")
	constraint = strings.ReplaceAll(constraint, ".", "_")
	constraint = strings.ReplaceAll(constraint, "<=", "le")
	constraint = strings.ReplaceAll(constraint, ">=", "ge")
	constraint = strings.ReplaceAll(constraint, "<", "lt")
	constraint = strings.ReplaceAll(constraint, ">", "gt")
	constraint = strings.ReplaceAll(constraint, "!=", "ne")
	constraint = strings.ReplaceAll(constraint, "=", "eq")

	return constraint
}
