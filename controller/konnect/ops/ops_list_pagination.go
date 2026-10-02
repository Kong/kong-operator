package ops

import (
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	"github.com/kong/kong-operator/v2/internal/utils/konnectpagination"
)

// listPageSize is the page size requested when listing Konnect entities page
// by page, e.g. to find an entity by its Kubernetes UID: the largest page size
// every Konnect list endpoint accepts. It is also Konnect's default page size
// for endpoints whose next page the SDK cannot see (e.g. config stores, whose
// cursor is not in meta.page.next as the spec says), so that such a lookup
// scans as many entities as when no page size was requested.
const listPageSize int64 = 100

// nextPageCursor returns the page[after] cursor of the page following the one
// whose meta.page.next is next, or nil when that page was the last one.
// seenCursors holds the cursors already requested: a cursor seen again is
// reported as an error, as following it would never end the listing.
func nextPageCursor(next *string, seenCursors map[string]struct{}) (*string, error) {
	if next == nil || *next == "" {
		return nil, nil
	}
	cursor, err := konnectpagination.PageAfterCursorFromNextPageURL(*next)
	if err != nil {
		return nil, err
	}
	if _, ok := seenCursors[cursor]; ok {
		return nil, fmt.Errorf("next page cursor %q repeated", cursor)
	}
	seenCursors[cursor] = struct{}{}
	return &cursor, nil
}

// hasNextNumberedPage reports whether a page-number paginated listing has a
// page after page pageNumber, whose meta.page is page and which returned items
// items. An empty page ends the listing even when the total says otherwise, so
// that a total that never matches the items cannot page forever.
func hasNextNumberedPage(pageNumber int64, page sdkkonnectcomp.PageMeta, items int) bool {
	if items == 0 || page.GetSize() <= 0 {
		return false
	}
	return float64(pageNumber)*page.GetSize() < page.GetTotal()
}
