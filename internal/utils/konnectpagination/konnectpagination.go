// Package konnectpagination contains helpers to page through Konnect list
// endpoints.
package konnectpagination

import (
	"fmt"
	"net/url"
	"strings"
)

// PageAfterCursorFromNextPageURL extracts the page[after] item cursor from a
// Konnect list response's meta.page.next. Depending on the API, it is either a
// next-page URI (e.g. Event Gateway APIs:
// "/v1/event-gateways?page%5Bafter%5D=<cursor>&page%5Bsize%5D=1"), from which
// the cursor is parsed, as the list requests' PageAfter parameter expects only
// the cursor, or the bare cursor itself (e.g. AI Gateway sub-collections),
// which is returned as is. A value with no query and no scheme is taken for a
// bare cursor, even one starting with "/" (a base64 character): a next-page
// URI without a query would carry no cursor anyway.
//
// next must be non-empty: callers treat an absent or empty meta.page.next as
// the last page. A next-page URI carrying no cursor is reported as an error
// rather than as the last page, so that an unfollowable page never passes for
// a complete listing.
func PageAfterCursorFromNextPageURL(next string) (string, error) {
	if !strings.Contains(next, "?") && !strings.Contains(next, "://") {
		return next, nil
	}
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
