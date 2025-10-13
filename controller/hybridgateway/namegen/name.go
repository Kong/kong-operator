package namegen

import (
	"strings"

	"github.com/kong/kong-operator/controller/hybridgateway/utils"
)

// Name represents a structured naming scheme for Kong entities to be identified by the HTTPRoute, ParentReference,
// and HTTPRoute section from which the resource is derived.
type Name struct {
	httpRouteID string
	parentRefID string
	sectionID   string
}

// String returns the full name as a dot-separated string.
func (h *Name) String() string {
	const maxLen = 253 - len("faf385ae") - 1 // reserve space for one extra hash (+ 1 dot) for child resources (e.g., KongTargets)

	parts := []string{}
	if h.httpRouteID != "" {
		parts = append(parts, h.httpRouteID)
	}
	if h.parentRefID != "" {
		parts = append(parts, h.parentRefID)
	}
	if h.sectionID != "" {
		parts = append(parts, h.sectionID)
	}
	fullName := strings.Join(parts, ".")

	if len(fullName) <= maxLen {
		return fullName
	}

	// If the full name exceeds the max length, we fallback to a hashed version of each component
	const DefaultHTTPRoutePrefix = "http"
	const DefaultParentRefPrefix = "cp"
	const DefaultSectPrefix = "res"
	const maxCompLen = (maxLen - 2) / 3

	httpRouteID := h.httpRouteID
	parentRefID := h.parentRefID
	sectionID := h.sectionID

	if len(httpRouteID) > maxCompLen {
		httpRouteID = DefaultHTTPRoutePrefix + utils.Hash32(httpRouteID)
	}
	if len(parentRefID) > maxCompLen {
		parentRefID = DefaultParentRefPrefix + utils.Hash32(parentRefID)
	}
	if len(sectionID) > maxCompLen {
		sectionID = DefaultSectPrefix + utils.Hash32(sectionID)
	}

	parts = []string{httpRouteID, parentRefID}
	if sectionID != "" {
		parts = append(parts, sectionID)
	}
	fullName = strings.Join(parts, ".")

	return fullName
}

// NewName creates a new Name instance with the given components.
func NewName(httpRouteID, parentRefID, sectionID string) *Name {
	return &Name{
		httpRouteID: httpRouteID,
		parentRefID: parentRefID,
		sectionID:   sectionID,
	}
}
