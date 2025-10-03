package intermediate

import (
	"strings"
)

// HashName represents a structured naming scheme for Kong entities to be identified by a hash value.
type HashName struct {
	prefix    string
	namespace string
	hash      string
}

// String returns the full hash name as a dot-separated string.
func (h *HashName) String() string {
	const maxLen = 253

	parts := []string{h.prefix, h.namespace, h.hash}
	fullName := strings.Join(parts, ".")

	if len(fullName) <= maxLen {
		return fullName
	}

	reserved := len(h.hash) + 2 // hash + 2 dots
	// We need at least one dot between prefix, namespace, and hash
	remaining := maxLen - reserved
	// Truncate prefix and namespace proportionally if needed
	prefixMax := remaining / 2
	namespaceMax := remaining - prefixMax

	truncPrefix := h.prefix
	truncNamespace := h.namespace
	if len(truncPrefix) > prefixMax {
		truncPrefix = truncPrefix[:prefixMax]
	}
	if len(truncNamespace) > namespaceMax {
		truncNamespace = truncNamespace[:namespaceMax]
	}

	parts = []string{truncPrefix, truncNamespace, h.hash}
	return strings.Join(parts, ".")
}

// GetHash returns the hash part of the HashName.
func (h *HashName) GetHash() string {
	return h.hash
}
