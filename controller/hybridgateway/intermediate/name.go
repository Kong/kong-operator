package intermediate

import (
	"strconv"
	"strings"
)

// Name represents a structured naming scheme for Kong entities derived from HTTPRoute resources.
// It provides a hierarchical naming structure with prefix, namespace, name, and optional indexes
// for parent references, rules, and matches. The String() method ensures names stay within
// Kubernetes resource name length limits through intelligent truncation.
type Name struct {
	prefix    string
	namespace string
	name      string
	// indexes represents [parentRefIndex, ruleIndex, matchIndex] for hierarchical identification
	indexes []int
}

// String returns the full name as a dot-separated string, truncating if necessary to stay within
// the 253 character limit for Kubernetes resource names. It preserves the prefix and indexes
// while proportionally truncating namespace and name if needed.
func (n *Name) String() string {
	const maxLen = 253

	parts := []string{n.prefix, n.namespace, n.name}
	for _, idx := range n.indexes {
		parts = append(parts, strconv.Itoa(idx))
	}
	fullName := strings.Join(parts, ".")

	if len(fullName) <= maxLen {
		return fullName
	}

	// Calculate reserved length for prefix and indexes
	reserved := len(n.prefix) + 1 // prefix + dot
	for _, idx := range n.indexes {
		reserved += len(strconv.Itoa(idx)) + 1 // index + dot
	}
	// We need at least one dot between prefix, namespace, name, and indexes
	// Truncate namespace and name proportionally if needed
	remaining := maxLen - reserved

	// Split remaining between namespace and name
	nsMax := remaining / 2
	nameMax := remaining - nsMax

	truncNamespace := n.namespace
	truncName := n.name
	if len(truncNamespace) > nsMax {
		truncNamespace = truncNamespace[:nsMax]
	}
	if len(truncName) > nameMax {
		truncName = truncName[:nameMax]
	}

	parts = []string{n.prefix, truncNamespace, truncName}
	for _, idx := range n.indexes {
		parts = append(parts, strconv.Itoa(idx))
	}
	return strings.Join(parts, ".")
}

// GetParentRefIndex returns the parent reference index from the indexes array.
// Returns -1 if the Name is nil or if no parent reference index is available.
func (n *Name) GetParentRefIndex() int {
	if n == nil || len(n.indexes) < 1 {
		return -1
	}
	return n.indexes[0]
}

// GetRuleIndex returns the rule index from the indexes array.
// Returns -1 if the Name is nil or if no rule index is available.
func (n *Name) GetRuleIndex() int {
	if n == nil || len(n.indexes) < 2 {
		return -1
	}
	return n.indexes[1]
}

// GetMatchIndex returns the match index from the indexes array.
// Returns -1 if the Name is nil or if no match index is available.
func (n *Name) GetMatchIndex() int {
	if n == nil || len(n.indexes) < 3 {
		return -1
	}
	return n.indexes[2]
}
