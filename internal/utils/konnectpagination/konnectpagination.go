// Package konnectpagination contains helpers to page through Konnect list
// endpoints.
package konnectpagination

import (
	"fmt"
	"net/url"
)

// PageAfterCursorFromNextPageURL extracts the page[after] item cursor from a
// next-page URI as returned in Konnect list responses' meta.page.next. The SDK
// models Next as a full URI while the list requests' PageAfter parameter
// expects only the item cursor, so the URI must be parsed.
//
// next must be non-empty: callers treat an absent or empty meta.page.next as
// the last page. A next-page URI carrying no cursor is reported as an error
// rather than as the last page, so that an unfollowable page never passes for
// a complete listing.
func PageAfterCursorFromNextPageURL(next string) (string, error) {
	u, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("failed to parse next page URI %q: %w", next, err)
	}
	cursor := u.Query().Get("page[after]")
	if cursor == "" {
		return "", fmt.Errorf("next page URI %q carries no page[after] cursor", next)
	}
	return cursor, nil
}
