package intermediate

import (
	"strconv"
	"strings"
)

type Name struct {
	prefix    string
	namespace string
	name      string
	// [parentRefIndex, ruleIndex, matchIndex]
	indexes []int
}

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

func (n *Name) GetParentRefIndex() int {
	if n == nil || len(n.indexes) < 1 {
		return -1
	}
	return n.indexes[0]
}

func (n *Name) GetRuleIndex() int {
	if n == nil || len(n.indexes) < 2 {
		return -1
	}
	return n.indexes[1]
}

func (n *Name) GetMatchIndex() int {
	if n == nil || len(n.indexes) < 3 {
		return -1
	}
	return n.indexes[2]
}
